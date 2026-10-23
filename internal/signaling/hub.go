// Package signaling implements the WebSocket signaling layer: connection
// lifecycle, protocol validation, SDP/ICE relaying and session termination.
package signaling

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"

	"random-chat/internal/config"
	"random-chat/internal/invites"
	"random-chat/internal/matching"
	"random-chat/internal/metrics"
	"random-chat/internal/moderation"
	"random-chat/internal/sessions"
	"random-chat/internal/storage"
)

const (
	maxSDPBytes       = 100 * 1024
	maxICEBytes       = 2 * 1024
	maxReportReason   = 500
	maxReportDetails  = 2000
	maxClientIDLength = 64
	minClientIDLength = 8
)

// HubOptions wires the hub to the rest of the application.
type HubOptions struct {
	Config     *config.Config
	Store      storage.Store
	Sessions   *sessions.Manager
	Matcher    *matching.Matcher
	Moderation *moderation.Service
	Metrics    *metrics.Metrics
	Invites    *invites.Registry
	Logger     *slog.Logger
}

// Hub owns the live connection set and routes every protocol event.
type Hub struct {
	cfg        *config.Config
	store      storage.Store
	sessions   *sessions.Manager
	matcher    *matching.Matcher
	moderation *moderation.Service
	metrics    *metrics.Metrics
	log        *slog.Logger
	upgrader   websocket.Upgrader

	mu      sync.RWMutex
	clients map[string]*Client

	joinLimiter   *moderation.RateLimiter
	chatLimiter   *moderation.RateLimiter
	reportLimiter *moderation.RateLimiter
	signalLimiter *moderation.RateLimiter
	inviteLimiter *moderation.RateLimiter
	invites       *invites.Registry
}

// NewHub builds the hub and installs the origin policy on the upgrader.
func NewHub(opts HubOptions) *Hub {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	registry := opts.Invites
	if registry == nil {
		registry = invites.NewRegistry()
	}
	h := &Hub{
		cfg:           opts.Config,
		store:         opts.Store,
		sessions:      opts.Sessions,
		matcher:       opts.Matcher,
		moderation:    opts.Moderation,
		metrics:       opts.Metrics,
		invites:       registry,
		log:           logger,
		clients:       make(map[string]*Client),
		joinLimiter:   moderation.NewRateLimiter(opts.Config.JoinRatePerMin, 6),
		chatLimiter:   moderation.NewRateLimiter(opts.Config.ChatRatePerMin, 15),
		reportLimiter: moderation.NewRateLimiter(opts.Config.ReportRatePerMin, 3),
		signalLimiter: moderation.NewRateLimiter(opts.Config.SignalRatePerMin, 120),
		inviteLimiter: moderation.NewRateLimiter(opts.Config.ReportRatePerMin, 3),
	}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if !h.cfg.AllowsOrigin(origin, r.Host) {
				h.log.Warn("websocket upgrade rejected",
					"origin", origin,
					"host", r.Host,
					"ip", clientIP(r),
					"hint", "add this origin to ALLOWED_ORIGINS")
				return false
			}
			return true
		},
	}
	return h
}

// Run performs periodic housekeeping until ctx is cancelled.
func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.joinLimiter.Sweep(10 * time.Minute)
			h.chatLimiter.Sweep(10 * time.Minute)
			h.reportLimiter.Sweep(30 * time.Minute)
			h.signalLimiter.Sweep(5 * time.Minute)
			h.inviteLimiter.Sweep(30 * time.Minute)
			h.log.Debug("hub stats",
				"connections", h.Count(),
				"sessions", h.sessions.ActiveCount(),
				"queue", h.matcher.Len())
		}
	}
}

// Count returns the number of live connections.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// QueueLength returns how many peers are waiting for a partner.
func (h *Hub) QueueLength() int { return h.matcher.Len() }

func (h *Hub) client(id string) *Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clients[id]
}

func (h *Hub) register(c *Client) (replaced *Client) {
	h.mu.Lock()
	replaced = h.clients[c.id]
	h.clients[c.id] = c
	h.mu.Unlock()
	h.metrics.ConnectionsOpened.Inc()
	h.metrics.ConnectionsActive.Inc()
	return replaced
}

