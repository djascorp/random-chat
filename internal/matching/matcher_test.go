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
	mode    string
	max     int
	partner string
	session string
	events  []recordedEvent
	closed  bool
}

func newFakePeer(id string) *fakePeer {
	return &fakePeer{id: id, state: StateIdle, mode: soloMode}
}

// newGroupPeer builds a waiter for a conference with the given cap
// (0 means the peer sets no constraint).
func newGroupPeer(id string, max int) *fakePeer {
	return &fakePeer{id: id, state: StateIdle, mode: groupMode, max: max}
}

func (p *fakePeer) Mode() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mode
}

func (p *fakePeer) MaxParticipants() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.max
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

func matchPayloadOf(t *testing.T, p *fakePeer) matchPayloadData {
	t.Helper()
	events := p.eventsNamed("match.found")
	if len(events) != 1 {
		t.Fatalf("peer %s received %d match.found events, want 1", p.id, len(events))
	}
	payload, ok := events[0].payload.(matchPayloadData)
	if !ok {
		t.Fatalf("peer %s match.found payload has type %T", p.id, events[0].payload)
	}
	return payload
}

// Two group waiters form a conference as soon as they meet; the member
// list each of them receives carries the mode and the shared cap.
func TestGroupFormsConference(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	a, b := newGroupPeer("a", 0), newGroupPeer("b", 4)
	m.Join(a)
	if len(a.eventsNamed("match.found")) != 0 {
		t.Fatal("a single group peer must not be matched")
	}
	m.Join(b)
	if got := manager.ActiveCount(); got != 1 {
		t.Fatalf("active sessions = %d, want 1", got)
	}
	for _, p := range []*fakePeer{a, b} {
		payload := matchPayloadOf(t, p)
		if payload.Mode != groupMode {
			t.Fatalf("peer %s mode = %q, want group", p.id, payload.Mode)
		}
		if len(payload.Peers) != 2 {
			t.Fatalf("peer %s sees %d peers, want 2", p.id, len(payload.Peers))
		}
		if payload.MaxParticipants != 4 {
			t.Fatalf("peer %s cap = %d, want 4", p.id, payload.MaxParticipants)
		}
		if p.State() != StateInCall {
			t.Fatalf("peer %s state = %s, want in_call", p.id, p.State())
		}
	}
}

func TestGroupRespectsSmallestCap(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	peers := []*fakePeer{
		newGroupPeer("a", 3),
		newGroupPeer("b", 0),
		newGroupPeer("c", 0),
		newGroupPeer("d", 0),
		newGroupPeer("e", 0),
	}
	for _, p := range peers {
		m.Join(p)
	}
	// a and b open the first room (capped at 3 by a), c and d open the
	// second one, e waits alone: rooms never absorb waiters on their own.
	if got := manager.ActiveCount(); got != 2 {
		t.Fatalf("active sessions = %d, want 2", got)
	}
	if m.Len() != 1 {
		t.Fatalf("queue length = %d, want only e waiting", m.Len())
	}
	if got := len(matchPayloadOf(t, peers[0]).Peers); got != 2 {
		t.Fatalf("first room has %d members, want 2", got)
	}
	// An explicit add pulls e into the first room, which a capped at 3.
	rooms := manager.GroupSessions()
	if filled, _ := m.Fill(rooms[0].ID); !filled {
		t.Fatal("Fill must pull e into the first room")
	}
	if got := len(matchPayloadOf(t, peers[4]).Peers); got != 3 {
		t.Fatalf("first room has %d members after Fill, want 3", got)
	}
	if filled, _ := m.Fill(rooms[0].ID); filled {
		t.Fatal("Fill must refuse once the cap is reached")
	}
}

// Live rooms never absorb waiters on their own: a group joiner that arrives
// after the room formed waits until an explicit Fill pulls them in.
func TestLateJoinerWaitsForExplicitFill(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	a, b := newGroupPeer("a", 3), newGroupPeer("b", 0)
	m.Join(a)
	m.Join(b)
	c := newGroupPeer("c", 0)
	m.Join(c)

	if got := manager.ActiveCount(); got != 1 {
		t.Fatalf("active sessions = %d, want 1", got)
	}
	if len(c.eventsNamed("match.found")) != 0 {
		t.Fatal("a late joiner must not be pulled in without an explicit add")
	}
	rooms := manager.GroupSessions()
	if len(rooms) != 1 {
		t.Fatalf("group sessions = %d, want 1", len(rooms))
	}
	if filled, _ := m.Fill(rooms[0].ID); !filled {
		t.Fatal("Fill must pull the fitting waiter into the room")
	}

	payload := matchPayloadOf(t, c)
	if len(payload.Peers) != 3 {
		t.Fatalf("late joiner sees %d peers, want 3", len(payload.Peers))
	}
	for _, p := range []*fakePeer{a, b} {
		if got := len(p.eventsNamed("peer.joined")); got != 1 {
			t.Fatalf("peer %s received %d peer.joined events, want 1", p.id, got)
		}
	}
	if got := len(c.eventsNamed("peer.joined")); got != 0 {
		t.Fatalf("late joiner received %d peer.joined events, want 0", got)
	}
	if m.Len() != 0 {
		t.Fatalf("queue length = %d, want 0", m.Len())
	}
}

