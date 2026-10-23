package matching

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"random-chat/internal/sessions"
	"random-chat/internal/storage"
)

// BlockChecker reports whether two users must never be paired.
type BlockChecker interface {
	IsBlocked(a, b string) bool
}

// Match describes a call that was just created.
type Match struct {
	SessionID string
	Members   []string
	WaitTime  time.Duration
}

// Metrics is the subset of the metrics registry the matcher reports to.
type Metrics interface {
	QueueJoin()
	QueueCancel()
	QueueTimeout()
	MatchCreated()
	MatchWait(d time.Duration)
}

// Options configures the matcher.
type Options struct {
	QueueTimeout time.Duration
	Sessions     *sessions.Manager
	Blocks       BlockChecker
	Metrics      Metrics
	Logger       *slog.Logger
}

// Matcher pairs waiting peers. A single mutex serialises join, cancel and the
// pairing pass, which guarantees that a peer belongs to at most one session even
// when two clients press "next" at the same instant.
type Matcher struct {
	mu       sync.Mutex
	queue    *Queue
	enqueued map[string]time.Time
	// live tracks every peer currently bound to a session so late joiners can
	// be announced to members that left the queue long ago.
	live     map[string]Peer
	timeout  time.Duration
	sessions *sessions.Manager
	blocks   BlockChecker
	metrics  Metrics
	log      *slog.Logger
	onMatch  func(Match)
}

// NewMatcher builds a matcher from the given options.
func NewMatcher(opts Options) *Matcher {
	return &Matcher{
		queue:    NewQueue(),
		enqueued: make(map[string]time.Time),
		live:     make(map[string]Peer),
		timeout:  opts.QueueTimeout,
		sessions: opts.Sessions,
		blocks:   opts.Blocks,
		metrics:  opts.Metrics,
		log:      opts.Logger,
	}
}

// Forget drops a peer from the live registry. The hub calls it whenever a peer
// leaves a session, cancels a search or disconnects.
func (m *Matcher) Forget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.live, id)
}

// OnMatch registers an observer invoked after a session is created.
func (m *Matcher) OnMatch(fn func(Match)) {
	m.mu.Lock()
	m.onMatch = fn
	m.mu.Unlock()
}

// Join puts a peer in the queue and immediately attempts a pairing pass.
// It is a no-op when the peer is already waiting or currently in a call.
func (m *Matcher) Join(p Peer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !p.State().CanJoinQueue() {
		return
	}
	if !m.queue.Push(p) {
		return
	}
	m.enqueued[p.ID()] = time.Now()
	p.SetState(StateWaiting)
	if m.metrics != nil {
		m.metrics.QueueJoin()
	}
	m.pairLocked()
}

// Cancel removes a peer from the queue. It reports whether the peer was waiting.
func (m *Matcher) Cancel(p Peer) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.live, p.ID())
	return m.cancelLocked(p)
}

func (m *Matcher) cancelLocked(p Peer) bool {
	removed, ok := m.queue.Remove(p.ID())
	if !ok {
		return false
	}
	delete(m.enqueued, p.ID())
	if removed.State() == StateWaiting {
		removed.SetState(StateIdle)
	}
	if m.metrics != nil {
		m.metrics.QueueCancel()
	}
	return true
}

// Len returns the number of peers currently waiting.
func (m *Matcher) Len() int { return m.queue.Len() }

// Waiting returns how many peers are in the queue, plus a snapshot of their ids.
func (m *Matcher) Waiting() (int, []string) {
	ids := m.queue.IDs()
	return len(ids), ids
}

// pairLocked pairs solo callers 1:1, then forms new conferences from the
// group waiters. Live rooms never absorb waiters on their own: late joiners
// enter through an explicit session.add (the Ajouter button), so nobody lands
// mid-conversation without the room's consent. Called with the mutex held.
func (m *Matcher) pairLocked() {
	m.pairSoloLocked()
	m.formGroupsLocked()
}

