// Package signaling implements the WebSocket signaling layer: connection
// lifecycle, protocol validation, SDP/ICE relaying and session termination.
package signaling

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"

	"random-chat/internal/config"
	"random-chat/internal/matching"
	"random-chat/internal/metrics"
	"random-chat/internal/moderation"
	"random-chat/internal/sessions"
	"random-chat/internal/storage"
)

const (
	maxSDPBytes       = 100 * 1024
	maxICEBytes       = 2 * 1024
	maxReportReason   = 500
	maxReportDetails  = 2000
	maxClientIDLength = 64
	minClientIDLength = 8
)

// HubOptions wires the hub to the rest of the application.
type HubOptions struct {
	Config     *config.Config
	Store      storage.Store
	Sessions   *sessions.Manager
	Matcher    *matching.Matcher
	Moderation *moderation.Service
	Metrics    *metrics.Metrics
	Logger     *slog.Logger
}

// Hub owns the live connection set and routes every protocol event.
type Hub struct {
	cfg        *config.Config
	store      storage.Store
	sessions   *sessions.Manager
	matcher    *matching.Matcher
	moderation *moderation.Service
	metrics    *metrics.Metrics
	log        *slog.Logger
	upgrader   websocket.Upgrader

	mu      sync.RWMutex
	clients map[string]*Client

	joinLimiter   *moderation.RateLimiter
	chatLimiter   *moderation.RateLimiter
	reportLimiter *moderation.RateLimiter
	signalLimiter *moderation.RateLimiter
}

// NewHub builds the hub and installs the origin policy on the upgrader.
func NewHub(opts HubOptions) *Hub {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	h := &Hub{
		cfg:           opts.Config,
		store:         opts.Store,
		sessions:      opts.Sessions,
		matcher:       opts.Matcher,
		moderation:    opts.Moderation,
		metrics:       opts.Metrics,
		log:           logger,
		clients:       make(map[string]*Client),
		joinLimiter:   moderation.NewRateLimiter(opts.Config.JoinRatePerMin, 6),
		chatLimiter:   moderation.NewRateLimiter(opts.Config.ChatRatePerMin, 15),
		reportLimiter: moderation.NewRateLimiter(opts.Config.ReportRatePerMin, 3),
		signalLimiter: moderation.NewRateLimiter(opts.Config.SignalRatePerMin, 120),
	}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if !h.cfg.AllowsOrigin(origin, r.Host) {
				h.log.Warn("websocket upgrade rejected",
					"origin", origin,
					"host", r.Host,
					"ip", clientIP(r),
					"hint", "add this origin to ALLOWED_ORIGINS")
				return false
			}
			return true
		},
	}
	return h
}

// Run performs periodic housekeeping until ctx is cancelled.
func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.joinLimiter.Sweep(10 * time.Minute)
			h.chatLimiter.Sweep(10 * time.Minute)
			h.reportLimiter.Sweep(30 * time.Minute)
			h.signalLimiter.Sweep(5 * time.Minute)
			h.log.Debug("hub stats",
				"connections", h.Count(),
				"sessions", h.sessions.ActiveCount(),
				"queue", h.matcher.Len())
		}
	}
}

// Count returns the number of live connections.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// QueueLength returns how many peers are waiting for a partner.
func (h *Hub) QueueLength() int { return h.matcher.Len() }

func (h *Hub) client(id string) *Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clients[id]
}

func (h *Hub) register(c *Client) (replaced *Client) {
	h.mu.Lock()
	replaced = h.clients[c.id]
	h.clients[c.id] = c
	h.mu.Unlock()
	h.metrics.ConnectionsOpened.Inc()
	h.metrics.ConnectionsActive.Inc()
	return replaced
}

func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	current, ok := h.clients[c.id]
	if !ok || current != c {
		h.mu.Unlock()
		return
	}
	delete(h.clients, c.id)
	h.mu.Unlock()

	c.CloseWithReason("disconnected")
	h.matcher.Cancel(c)
	h.terminate(c, storage.SessionEnded, "peer_disconnected")

	h.metrics.ConnectionsClosed.Inc()
	h.metrics.ConnectionsActive.Dec()
	h.log.Info("client disconnected", "user_id", c.id, "ip", c.ip, "duration_s", int(time.Since(c.connectedAt).Seconds()))
}

