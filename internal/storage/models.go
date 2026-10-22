package storage

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("storage: not found")

// NewID returns a 128-bit cryptographically random identifier, hex encoded.
// Session and user identifiers must not be guessable by peers.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("storage: entropy source unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// UserStatus tracks whether a participant may open a session.
type UserStatus string

const (
	UserActive    UserStatus = "active"
	UserSuspended UserStatus = "suspended"
)

// User is an anonymous visitor. The identifier is client supplied and persisted
// in browser storage, which keeps blocks and suspensions meaningful across visits
// without requiring an account.
type User struct {
	ID             string     `json:"id"`
	Status         UserStatus `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	LastSeenAt     time.Time  `json:"last_seen_at"`
	SuspendedUntil *time.Time `json:"suspended_until,omitempty"`
	SuspendReason  string     `json:"suspend_reason,omitempty"`
	LastIP         string     `json:"last_ip,omitempty"`
}

// IsSuspended reports whether the suspension window is still open at now.
func (u User) IsSuspended(now time.Time) bool {
	return u.SuspendedUntil != nil && u.SuspendedUntil.After(now)
}

// SessionStatus is the lifecycle state persisted for auditing.
type SessionStatus string

const (
	SessionActive   SessionStatus = "active"
	SessionEnded    SessionStatus = "ended"
	SessionReported SessionStatus = "reported"
)

// Session modes: a strict 1:1 pairing, or a group conference that late
// joiners can be pulled into while it has room.
const (
	SessionSolo  = "solo"
	SessionGroup = "group"
)

// Session is one call between two or more participants. MaxParticipants caps
// the conference size; 0 means the members set no constraint.
type Session struct {
	ID              string        `json:"id"`
	Participants    []string      `json:"participants"`
	Mode            string        `json:"mode"`
	MaxParticipants int           `json:"max_participants"`
	StartedAt       time.Time     `json:"started_at"`
	EndedAt         *time.Time    `json:"ended_at,omitempty"`
	Status          SessionStatus `json:"status"`
	EndReason       string        `json:"end_reason,omitempty"`
}

// Duration returns the session length, using now for still-active sessions.
func (s *Session) Duration(now time.Time) time.Duration {
	end := now
	if s.EndedAt != nil {
		end = *s.EndedAt
	}
	return end.Sub(s.StartedAt)
}

// Members returns a copy of the member identifiers in join order.
func (s *Session) Members() []string {
	out := make([]string, len(s.Participants))
	copy(out, s.Participants)
	return out
}

// Has reports whether peer is a member of the session.
func (s *Session) Has(peer string) bool {
	for _, p := range s.Participants {
		if p == peer {
			return true
		}
	}
	return false
}

// Full reports whether the session reached its member cap.
func (s *Session) Full() bool {
	return s.MaxParticipants > 0 && len(s.Participants) >= s.MaxParticipants
}

// Fits reports whether extra newcomers keep the session within its cap and
// within the newcomer's own limit (0 means no constraint on that side).
func (s *Session) Fits(extra, newcomerMax int) bool {
	if s.Full() {
		return false
	}
	if s.MaxParticipants > 0 && len(s.Participants)+extra > s.MaxParticipants {
		return false
	}
	if newcomerMax > 0 && len(s.Participants)+extra > newcomerMax {
		return false
	}
	return true
}

// ReportStatus tracks moderation triage.
type ReportStatus string

const (
	ReportOpen      ReportStatus = "open"
	ReportReviewed  ReportStatus = "reviewed"
	ReportActioned  ReportStatus = "actioned"
	ReportDismissed ReportStatus = "dismissed"
)

// ValidReportStatus guards the admin API against arbitrary values.
func ValidReportStatus(s ReportStatus) bool {
	switch s {
	case ReportOpen, ReportReviewed, ReportActioned, ReportDismissed:
		return true
	}
	return false
}

// Report is a user-submitted moderation signal.
type Report struct {
	ID         string       `json:"id"`
	ReporterID string       `json:"reporter_id"`
	TargetID   string       `json:"target_id"`
	SessionID  string       `json:"session_id"`
	Category   string       `json:"category"`
	Reason     string       `json:"reason"`
	Details    string       `json:"details,omitempty"`
	Status     ReportStatus `json:"status"`
	CreatedAt  time.Time    `json:"created_at"`
}

// Block prevents two users from ever being matched again.
type Block struct {
	BlockerID string    `json:"blocker_id"`
	BlockedID string    `json:"blocked_id"`
	CreatedAt time.Time `json:"created_at"`
}

// ModerationAction is the append-only audit log of admin operations.
type ModerationAction struct {
	ID        string    `json:"id"`
	TargetID  string    `json:"target_id"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason,omitempty"`
	Actor     string    `json:"actor"`
	CreatedAt time.Time `json:"created_at"`
}