// Fill pulls one waiting group peer into the given session. It reports
// whether anyone was available and whether the room was promoted from solo to
// group on the way in; the hub maps a miss to an "empty queue" error for the
// Ajouter button and counts promotions. A solo room is promoted before the
// draw so the Ajouter button works there too, and the existing members learn
// about the new shape through session.updated before peer.joined.
func (m *Matcher) Fill(sessionID string) (filled, promoted bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions.Get(sessionID)
	if !ok {
		return false, false
	}
	if s.Mode == soloMode {
		// A solo room is born full (two members for a cap of two): it must
		// be promoted — which lifts the 1:1 cap — before any room check.
		if !m.sessions.Promote(s.ID) {
			return false, false
		}
		promoted = true
		if fresh, ok := m.sessions.Get(sessionID); ok {
			s = fresh
		}
		m.announceUpdatedLocked(s)
	}
	if s.Full() {
		return false, promoted
	}
	members := s.Members()
	for _, p := range m.queue.Snapshot() {
		if p.Mode() != groupMode || p.State() != StateWaiting {
			continue
		}
		if !s.Fits(1, p.MaxParticipants()) || m.blockedWithAny(p, members) {
			continue
		}
		m.joinSessionLocked(s.ID, p)
		return true, promoted
	}
	return false, promoted
}

// JoinError codes returned by JoinSession; they map one-to-one to the stable
// server.error codes the client localises.
const (
	JoinNoSession   = "no_session"
	JoinSessionFull = "session_full"
	JoinBlocked     = "blocked_from_session"
	JoinAlreadyIn   = "already_in_session"
)

// JoinResult is the outcome of pulling a peer into a live session by id,
// which is how invitation links enter: the peer never waited in the queue.
type JoinResult struct {
	Session  *storage.Session
	Members  []string
	Promoted bool
	Err      string
}

// JoinSession moves p into a live session, promoting a solo room to a group
// conference first. Every validation runs under the matcher lock, so ten
// simultaneous joins on the last seat let exactly one in. The existing
// members receive session.updated before peer.joined when the room changed
// shape, and the newcomer receives the full member list.
func (m *Matcher) JoinSession(p Peer, sessionID string) JoinResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p.State() == StateInCall || p.State() == StateMatched {
		return JoinResult{Err: JoinAlreadyIn}
	}
	s, ok := m.sessions.Get(sessionID)
	if !ok {
		return JoinResult{Err: JoinNoSession}
	}
	members := s.Members()
	if m.blockedWithAny(p, members) {
		return JoinResult{Err: JoinBlocked}
	}
	var promoted bool
	if s.Mode == soloMode && !s.Fits(1, p.MaxParticipants()) {
		// Promote before the room check, and only when the room is full: a
		// pair (2/2) grows into a conference, while an empty hosted room
		// (1/2) keeps its solo shape and simply takes the invitee in.
		if !m.sessions.Promote(s.ID) {
			return JoinResult{Err: JoinNoSession}
		}
		promoted = true
		if fresh, ok := m.sessions.Get(sessionID); ok {
			s = fresh
		}
		m.announceUpdatedLocked(s)
	}
	members = s.Members()
	if !s.Fits(1, p.MaxParticipants()) {
		return JoinResult{Err: JoinSessionFull}
	}
	m.joinSessionLocked(s.ID, p)
	return JoinResult{Session: s, Members: members, Promoted: promoted}
}

// OpenRoom creates an empty session owned by p: an invitation link minted
// before anyone has matched. A solo room holds the host plus exactly one
// invitee (cap 2); a group room starts uncapped or with the host's cap. The
// host is bound and in-call from now on, so the link's first join flows
// through the same late-joiner path as any other arrival. It fails when p is
// not idle — a caller still bound to another room must leave it first.
func (m *Matcher) OpenRoom(p Peer) (*storage.Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p.State() != StateIdle {
		return nil, false
	}
	mode := p.Mode()
	if mode != groupMode {
		mode = soloMode
	}
	roomCap := 0
	if mode == soloMode {
		roomCap = 2
	} else if wanted := p.MaxParticipants(); wanted >= 2 {
		roomCap = wanted
	}
	s := m.sessions.Create([]string{p.ID()}, mode, roomCap)
	p.Bind("", s.ID)
	p.SetState(StateInCall)
	m.live[p.ID()] = p
	if m.log != nil {
		m.log.Info("empty room opened", "session_id", s.ID, "peer", p.ID(), "mode", mode, "max", roomCap)
	}
	return s, true
}