// ServeWS upgrades an HTTP request into a signaling connection.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)

	if until, suspended := h.moderation.IPSuspendedUntil(ip); suspended {
		http.Error(w, "temporarily suspended until "+until.Format(time.RFC3339), http.StatusForbidden)
		return
	}

	userID, ok := normalizeClientID(r.URL.Query().Get("cid"))
	if !ok {
		userID = storage.NewID()
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Debug("websocket upgrade failed", "err", err, "ip", ip)
		return
	}

	c := newClient(h, conn, userID, ip, r.UserAgent())
	if replaced := h.register(c); replaced != nil {
		replaced.SendError("replaced", "connexion reprise depuis un autre onglet", "")
		replaced.CloseWithReason("replaced")
		h.metrics.ConnectionsActive.Dec()
		h.metrics.ConnectionsClosed.Inc()
	}

	h.trackUser(userID, ip)

	c.Send(EvtHello, HelloPayload{
		UserID:          userID,
		ProtocolVersion: config.ProtocolVersion,
		IceServers:      iceServerConfigs(h.cfg.IceServers),
		ServerTime:      nowRFC3339(),
		QueueLength:     h.matcher.Len(),
		Limits: LimitsPayload{
			MaxChatRunes:   h.cfg.MaxChatRunes,
			MaxMessageByte: int(h.cfg.MaxMessageBytes),
		},
	})

	go c.writePump()
	go c.readPump()

	h.log.Info("client connected", "user_id", userID, "ip", ip, "connections", h.Count())
}

func (h *Hub) trackUser(userID, ip string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		now := time.Now().UTC()
		if err := h.store.UpsertUser(ctx, storage.User{
			ID:         userID,
			Status:     storage.UserActive,
			CreatedAt:  now,
			LastSeenAt: now,
			LastIP:     ip,
		}); err != nil {
			h.log.Warn("could not persist user", "err", err)
		}
	}()
}

// handle dispatches one validated client event.
func (h *Hub) handle(c *Client, env Envelope) {
	h.metrics.MessagesIn.Inc()
	switch env.Event {
	case EvtQueueJoin:
		h.handleQueueJoin(c, env)
	case EvtQueueCancel:
		h.handleQueueCancel(c, env)
	case EvtSessionNext:
		h.handleNext(c, env)
	case EvtSessionLeave:
		h.handleLeave(c, env)
	case EvtRTCOffer, EvtRTCAnswer:
		h.handleSDP(c, env)
	case EvtRTCICE:
		h.handleICE(c, env)
	case EvtChatSend:
		h.handleChat(c, env)
	case EvtReportCreate:
		h.handleReport(c, env)
	case EvtBlockCreate:
		h.handleBlock(c, env)
	default:
		h.metrics.MessagesRejected.Inc()
		c.SendError("unknown_event", "événement inconnu", env.Event)
	}
}

func (h *Hub) handleQueueJoin(c *Client, env Envelope) {
	if !h.joinLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "trop de recherches en peu de temps", env.Event)
		return
	}
	if until, suspended := h.moderation.IPSuspendedUntil(c.ip); suspended {
		c.SendError("suspended", "accès temporairement suspendu jusqu'à "+until.Format(time.RFC3339), env.Event)
		return
	}

	if c.State() == matching.StateInCall {
		h.terminate(c, storage.SessionEnded, "requeue")
	}
	h.matcher.Join(c)
	if c.State() == matching.StateWaiting {
		c.Send(EvtQueueWaiting, QueuePayload{Position: h.matcher.Len(), Since: nowRFC3339()})
	}
}

func (h *Hub) handleQueueCancel(c *Client, env Envelope) {
	if h.matcher.Cancel(c) {
		c.Send(EvtQueueCanceled, nil)
		c.Send(EvtStateChanged, StatePayload{State: string(matching.StateIdle)})
		return
	}
	c.SendError("not_waiting", "aucune recherche en cours", env.Event)
}

func (h *Hub) handleNext(c *Client, env Envelope) {
	if !h.joinLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "trop de passages en peu de temps", env.Event)
		return
	}
	if c.State() == matching.StateInCall {
		h.terminate(c, storage.SessionEnded, "next")
	}
	h.matcher.Join(c)
	if c.State() == matching.StateWaiting {
		c.Send(EvtQueueWaiting, QueuePayload{Position: h.matcher.Len(), Since: nowRFC3339()})
	}
}