func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	current, ok := h.clients[c.id]
	if !ok || current != c {
		h.mu.Unlock()
		return
	}
	delete(h.clients, c.id)
	h.mu.Unlock()

	c.CloseWithReason("disconnected")
	h.matcher.Cancel(c)
	h.removeMember(c.SessionID(), c.id, "peer_disconnected", storage.SessionEnded, false)

	h.metrics.ConnectionsClosed.Inc()
	h.metrics.ConnectionsActive.Dec()
	h.log.Info("client disconnected", "user_id", c.id, "ip", c.ip, "duration_s", int(time.Since(c.connectedAt).Seconds()))
}

// ServeWS upgrades an HTTP request into a signaling connection.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)

	if until, suspended := h.moderation.IPSuspendedUntil(ip); suspended {
		http.Error(w, "temporarily suspended until "+until.Format(time.RFC3339), http.StatusForbidden)
		return
	}

	userID, ok := normalizeClientID(r.URL.Query().Get("cid"))
	if !ok {
		userID = storage.NewID()
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Debug("websocket upgrade failed", "err", err, "ip", ip)
		return
	}

	c := newClient(h, conn, userID, ip, r.UserAgent())
	if replaced := h.register(c); replaced != nil {
		replaced.SendError("replaced", "connexion reprise depuis un autre onglet", "")
		replaced.CloseWithReason("replaced")
		h.metrics.ConnectionsActive.Dec()
		h.metrics.ConnectionsClosed.Inc()
	}

	h.trackUser(userID, ip)

	c.Send(EvtHello, HelloPayload{
		UserID:          userID,
		ProtocolVersion: config.ProtocolVersion,
		IceServers:      iceServerConfigs(h.cfg.IceServers),
		ServerTime:      nowRFC3339(),
		QueueLength:     h.matcher.Len(),
		Limits: LimitsPayload{
			MaxChatRunes:   h.cfg.MaxChatRunes,
			MaxMessageByte: int(h.cfg.MaxMessageBytes),
		},
	})

	go c.writePump()
	go c.readPump()

	h.log.Info("client connected", "user_id", userID, "ip", ip, "connections", h.Count())
}

func (h *Hub) trackUser(userID, ip string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		now := time.Now().UTC()
		if err := h.store.UpsertUser(ctx, storage.User{
			ID:         userID,
			Status:     storage.UserActive,
			CreatedAt:  now,
			LastSeenAt: now,
			LastIP:     ip,
		}); err != nil {
			h.log.Warn("could not persist user", "err", err)
		}
	}()
}

// handle dispatches one validated client event.
func (h *Hub) handle(c *Client, env Envelope) {
	h.metrics.MessagesIn.Inc()
	switch env.Event {
	case EvtQueueJoin:
		h.handleQueueJoin(c, env)
	case EvtQueueCancel:
		h.handleQueueCancel(c, env)
	case EvtSessionNext:
		h.handleNext(c, env)
	case EvtSessionLeave:
		h.handleLeave(c, env)
	case EvtSessionAdd:
		h.handleAdd(c, env)
	case EvtInviteCreate:
		h.handleInviteCreate(c, env)
	case EvtInviteRevoke:
		h.handleInviteRevoke(c, env)
	case EvtSessionJoin:
		h.handleSessionJoin(c, env)
	case EvtSessionUpdate:
		h.handleSessionUpdate(c, env)
	case EvtRTCOffer, EvtRTCAnswer:
		h.handleSDP(c, env)
	case EvtRTCICE:
		h.handleICE(c, env)
	case EvtChatSend:
		h.handleChat(c, env)
	case EvtReportCreate:
		h.handleReport(c, env)
	case EvtBlockCreate:
		h.handleBlock(c, env)
	default:
		h.metrics.MessagesRejected.Inc()
		c.SendError("unknown_event", "événement inconnu", env.Event)
	}
}

