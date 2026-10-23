// Package sessions owns the lifecycle of calls. It guarantees that a
// participant is mapped to at most one active session at a time, whether the
// session is a 1:1 pairing or a group conference.
package sessions

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"random-chat/internal/storage"
)

// sortByStarted orders sessions oldest first so fills prefer the longest
// waiting room.
func sortByStarted(sessions []*storage.Session) {
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.Before(sessions[j].StartedAt)
	})
}

// recorder is the narrow persistence port needed here; storage.Store satisfies it.
type recorder interface {
	CreateSession(ctx context.Context, s *storage.Session) error
	AddSessionParticipant(ctx context.Context, sessionID, peerID string) error
	EndSession(ctx context.Context, id string, at time.Time, status storage.SessionStatus, reason string) error
	UpdateSessionShape(ctx context.Context, id, mode string, max int) error
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

// Create registers a new active session between the given participants.
// Identifiers are 128-bit random so peers can never guess a session they are not
// part of. max caps the conference size; 0 means the members set no constraint.
func (m *Manager) Create(members []string, mode string, max int) *storage.Session {
	s := &storage.Session{
		ID:              storage.NewID(),
		Participants:    append([]string(nil), members...),
		Mode:            mode,
		MaxParticipants: max,
		StartedAt:       time.Now().UTC(),
		Status:          storage.SessionActive,
	}
	m.mu.Lock()
	m.active[s.ID] = s
	for _, p := range s.Participants {
		m.byPeer[p] = s.ID
	}
	m.mu.Unlock()

	if m.store != nil {
		// The store receives a snapshot: `s` is mutated again by End while the
		// write is in flight, and sharing the pointer would be a data race.
		snapshot := *s
		snapshot.Participants = append([]string(nil), s.Participants...)
		m.persist(func(ctx context.Context) error { return m.store.CreateSession(ctx, &snapshot) })
	}
	return s
}

// AddMember pulls a participant into a live session and returns it.
// Late joiners keep the session open instead of starting a new call.
func (m *Manager) AddMember(id, peer string) (*storage.Session, bool) {
	m.mu.Lock()
	s, ok := m.active[id]
	if !ok || s.Has(peer) {
		m.mu.Unlock()
		return s, false
	}
	s.Participants = append(s.Participants, peer)
	m.byPeer[peer] = id
	m.mu.Unlock()

	if m.store != nil {
		m.persist(func(ctx context.Context) error { return m.store.AddSessionParticipant(ctx, id, peer) })
	}
	return s, true
}

// RemoveMember detaches a participant from a live session. It returns a
// snapshot of the session and whether no member is left, in which case the
// caller ends it.
//
// The snapshot is a copy: callers keep reading it after the lock is released
// while other members may leave concurrently, so it must never alias the live
// record. For the same reason the filter builds a fresh slice instead of
// reusing the backing array in place.
func (m *Manager) RemoveMember(id, peer string) (*storage.Session, bool) {
	m.mu.Lock()
	s, ok := m.active[id]
	if !ok {
		m.mu.Unlock()
		return nil, false
	}
	kept := make([]string, 0, len(s.Participants))
	for _, p := range s.Participants {
		if p != peer {
			kept = append(kept, p)
		}
	}
	s.Participants = kept
	if m.byPeer[peer] == id {
		delete(m.byPeer, peer)
	}
	empty := len(s.Participants) == 0
	cpy := *s
	cpy.Participants = append([]string(nil), s.Participants...)
	m.mu.Unlock()
	return &cpy, empty
}

// GroupSessions returns a snapshot of the live conferences, oldest first.
// The matcher uses it to fill existing rooms before opening new ones.
func (m *Manager) GroupSessions() []*storage.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*storage.Session
	for _, s := range m.active {
		if s.Mode != storage.SessionGroup {
			continue
		}
		cpy := *s
		cpy.Participants = append([]string(nil), s.Participants...)
		out = append(out, &cpy)
	}
	sortByStarted(out)
	return out
}

// Promote switches a solo session to a group conference. It is a no-op for
// sessions that are already grouped or gone, and reports whether the switch
// happened so callers can announce it exactly once.
//
// Promotion lifts the 1:1 cap: a solo room is created full (two members for a
// cap of two), so keeping it would refuse every future arrival. The room
// restarts uncapped — the default ceiling — and any member may set a new cap
// afterwards.
func (m *Manager) Promote(id string) bool {
	m.mu.Lock()
	s, ok := m.active[id]
	if !ok || s.Mode != storage.SessionSolo {
		m.mu.Unlock()
		return false
	}
	s.Mode = storage.SessionGroup
	s.MaxParticipants = 0
	mode, max := s.Mode, s.MaxParticipants
	m.mu.Unlock()

	if m.store != nil {
		m.persist(func(ctx context.Context) error {
			return m.store.UpdateSessionShape(ctx, id, mode, max)
		})
	}
	return true
}

// SetMax caps the conference size of a live session (0 lifts the cap). It
// reports false when the session is gone. Lowering the cap below the current
// headcount ejects nobody: only future arrivals are blocked.
func (m *Manager) SetMax(id string, max int) bool {
	m.mu.Lock()
	s, ok := m.active[id]
	if !ok {
		m.mu.Unlock()
		return false
	}
	s.MaxParticipants = max
	mode := s.Mode
	m.mu.Unlock()

	if m.store != nil {
		m.persist(func(ctx context.Context) error {
			return m.store.UpdateSessionShape(ctx, id, mode, max)
		})
	}
	return true
}

// Get returns a snapshot of the active session with this id, if any. The
// copy keeps readers safe after the lock is released while the room evolves.
func (m *Manager) Get(id string) (*storage.Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.active[id]
	if !ok {
		return nil, false
	}
	cpy := *s
	cpy.Participants = append([]string(nil), s.Participants...)
	return &cpy, true
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
	for _, p := range s.Members() {
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
