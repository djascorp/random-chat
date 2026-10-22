package signaling

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"random-chat/internal/config"
	"random-chat/internal/matching"
	"random-chat/internal/metrics"
	"random-chat/internal/moderation"
	"random-chat/internal/sessions"
	"random-chat/internal/storage"
)

func testConfig() *config.Config {
	return &config.Config{
		Addr: ":0",
		// Deliberately strict: same-origin must work without being listed, and a
		// foreign origin must be refused unless it is spelled out here.
		AllowedOrigins:   []string{"https://app.example.com"},
		MaxMessageBytes:  128 * 1024,
		SendBuffer:       64,
		QueueTimeout:     2 * time.Second,
		WriteTimeout:     5 * time.Second,
		PongTimeout:      30 * time.Second,
		PingInterval:     10 * time.Second,
		JoinRatePerMin:   600,
		ChatRatePerMin:   600,
		ReportRatePerMin: 60,
		SignalRatePerMin: 6000,
		MaxChatRunes:     200,
		SuspendDuration:  time.Minute,
		ViolationLimit:   100,
		IceServers:       []config.IceServer{{URLs: []string{"stun:stun.example.org:3478"}}},
	}
}

func newTestHub(t *testing.T) *Hub {
	t.Helper()
	cfg := testConfig()
	store := storage.NewMemory()
	manager := sessions.NewManager(store, discardLogger())
	return NewHub(HubOptions{
		Config:     cfg,
		Store:      store,
		Sessions:   manager,
		Matcher:    matching.NewMatcher(matching.Options{QueueTimeout: cfg.QueueTimeout, Sessions: manager, Logger: discardLogger()}),
		Moderation: moderation.NewService(t.Context(), store, moderation.Options{SuspendDuration: time.Minute}, discardLogger()),
		Metrics:    metrics.New(),
		Logger:     discardLogger(),
	})
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testClient is a WebSocket client that can await a specific event while
// stashing anything that arrives out of order.
type testClient struct {
	t        *testing.T
	conn     *websocket.Conn
	userID   string
	pending  []Envelope
	buffered bool
}

func dial(t *testing.T, srv *httptest.Server, cid string) *testClient {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	if cid != "" {
		url += "?cid=" + cid
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	c := &testClient{t: t, conn: conn}
	hello := c.await(EvtHello, 2*time.Second)
	var payload HelloPayload
	if err := json.Unmarshal(hello.Data, &payload); err != nil {
		t.Fatalf("decode hello: %v", err)
	}
	if payload.ProtocolVersion != config.ProtocolVersion {
		t.Fatalf("protocol version = %q, want %q", payload.ProtocolVersion, config.ProtocolVersion)
	}
	if len(payload.IceServers) == 0 {
		t.Fatal("server.hello must advertise at least one ICE server")
	}
	c.userID = payload.UserID
	return c
}

func (c *testClient) send(event string, data any) {
	c.t.Helper()
	env := Envelope{Event: event}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			c.t.Fatalf("marshal payload for %s: %v", event, err)
		}
		env.Data = raw
	}
	if err := c.conn.WriteJSON(env); err != nil {
		c.t.Fatalf("write %s: %v", event, err)
	}
}

func (c *testClient) rawSend(payload string) {
	c.t.Helper()
	if err := c.conn.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
		c.t.Fatalf("raw write: %v", err)
	}
}

func (c *testClient) read() (Envelope, error) {
	_ = c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var env Envelope
	if err := c.conn.ReadJSON(&env); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

// await returns the next envelope of the given event, consuming buffered and
// freshly read frames until it finds one.
func (c *testClient) await(event string, timeout time.Duration) Envelope {
	c.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for i, env := range c.pending {
			if env.Event == event {
				c.pending = append(c.pending[:i], c.pending[i+1:]...)
				return env
			}
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("timed out waiting for %s (buffered: %v)", event, c.eventNames())
		}
		env, err := c.read()
		if err != nil {
			c.t.Fatalf("waiting for %s: read failed: %v", event, err)
		}
		c.pending = append(c.pending, env)
	}
}

