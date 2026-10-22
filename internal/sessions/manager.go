// Package sessions owns the lifecycle of 1:1 pairings. It guarantees that a
// participant is mapped to at most one active session at a time.
package sessions

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"random-chat/internal/storage"
)

// recorder is the narrow persistence port needed here; storage.Store satisfies it.
type recorder interface {
	CreateSession(ctx context.Context, s *storage.Session) error
	EndSession(ctx context.Context, id string, at time.Time, status storage.SessionStatus, reason string) error
}

// Manager keeps active sessions in memory and mirrors them to the store.
type Manager struct {
	mu     sync.RWMutex
	active map[string]*storage.Session
	byPeer map[string]string
	store  recorder
	log    *slog.Logger
}

// NewManager builds a Manager backed by the given store (may be nil).
func NewManager(store recorder, log *slog.Logger) *Manager {
	return &Manager{
		active: make(map[string]*storage.Session),
		byPeer: make(map[string]string),
		store:  store,
		log:    log,
	}
}

// Create registers a new active session between two participants and returns it.
// Identifiers are 128-bit random so peers can never guess a session they are not
// part of.
func (m *Manager) Create(a, b string) *storage.Session {
	s := &storage.Session{
		ID:           storage.NewID(),
		ParticipantA: a,
		ParticipantB: b,
		StartedAt:    time.Now().UTC(),
		Status:       storage.SessionActive,
	}
	m.mu.Lock()
	m.active[s.ID] = s
	m.byPeer[a] = s.ID
	m.byPeer[b] = s.ID
	m.mu.Unlock()

	if m.store != nil {
		// The store receives a snapshot: `s` is mutated again by End while the
		// write is in flight, and sharing the pointer would be a data race.
		snapshot := *s
		m.persist(func(ctx context.Context) error { return m.store.CreateSession(ctx, &snapshot) })
	}
	return s
}

// Get returns the active session with this id, if any.
func (m *Manager) Get(id string) (*storage.Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.active[id]
	return s, ok
}

// ByPeer returns the active session a participant currently belongs to.
func (m *Manager) ByPeer(peerID string) (*storage.Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.byPeer[peerID]
	if !ok {
		return nil, false
	}
	s, ok := m.active[id]
	return s, ok
}

// End closes an active session. It returns false when the session was already
// closed, which is what makes concurrent termination safe: only the first caller
// wins and therefore only one side notifies the other peer.
func (m *Manager) End(id string, status storage.SessionStatus, reason string) (*storage.Session, bool) {
	m.mu.Lock()
	s, ok := m.active[id]
	if !ok {
		m.mu.Unlock()
		return nil, false
	}
	delete(m.active, id)
	for _, p := range s.Participants() {
		if m.byPeer[p] == id {
			delete(m.byPeer, p)
		}
	}
	at := time.Now().UTC()
	s.EndedAt = &at
	s.Status = status
	s.EndReason = reason
	m.mu.Unlock()

	if m.store != nil {
		m.persist(func(ctx context.Context) error { return m.store.EndSession(ctx, id, at, status, reason) })
	}
	return s, true
}

// EndByPeer is a convenience wrapper for callers that only know a participant.
func (m *Manager) EndByPeer(peerID string, status storage.SessionStatus, reason string) (*storage.Session, bool) {
	s, ok := m.ByPeer(peerID)
	if !ok {
		return nil, false
	}
	return m.End(s.ID, status, reason)
}

// ActiveCount returns how many sessions are currently running.
func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.active)
}

// EndAll closes every active session, used during graceful shutdown.
func (m *Manager) EndAll(reason string) {
	m.mu.RLock()
	ids := make([]string, 0, len(m.active))
	for id := range m.active {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		m.End(id, storage.SessionEnded, reason)
	}
}

// persist writes to the store asynchronously so the signaling path never blocks
// on a database round trip.
func (m *Manager) persist(fn func(ctx context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil && m.log != nil {
			m.log.Warn("session persistence failed", "err", err)
		}
	}()
}