func TestFillRefusesFullRoom(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	m.Join(newGroupPeer("a", 2))
	m.Join(newGroupPeer("b", 0))
	m.Join(newGroupPeer("c", 0))

	rooms := manager.GroupSessions()
	if len(rooms) != 1 {
		t.Fatalf("group sessions = %d, want 1", len(rooms))
	}
	// The room holds {a, b} at a's cap of 2; c waits for another room.
	if filled, _ := m.Fill(rooms[0].ID); filled {
		t.Fatal("Fill must refuse a room that reached its cap")
	}
	if got := manager.ActiveCount(); got != 1 {
		t.Fatalf("active sessions = %d, want 1", got)
	}
}

func TestSoloAndGroupNeverMix(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	m.Join(newFakePeer("solo"))
	m.Join(newGroupPeer("group", 0))
	if got := manager.ActiveCount(); got != 0 {
		t.Fatalf("mixed modes were paired, active sessions = %d", got)
	}
	if m.Len() != 2 {
		t.Fatalf("queue length = %d, want 2", m.Len())
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

// Fill on a solo call promotes it to a conference before drawing: the room
// learns its new shape through session.updated ahead of peer.joined.
func TestFillPromotesSoloSession(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	a, b := newFakePeer("a"), newFakePeer("b")
	m.Join(a)
	m.Join(b)
	if got := manager.ActiveCount(); got != 1 {
		t.Fatalf("active sessions = %d, want 1", got)
	}

	c := newGroupPeer("c", 0)
	m.Join(c)

	var sessionID string
	for _, p := range []*fakePeer{a, b} {
		events := p.eventsNamed("match.found")
		if len(events) != 1 {
			t.Fatalf("peer %s received %d match.found events, want 1", p.id, len(events))
		}
		if payload, ok := events[0].payload.(matchPayloadData); ok {
			sessionID = payload.SessionID
		}
	}

	filled, promoted := m.Fill(sessionID)
	if !filled || !promoted {
		t.Fatalf("Fill = (%v, %v), want (true, true)", filled, promoted)
	}
	s, ok := manager.Get(sessionID)
	if !ok || s.Mode != groupMode {
		t.Fatalf("session mode = %v, want group", s)
	}
	for _, p := range []*fakePeer{a, b} {
		updated := p.eventsNamed("session.updated")
		if len(updated) != 1 {
			t.Fatalf("peer %s received %d session.updated events, want 1", p.id, len(updated))
		}
		// The shape announcement must precede the newcomer announcement.
		joined := p.eventsNamed("peer.joined")
		if len(joined) != 1 {
			t.Fatalf("peer %s received %d peer.joined events, want 1", p.id, len(joined))
		}
		if firstIndex(p, "session.updated") > firstIndex(p, "peer.joined") {
			t.Fatalf("peer %s learned peer.joined before session.updated", p.id)
		}
	}
}

// Two simultaneous Fills on one solo call promote exactly once: a single
// waiter is pulled in and the room announces its new shape a single time.
func TestConcurrentFillPromotesOnce(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	a, b := newFakePeer("a"), newFakePeer("b")
	m.Join(a)
	m.Join(b)
	c := newGroupPeer("c", 0)
	m.Join(c)

	var sessionID string
	if events := a.eventsNamed("match.found"); len(events) == 1 {
		if payload, ok := events[0].payload.(matchPayloadData); ok {
			sessionID = payload.SessionID
		}
	}
	if sessionID == "" {
		t.Fatal("no session was created")
	}

	var wg sync.WaitGroup
	results := make([][2]bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			filled, promoted := m.Fill(sessionID)
			results[i] = [2]bool{filled, promoted}
		}(i)
	}
	wg.Wait()

	filled := 0
	promoted := 0
	for _, r := range results {
		if r[0] {
			filled++
		}
		if r[1] {
			promoted++
		}
	}
	if filled != 1 {
		t.Fatalf("filled = %d, want the single waiter pulled in once", filled)
	}
	if promoted != 1 {
		t.Fatalf("promotions = %d, want exactly 1", promoted)
	}
	for _, p := range []*fakePeer{a, b} {
		if got := len(p.eventsNamed("session.updated")); got != 1 {
			t.Fatalf("peer %s received %d session.updated events, want 1", p.id, got)
		}
	}
	s, _ := manager.Get(sessionID)
	if len(s.Members()) != 3 {
		t.Fatalf("room has %d members, want 3", len(s.Members()))
	}
}

// JoinSession honours the member cap under concurrency: ten simultaneous
// joins on the last seat let exactly one peer in.
func TestJoinSessionLastSeatSingleWinner(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	a, b := newGroupPeer("a", 0), newGroupPeer("b", 0)
	m.Join(a)
	m.Join(b)

	var sessionID string
	if events := a.eventsNamed("match.found"); len(events) == 1 {
		if payload, ok := events[0].payload.(matchPayloadData); ok {
			sessionID = payload.SessionID
		}
	}
	manager.SetMax(sessionID, 3)

	const contenders = 10
	peers := make([]*fakePeer, contenders)
	for i := range peers {
		peers[i] = newFakePeer(fmt.Sprintf("joiner-%02d", i))
	}
	results := make([]JoinResult, contenders)
	var wg sync.WaitGroup
	for i := range peers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = m.JoinSession(peers[i], sessionID)
		}(i)
	}
	wg.Wait()

	won := 0
	for _, r := range results {
		if r.Err == "" {
			won++
		} else if r.Err != JoinSessionFull {
			t.Fatalf("loser error = %q, want %q", r.Err, JoinSessionFull)
		}
	}
	if won != 1 {
		t.Fatalf("winners = %d, want exactly 1", won)
	}
	s, _ := manager.Get(sessionID)
	if len(s.Members()) != 3 {
		t.Fatalf("room has %d members, want 3", len(s.Members()))
	}
}

