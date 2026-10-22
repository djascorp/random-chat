package matching

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"random-chat/internal/sessions"
)

type recordedEvent struct {
	event   string
	payload any
}

// fakePeer is a minimal in-process matching.Peer used to exercise the engine.
type fakePeer struct {
	mu      sync.Mutex
	id      string
	state   State
	partner string
	session string
	events  []recordedEvent
	closed  bool
}

func newFakePeer(id string) *fakePeer {
	return &fakePeer{id: id, state: StateIdle}
}

func (p *fakePeer) ID() string { return p.id }

func (p *fakePeer) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func (p *fakePeer) SetState(s State) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == StateDisconnected {
		return
	}
	p.state = s
}

func (p *fakePeer) PartnerID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.partner
}

func (p *fakePeer) SessionID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.session
}

func (p *fakePeer) Bind(partnerID, sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.partner = partnerID
	p.session = sessionID
	p.state = StateInCall
}

func (p *fakePeer) Unbind() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.partner = ""
	p.session = ""
	if p.state != StateDisconnected {
		p.state = StateIdle
	}
}

func (p *fakePeer) Send(event string, payload any) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.events = append(p.events, recordedEvent{event: event, payload: payload})
	return true
}

func (p *fakePeer) Close(string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.state = StateDisconnected
}

func (p *fakePeer) eventsNamed(name string) []recordedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []recordedEvent{}
	for _, e := range p.events {
		if e.event == name {
			out = append(out, e)
		}
	}
	return out
}

func newTestMatcher(t *testing.T, blocks BlockChecker, timeout time.Duration) (*Matcher, *sessions.Manager) {
	t.Helper()
	manager := sessions.NewManager(nil, nil)
	m := NewMatcher(Options{
		QueueTimeout: timeout,
		Sessions:     manager,
		Blocks:       blocks,
	})
	return m, manager
}

func TestJoinPairsTwoWaitingPeers(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	a, b := newFakePeer("a"), newFakePeer("b")

	m.Join(a)
	if len(a.eventsNamed("match.found")) != 0 {
		t.Fatal("a single peer must not be matched")
	}
	m.Join(b)

	for _, p := range []*fakePeer{a, b} {
		if got := len(p.eventsNamed("match.found")); got != 1 {
			t.Fatalf("peer %s received %d match.found events, want 1", p.id, got)
		}
		if p.State() != StateInCall {
			t.Fatalf("peer %s state = %s, want in_call", p.id, p.State())
		}
	}
	if a.PartnerID() != "b" || b.PartnerID() != "a" {
		t.Fatalf("partners are not symmetric: a->%s b->%s", a.PartnerID(), b.PartnerID())
	}
	if a.SessionID() == "" || a.SessionID() != b.SessionID() {
		t.Fatalf("peers do not share a session id: %q vs %q", a.SessionID(), b.SessionID())
	}
	if got := manager.ActiveCount(); got != 1 {
		t.Fatalf("active sessions = %d, want 1", got)
	}
	if m.Len() != 0 {
		t.Fatalf("queue length = %d, want 0", m.Len())
	}
}

// TestConcurrentJoinsNeverDoubleAssign is the load-bearing guarantee of the
// matching engine: no matter how the joins interleave, a peer belongs to exactly
// one session and no peer is handed out twice.
func TestConcurrentJoinsNeverDoubleAssign(t *testing.T) {
	const peers = 200

	m, manager := newTestMatcher(t, nil, time.Minute)
	all := make([]*fakePeer, peers)
	for i := range all {
		all[i] = newFakePeer(fmt.Sprintf("peer-%03d", i))
	}

	var wg sync.WaitGroup
	for _, p := range all {
		wg.Add(1)
		go func(p *fakePeer) {
			defer wg.Done()
			m.Join(p)
		}(p)
	}
	wg.Wait()

	// Every session must contain exactly two distinct peers, and no peer may
	// appear in more than one session.
	membership := map[string]map[string]int{}
	bound := 0
	for _, p := range all {
		if p.State() != StateInCall {
			continue
		}
		bound++
		if membership[p.SessionID()] == nil {
			membership[p.SessionID()] = map[string]int{}
		}
		membership[p.SessionID()][p.id]++
	}

	for sessionID, members := range membership {
		if len(members) != 2 {
			t.Fatalf("session %s has %d members, want 2", sessionID, len(members))
		}
		for id, count := range members {
			if count != 1 {
				t.Fatalf("peer %s appears %d times in session %s", id, count, sessionID)
			}
		}
	}

	queueLength := m.Len()
	if bound+queueLength != peers {
		t.Fatalf("accounting mismatch: %d bound + %d queued != %d peers", bound, queueLength, peers)
	}
	if got := manager.ActiveCount(); got != bound/2 {
		t.Fatalf("active sessions = %d, want %d", got, bound/2)
	}

	// Every bound peer must point at a partner that points back, in the same session.
	for _, p := range all {
		if p.State() != StateInCall {
			continue
		}
		partnerID := p.PartnerID()
		var partner *fakePeer
		for _, candidate := range all {
			if candidate.ID() == partnerID {
				partner = candidate
				break
			}
		}
		if partner == nil {
			t.Fatalf("peer %s points at unknown partner %s", p.id, partnerID)
		}
		if partner.PartnerID() != p.id {
			t.Fatalf("asymmetric binding: %s -> %s -> %s", p.id, partnerID, partner.PartnerID())
		}
		if partner.SessionID() != p.SessionID() {
			t.Fatalf("session mismatch between %s and %s", p.id, partnerID)
		}
	}
}