func (h *Hub) handleQueueJoin(c *Client, env Envelope) {
	if !h.joinLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "trop de recherches en peu de temps", env.Event)
		return
	}
	if until, suspended := h.moderation.IPSuspendedUntil(c.ip); suspended {
		c.SendError("suspended", "accès temporairement suspendu jusqu'à "+until.Format(time.RFC3339), env.Event)
		return
	}
	payload, err := decodeData[QueueJoinPayload](env)
	if err != nil {
		h.metrics.MessagesRejected.Inc()
		c.SendError("malformed", "recherche invalide", env.Event)
		return
	}
	mode, max, ok := normalizeSearchPrefs(payload)
	if !ok {
		c.SendError("invalid_limit", "le nombre maximal doit valoir 0 (sans contrainte) ou au moins 2", env.Event)
		return
	}
	c.SetSearchPrefs(mode, max)

	if c.State() == matching.StateInCall {
		h.removeMember(c.SessionID(), c.id, "requeue", storage.SessionEnded, false)
	}
	h.matcher.Join(c)
	if c.State() == matching.StateWaiting {
		c.Send(EvtQueueWaiting, QueuePayload{Position: h.matcher.Len(), Since: nowRFC3339()})
	}
}

// normalizeSearchPrefs validates the requested call shape. Solo ignores the
// cap; a group cap of 1 could never form a call, so it is rejected.
func normalizeSearchPrefs(payload QueueJoinPayload) (string, int, bool) {
	mode := strings.ToLower(strings.TrimSpace(payload.Mode))
	if mode == "" {
		mode = storage.SessionSolo
	}
	if mode != storage.SessionSolo && mode != storage.SessionGroup {
		mode = storage.SessionSolo
	}
	max := payload.MaxParticipants
	if max < 0 {
		max = 0
	}
	if mode == storage.SessionSolo {
		return mode, 0, true
	}
	if max == 1 {
		return "", 0, false
	}
	return mode, max, true
}

func (h *Hub) handleQueueCancel(c *Client, env Envelope) {
	if h.matcher.Cancel(c) {
		c.Send(EvtQueueCanceled, nil)
		c.Send(EvtStateChanged, StatePayload{State: string(matching.StateIdle)})
		return
	}
	c.SendError("not_waiting", "aucune recherche en cours", env.Event)
}

func (h *Hub) handleNext(c *Client, env Envelope) {
	if !h.joinLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "trop de passages en peu de temps", env.Event)
		return
	}
	// Suivant carries the current search shape so a setting changed mid-call
	// is already in effect. An empty payload keeps the previous prefs.
	if len(env.Data) > 0 {
		if payload, err := decodeData[QueueJoinPayload](env); err == nil {
			if mode, max, ok := normalizeSearchPrefs(payload); ok {
				c.SetSearchPrefs(mode, max)
			}
		}
	}
	if c.State() == matching.StateInCall {
		h.removeMember(c.SessionID(), c.id, "next", storage.SessionEnded, false)
	}
	h.matcher.Join(c)
	if c.State() == matching.StateWaiting {
		c.Send(EvtQueueWaiting, QueuePayload{Position: h.matcher.Len(), Since: nowRFC3339()})
	}
}

func (h *Hub) handleLeave(c *Client, _ Envelope) {
	h.matcher.Cancel(c)
	if c.State() == matching.StateInCall {
		h.removeMember(c.SessionID(), c.id, "left", storage.SessionEnded, false)
	}
	c.Send(EvtStateChanged, StatePayload{State: string(matching.StateIdle)})
}

// handleAdd pulls one waiting peer into the caller's session. This is the
// Ajouter button: rooms grow on demand instead of forking new calls. A solo
// call is promoted to a conference on the way in; the room learns about the
// new shape before the newcomer is announced.
func (h *Hub) handleAdd(c *Client, env Envelope) {
	if !h.joinLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "trop d'ajouts en peu de temps", env.Event)
		return
	}
	sessionID := c.SessionID()
	s, ok := h.sessions.Get(sessionID)
	if !ok || !s.Has(c.id) {
		c.SendError("no_session", "aucune conférence active", env.Event)
		return
	}
	// A solo room reads as full (two members for a cap of two) but Fill
	// promotes it first, which lifts the 1:1 cap — so only grouped rooms are
	// rejected here.
	if s.Mode != storage.SessionSolo && s.Full() {
		c.SendError("session_full", "la conférence est complète", env.Event)
		return
	}
	filled, promoted := h.matcher.Fill(sessionID)
	if promoted {
		h.metrics.SessionPromotions.Inc()
		h.log.Info("session promoted", "session_id", sessionID, "via", "session.add")
	}
	if !filled {
		c.SendError("empty_queue", "personne en attente pour le moment", env.Event)
	}
}