// A joiner blocked with any member is refused and the room is untouched.
func TestJoinSessionRefusesBlockedPeer(t *testing.T) {
	blocks := blockedSet{"a": {"evil": {}}}
	m, manager := newTestMatcher(t, blocks, time.Minute)
	a, b := newFakePeer("a"), newFakePeer("b")
	m.Join(a)
	m.Join(b)

	var sessionID string
	if events := a.eventsNamed("match.found"); len(events) == 1 {
		if payload, ok := events[0].payload.(matchPayloadData); ok {
			sessionID = payload.SessionID
		}
	}
	before, _ := manager.Get(sessionID)
	beforeCount := len(before.Members())

	evil := newFakePeer("evil")
	if res := m.JoinSession(evil, sessionID); res.Err != JoinBlocked {
		t.Fatalf("JoinSession error = %q, want %q", res.Err, JoinBlocked)
	}
	after, _ := manager.Get(sessionID)
	if len(after.Members()) != beforeCount {
		t.Fatalf("room changed after a refused join: %d members, want %d", len(after.Members()), beforeCount)
	}
	if evil.State() == StateInCall {
		t.Fatal("the refused joiner must not be in a call")
	}
}

// JoinSession respects the newcomer's own cap: a max=2 caller never lands in
// a room that would hold 3.
func TestJoinSessionRespectsNewcomerCap(t *testing.T) {
	m, _ := newTestMatcher(t, nil, time.Minute)
	a, b := newGroupPeer("a", 0), newGroupPeer("b", 0)
	m.Join(a)
	m.Join(b)

	var sessionID string
	if events := a.eventsNamed("match.found"); len(events) == 1 {
		if payload, ok := events[0].payload.(matchPayloadData); ok {
			sessionID = payload.SessionID
		}
	}
	picky := newGroupPeer("picky", 2)
	if res := m.JoinSession(picky, sessionID); res.Err != JoinSessionFull {
		t.Fatalf("JoinSession error = %q, want %q", res.Err, JoinSessionFull)
	}
}

// JoinSession on a solo room promotes it and announces the shape first.
func TestJoinSessionPromotesSolo(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	a, b := newFakePeer("a"), newFakePeer("b")
	m.Join(a)
	m.Join(b)

	var sessionID string
	if events := a.eventsNamed("match.found"); len(events) == 1 {
		if payload, ok := events[0].payload.(matchPayloadData); ok {
			sessionID = payload.SessionID
		}
	}
	guest := newFakePeer("guest")
	res := m.JoinSession(guest, sessionID)
	if res.Err != "" || !res.Promoted {
		t.Fatalf("JoinSession = %+v, want success with promotion", res)
	}
	s, _ := manager.Get(sessionID)
	if s.Mode != groupMode || len(s.Members()) != 3 {
		t.Fatalf("room = mode %q with %d members, want group with 3", s.Mode, len(s.Members()))
	}
	if got := len(guest.eventsNamed("match.found")); got != 1 {
		t.Fatalf("guest received %d match.found events, want 1", got)
	}
}

