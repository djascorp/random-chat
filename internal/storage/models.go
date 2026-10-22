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

// Session is one 1:1 pairing between two participants.
type Session struct {
	ID           string        `json:"id"`
	ParticipantA string        `json:"participant_a"`
	ParticipantB string        `json:"participant_b"`
	StartedAt    time.Time     `json:"started_at"`
	EndedAt      *time.Time    `json:"ended_at,omitempty"`
	Status       SessionStatus `json:"status"`
	EndReason    string        `json:"end_reason,omitempty"`
}

// Duration returns the session length, using now for still-active sessions.
func (s *Session) Duration(now time.Time) time.Duration {
	end := now
	if s.EndedAt != nil {
		end = *s.EndedAt
	}
	return end.Sub(s.StartedAt)
}

// Participants returns both participant identifiers.
func (s *Session) Participants() [2]string { return [2]string{s.ParticipantA, s.ParticipantB} }

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