func (h *Hub) handleLeave(c *Client, _ Envelope) {
	h.matcher.Cancel(c)
	if c.State() == matching.StateInCall {
		h.terminate(c, storage.SessionEnded, "left")
	}
	c.Send(EvtStateChanged, StatePayload{State: string(matching.StateIdle)})
}

func (h *Hub) handleSDP(c *Client, env Envelope) {
	if !h.signalLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "signalisation trop fréquente", env.Event)
		return
	}
	if !c.State().CanAcceptSignaling() || c.PartnerID() == "" {
		c.SendError("no_session", "aucune session active", env.Event)
		return
	}
	payload, err := decodeData[SDPPayload](env)
	if err != nil {
		h.metrics.MessagesRejected.Inc()
		c.SendError("malformed", "description SDP invalide", env.Event)
		return
	}
	expected := "offer"
	if env.Event == EvtRTCAnswer {
		expected = "answer"
	}
	if payload.Type != expected || payload.SDP == "" || len(payload.SDP) > maxSDPBytes {
		h.metrics.MessagesRejected.Inc()
		c.SendError("invalid_sdp", "description SDP rejetée", env.Event)
		return
	}
	h.forward(c, env.Event, payload)
}

func (h *Hub) handleICE(c *Client, env Envelope) {
	if !h.signalLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "signalisation trop fréquente", env.Event)
		return
	}
	if !c.State().CanAcceptSignaling() || c.PartnerID() == "" {
		c.SendError("no_session", "aucune session active", env.Event)
		return
	}
	payload, err := decodeData[ICEPayload](env)
	if err != nil || payload.Candidate == "" || len(payload.Candidate) > maxICEBytes {
		h.metrics.MessagesRejected.Inc()
		c.SendError("invalid_candidate", "candidat ICE rejeté", env.Event)
		return
	}
	h.forward(c, env.Event, payload)
}

func (h *Hub) handleChat(c *Client, env Envelope) {
	if !h.chatLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "vous envoyez des messages trop vite", env.Event)
		return
	}
	if c.PartnerID() == "" {
		c.SendError("no_peer", "aucun interlocuteur", env.Event)
		return
	}
	payload, err := decodeData[ChatSendPayload](env)
	if err != nil {
		h.metrics.MessagesRejected.Inc()
		c.SendError("malformed", "message invalide", env.Event)
		return
	}
	text := strings.TrimSpace(payload.Text)
	if text == "" {
		c.SendError("empty_message", "message vide", env.Event)
		return
	}
	if runes := []rune(text); len(runes) > h.cfg.MaxChatRunes {
		text = string(runes[:h.cfg.MaxChatRunes])
	}

	msg := ChatMessagePayload{ID: storage.NewID(), Text: text, From: "peer", At: nowRFC3339()}
	h.forward(c, EvtChatMessage, msg)
	c.Send(EvtChatAck, ChatAckPayload{ID: msg.ID, At: msg.At})
}

func (h *Hub) handleReport(c *Client, env Envelope) {
	if !h.reportLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "trop de signalements", env.Event)
		return
	}
	target := c.PartnerID()
	if target == "" {
		c.SendError("no_peer", "aucun interlocuteur à signaler", env.Event)
		return
	}
	payload, err := decodeData[ReportPayload](env)
	if err != nil {
		h.metrics.MessagesRejected.Inc()
		c.SendError("malformed", "signalement invalide", env.Event)
		return
	}
	category := strings.ToLower(strings.TrimSpace(payload.Category))
	if !moderation.ValidCategory(category) {
		category = "other"
	}
	reason := truncateRunes(strings.TrimSpace(payload.Reason), maxReportReason)
	details := truncateRunes(strings.TrimSpace(payload.Details), maxReportDetails)

	reportID, suspended, err := h.moderation.Report(c.id, target, c.SessionID(), category, reason, details)
	if err != nil {
		h.log.Error("report persistence failed", "err", err, "target", target)
	}
	h.metrics.Reports.Inc()
	if suspended {
		h.metrics.Suspensions.Inc()
	}

	blocked := false
	if payload.Block {
		if err := h.moderation.Block(c.id, target); err == nil {
			blocked = true
			h.metrics.Blocks.Inc()
		}
	}

	h.terminate(c, storage.SessionReported, "reported")
	c.Send(EvtReportAck, ReportAckPayload{ReportID: reportID, Blocked: blocked, Suspended: suspended})
}