// announceUpdatedLocked tells the live members that their room changed shape
// (promotion or cap change from the matcher side). Called with the mutex held.
func (m *Matcher) announceUpdatedLocked(s *storage.Session) {
	payload := sessionUpdatedPayload(s.ID, s.Mode, s.MaxParticipants)
	for _, id := range s.Members() {
		if q := m.live[id]; q != nil {
			q.Send("session.updated", payload)
		}
	}
}

// joinSessionLocked moves a waiter into a live session and notifies everyone:
// the newcomer gets the full member list and the "joiner" role so it offers to
// every one of them, and every existing member learns who arrived so it waits
// for that offer. The role matters: veterans must never offer (they would
// glare with each other), and the joiner cannot rely on the identifier-order
// rule — with the highest id nobody would ever negotiate.
func (m *Matcher) joinSessionLocked(sessionID string, p Peer) {
	m.queue.Remove(p.ID())
	s, added := m.sessions.AddMember(sessionID, p.ID())
	if !added {
		return
	}
	p.Bind("", s.ID)
	p.SetState(StateInCall)
	m.live[p.ID()] = p

	wait := m.takeWait(p)
	members := s.Members()
	p.Send("match.found", matchFoundPayload(s, members, "", "joiner", time.Now()))
	for _, id := range members {
		if id == p.ID() {
			continue
		}
		if q := m.live[id]; q != nil {
			q.Send("peer.joined", peerJoinedPayload(s.ID, p.ID()))
		}
	}

	if m.metrics != nil {
		m.metrics.MatchCreated()
		m.metrics.MatchWait(wait)
	}
	if m.log != nil {
		m.log.Info("peer joined session",
			"session_id", s.ID, "peer", p.ID(), "members", len(members))
	}
}

// pairSoloLocked keeps pairing solo callers 1:1 until no compatible pair
// remains. Group callers never enter this path.
func (m *Matcher) pairSoloLocked() {
	for {
		a, b, ok := m.queue.PopPair(func(x, y Peer) bool {
			if x.Mode() != soloMode || y.Mode() != soloMode {
				return false
			}
			if m.blocks != nil && m.blocks.IsBlocked(x.ID(), y.ID()) {
				return false
			}
			return true
		})
		if !ok {
			return
		}
		m.startSessionLocked([]Peer{a, b}, soloMode, 2)
	}
}

// formGroupsLocked gathers waiting group callers into new conferences.
// The oldest waiter opens the room; followers join while they fit both the
// room's cap and their own limit, so a max=3 caller never lands in a room of 5.
func (m *Matcher) formGroupsLocked() {
	waiters := m.groupWaiters()
	taken := map[string]struct{}{}
	for _, first := range waiters {
		if _, done := taken[first.ID()]; done {
			continue
		}
		group := []Peer{first}
		cap := first.MaxParticipants()
		for _, w := range waiters {
			if w.ID() == first.ID() {
				continue
			}
			if _, done := taken[w.ID()]; done {
				continue
			}
			if cap > 0 && len(group)+1 > cap {
				continue
			}
			if w.MaxParticipants() > 0 && len(group)+1 > w.MaxParticipants() {
				continue
			}
			if m.blockedWithMembers(w, group) {
				continue
			}
			group = append(group, w)
			if w.MaxParticipants() > 0 && (cap == 0 || w.MaxParticipants() < cap) {
				cap = w.MaxParticipants()
			}
		}
		if len(group) < 2 {
			continue
		}
		for _, p := range group {
			taken[p.ID()] = struct{}{}
		}
		m.startSessionLocked(group, groupMode, cap)
	}
}

// groupWaiters returns the waiting group callers oldest first.
func (m *Matcher) groupWaiters() []Peer {
	var out []Peer
	for _, p := range m.queue.Snapshot() {
		if p.Mode() == groupMode && p.State() == StateWaiting {
			out = append(out, p)
		}
	}
	return out
}

