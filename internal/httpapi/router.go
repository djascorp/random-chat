// Package httpapi exposes the REST surface, the WebSocket endpoint, the metrics
// endpoint and the moderation admin API.
package httpapi

import (
	"log/slog"
	"net/http"

	"random-chat/internal/config"
	"random-chat/internal/invites"
	"random-chat/internal/metrics"
	"random-chat/internal/moderation"
	"random-chat/internal/sessions"
	"random-chat/internal/signaling"
	"random-chat/internal/storage"
)

// Options carries every dependency the router needs.
type Options struct {
	Config     *config.Config
	Hub        *signaling.Hub
	Store      storage.Store
	Sessions   *sessions.Manager
	Moderation *moderation.Service
	Metrics    *metrics.Metrics
	Invites    *invites.Registry
	Logger     *slog.Logger
}

// NewRouter builds the fully wrapped HTTP handler.
func NewRouter(o Options) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", o.health)
	mux.HandleFunc("GET /api/config", o.publicConfig)
	mux.HandleFunc("GET /api/stats", o.stats)
	mux.HandleFunc("GET /api/invites/{token}", o.invitePreview)
	mux.HandleFunc("GET /metrics", o.prometheus)
	mux.HandleFunc("GET /ws", o.Hub.ServeWS)

	mux.Handle("GET /api/admin/stats", o.requireAdmin(http.HandlerFunc(o.adminStats)))
	mux.Handle("GET /api/admin/reports", o.requireAdmin(http.HandlerFunc(o.adminListReports)))
	mux.Handle("POST /api/admin/reports/{id}/status", o.requireAdmin(http.HandlerFunc(o.adminUpdateReport)))
	mux.Handle("POST /api/admin/users/{id}/suspend", o.requireAdmin(http.HandlerFunc(o.adminSuspendUser)))
	mux.Handle("GET /api/admin/actions", o.requireAdmin(http.HandlerFunc(o.adminListActions)))

	mux.Handle("/", o.spaHandler())

	var handler http.Handler = mux
	handler = o.cors(handler)
	handler = o.securityHeaders(handler)
	handler = o.logRequests(handler)
	handler = o.recoverPanics(handler)
	return handler
}