// handleInviteCreate mints (or returns) the single active invitation link of
// the caller's session. A supplied cap is applied to the session first. When
// the caller has no session yet — idle, or still waiting in the random queue
// — it opens an empty hosted room instead: the link then fills a discussion
// that does not exist yet, and the host's own search preferences are left
// untouched for later searches.
func (h *Hub) handleInviteCreate(c *Client, env Envelope) {
	if !h.inviteLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "trop d'invitations en peu de temps", env.Event)
		return
	}
	payload, err := decodeData[InviteCreatePayload](env)
	if err != nil {
		h.metrics.MessagesRejected.Inc()
		c.SendError("malformed", "invitation invalide", env.Event)
		return
	}
	if payload.MaxParticipants != nil {
		max := *payload.MaxParticipants
		if max < 0 || max == 1 {
			c.SendError("invalid_limit", "le nombre maximal doit valoir 0 (sans contrainte) ou au moins 2", env.Event)
			return
		}
	}
	sessionID := c.SessionID()
	s, ok := h.sessions.Get(sessionID)
	if !ok {
		// No live session: open an empty room for the link to fill. Waiting
		// callers leave the queue first; a stale in-call state is refused.
		// A valid mode in the payload carries the host's landing choice even
		// before their first search; anything else keeps the server prefs.
		if mode := strings.ToLower(strings.TrimSpace(payload.Mode)); mode == storage.SessionSolo || mode == storage.SessionGroup {
			max := c.MaxParticipants()
			if mode == storage.SessionSolo {
				max = 0
			}
			c.SetSearchPrefs(mode, max)
		}
		if c.State() == matching.StateWaiting {
			h.matcher.Cancel(c)
		}
		s, ok = h.matcher.OpenRoom(c)
		if !ok {
			c.SendError("no_session", "impossible d'ouvrir une salle depuis cet état", env.Event)
			return
		}
	} else if !s.Has(c.id) {
		c.SendError("no_session", "aucune session active", env.Event)
		return
	}
	if payload.MaxParticipants != nil {
		if h.sessions.SetMax(s.ID, *payload.MaxParticipants) {
			if fresh, ok := h.sessions.Get(s.ID); ok {
				s = fresh
			}
			h.broadcastSessionUpdated(s)
		}
	}
	token := h.invites.ActiveOrCreate(s.ID, payload.Rotate)
	h.metrics.InvitesCreated.Inc()
	h.log.Info("invite created", "session_id", s.ID, "mode", s.Mode, "rotated", payload.Rotate)
	c.Send(EvtInviteCreated, InviteCreatedPayload{
		Token:           token,
		Mode:            s.Mode,
		MaxParticipants: s.MaxParticipants,
	})
}

// handleInviteRevoke kills the session's link. Members stay; only future
// arrivals through the link are blocked.
func (h *Hub) handleInviteRevoke(c *Client, env Envelope) {
	if !h.inviteLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "trop d'invitations en peu de temps", env.Event)
		return
	}
	sessionID := c.SessionID()
	s, ok := h.sessions.Get(sessionID)
	if !ok || !s.Has(c.id) {
		c.SendError("no_session", "aucune session active", env.Event)
		return
	}
	h.invites.Revoke(s.ID)
	h.log.Info("invite revoked", "session_id", s.ID)
	for _, id := range s.Members() {
		if m := h.client(id); m != nil {
			m.Send(EvtInviteRevoked, InviteRevokedPayload{SessionID: s.ID})
		}
	}
}

