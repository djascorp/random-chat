package storage

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Memory is a process-local Store used for development and for deployments that
// deliberately run without PostgreSQL.
//
// It is not an audit log of record: PostgreSQL plays that role. Every collection
// is therefore bounded, because a long running instance would otherwise retain
// each visitor and each finished session forever.
type Memory struct {
	mu       sync.RWMutex
	users    map[string]User
	sessions map[string]Session
	reports  []Report
	blocks   []Block
	actions  []ModerationAction
}

const (
	// memoryRetention is the point at which a collection is trimmed.
	memoryRetention = 5000
	// memoryTrimTarget is where a trim stops. Dropping a batch instead of a
	// single entry amortises the scan over many inserts.
	memoryTrimTarget = memoryRetention - memoryRetention/5
)

// NewMemory builds an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		users:    make(map[string]User),
		sessions: make(map[string]Session),
	}
}

// trimSessionsLocked drops the oldest finished sessions. Live sessions keep
// their row: the matching engine still references them. Called with the lock held.
func (m *Memory) trimSessionsLocked() {
	if len(m.sessions) <= memoryRetention {
		return
	}
	finished := make([]string, 0, len(m.sessions))
	for id, session := range m.sessions {
		if session.EndedAt != nil {
			finished = append(finished, id)
		}
	}
	sort.Slice(finished, func(i, j int) bool {
		return m.sessions[finished[i]].StartedAt.Before(m.sessions[finished[j]].StartedAt)
	})
	for _, id := range finished {
		if len(m.sessions) <= memoryTrimTarget {
			return
		}
		delete(m.sessions, id)
	}
}

// trimUsersLocked drops the least recently seen visitors. Suspended users are
// never evicted: losing a live sanction would let an abusive visitor straight
// back into the matching queue.
func (m *Memory) trimUsersLocked() {
	if len(m.users) <= memoryRetention {
		return
	}
	now := time.Now()
	candidates := make([]string, 0, len(m.users))
	for id, user := range m.users {
		if user.IsSuspended(now) {
			continue
		}
		candidates = append(candidates, id)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return m.users[candidates[i]].LastSeenAt.Before(m.users[candidates[j]].LastSeenAt)
	})
	for _, id := range candidates {
		if len(m.users) <= memoryTrimTarget {
			return
		}
		delete(m.users, id)
	}
}

func (m *Memory) UpsertUser(_ context.Context, u User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.users[u.ID]; ok {
		existing.LastSeenAt = u.LastSeenAt
		existing.LastIP = u.LastIP
		if u.Status != "" {
			existing.Status = u.Status
		}
		m.users[u.ID] = existing
		return nil
	}
	if u.Status == "" {
		u.Status = UserActive
	}
	m.users[u.ID] = u
	m.trimUsersLocked()
	return nil
}

func (m *Memory) GetUser(_ context.Context, id string) (User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (m *Memory) SuspendUser(_ context.Context, id string, until time.Time, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		u = User{ID: id, CreatedAt: time.Now().UTC()}
	}
	u.Status = UserSuspended
	u.SuspendedUntil = &until
	u.SuspendReason = reason
	m.users[id] = u
	m.trimUsersLocked()
	return nil
}

func (m *Memory) CreateSession(_ context.Context, s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := *s
	stored.Participants = append([]string(nil), s.Participants...)
	m.sessions[s.ID] = stored
	m.trimSessionsLocked()
	return nil
}

// AddSessionParticipant records a late joiner of a conference.
func (m *Memory) AddSessionParticipant(_ context.Context, sessionID, peerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	for _, p := range s.Participants {
		if p == peerID {
			return nil
		}
	}
	s.Participants = append(s.Participants, peerID)
	m.sessions[sessionID] = s
	return nil
}

func (m *Memory) EndSession(_ context.Context, id string, at time.Time, status SessionStatus, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	s.EndedAt = &at
	s.Status = status
	s.EndReason = reason
	m.sessions[id] = s
	return nil
}

func (m *Memory) CreateReport(_ context.Context, r *Report) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reports = append(m.reports, *r)
	if len(m.reports) > memoryRetention {
		m.reports = m.reports[len(m.reports)-memoryRetention:]
	}
	return nil
}

func (m *Memory) ListReports(_ context.Context, status ReportStatus, limit int) ([]Report, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Report, 0, len(m.reports))
	for i := len(m.reports) - 1; i >= 0; i-- {
		r := m.reports[i]
		if status != "" && r.Status != status {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *Memory) UpdateReportStatus(_ context.Context, id string, status ReportStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.reports {
		if m.reports[i].ID == id {
			m.reports[i].Status = status
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) CountReportsAgainst(_ context.Context, targetID string, since time.Time) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, r := range m.reports {
		if r.TargetID == targetID && r.CreatedAt.After(since) {
			count++
		}
	}
	return count, nil
}

func (m *Memory) CreateBlock(_ context.Context, b *Block) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.blocks {
		if existing.BlockerID == b.BlockerID && existing.BlockedID == b.BlockedID {
			return nil
		}
	}
	m.blocks = append(m.blocks, *b)
	if len(m.blocks) > memoryRetention {
		m.blocks = m.blocks[len(m.blocks)-memoryRetention:]
	}
	return nil
}

func (m *Memory) IsBlocked(_ context.Context, a, b string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, blk := range m.blocks {
		if (blk.BlockerID == a && blk.BlockedID == b) || (blk.BlockerID == b && blk.BlockedID == a) {
			return true, nil
		}
	}
	return false, nil
}

func (m *Memory) ListBlocks(_ context.Context, blockerID string) ([]Block, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []Block{}
	for _, b := range m.blocks {
		if b.BlockerID == blockerID {
			out = append(out, b)
		}
	}
	return out, nil
}

func (m *Memory) CreateModerationAction(_ context.Context, a *ModerationAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = append(m.actions, *a)
	if len(m.actions) > memoryRetention {
		m.actions = m.actions[len(m.actions)-memoryRetention:]
	}
	return nil
}

func (m *Memory) ListModerationActions(_ context.Context, limit int) ([]ModerationAction, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ModerationAction, 0, len(m.actions))
	for i := len(m.actions) - 1; i >= 0; i-- {
		out = append(out, m.actions[i])
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *Memory) Stats(_ context.Context) (map[string]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	open := 0
	for _, r := range m.reports {
		if r.Status == ReportOpen {
			open++
		}
	}
	return map[string]int{
		"users":              len(m.users),
		"sessions":           len(m.sessions),
		"reports":            len(m.reports),
		"reports_open":       open,
		"blocks":             len(m.blocks),
		"moderation_actions": len(m.actions),
	}, nil
}

// Close drops every record. Present so Memory satisfies Store.
func (m *Memory) Close() error { return nil }
