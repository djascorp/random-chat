package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"random-chat/internal/config"
	"random-chat/internal/storage"
)

// health is the liveness probe used by the container orchestrator.
func (o Options) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"protocol_version": config.ProtocolVersion,
		"uptime_seconds":   int64(o.Metrics.Uptime().Seconds()),
		"connections":      o.Hub.Count(),
		"sessions":         o.Sessions.ActiveCount(),
		"queue":            o.Hub.QueueLength(),
	})
}

// publicConfig tells the browser which ICE servers and limits to use. It is the
// only place TURN credentials are handed out, so a rotating credential scheme
// can be added here later without touching the client.
func (o Options) publicConfig(w http.ResponseWriter, _ *http.Request) {
	servers := make([]map[string]any, 0, len(o.Config.IceServers))
	for _, s := range o.Config.IceServers {
		entry := map[string]any{"urls": s.URLs}
		if s.Username != "" {
			entry["username"] = s.Username
			entry["credential"] = s.Credential
		}
		servers = append(servers, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"protocol_version": config.ProtocolVersion,
		"ice_servers":      servers,
		"limits": map[string]any{
			"max_chat_runes":    o.Config.MaxChatRunes,
			"max_message_bytes": o.Config.MaxMessageBytes,
			"queue_timeout_ms":  o.Config.QueueTimeout.Milliseconds(),
		},
		"features": map[string]any{
			"text_chat":  true,
			"reporting":  true,
			"blocking":   true,
			"turn_relay": hasTurn(o.Config.IceServers),
		},
	})
}

// stats exposes anonymous, aggregate runtime metrics for the landing page.
func (o Options) stats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"online":            o.Hub.Count(),
		"active_sessions":   o.Sessions.ActiveCount(),
		"queue_length":      o.Hub.QueueLength(),
		"matches_total":     o.Metrics.Matches.Value(),
		"uptime_seconds":    int64(o.Metrics.Uptime().Seconds()),
		"match_wait_avg_ms": o.Metrics.MatchWait.Mean(),
		"match_wait_p95_ms": o.Metrics.MatchWait.Quantile(0.95),
	})
}

// prometheus renders the metrics registry for scraping.
func (o Options) prometheus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	o.Metrics.WritePrometheus(w)
}

// invitePreview answers the invitation screen before the camera is on: the
// discussion shape (type, headcount, cap) without any session or member id.
// Unknown tokens report not_found, revoked or ended links their own error so
// the UI can word the dead end precisely. Never cached: it mirrors live state.
func (o Options) invitePreview(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	sessionID, ok := o.Invites.Resolve(token)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	s, alive := o.Sessions.Get(sessionID)
	if !alive {
		o.Invites.Revoke(sessionID)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session_ended"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":             s.Mode,
		"members":          len(s.Members()),
		"max_participants": s.MaxParticipants,
	})
}

func (o Options) adminStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()

	storeStats, err := o.Store.Stats(ctx)
	if err != nil {
		o.Logger.Error("admin stats failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "storage_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"runtime": o.Metrics.Snapshot(),
		"storage": storeStats,
	})
}

func (o Options) adminListReports(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()

	status := storage.ReportStatus(r.URL.Query().Get("status"))
	if status != "" && !storage.ValidReportStatus(status) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_status"})
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}

	reports, err := o.Store.ListReports(ctx, status, limit)
	if err != nil {
		o.Logger.Error("admin list reports failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "storage_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports, "count": len(reports)})
}

func (o Options) adminUpdateReport(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}
	status := storage.ReportStatus(body.Status)
	if !storage.ValidReportStatus(status) || status == storage.ReportOpen {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_status"})
		return
	}

	id := r.PathValue("id")
	if err := o.Store.UpdateReportStatus(ctx, id, status); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "storage_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": status})
}

func (o Options) adminSuspendUser(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()

	var body struct {
		DurationMinutes int    `json:"duration_minutes"`
		Reason          string `json:"reason"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	if body.DurationMinutes <= 0 {
		body.DurationMinutes = int(o.Config.SuspendDuration.Minutes())
	}
	if body.Reason == "" {
		body.Reason = "manual moderation action"
	}

	target := r.PathValue("id")
	duration := time.Duration(body.DurationMinutes) * time.Minute
	if err := o.Store.SuspendUser(ctx, target, time.Now().UTC().Add(duration), body.Reason); err != nil {
		o.Logger.Error("admin suspend failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "storage_unavailable"})
		return
	}
	if err := o.Store.CreateModerationAction(ctx, &storage.ModerationAction{
		ID:        storage.NewID(),
		TargetID:  target,
		Action:    "suspend",
		Reason:    body.Reason,
		Actor:     "admin",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		o.Logger.Warn("audit entry failed", "err", err)
	}
	o.Metrics.Suspensions.Inc()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":              target,
		"suspended_until": time.Now().UTC().Add(duration),
	})
}

func (o Options) adminListActions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()

	actions, err := o.Store.ListModerationActions(ctx, 200)
	if err != nil {
		o.Logger.Error("admin list actions failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "storage_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions, "count": len(actions)})
}

func hasTurn(servers []config.IceServer) bool {
	for _, s := range servers {
		for _, u := range s.URLs {
			if strings.HasPrefix(u, "turn:") || strings.HasPrefix(u, "turns:") {
				return true
			}
		}
	}
	return false
}