// handleSessionJoin enters a precise session through its invitation link:
// direct access, no knock, no approval. The joiner's own search preferences
// are never touched; Next and Leave keep using them.
func (h *Hub) handleSessionJoin(c *Client, env Envelope) {
	if c.State() != matching.StateIdle {
		c.SendError("already_in_session", "vous êtes déjà en session", env.Event)
		return
	}
	if !h.joinLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "trop de recherches en peu de temps", env.Event)
		return
	}
	if until, suspended := h.moderation.IPSuspendedUntil(c.ip); suspended {
		c.SendError("suspended", "accès temporairement suspendu jusqu'à "+until.Format(time.RFC3339), env.Event)
		return
	}
	if h.userSuspended(c.id) {
		c.SendError("suspended", "accès temporairement suspendu", env.Event)
		return
	}
	payload, err := decodeData[SessionJoinPayload](env)
	if err != nil {
		h.metrics.MessagesRejected.Inc()
		c.SendError("malformed", "invitation invalide", env.Event)
		return
	}
	if strings.TrimSpace(payload.Token) == "" {
		c.SendError("invalid_token", "lien d'invitation invalide", env.Event)
		return
	}
	sessionID, ok := h.invites.Resolve(payload.Token)
	if !ok {
		h.metrics.InviteRejected.Inc()
		c.SendError("invalid_token", "lien d'invitation invalide ou révoqué", env.Event)
		return
	}
	if _, alive := h.sessions.Get(sessionID); !alive {
		h.invites.Revoke(sessionID)
		h.metrics.InviteRejected.Inc()
		c.SendError("invalid_token", "cette discussion est terminée", env.Event)
		return
	}
	res := h.matcher.JoinSession(c, sessionID)
	switch res.Err {
	case "":
		if res.Promoted {
			h.metrics.SessionPromotions.Inc()
			h.log.Info("session promoted", "session_id", sessionID, "via", "session.join")
		}
		h.metrics.InviteJoins.Inc()
		h.log.Info("invite join", "session_id", sessionID, "peer", c.id, "members", len(res.Members)+1)
	case matching.JoinAlreadyIn:
		c.SendError("already_in_session", "vous êtes déjà en session", env.Event)
	case matching.JoinSessionFull:
		h.metrics.InviteRejected.Inc()
		c.SendError("session_full", "cette discussion est complète", env.Event)
	case matching.JoinBlocked:
		h.metrics.InviteRejected.Inc()
		c.SendError("blocked_from_session", "vous ne pouvez pas rejoindre cette discussion", env.Event)
	default:
		h.metrics.InviteRejected.Inc()
		c.SendError("invalid_token", "lien d'invitation invalide ou révoqué", env.Event)
	}
}

// handleSessionUpdate changes the member cap of the caller's live session.
// Any member may change it: anonymous rooms have no owner. Lowering the cap
// below the headcount ejects nobody; only newcomers are blocked.
func (h *Hub) handleSessionUpdate(c *Client, env Envelope) {
	if !h.joinLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "trop de modifications en peu de temps", env.Event)
		return
	}
	sessionID := c.SessionID()
	s, ok := h.sessions.Get(sessionID)
	if !ok || !s.Has(c.id) {
		c.SendError("no_session", "aucune session active", env.Event)
		return
	}
	payload, err := decodeData[SessionUpdatePayload](env)
	if err != nil {
		h.metrics.MessagesRejected.Inc()
		c.SendError("malformed", "paramètre invalide", env.Event)
		return
	}
	if payload.MaxParticipants < 0 || payload.MaxParticipants == 1 {
		c.SendError("invalid_limit", "le nombre maximal doit valoir 0 (sans contrainte) ou au moins 2", env.Event)
		return
	}
	if !h.sessions.SetMax(s.ID, payload.MaxParticipants) {
		c.SendError("no_session", "aucune session active", env.Event)
		return
	}
	if fresh, ok := h.sessions.Get(s.ID); ok {
		s = fresh
	}
	h.log.Info("session cap updated", "session_id", s.ID, "max", s.MaxParticipants)
	h.broadcastSessionUpdated(s)
}

// broadcastSessionUpdated tells every member that their room changed shape.
func (h *Hub) broadcastSessionUpdated(s *storage.Session) {
	payload := SessionUpdatedPayload{
		SessionID:       s.ID,
		Mode:            s.Mode,
		MaxParticipants: s.MaxParticipants,
	}
	for _, id := range s.Members() {
		if m := h.client(id); m != nil {
			m.Send(EvtSessionUpdated, payload)
		}
	}
}

// userSuspended reports whether the visitor's own account is suspended.
// Unknown visitors are not suspended; only the store knows otherwise.
func (h *Hub) userSuspended(id string) bool {
	if h.store == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	u, err := h.store.GetUser(ctx, id)
	if err != nil {
		return false
	}
	return u.IsSuspended(time.Now())
}

func (h *Hub) handleSDP(c *Client, env Envelope) {
	if !h.signalLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "signalisation trop fréquente", env.Event)
		return
	}
	if !c.State().CanAcceptSignaling() {
		c.SendError("no_session", "aucune session active", env.Event)
		return
	}
	payload, err := decodeData[SDPPayload](env)
	if err != nil {
		h.metrics.MessagesRejected.Inc()
		c.SendError("malformed", "description SDP invalide", env.Event)
		return
	}
	expected := "offer"
	if env.Event == EvtRTCAnswer {
		expected = "answer"
	}
	if payload.Type != expected || payload.SDP == "" || len(payload.SDP) > maxSDPBytes {
		h.metrics.MessagesRejected.Inc()
		c.SendError("invalid_sdp", "description SDP rejetée", env.Event)
		return
	}
	target, ok := h.resolveTarget(c, payload.To, env.Event)
	if !ok {
		return
	}
	payload.From = c.id
	h.forwardTo(c, target, env.Event, payload)
}

func (h *Hub) handleICE(c *Client, env Envelope) {
	if !h.signalLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "signalisation trop fréquente", env.Event)
		return
	}
	if !c.State().CanAcceptSignaling() {
		c.SendError("no_session", "aucune session active", env.Event)
		return
	}
	payload, err := decodeData[ICEPayload](env)
	if err != nil || payload.Candidate == "" || len(payload.Candidate) > maxICEBytes {
		h.metrics.MessagesRejected.Inc()
		c.SendError("invalid_candidate", "candidat ICE rejeté", env.Event)
		return
	}
	target, ok := h.resolveTarget(c, payload.To, env.Event)
	if !ok {
		return
	}
	payload.From = c.id
	h.forwardTo(c, target, env.Event, payload)
}

// resolveTarget maps a `to` recipient to a session member. Solo calls keep the
// historic behaviour (the single other member, `to` optional); conferences
// require an explicit member that is neither the sender nor a stranger.
func (h *Hub) resolveTarget(c *Client, to, event string) (string, bool) {
	s, ok := h.sessions.Get(c.SessionID())
	if !ok || !s.Has(c.id) {
		c.SendError("no_session", "aucune session active", event)
		return "", false
	}
	members := s.Members()
	others := make([]string, 0, len(members))
	for _, id := range members {
		if id != c.id {
			others = append(others, id)
		}
	}
	if len(others) == 0 {
		c.SendError("no_peer", "aucun interlocuteur", event)
		return "", false
	}
	if len(others) == 1 && to == "" {
		return others[0], true
	}
	if to == "" || to == c.id {
		c.SendError("no_peer", "destinataire inconnu", event)
		return "", false
	}
	for _, id := range others {
		if id == to {
			return to, true
		}
	}
	c.SendError("no_peer", "destinataire inconnu", event)
	return "", false
}

func (h *Hub) handleChat(c *Client, env Envelope) {
	if !h.chatLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "vous envoyez des messages trop vite", env.Event)
		return
	}
	s, ok := h.sessions.Get(c.SessionID())
	if !ok || !s.Has(c.id) {
		c.SendError("no_peer", "aucun interlocuteur", env.Event)
		return
	}
	others := without(s.Members(), c.id)
	if len(others) == 0 {
		c.SendError("no_peer", "aucun interlocuteur", env.Event)
		return
	}
	payload, err := decodeData[ChatSendPayload](env)
	if err != nil {
		h.metrics.MessagesRejected.Inc()
		c.SendError("malformed", "message invalide", env.Event)
		return
	}
	text := strings.TrimSpace(payload.Text)
	if text == "" {
		c.SendError("empty_message", "message vide", env.Event)
		return
	}
	if runes := []rune(text); len(runes) > h.cfg.MaxChatRunes {
		text = string(runes[:h.cfg.MaxChatRunes])
	}

	msg := ChatMessagePayload{ID: storage.NewID(), Text: text, From: "peer", At: nowRFC3339()}
	for _, id := range others {
		h.forwardTo(c, id, EvtChatMessage, msg)
	}
	c.Send(EvtChatAck, ChatAckPayload{ID: msg.ID, At: msg.At})
}