func (h *Hub) handleBlock(c *Client, env Envelope) {
	target := c.PartnerID()
	if target == "" {
		c.SendError("no_peer", "aucun interlocuteur à bloquer", env.Event)
		return
	}
	if err := h.moderation.Block(c.id, target); err != nil {
		c.SendError("block_failed", "le blocage n'a pas pu être enregistré", env.Event)
		return
	}
	h.metrics.Blocks.Inc()
	h.terminate(c, storage.SessionEnded, "blocked")
	c.Send(EvtBlockAck, BlockAckPayload{BlockedID: target})
}

// forward relays a validated payload to the peer of the current session only.
// A client can never address a session it does not participate in.
func (h *Hub) forward(c *Client, event string, payload any) {
	partnerID := c.PartnerID()
	if partnerID == "" {
		c.SendError("no_peer", "aucun interlocuteur", event)
		return
	}
	partner := h.client(partnerID)
	if partner == nil || partner.SessionID() != c.SessionID() || partner.PartnerID() != c.id {
		c.SendError("no_peer", "interlocuteur déconnecté", event)
		return
	}
	start := time.Now()
	if !partner.Send(event, payload) {
		h.metrics.MessagesRejected.Inc()
	}
	h.metrics.SignalLatency.Observe(time.Since(start))
}

// terminate closes the current session and notifies the peer. Only the first
// caller wins, which prevents a double notification when both sides hang up at
// the same moment.
func (h *Hub) terminate(c *Client, status storage.SessionStatus, reason string) {
	partnerID := c.PartnerID()
	sessionID := c.SessionID()
	c.Unbind()

	if sessionID == "" {
		return
	}
	session, ok := h.sessions.End(sessionID, status, reason)
	if !ok {
		return
	}
	h.metrics.SessionDuration.Observe(session.Duration(time.Now()))

	if partnerID == "" {
		return
	}
	if partner := h.client(partnerID); partner != nil {
		partner.Unbind()
		partner.Send(EvtSessionEnded, SessionEndedPayload{
			SessionID:  sessionID,
			Reason:     reason,
			DurationS:  int64(session.Duration(time.Now()).Seconds()),
			CanRequeue: reason != "reported",
		})
	}
}

func (h *Hub) penalize(c *Client, event string) {
	h.metrics.Violations.Inc()
	if h.moderation.RegisterViolation(c.ip) {
		c.SendError("suspended", "activité anormale détectée, accès suspendu temporairement", event)
		h.metrics.Suspensions.Inc()
		c.CloseWithReason("abuse")
	}
}

// normalizeClientID validates the browser-persisted identifier. Accepting a
// malformed value would let a client inject arbitrary keys into the store.
func normalizeClientID(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < minClientIDLength || len(raw) > maxClientIDLength {
		return "", false
	}
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return "", false
		}
	}
	return raw, true
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

func iceServerConfigs(in []config.IceServer) []IceServerConfig {
	out := make([]IceServerConfig, 0, len(in))
	for _, s := range in {
		out = append(out, IceServerConfig{URLs: s.URLs, Username: s.Username, Credential: s.Credential})
	}
	return out
}

// matcherMetrics adapts the metrics registry to matching.Metrics.
type matcherMetrics struct{ m *metrics.Metrics }

func (a matcherMetrics) QueueJoin()                { a.m.QueueJoins.Inc() }
func (a matcherMetrics) QueueCancel()              { a.m.QueueCancels.Inc() }
func (a matcherMetrics) QueueTimeout()             { a.m.QueueTimeouts.Inc() }
func (a matcherMetrics) MatchCreated()             { a.m.Matches.Inc() }
func (a matcherMetrics) MatchWait(d time.Duration) { a.m.MatchWait.Observe(d) }

// NewMatcherMetrics exposes the adapter to main so the matcher can be built
// before the hub exists.
func NewMatcherMetrics(m *metrics.Metrics) matching.Metrics { return matcherMetrics{m: m} }