func (m *Matcher) startSessionLocked(members []Peer, mode string, max int) {
	now := time.Now()

	var longest time.Duration
	ids := make([]string, 0, len(members))
	for _, p := range members {
		m.queue.Remove(p.ID())
		ids = append(ids, p.ID())
		if wait := m.takeWait(p); wait > longest {
			longest = wait
		}
	}

	session := m.sessions.Create(ids, mode, max)

	// Solo pairs keep the historic direction: the longest waiter offers.
	// Conferences resolve direction client-side by identifier order, so every
	// pair still negotiates exactly once without server coordination.
	if mode == soloMode {
		members[0].Bind(members[1].ID(), session.ID)
		members[1].Bind(members[0].ID(), session.ID)
		members[0].Send("match.found", matchFoundPayload(session, ids, members[1].ID(), "offerer", now))
		members[1].Send("match.found", matchFoundPayload(session, ids, members[0].ID(), "answerer", now))
	} else {
		for _, p := range members {
			p.Bind("", session.ID)
		}
		payload := matchFoundPayload(session, ids, "", "", now)
		for _, p := range members {
			p.Send("match.found", payload)
		}
	}
	for _, p := range members {
		m.live[p.ID()] = p
	}

	if m.metrics != nil {
		m.metrics.MatchCreated()
		m.metrics.MatchWait(longest)
	}
	if m.log != nil {
		m.log.Info("session created",
			"session_id", session.ID,
			"mode", mode,
			"members", len(ids),
			"wait_longest_ms", longest.Milliseconds())
	}
	if m.onMatch != nil {
		m.onMatch(Match{
			SessionID: session.ID,
			Members:   ids,
			WaitTime:  longest,
		})
	}
}

// takeWait removes a waiter from the enqueue ledger and returns its wait.
func (m *Matcher) takeWait(p Peer) time.Duration {
	since, ok := m.enqueued[p.ID()]
	delete(m.enqueued, p.ID())
	if !ok {
		return 0
	}
	return time.Since(since)
}

// livePeer returns the bound peer with this id, if any.
func (m *Matcher) livePeer(id string) Peer {
	return m.live[id]
}

// blockedWithAny reports whether p must never be paired with any member.
func (m *Matcher) blockedWithAny(p Peer, members []string) bool {
	if m.blocks == nil {
		return false
	}
	for _, id := range members {
		if id != p.ID() && m.blocks.IsBlocked(p.ID(), id) {
			return true
		}
	}
	return false
}

// blockedWithMembers reports whether w is blocked with anyone in the group.
func (m *Matcher) blockedWithMembers(w Peer, group []Peer) bool {
	if m.blocks == nil {
		return false
	}
	for _, p := range group {
		if m.blocks.IsBlocked(w.ID(), p.ID()) {
			return true
		}
	}
	return false
}

const (
	soloMode  = "solo"
	groupMode = "group"
)

type matchPayloadData struct {
	SessionID       string   `json:"session_id"`
	Peers           []string `json:"peers"`
	Mode            string   `json:"mode"`
	MaxParticipants int      `json:"max_participants"`
	PeerID          string   `json:"peer_id,omitempty"`
	Role            string   `json:"role,omitempty"`
	StartedAt       string   `json:"started_at"`
}

func matchFoundPayload(s *storage.Session, peers []string, peerID, role string, at time.Time) matchPayloadData {
	return matchPayloadData{
		SessionID:       s.ID,
		Peers:           peers,
		Mode:            s.Mode,
		MaxParticipants: s.MaxParticipants,
		PeerID:          peerID,
		Role:            role,
		StartedAt:       at.UTC().Format(time.RFC3339Nano),
	}
}

func peerJoinedPayload(sessionID, peerID string) map[string]any {
	return map[string]any{"session_id": sessionID, "peer_id": peerID}
}

func sessionUpdatedPayload(sessionID, mode string, max int) map[string]any {
	return map[string]any{"session_id": sessionID, "mode": mode, "max_participants": max}
}

// Run sweeps the queue for peers that waited longer than the timeout, until ctx
// is cancelled. Peers that time out are returned to idle so the UI can offer to
// retry instead of leaving a spinner forever.
func (m *Matcher) Run(ctx context.Context) {
	interval := m.timeout / 3
	if interval < time.Second {
		interval = time.Second
	}
	if interval > 10*time.Second {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sweep()
		}
	}
}

func (m *Matcher) sweep() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for id, since := range m.enqueued {
		if now.Sub(since) < m.timeout {
			continue
		}
		peer, ok := m.queue.Remove(id)
		delete(m.enqueued, id)
		if !ok {
			continue
		}
		if peer.State() == StateWaiting {
			peer.SetState(StateIdle)
		}
		peer.Send("queue.timeout", map[string]any{
			"reason":    "no_partner_found",
			"waited_ms": now.Sub(since).Milliseconds(),
		})
		if m.metrics != nil {
			m.metrics.QueueTimeout()
		}
	}
}
