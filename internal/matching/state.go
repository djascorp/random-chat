package matching

// State is the lifecycle of a participant, mirrored to the client.
type State string

const (
	// StateIdle: connected, not searching, not in a call.
	StateIdle State = "idle"
	// StateWaiting: in the queue, looking for a partner.
	StateWaiting State = "waiting"
	// StateMatched: a session was just assigned, WebRTC negotiation starting.
	StateMatched State = "matched"
	// StateInCall: signaling completed, media flowing.
	StateInCall State = "in_call"
	// StateDisconnected: the socket is gone; no further event may be sent.
	StateDisconnected State = "disconnected"
)

// String implements fmt.Stringer.
func (s State) String() string { return string(s) }

// CanAcceptSignaling reports whether the participant may exchange SDP/ICE.
func (s State) CanAcceptSignaling() bool {
	return s == StateMatched || s == StateInCall
}

// CanJoinQueue reports whether the participant may enter the matching queue.
func (s State) CanJoinQueue() bool {
	return s == StateIdle
}

// Peer is everything the matcher needs to know about a participant.
// signaling.Client implements it; matching never imports signaling.
type Peer interface {
	// ID is the stable identifier of the participant, unique among live peers.
	ID() string
	State() State
	SetState(State)
	// Mode is the requested call shape: "solo" for a strict 1:1 pairing,
	// "group" for a conference that late joiners can enter.
	Mode() string
	// MaxParticipants caps the conference the peer joins; 0 means the peer
	// sets no constraint.
	MaxParticipants() int
	// PartnerID is the other participant of a solo session, empty otherwise.
	PartnerID() string
	// SessionID is the current session identifier, empty when idle.
	SessionID() string
	// Bind attaches the peer to a session and moves it into a call.
	// Group members bind with an empty partnerID: the session owns the roster.
	Bind(partnerID, sessionID string)
	// Unbind detaches the peer from any session.
	Unbind()
	// Send queues an event to the peer; it must never block.
	Send(event string, payload any) bool
	// Close tears the connection down with a human readable reason.
	Close(reason string)
}