func (c *testClient) eventNames() []string {
	names := make([]string, 0, len(c.pending))
	for _, env := range c.pending {
		names = append(names, env.Event)
	}
	return names
}

func (c *testClient) close() { _ = c.conn.Close() }

func TestQueueJoinMatchesTwoClientsAndRelaysSignaling(t *testing.T) {
	hub := newTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	a := dial(t, srv, "client-aaaaaaaa")
	b := dial(t, srv, "client-bbbbbbbb")

	if a.userID != "client-aaaaaaaa" {
		t.Fatalf("user id = %q, want the client supplied identifier", a.userID)
	}

	a.send(EvtQueueJoin, nil)
	a.await(EvtQueueWaiting, time.Second)

	b.send(EvtQueueJoin, nil)

	matchA := a.await(EvtMatchFound, 2*time.Second)
	matchB := b.await(EvtMatchFound, 2*time.Second)

	var payloadA, payloadB struct {
		SessionID string `json:"session_id"`
		PeerID    string `json:"peer_id"`
		Role      string `json:"role"`
	}
	mustDecode(t, matchA.Data, &payloadA)
	mustDecode(t, matchB.Data, &payloadB)

	if payloadA.SessionID == "" || payloadA.SessionID != payloadB.SessionID {
		t.Fatalf("session ids differ: %q vs %q", payloadA.SessionID, payloadB.SessionID)
	}
	if payloadA.PeerID != b.userID || payloadB.PeerID != a.userID {
		t.Fatalf("peer ids are wrong: %q/%q", payloadA.PeerID, payloadB.PeerID)
	}
	if payloadA.Role == payloadB.Role {
		t.Fatalf("both peers got role %q, exactly one must offer", payloadA.Role)
	}

	offerer, answerer := a, b
	if payloadB.Role == "offerer" {
		offerer, answerer = b, a
	}

	offerer.send(EvtRTCOffer, SDPPayload{Type: "offer", SDP: "v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\n"})
	relayed := answerer.await(EvtRTCOffer, 2*time.Second)
	var sdp SDPPayload
	mustDecode(t, relayed.Data, &sdp)
	if sdp.Type != "offer" || sdp.SDP == "" {
		t.Fatalf("offer was not relayed intact: %+v", sdp)
	}

	answerer.send(EvtRTCAnswer, SDPPayload{Type: "answer", SDP: "v=0\r\no=- 2 2 IN IP4 127.0.0.1\r\n"})
	back := offerer.await(EvtRTCAnswer, 2*time.Second)
	mustDecode(t, back.Data, &sdp)
	if sdp.Type != "answer" {
		t.Fatalf("answer was not relayed intact: %+v", sdp)
	}

	mid := "0"
	index := 0
	offerer.send(EvtRTCICE, ICEPayload{Candidate: "candidate:1 1 udp 1 1.2.3.4 5000 typ host", SDPMid: &mid, SDPMLineIndex: &index})
	ice := answerer.await(EvtRTCICE, 2*time.Second)
	var candidate ICEPayload
	mustDecode(t, ice.Data, &candidate)
	if candidate.Candidate == "" || candidate.SDPMid == nil || *candidate.SDPMid != "0" {
		t.Fatalf("candidate was not relayed intact: %+v", candidate)
	}

	offerer.send(EvtChatSend, ChatSendPayload{Text: "  bonjour  "})
	chat := answerer.await(EvtChatMessage, 2*time.Second)
	var message ChatMessagePayload
	mustDecode(t, chat.Data, &message)
	if message.Text != "bonjour" {
		t.Fatalf("chat text = %q, want trimmed payload", message.Text)
	}
	if message.From != "peer" {
		t.Fatalf("chat from = %q, want peer", message.From)
	}
	offerer.await(EvtChatAck, time.Second)
}

func decodeMatch(t *testing.T, env Envelope) map[string]any {
	t.Helper()
	var payload map[string]any
	mustDecode(t, env.Data, &payload)
	return payload
}

