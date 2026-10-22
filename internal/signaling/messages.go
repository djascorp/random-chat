package signaling

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Envelope is the single wire format for every WebSocket frame in both
// directions. `data` is event specific and validated before it is trusted.
type Envelope struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
	ReqID string          `json:"req_id,omitempty"`
}

// Client to server events.
const (
	EvtQueueJoin    = "queue.join"
	EvtQueueCancel  = "queue.cancel"
	EvtSessionNext  = "session.next"
	EvtSessionLeave = "session.leave"
	EvtRTCOffer     = "rtc.offer"
	EvtRTCAnswer    = "rtc.answer"
	EvtRTCICE       = "rtc.ice"
	EvtChatSend     = "chat.send"
	EvtReportCreate = "report.create"
	EvtBlockCreate  = "block.create"
)

// Server to client events.
const (
	EvtHello         = "server.hello"
	EvtError         = "server.error"
	EvtQueueWaiting  = "queue.waiting"
	EvtQueueCanceled = "queue.canceled"
	EvtQueueTimeout  = "queue.timeout"
	EvtMatchFound    = "match.found"
	EvtSessionEnded  = "session.ended"
	EvtChatMessage   = "chat.message"
	EvtChatAck       = "chat.ack"
	EvtReportAck     = "report.ack"
	EvtBlockAck      = "block.ack"
	EvtStateChanged  = "session.state"
)

var clientEvents = map[string]struct{}{
	EvtQueueJoin:    {},
	EvtQueueCancel:  {},
	EvtSessionNext:  {},
	EvtSessionLeave: {},
	EvtRTCOffer:     {},
	EvtRTCAnswer:    {},
	EvtRTCICE:       {},
	EvtChatSend:     {},
	EvtReportCreate: {},
	EvtBlockCreate:  {},
}

// Sentinel errors returned while decoding a client frame.
var (
	ErrUnknownEvent = errors.New("unknown event")
	ErrMalformed    = errors.New("malformed envelope")
)

// decodeEnvelope parses and validates a raw frame. Unknown events are rejected
// instead of being silently ignored so a client cannot probe for behaviour.
func decodeEnvelope(raw []byte) (Envelope, error) {
	if len(raw) == 0 {
		return Envelope{}, ErrMalformed
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, ErrMalformed
	}
	if env.Event == "" {
		return Envelope{}, fmt.Errorf("%w: missing event", ErrMalformed)
	}
	if _, ok := clientEvents[env.Event]; !ok {
		return Envelope{}, fmt.Errorf("%w: %s", ErrUnknownEvent, env.Event)
	}
	return env, nil
}

// decodeData unmarshals the event payload, rejecting empty or oversized data.
func decodeData[T any](env Envelope) (T, error) {
	var out T
	if len(env.Data) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(env.Data, &out); err != nil {
		return out, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return out, nil
}

// HelloPayload is the first frame the server sends after the upgrade.
type HelloPayload struct {
	UserID          string            `json:"user_id"`
	ProtocolVersion string            `json:"protocol_version"`
	IceServers      []IceServerConfig `json:"ice_servers"`
	ServerTime      string            `json:"server_time"`
	QueueLength     int               `json:"queue_length"`
	Limits          LimitsPayload     `json:"limits"`
}

// IceServerConfig is the browser RTCIceServer shape.
type IceServerConfig struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// LimitsPayload communicates server-side caps so the UI can enforce them early.
type LimitsPayload struct {
	MaxChatRunes   int `json:"max_chat_runes"`
	MaxMessageByte int `json:"max_message_bytes"`
}

// ErrorPayload reports a recoverable protocol error to the client.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Event   string `json:"event,omitempty"`
}

// QueuePayload is sent while the user waits for a partner.
type QueuePayload struct {
	Position int    `json:"position"`
	Since    string `json:"since"`
}

// TimeoutPayload is sent when no partner was found in time.
type TimeoutPayload struct {
	Reason   string `json:"reason"`
	WaitedMS int64  `json:"waited_ms"`
}

// SessionEndedPayload tells a peer why the call stopped.
type SessionEndedPayload struct {
	SessionID  string `json:"session_id"`
	Reason     string `json:"reason"`
	DurationS  int64  `json:"duration_seconds"`
	CanRequeue bool   `json:"can_requeue"`
}

// StatePayload mirrors the server-side session state machine.
type StatePayload struct {
	State     string `json:"state"`
	SessionID string `json:"session_id,omitempty"`
	PeerID    string `json:"peer_id,omitempty"`
}

// SDPPayload carries one side of the WebRTC negotiation.
type SDPPayload struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

// ICEPayload carries a single ICE candidate.
type ICEPayload struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdpMid,omitempty"`
	SDPMLineIndex    *int    `json:"sdpMLineIndex,omitempty"`
	UsernameFragment *string `json:"usernameFragment,omitempty"`
}

// ChatSendPayload is the text a client wants to deliver.
type ChatSendPayload struct {
	Text string `json:"text"`
}

// ChatMessagePayload is the delivered message, enriched with routing metadata.
type ChatMessagePayload struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	From string `json:"from"`
	At   string `json:"at"`
}

// ChatAckPayload confirms delivery to the sender.
type ChatAckPayload struct {
	ID string `json:"id"`
	At string `json:"at"`
}

// ReportPayload is a moderation signal.
type ReportPayload struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
	Details  string `json:"details"`
	Block    bool   `json:"block"`
}

// ReportAckPayload confirms the report was recorded.
type ReportAckPayload struct {
	ReportID  string `json:"report_id"`
	Blocked   bool   `json:"blocked"`
	Suspended bool   `json:"suspended"`
}

// BlockAckPayload confirms a block.
type BlockAckPayload struct {
	BlockedID string `json:"blocked_id"`
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }
