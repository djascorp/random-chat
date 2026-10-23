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
	EvtQueueJoin     = "queue.join"
	EvtQueueCancel   = "queue.cancel"
	EvtSessionNext   = "session.next"
	EvtSessionLeave  = "session.leave"
	EvtSessionAdd    = "session.add"
	EvtSessionJoin   = "session.join"
	EvtSessionUpdate = "session.update"
	EvtInviteCreate  = "invite.create"
	EvtInviteRevoke  = "invite.revoke"
	EvtRTCOffer      = "rtc.offer"
	EvtRTCAnswer     = "rtc.answer"
	EvtRTCICE        = "rtc.ice"
	EvtChatSend      = "chat.send"
	EvtReportCreate  = "report.create"
	EvtBlockCreate   = "block.create"
)

// Server to client events.
const (
	EvtHello          = "server.hello"
	EvtError          = "server.error"
	EvtQueueWaiting   = "queue.waiting"
	EvtQueueCanceled  = "queue.canceled"
	EvtQueueTimeout   = "queue.timeout"
	EvtMatchFound     = "match.found"
	EvtSessionEnded   = "session.ended"
	EvtSessionUpdated = "session.updated"
	EvtInviteCreated  = "invite.created"
	EvtInviteRevoked  = "invite.revoked"
	EvtPeerJoined     = "peer.joined"
	EvtPeerLeft       = "peer.left"
	EvtChatMessage    = "chat.message"
	EvtChatAck        = "chat.ack"
	EvtReportAck      = "report.ack"
	EvtBlockAck       = "block.ack"
	EvtStateChanged   = "session.state"
)

var clientEvents = map[string]struct{}{
	EvtQueueJoin:     {},
	EvtQueueCancel:   {},
	EvtSessionNext:   {},
	EvtSessionLeave:  {},
	EvtSessionAdd:    {},
	EvtSessionJoin:   {},
	EvtSessionUpdate: {},
	EvtInviteCreate:  {},
	EvtInviteRevoke:  {},
	EvtRTCOffer:      {},
	EvtRTCAnswer:     {},
	EvtRTCICE:        {},
	EvtChatSend:      {},
	EvtReportCreate:  {},
	EvtBlockCreate:   {},
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

// QueueJoinPayload is the call shape requested at queue.join time.
// Mode is "solo" (strict 1:1) or "group" (conference). MaxParticipants caps
// the conference the caller joins; 0 means the caller sets no constraint.
type QueueJoinPayload struct {
	Mode            string `json:"mode,omitempty"`
	MaxParticipants int    `json:"max_participants,omitempty"`
}

// QueuePayload is sent while the user waits for a partner.
type QueuePayload struct {
	Position int    `json:"position"`
	Since    string `json:"since"`
}

// PeerJoinedPayload announces a late joiner to the existing members.
// The newcomer offers to each of them; they only wait for those offers.
type PeerJoinedPayload struct {
	SessionID string `json:"session_id"`
	PeerID    string `json:"peer_id"`
}

// PeerLeftPayload tells the survivors who left and how many remain.
type PeerLeftPayload struct {
	SessionID string `json:"session_id"`
	PeerID    string `json:"peer_id"`
	Reason    string `json:"reason"`
	Members   int    `json:"members"`
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
// To selects the recipient inside a conference (optional for 1:1 calls);
// the server stamps From before relaying so receivers know who offered.
type SDPPayload struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
	To   string `json:"to,omitempty"`
	From string `json:"from,omitempty"`
}

// ICEPayload carries a single ICE candidate, routed like SDP.
type ICEPayload struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdpMid,omitempty"`
	SDPMLineIndex    *int    `json:"sdpMLineIndex,omitempty"`
	UsernameFragment *string `json:"usernameFragment,omitempty"`
	To               string  `json:"to,omitempty"`
	From             string  `json:"from,omitempty"`
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

// ReportPayload is a moderation signal. Target selects the reported member
// inside a conference; it defaults to the other participant of a 1:1 call.
type ReportPayload struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
	Details  string `json:"details"`
	Block    bool   `json:"block"`
	Target   string `json:"target,omitempty"`
}

// BlockPayload selects the blocked member; same default as reports.
type BlockPayload struct {
	Target string `json:"target,omitempty"`
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

// InviteCreatePayload asks the server for the session's invitation link.
// MaxParticipants, when present, is validated (0 or >= 2) and applied to the
// session first; Rotate revokes the previous token before minting a new one.
type InviteCreatePayload struct {
	MaxParticipants *int `json:"max_participants,omitempty"`
	Rotate          bool `json:"rotate,omitempty"`
}

// SessionJoinPayload enters a precise session through its invitation link.
// The token is opaque and never the session id.
type SessionJoinPayload struct {
	Token string `json:"token"`
}

// SessionUpdatePayload changes the member cap of the caller's live session.
// Any member may change it (anonymous rooms have no owner); 0 lifts the cap.
type SessionUpdatePayload struct {
	MaxParticipants int `json:"max_participants"`
}

// InviteCreatedPayload answers invite.create; the client builds the full URL
// itself (${origin}/j/${token}) so the server needs no base-URL setting.
type InviteCreatedPayload struct {
	Token           string `json:"token"`
	Mode            string `json:"mode"`
	MaxParticipants int    `json:"max_participants"`
}

// InviteRevokedPayload tells every member that the link is dead: open share
// dialogs must disable copy/regenerate against it.
type InviteRevokedPayload struct {
	SessionID string `json:"session_id"`
}

// SessionUpdatedPayload announces a promotion or a cap change to the members.
type SessionUpdatedPayload struct {
	SessionID       string `json:"session_id"`
	Mode            string `json:"mode"`
	MaxParticipants int    `json:"max_participants"`
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }
