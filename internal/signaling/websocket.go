package signaling

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"random-chat/internal/matching"
	"random-chat/internal/storage"
)

// Client is one WebSocket connection. It is the only writer of its socket: every
// outbound frame goes through the buffered `send` channel consumed by writePump,
// which removes any chance of concurrent writes on the same connection.
type Client struct {
	id   string
	conn *websocket.Conn
	hub  *Hub
	send chan []byte
	done chan struct{}

	closeOnce sync.Once
	closed    atomic.Bool

	mu        sync.RWMutex
	state     matching.State
	partnerID string
	sessionID string
	// mode and maxParticipants are the call shape requested at queue.join
	// time: "solo" (strict 1:1) or "group" (conference), with an optional cap
	// where 0 means the caller sets no constraint.
	mode            string
	maxParticipants int
	matchedAt       time.Time
	joinedAt        time.Time

	ip          string
	userAgent   string
	connectedAt time.Time
	lastSeen    atomic.Int64

	signalingSent int64
}

func newClient(hub *Hub, conn *websocket.Conn, id, ip, userAgent string) *Client {
	now := time.Now()
	c := &Client{
		id:          id,
		conn:        conn,
		hub:         hub,
		send:        make(chan []byte, hub.cfg.SendBuffer),
		done:        make(chan struct{}),
		state:       matching.StateIdle,
		ip:          ip,
		userAgent:   userAgent,
		connectedAt: now,
	}
	c.lastSeen.Store(now.UnixNano())
	return c
}

// ID implements matching.Peer.
func (c *Client) ID() string { return c.id }

// IP is the observed network origin, used for abuse throttling.
func (c *Client) IP() string { return c.ip }

// State implements matching.Peer.
func (c *Client) State() matching.State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// SetState implements matching.Peer. It is a no-op once disconnected so a late
// callback cannot resurrect a dead peer.
func (c *Client) SetState(s matching.State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == matching.StateDisconnected {
		return
	}
	c.state = s
}

// PartnerID implements matching.Peer.
func (c *Client) PartnerID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.partnerID
}

// SessionID implements matching.Peer.
func (c *Client) SessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID
}

// Mode implements matching.Peer.
func (c *Client) Mode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.mode == "" {
		return storage.SessionSolo
	}
	return c.mode
}

// MaxParticipants implements matching.Peer.
func (c *Client) MaxParticipants() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.maxParticipants
}

// SetSearchPrefs records the call shape requested at queue.join time.
func (c *Client) SetSearchPrefs(mode string, max int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = mode
	c.maxParticipants = max
}

// Bind implements matching.Peer.
func (c *Client) Bind(partnerID, sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == matching.StateDisconnected {
		return
	}
	c.partnerID = partnerID
	c.sessionID = sessionID
	c.state = matching.StateInCall
	c.matchedAt = time.Now()
}

// Unbind implements matching.Peer.
func (c *Client) Unbind() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partnerID = ""
	c.sessionID = ""
	c.matchedAt = time.Time{}
	if c.state != matching.StateDisconnected {
		c.state = matching.StateIdle
	}
}

// MatchedAt returns when the current session started, zero when idle.
func (c *Client) MatchedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.matchedAt
}

// Send implements matching.Peer. It marshals the payload and queues it without
// blocking; a peer that cannot keep up is disconnected rather than stalling the
// hub goroutine.
func (c *Client) Send(event string, payload any) bool {
	var data json.RawMessage
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return false
		}
		data = encoded
	}
	return c.SendRaw(Envelope{Event: event, Data: data})
}

// SendRaw queues an already built envelope.
func (c *Client) SendRaw(env Envelope) bool {
	if c.closed.Load() {
		return false
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		return false
	}
	c.lastSeen.Store(time.Now().UnixNano())
	select {
	case c.send <- encoded:
		c.hub.metrics.MessagesOut.Inc()
		return true
	default:
		c.hub.metrics.ConnectionsKicked.Inc()
		c.CloseWithReason("slow_consumer")
		return false
	}
}

// SendError reports a protocol error to the client.
func (c *Client) SendError(code, message, event string) {
	c.Send(EvtError, ErrorPayload{Code: code, Message: message, Event: event})
}

// Close implements matching.Peer.
func (c *Client) Close(reason string) { c.CloseWithReason(reason) }

// CloseWithReason tears the connection down exactly once and unblocks writePump.
func (c *Client) CloseWithReason(reason string) {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		c.mu.Lock()
		c.state = matching.StateDisconnected
		c.mu.Unlock()
		close(c.done)
	})
}

func (c *Client) readPump() {
	defer c.hub.unregister(c)

	c.conn.SetReadLimit(c.hub.cfg.MaxMessageBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(c.hub.cfg.PongTimeout))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(c.hub.cfg.PongTimeout))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(c.hub.cfg.PongTimeout))
		c.lastSeen.Store(time.Now().UnixNano())
		c.hub.metrics.BytesIn.Add(int64(len(raw)))

		env, err := decodeEnvelope(raw)
		if err != nil {
			c.hub.metrics.MessagesRejected.Inc()
			c.hub.metrics.MessagesIn.Inc()
			if errors.Is(err, ErrUnknownEvent) {
				c.SendError("unknown_event", err.Error(), "")
			} else {
				c.SendError("malformed", "message illisible", "")
			}
			continue
		}
		c.hub.handle(c, env)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(c.hub.cfg.PingInterval)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case <-c.done:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.hub.cfg.WriteTimeout))
			frame := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye")
			_ = c.conn.WriteMessage(websocket.CloseMessage, frame)
			return
		case frame := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.hub.cfg.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.TextMessage, frame); err != nil {
				return
			}
			c.hub.metrics.BytesOut.Add(int64(len(frame)))
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.hub.cfg.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// clientIP extracts the caller address, honouring one level of reverse proxy.
func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if first, _, found := strings.Cut(forwarded, ","); found {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(forwarded)
	}
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return strings.TrimSpace(real)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