func peerIDs(t *testing.T, payload map[string]any) []string {
	t.Helper()
	raw, ok := payload["peers"].([]any)
	if !ok {
		t.Fatalf("match.found carries no peers list: %v", payload)
	}
	out := make([]string, 0, len(raw))
	for _, id := range raw {
		s, ok := id.(string)
		if !ok || s == "" {
			t.Fatalf("match.found carries an invalid peer: %v", payload)
		}
		out = append(out, s)
	}
	return out
}

func joinGroup(t *testing.T, c *testClient, max int) {
	t.Helper()
	c.send(EvtQueueJoin, QueueJoinPayload{Mode: "group", MaxParticipants: max})
}

// TestGroupConferenceAddAndMesh verifies the full conference path: group
// search prefs form a room, session.add pulls a waiter in, chat fans out to
// every member, and SDP carries an explicit recipient plus the sender.
func TestGroupConferenceAddAndMesh(t *testing.T) {
	hub := newTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	a := dial(t, srv, "client-aaaaaaaa")
	b := dial(t, srv, "client-bbbbbbbb")
	c := dial(t, srv, "client-cccccccc")

	joinGroup(t, a, 4)
	a.await(EvtQueueWaiting, 2*time.Second)
	joinGroup(t, b, 0)
	match := decodeMatch(t, a.await(EvtMatchFound, 2*time.Second))
	decodeMatch(t, b.await(EvtMatchFound, 2*time.Second))
	if got := len(peerIDs(t, match)); got != 2 {
		t.Fatalf("room has %d members, want 2", got)
	}
	if match["mode"] != "group" {
		t.Fatalf("mode = %v, want group", match["mode"])
	}

	// A late group waiter does not barge in on its own.
	joinGroup(t, c, 0)
	c.await(EvtQueueWaiting, 2*time.Second)
	if got := hub.sessions.ActiveCount(); got != 1 {
		t.Fatalf("active sessions = %d, want 1", got)
	}

	// The Ajouter button pulls them in: newcomer first, then the room.
	a.send(EvtSessionAdd, nil)
	joined := decodeMatch(t, c.await(EvtMatchFound, 2*time.Second))
	if got := len(peerIDs(t, joined)); got != 3 {
		t.Fatalf("room has %d members after add, want 3", got)
	}
	for _, m := range []*testClient{a, b} {
		notice := m.await(EvtPeerJoined, 2*time.Second)
		var payload PeerJoinedPayload
		mustDecode(t, notice.Data, &payload)
		if payload.PeerID != c.userID {
			t.Fatalf("peer.joined announces %q, want %q", payload.PeerID, c.userID)
		}
	}
	if got := hub.sessions.ActiveCount(); got != 1 {
		t.Fatalf("active sessions = %d, want 1", got)
	}

	// Chat fans out to every other member.
	a.send(EvtChatSend, ChatSendPayload{Text: "salut à tous"})
	for _, m := range []*testClient{b, c} {
		msg := m.await(EvtChatMessage, 2*time.Second)
		var payload ChatMessagePayload
		mustDecode(t, msg.Data, &payload)
		if payload.Text != "salut à tous" {
			t.Fatalf("chat text = %q, want the broadcast", payload.Text)
		}
	}

	// Signaling names its recipient and the server stamps the sender.
	a.send(EvtRTCOffer, SDPPayload{Type: "offer", SDP: "v=0", To: c.userID})
	relayed := c.await(EvtRTCOffer, 2*time.Second)
	var sdp SDPPayload
	mustDecode(t, relayed.Data, &sdp)
	if sdp.From != a.userID {
		t.Fatalf("relayed offer from = %q, want %q", sdp.From, a.userID)
	}

	// An offer without a recipient is ambiguous in a conference.
	b.send(EvtRTCOffer, SDPPayload{Type: "offer", SDP: "v=0"})
	failed := b.await(EvtError, 2*time.Second)
	var protocolErr ErrorPayload
	mustDecode(t, failed.Data, &protocolErr)
	if protocolErr.Code != "no_peer" {
		t.Fatalf("error code = %q, want no_peer", protocolErr.Code)
	}
}

// TestReportEjectsOnlyTargetInConference checks that reporting one tile does
// not collapse the room: the target is ejected, the survivors stay connected.
func TestReportEjectsOnlyTargetInConference(t *testing.T) {
	hub := newTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	a := dial(t, srv, "client-aaaaaaaa")
	b := dial(t, srv, "client-bbbbbbbb")
	c := dial(t, srv, "client-cccccccc")

	joinGroup(t, a, 0)
	a.await(EvtQueueWaiting, 2*time.Second)
	joinGroup(t, b, 0)
	a.await(EvtMatchFound, 2*time.Second)
	b.await(EvtMatchFound, 2*time.Second)
	joinGroup(t, c, 0)
	c.await(EvtQueueWaiting, 2*time.Second)
	a.send(EvtSessionAdd, nil)
	c.await(EvtMatchFound, 2*time.Second)
	a.await(EvtPeerJoined, 2*time.Second)
	b.await(EvtPeerJoined, 2*time.Second)

	a.send(EvtReportCreate, ReportPayload{Category: "spam", Reason: "pub", Target: c.userID})
	a.await(EvtReportAck, 2*time.Second)

	ended := c.await(EvtSessionEnded, 2*time.Second)
	var endPayload SessionEndedPayload
	mustDecode(t, ended.Data, &endPayload)
	if endPayload.Reason != "reported" {
		t.Fatalf("end reason = %q, want reported", endPayload.Reason)
	}

	for _, m := range []*testClient{a, b} {
		left := m.await(EvtPeerLeft, 2*time.Second)
		var payload PeerLeftPayload
		mustDecode(t, left.Data, &payload)
		if payload.PeerID != c.userID || payload.Members != 2 {
			t.Fatalf("peer.left = %+v, want %q with 2 members left", payload, c.userID)
		}
	}
	if got := hub.sessions.ActiveCount(); got != 1 {
		t.Fatalf("active sessions = %d, want the room to survive", got)
	}
	if got := hub.QueueLength(); got != 0 {
		t.Fatalf("queue length = %d, want 0", got)
	}
}

func TestNextEndsSessionAndRequeuesOnlyRequester(t *testing.T) {
	hub := newTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	a := dial(t, srv, "client-aaaaaaaa")
	b := dial(t, srv, "client-bbbbbbbb")

	a.send(EvtQueueJoin, nil)
	b.send(EvtQueueJoin, nil)
	a.await(EvtMatchFound, 2*time.Second)
	b.await(EvtMatchFound, 2*time.Second)

	a.send(EvtSessionNext, nil)
	a.await(EvtQueueWaiting, 2*time.Second)

	ended := b.await(EvtSessionEnded, 2*time.Second)
	var payload SessionEndedPayload
	mustDecode(t, ended.Data, &payload)
	if payload.Reason != "next" {
		t.Fatalf("end reason = %q, want next", payload.Reason)
	}
	if !payload.CanRequeue {
		t.Fatal("the abandoned peer should be allowed to search again")
	}

	if got := hub.QueueLength(); got != 1 {
		t.Fatalf("queue length = %d, want only the requester waiting", got)
	}
	if got := hub.sessions.ActiveCount(); got != 0 {
		t.Fatalf("active sessions = %d, want the session closed", got)
	}
}

func TestUnknownAndUnauthorizedEventsAreRejected(t *testing.T) {
	hub := newTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	a := dial(t, srv, "client-aaaaaaaa")

	// Signaling before any session exists must never be forwarded.
	a.send(EvtRTCOffer, SDPPayload{Type: "offer", SDP: "v=0"})
	errPayload := a.await(EvtError, 2*time.Second)
	var protocolErr ErrorPayload
	mustDecode(t, errPayload.Data, &protocolErr)
	if protocolErr.Code != "no_session" {
		t.Fatalf("error code = %q, want no_session", protocolErr.Code)
	}

	// Malformed frames are rejected without dropping the connection.
	a.rawSend("this is not json")
	malformed := a.await(EvtError, 2*time.Second)
	mustDecode(t, malformed.Data, &protocolErr)
	if protocolErr.Code != "malformed" {
		t.Fatalf("error code = %q, want malformed", protocolErr.Code)
	}

	// Unknown event names are rejected explicitly.
	a.rawSend(`{"event":"rtc.evil","data":{}}`)
	unknown := a.await(EvtError, 2*time.Second)
	mustDecode(t, unknown.Data, &protocolErr)
	if protocolErr.Code != "unknown_event" {
		t.Fatalf("error code = %q, want unknown_event", protocolErr.Code)
	}

	// The connection is still usable afterwards.
	a.send(EvtQueueJoin, nil)
	a.await(EvtQueueWaiting, 2*time.Second)
}

