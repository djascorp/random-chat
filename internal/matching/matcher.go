package matching

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"random-chat/internal/sessions"
)

// BlockChecker reports whether two users must never be paired.
type BlockChecker interface {
	IsBlocked(a, b string) bool
}

// Match describes a pairing that was just created.
type Match struct {
	SessionID string
	Offerer   string
	Answerer  string
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
		timeout:  opts.QueueTimeout,
		sessions: opts.Sessions,
		blocks:   opts.Blocks,
		metrics:  opts.Metrics,
		log:      opts.Logger,
	}
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

// pairLocked keeps matching until no compatible pair remains. Called with the
// matcher mutex held.
func (m *Matcher) pairLocked() {
	for {
		a, b, ok := m.queue.PopPair(func(x, y Peer) bool {
			if m.blocks != nil && m.blocks.IsBlocked(x.ID(), y.ID()) {
				return false
			}
			return true
		})
		if !ok {
			return
		}
		m.startSessionLocked(a, b)
	}
}

func (m *Matcher) startSessionLocked(a, b Peer) {
	now := time.Now()

	waitA := now.Sub(m.enqueued[a.ID()])
	waitB := now.Sub(m.enqueued[b.ID()])
	delete(m.enqueued, a.ID())
	delete(m.enqueued, b.ID())

	session := m.sessions.Create(a.ID(), b.ID())

	// The peer that waited longest becomes the offerer so the SDP exchange has a
	// deterministic direction without an extra round trip.
	a.Bind(b.ID(), session.ID)
	b.Bind(a.ID(), session.ID)

	a.Send("match.found", matchPayload(session.ID, b.ID(), "offerer", now))
	b.Send("match.found", matchPayload(session.ID, a.ID(), "answerer", now))

	if m.metrics != nil {
		m.metrics.MatchCreated()
		if waitB > waitA {
			m.metrics.MatchWait(waitB)
		} else {
			m.metrics.MatchWait(waitA)
		}
	}
	if m.log != nil {
		m.log.Info("session created",
			"session_id", session.ID,
			"offerer", a.ID(),
			"answerer", b.ID(),
			"wait_offerer_ms", waitA.Milliseconds(),
			"wait_answerer_ms", waitB.Milliseconds())
	}
	if m.onMatch != nil {
		m.onMatch(Match{
			SessionID: session.ID,
			Offerer:   a.ID(),
			Answerer:  b.ID(),
			WaitTime:  waitA,
		})
	}
}

type matchPayloadData struct {
	SessionID string `json:"session_id"`
	PeerID    string `json:"peer_id"`
	Role      string `json:"role"`
	StartedAt string `json:"started_at"`
}

func matchPayload(sessionID, peerID, role string, at time.Time) matchPayloadData {
	return matchPayloadData{
		SessionID: sessionID,
		PeerID:    peerID,
		Role:      role,
		StartedAt: at.UTC().Format(time.RFC3339Nano),
	}
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