// without returns the members other than self.
func without(members []string, self string) []string {
	others := make([]string, 0, len(members))
	for _, id := range members {
		if id != self {
			others = append(others, id)
		}
	}
	return others
}

func (h *Hub) handleReport(c *Client, env Envelope) {
	if !h.reportLimiter.Allow(c.id) {
		h.penalize(c, env.Event)
		c.SendError("rate_limited", "trop de signalements", env.Event)
		return
	}
	payload, err := decodeData[ReportPayload](env)
	if err != nil {
		h.metrics.MessagesRejected.Inc()
		c.SendError("malformed", "signalement invalide", env.Event)
		return
	}
	target, ok := h.resolveMemberTarget(c, payload.Target, env.Event)
	if !ok {
		return
	}
	category := strings.ToLower(strings.TrimSpace(payload.Category))
	if !moderation.ValidCategory(category) {
		category = "other"
	}
	reason := truncateRunes(strings.TrimSpace(payload.Reason), maxReportReason)
	details := truncateRunes(strings.TrimSpace(payload.Details), maxReportDetails)

	reportID, suspended, err := h.moderation.Report(c.id, target, c.SessionID(), category, reason, details)
	if err != nil {
		h.log.Error("report persistence failed", "err", err, "target", target)
	}
	h.metrics.Reports.Inc()
	if suspended {
		h.metrics.Suspensions.Inc()
	}

	blocked := false
	if payload.Block {
		if err := h.moderation.Block(c.id, target); err == nil {
			blocked = true
			h.metrics.Blocks.Inc()
		}
	}

	// Only the reported member is ejected: the reporter's conference goes on
	// with one tile fewer instead of collapsing for everyone.
	h.removeMember(c.SessionID(), target, "reported", storage.SessionReported, true)
	c.Send(EvtReportAck, ReportAckPayload{ReportID: reportID, Blocked: blocked, Suspended: suspended})
}

func (h *Hub) handleBlock(c *Client, env Envelope) {
	payload, err := decodeData[BlockPayload](env)
	if err != nil {
		h.metrics.MessagesRejected.Inc()
		c.SendError("malformed", "blocage invalide", env.Event)
		return
	}
	target, ok := h.resolveMemberTarget(c, payload.Target, env.Event)
	if !ok {
		return
	}
	if err := h.moderation.Block(c.id, target); err != nil {
		c.SendError("block_failed", "le blocage n'a pas pu être enregistré", env.Event)
		return
	}
	h.metrics.Blocks.Inc()
	h.removeMember(c.SessionID(), target, "blocked", storage.SessionEnded, true)
	c.Send(EvtBlockAck, BlockAckPayload{BlockedID: target})
}

// resolveMemberTarget maps a report/block target to a session member. Solo
// calls keep the historic default (the other participant); conferences
// require the client to name the tile it acted on.
func (h *Hub) resolveMemberTarget(c *Client, target, event string) (string, bool) {
	s, ok := h.sessions.Get(c.SessionID())
	if !ok || !s.Has(c.id) {
		c.SendError("no_peer", "aucun interlocuteur", event)
		return "", false
	}
	others := without(s.Members(), c.id)
	if len(others) == 0 {
		c.SendError("no_peer", "aucun interlocuteur", event)
		return "", false
	}
	if target == "" {
		if len(others) == 1 {
			return others[0], true
		}
		c.SendError("no_target", "choisissez l'interlocuteur concerné", event)
		return "", false
	}
	if target == c.id {
		c.SendError("invalid_target", "cible invalide", event)
		return "", false
	}
	for _, id := range others {
		if id == target {
			return target, true
		}
	}
	c.SendError("no_peer", "interlocuteur inconnu", event)
	return "", false
}

// forwardTo relays a validated payload to one session member. A client can
// never address a session it does not participate in.
func (h *Hub) forwardTo(c *Client, targetID, event string, payload any) {
	target := h.client(targetID)
	if target == nil || target.SessionID() != c.SessionID() || !target.State().CanAcceptSignaling() {
		c.SendError("no_peer", "interlocuteur déconnecté", event)
		return
	}
	start := time.Now()
	if !target.Send(event, payload) {
		h.metrics.MessagesRejected.Inc()
	}
	h.metrics.SignalLatency.Observe(time.Since(start))
}