func TestQueueCancelWithoutSearchIsRejected(t *testing.T) {
	hub := newTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	a := dial(t, srv, "client-aaaaaaaa")
	a.send(EvtQueueCancel, nil)

	payload := a.await(EvtError, 2*time.Second)
	var protocolErr ErrorPayload
	mustDecode(t, payload.Data, &protocolErr)
	if protocolErr.Code != "not_waiting" {
		t.Fatalf("error code = %q, want not_waiting", protocolErr.Code)
	}

	a.send(EvtQueueJoin, nil)
	a.await(EvtQueueWaiting, 2*time.Second)
	a.send(EvtQueueCancel, nil)
	a.await(EvtQueueCanceled, 2*time.Second)
	if got := hub.QueueLength(); got != 0 {
		t.Fatalf("queue length = %d, want 0 after cancelling", got)
	}
}

func TestDisconnectEndsSessionForPartner(t *testing.T) {
	hub := newTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	a := dial(t, srv, "client-aaaaaaaa")
	b := dial(t, srv, "client-bbbbbbbb")

	a.send(EvtQueueJoin, nil)
	b.send(EvtQueueJoin, nil)
	a.await(EvtMatchFound, 2*time.Second)
	b.await(EvtMatchFound, 2*time.Second)

	a.close()

	ended := b.await(EvtSessionEnded, 3*time.Second)
	var payload SessionEndedPayload
	mustDecode(t, ended.Data, &payload)
	if payload.Reason != "peer_disconnected" {
		t.Fatalf("end reason = %q, want peer_disconnected", payload.Reason)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.sessions.ActiveCount() == 0 && hub.Count() == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("resources were not released: sessions=%d connections=%d", hub.sessions.ActiveCount(), hub.Count())
}

func TestReportEndsSessionAndRecordsIt(t *testing.T) {
	hub := newTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	a := dial(t, srv, "client-aaaaaaaa")
	b := dial(t, srv, "client-bbbbbbbb")

	a.send(EvtQueueJoin, nil)
	b.send(EvtQueueJoin, nil)
	a.await(EvtMatchFound, 2*time.Second)
	b.await(EvtMatchFound, 2*time.Second)

	a.send(EvtReportCreate, ReportPayload{Category: "harassment", Reason: "insultes", Block: true})

	ack := a.await(EvtReportAck, 2*time.Second)
	var payload ReportAckPayload
	mustDecode(t, ack.Data, &payload)
	if payload.ReportID == "" {
		t.Fatal("report acknowledgement must carry an identifier")
	}
	if !payload.Blocked {
		t.Fatal("the report asked for a block, the acknowledgement must confirm it")
	}

	ended := b.await(EvtSessionEnded, 2*time.Second)
	var endPayload SessionEndedPayload
	mustDecode(t, ended.Data, &endPayload)
	if endPayload.Reason != "reported" {
		t.Fatalf("end reason = %q, want reported", endPayload.Reason)
	}
	if endPayload.CanRequeue {
		t.Fatal("a reported session must not offer to requeue against the same peer")
	}
	if hub.metrics.Reports.Value() != 1 {
		t.Fatalf("report counter = %d, want 1", hub.metrics.Reports.Value())
	}
}

func TestConcurrentNextCallsKeepQueueConsistent(t *testing.T) {
	hub := newTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	const pairs = 6
	clients := make([]*testClient, 0, pairs*2)
	for i := 0; i < pairs*2; i++ {
		clients = append(clients, dial(t, srv, "client-"+string(rune('a'+i))+"00000000"))
	}

	for _, c := range clients {
		c.send(EvtQueueJoin, nil)
	}
	for _, c := range clients {
		c.await(EvtMatchFound, 5*time.Second)
	}
	if got := hub.sessions.ActiveCount(); got != pairs {
		t.Fatalf("active sessions = %d, want %d", got, pairs)
	}

	// Every client presses "next" at the same time. The requester immediately
	// re-enters the queue, so peers may pair up again.
	//
	// Sampling the hub mid-flight would race with terminate(), which unbinds the
	// requester before notifying the partner: a reader can legitimately observe a
	// session whose second member has not been detached yet. So we assert the
	// hard invariant on every sample (never more than two peers in a session) and
	// require a stable, fully consistent sample before checking the accounting.
	for _, c := range clients {
		c.send(EvtSessionNext, nil)
	}

	var bound int
	var membership map[string]int
	deadline := time.Now().Add(4 * time.Second)
	for {
		bound, membership = hub.snapshotSessions()

		for sessionID, members := range membership {
			if members > 2 {
				t.Fatalf("session %s holds %d peers, a session can only ever hold 2", sessionID, members)
			}
		}

		consistent := bound%2 == 0 &&
			hub.sessions.ActiveCount() == bound/2 &&
			hub.QueueLength()+bound == pairs*2 &&
			!anySessionWithOneMember(hub, membership)

		if consistent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hub never settled: bound=%d queue=%d sessions=%d membership=%v",
				bound, hub.QueueLength(), hub.sessions.ActiveCount(), membership)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// snapshotSessions returns how many peers are in a call and how many peers point
// at each session identifier.
func (h *Hub) snapshotSessions() (int, map[string]int) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	bound := 0
	membership := make(map[string]int)
	for _, c := range h.clients {
		sessionID := c.SessionID()
		if sessionID == "" {
			continue
		}
		if c.State() == matching.StateInCall {
			bound++
		}
		membership[sessionID]++
	}
	return bound, membership
}

// anySessionWithOneMember reports whether a still-active session is referenced by
// exactly one peer, which would be a half-open session rather than a transient.
func anySessionWithOneMember(h *Hub, membership map[string]int) bool {
	for sessionID, members := range membership {
		if members != 1 {
			continue
		}
		if _, stillActive := h.sessions.Get(sessionID); stillActive {
			return true
		}
	}
	return false
}

// TestWebSocketOriginPolicy reproduces the single-binary deployment: the SPA is
// served by the same server that upgrades the socket, so the page origin equals
// the request host and must be accepted even when ALLOWED_ORIGINS is strict.
func TestWebSocketOriginPolicy(t *testing.T) {
	hub := newTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	t.Run("same origin is accepted", func(t *testing.T) {
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{srv.URL}})
		if err != nil {
			t.Fatalf("same-origin upgrade rejected: %v (status %s)", err, statusText(resp))
		}
		defer conn.Close()

		var hello Envelope
		if err := conn.ReadJSON(&hello); err != nil {
			t.Fatalf("reading server.hello: %v", err)
		}
		if hello.Event != EvtHello {
			t.Fatalf("first frame = %q, want %q", hello.Event, EvtHello)
		}
	})

	t.Run("listed origin is accepted", func(t *testing.T) {
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{"https://app.example.com"}})
		if err != nil {
			t.Fatalf("allowlisted origin rejected: %v (status %s)", err, statusText(resp))
		}
		conn.Close()
	})

	t.Run("foreign origin is rejected with 403", func(t *testing.T) {
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{"https://evil.example"}})
		if err == nil {
			conn.Close()
			t.Fatal("a cross-origin upgrade must be refused")
		}
		if resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %s, want 403 Forbidden", statusText(resp))
		}
	})
}

func statusText(resp *http.Response) string {
	if resp == nil {
		return "<no response>"
	}
	return resp.Status
}

func mustDecode(t *testing.T, raw json.RawMessage, into any) {
	t.Helper()
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
}