func TestBlockedPeersAreNotPaired(t *testing.T) {
	blocks := blockedSet{"a": {"b": {}}}
	m, manager := newTestMatcher(t, blocks, time.Minute)

	m.Join(newFakePeer("a"))
	m.Join(newFakePeer("b"))
	if got := manager.ActiveCount(); got != 0 {
		t.Fatalf("blocked peers were paired, active sessions = %d", got)
	}
	if m.Len() != 2 {
		t.Fatalf("queue length = %d, want both peers still waiting", m.Len())
	}

	c := newFakePeer("c")
	m.Join(c)
	if got := manager.ActiveCount(); got != 1 {
		t.Fatalf("unblocked peer was not paired, active sessions = %d", got)
	}
}

func TestCancelRemovesPeerFromQueue(t *testing.T) {
	m, _ := newTestMatcher(t, nil, time.Minute)
	p := newFakePeer("a")

	m.Join(p)
	if p.State() != StateWaiting {
		t.Fatalf("state = %s, want waiting", p.State())
	}
	if !m.Cancel(p) {
		t.Fatal("cancel reported no waiting peer")
	}
	if p.State() != StateIdle {
		t.Fatalf("state = %s, want idle", p.State())
	}
	if m.Cancel(p) {
		t.Fatal("second cancel must be a no-op")
	}
}

func TestSweepTimesOutWaitingPeer(t *testing.T) {
	m, _ := newTestMatcher(t, nil, 30*time.Millisecond)
	p := newFakePeer("a")

	m.Join(p)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.sweep()
		if len(p.eventsNamed("queue.timeout")) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if got := len(p.eventsNamed("queue.timeout")); got != 1 {
		t.Fatalf("queue.timeout events = %d, want 1", got)
	}
	if p.State() != StateIdle {
		t.Fatalf("state = %s, want idle", p.State())
	}
	if m.Len() != 0 {
		t.Fatalf("timed out peer still queued: %d", m.Len())
	}
}

func TestJoinIsIgnoredWhileInCall(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	a, b, c := newFakePeer("a"), newFakePeer("b"), newFakePeer("c")

	m.Join(a)
	m.Join(b)
	// a is now in a call: joining again must not create a second session.
	m.Join(a)
	if got := manager.ActiveCount(); got != 1 {
		t.Fatalf("active sessions = %d, want 1", got)
	}
	m.Join(c)
	if got := manager.ActiveCount(); got != 1 {
		t.Fatalf("active sessions = %d, want 1", got)
	}
	if c.State() != StateWaiting {
		t.Fatalf("c state = %s, want waiting", c.State())
	}
}

func TestMatcherRunStopsOnContextCancel(t *testing.T) {
	m, _ := newTestMatcher(t, nil, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("matcher.Run did not return after cancellation")
	}
}

type blockedSet map[string]map[string]struct{}

func (b blockedSet) IsBlocked(a, other string) bool {
	if set, ok := b[a]; ok {
		if _, found := set[other]; found {
			return true
		}
	}
	if set, ok := b[other]; ok {
		if _, found := set[a]; found {
			return true
		}
	}
	return false
}