// removeMember detaches one participant from a session and settles the room:
// an emptied session closes, a solo session reduced to one member ends for
// the last peer too, and conference survivors learn who left via peer.left.
// notifyRemoved also tells the ejected peer their call is over, which matters
// for moderation ejects; self-initiated leaves need no such notice.
func (h *Hub) removeMember(sessionID, peerID, reason string, status storage.SessionStatus, notifyRemoved bool) {
	if sessionID == "" {
		return
	}
	removed := h.client(peerID)
	if removed != nil {
		removed.Unbind()
	}
	h.matcher.Forget(peerID)

	session, empty := h.sessions.RemoveMember(sessionID, peerID)
	if session == nil {
		return
	}
	endPayload := func() SessionEndedPayload {
		return SessionEndedPayload{
			SessionID:  sessionID,
			Reason:     reason,
			DurationS:  int64(session.Duration(time.Now()).Seconds()),
			CanRequeue: reason != "reported",
		}
	}
	if empty {
		if s, ok := h.sessions.End(sessionID, status, reason); ok {
			h.metrics.SessionDuration.Observe(s.Duration(time.Now()))
		}
		// The link dies with the session: a token must never outlive its room.
		h.invites.Revoke(sessionID)
		if notifyRemoved && removed != nil {
			removed.Send(EvtSessionEnded, endPayload())
		}
		return
	}
	members := session.Members()
	if len(members) == 1 && session.Mode == storage.SessionSolo {
		if s, ok := h.sessions.End(sessionID, status, reason); ok {
			h.metrics.SessionDuration.Observe(s.Duration(time.Now()))
		}
		h.invites.Revoke(sessionID)
		if notifyRemoved && removed != nil {
			removed.Send(EvtSessionEnded, endPayload())
		}
		if last := h.client(members[0]); last != nil {
			last.Unbind()
			h.matcher.Forget(last.ID())
			last.Send(EvtSessionEnded, endPayload())
		}
		return
	}
	if notifyRemoved && removed != nil {
		removed.Send(EvtSessionEnded, endPayload())
	}
	left := PeerLeftPayload{
		SessionID: sessionID,
		PeerID:    peerID,
		Reason:    reason,
		Members:   len(members),
	}
	for _, id := range members {
		if m := h.client(id); m != nil {
			m.Send(EvtPeerLeft, left)
		}
	}
}

func (h *Hub) penalize(c *Client, event string) {
	h.metrics.Violations.Inc()
	if h.moderation.RegisterViolation(c.ip) {
		c.SendError("suspended", "activité anormale détectée, accès suspendu temporairement", event)
		h.metrics.Suspensions.Inc()
		c.CloseWithReason("abuse")
	}
}

// normalizeClientID validates the browser-persisted identifier. Accepting a
// malformed value would let a client inject arbitrary keys into the store.
func normalizeClientID(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < minClientIDLength || len(raw) > maxClientIDLength {
		return "", false
	}
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return "", false
		}
	}
	return raw, true
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

func iceServerConfigs(in []config.IceServer) []IceServerConfig {
	out := make([]IceServerConfig, 0, len(in))
	for _, s := range in {
		out = append(out, IceServerConfig{URLs: s.URLs, Username: s.Username, Credential: s.Credential})
	}
	return out
}

// matcherMetrics adapts the metrics registry to matching.Metrics.
type matcherMetrics struct{ m *metrics.Metrics }

func (a matcherMetrics) QueueJoin()                { a.m.QueueJoins.Inc() }
func (a matcherMetrics) QueueCancel()              { a.m.QueueCancels.Inc() }
func (a matcherMetrics) QueueTimeout()             { a.m.QueueTimeouts.Inc() }
func (a matcherMetrics) MatchCreated()             { a.m.Matches.Inc() }
func (a matcherMetrics) MatchWait(d time.Duration) { a.m.MatchWait.Observe(d) }

// NewMatcherMetrics exposes the adapter to main so the matcher can be built
// before the hub exists.
func NewMatcherMetrics(m *metrics.Metrics) matching.Metrics { return matcherMetrics{m: m} }