// OpenRoom creates an empty hosted session and binds the host in-call; a
// second call on the same host fails — one room per host.
func TestOpenRoomBindsIdleHost(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	host := newFakePeer("host")
	s, ok := m.OpenRoom(host)
	if !ok || host.State() != StateInCall || host.SessionID() != s.ID {
		t.Fatalf("OpenRoom = %v, host state %v, session %q", ok, host.State(), host.SessionID())
	}
	if s.Mode != soloMode || s.MaxParticipants != 2 || len(s.Members()) != 1 {
		t.Fatalf("room = mode %q cap %d members %d, want solo/2/[host]", s.Mode, s.MaxParticipants, len(s.Members()))
	}

	if _, ok := m.OpenRoom(host); ok {
		t.Fatal("an in-call host must not open a second room")
	}
	waiter := newGroupPeer("group-waiter", 0)
	m.Join(waiter) // alone in the queue: left waiting, not idle
	if _, ok := m.OpenRoom(waiter); ok {
		t.Fatal("a waiting peer must not open a room")
	}
	if got := manager.ActiveCount(); got != 1 {
		t.Fatalf("active sessions = %d, want 1", got)
	}
}

// An empty hosted group room honours the host's cap.
func TestOpenRoomGroupHonoursCap(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	host := newGroupPeer("host", 5)
	s, ok := m.OpenRoom(host)
	if !ok || s.Mode != groupMode || s.MaxParticipants != 5 {
		t.Fatalf("OpenRoom = %v mode %q cap %d, want group capped at 5", ok, s.Mode, s.MaxParticipants)
	}
	if uncapped, ok := m.OpenRoom(newGroupPeer("host2", 0)); !ok || uncapped.MaxParticipants != 0 {
		t.Fatalf("uncapped group room cap = %d, want 0", uncapped.MaxParticipants)
	}
	if got := manager.ActiveCount(); got != 2 {
		t.Fatalf("active sessions = %d, want 2", got)
	}
}

// Joining an empty hosted solo room keeps it solo: host + invitee is a normal
// 1:1, no promotion and no session.updated announcement.
func TestJoinSessionLoneSoloRoomStaysSolo(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	host := newFakePeer("host")
	s, ok := m.OpenRoom(host)
	if !ok {
		t.Fatal("OpenRoom failed")
	}

	guest := newFakePeer("guest")
	res := m.JoinSession(guest, s.ID)
	if res.Err != "" || res.Promoted {
		t.Fatalf("JoinSession = %+v, want success without promotion", res)
	}
	after, _ := manager.Get(s.ID)
	if after.Mode != soloMode || len(after.Members()) != 2 {
		t.Fatalf("room = mode %q with %d members, want solo with 2", after.Mode, len(after.Members()))
	}
	if guest.State() != StateInCall || guest.SessionID() != s.ID {
		t.Fatalf("guest state = %v session %q, want in-call in %q", guest.State(), guest.SessionID(), s.ID)
	}
	if got := len(host.eventsNamed("session.updated")); got != 0 {
		t.Fatalf("host received %d session.updated, want 0", got)
	}
	// The guest is a joiner: it offers, the host waits.
	if got := len(guest.eventsNamed("match.found")); got != 1 {
		t.Fatalf("guest received %d match.found, want 1", got)
	}
	if got := len(host.eventsNamed("peer.joined")); got != 1 {
		t.Fatalf("host received %d peer.joined, want 1", got)
	}
}

// A full solo room (2/2) still promotes on join — the historic path.
func TestJoinSessionFullSoloPromotes(t *testing.T) {
	m, manager := newTestMatcher(t, nil, time.Minute)
	a, b := newFakePeer("a"), newFakePeer("b")
	m.Join(a)
	m.Join(b)

	var sessionID string
	if events := a.eventsNamed("match.found"); len(events) == 1 {
		if payload, ok := events[0].payload.(matchPayloadData); ok {
			sessionID = payload.SessionID
		}
	}
	third := newFakePeer("third")
	res := m.JoinSession(third, sessionID)
	if res.Err != "" || !res.Promoted {
		t.Fatalf("JoinSession = %+v, want success with promotion", res)
	}
	s, _ := manager.Get(sessionID)
	if s.Mode != groupMode || len(s.Members()) != 3 {
		t.Fatalf("room = mode %q with %d members, want group with 3", s.Mode, len(s.Members()))
	}
}

// firstIndex returns the position of the first event with this name in the
// peer's full log, for ordering checks across event kinds.
func firstIndex(p *fakePeer, name string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, e := range p.events {
		if e.event == name {
			return i
		}
	}
	return -1
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
