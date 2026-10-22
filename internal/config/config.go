package config

import (
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ProtocolVersion identifies the WebSocket event protocol implemented by this build.
// Clients negotiate against it during the handshake so a stale SPA can be detected.
const ProtocolVersion = "1"

// IceServer mirrors the subset of the RTCIceServer dictionary the browser accepts.
type IceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// Config holds every tunable of the service, sourced from the environment.
type Config struct {
	Addr            string
	LogLevel        slog.Level
	AllowedOrigins  []string
	DatabaseURL     string
	MigrationsDir   string
	AutoMigrate     bool
	AdminToken      string
	WebDir          string
	IceServers      []IceServer
	MaxMessageBytes int64

	SendBuffer     int
	QueueTimeout   time.Duration
	WriteTimeout   time.Duration
	PongTimeout    time.Duration
	PingInterval   time.Duration
	ShutdownGrace  time.Duration
	MediaIceMaxAge time.Duration

	JoinRatePerMin   int
	ChatRatePerMin   int
	ReportRatePerMin int
	SignalRatePerMin int
	MaxChatRunes     int

	AutoSuspendAfter int
	SuspendDuration  time.Duration
	ViolationLimit   int
}

// Load reads the configuration from the process environment, falling back to
// development-friendly defaults so `go run ./cmd/api` works with zero setup.
func Load() *Config {
	cfg := &Config{
		Addr:     envString("ADDR", ":8080"),
		LogLevel: parseLevel(envString("LOG_LEVEL", "info")),
		AllowedOrigins: envList("ALLOWED_ORIGINS", []string{
			"http://localhost:5173", "http://127.0.0.1:5173",
			"http://localhost:8080", "http://127.0.0.1:8080",
		}),
		DatabaseURL:      envString("DATABASE_URL", ""),
		MigrationsDir:    envString("MIGRATIONS_DIR", "migrations"),
		AutoMigrate:      envBool("AUTO_MIGRATE", true),
		AdminToken:       envString("ADMIN_TOKEN", ""),
		WebDir:           envString("WEB_DIR", "web/dist"),
		MaxMessageBytes:  int64(envInt("MAX_MESSAGE_BYTES", 64*1024)),
		SendBuffer:       envInt("SEND_BUFFER", 256),
		QueueTimeout:     envDuration("QUEUE_TIMEOUT", 90*time.Second),
		WriteTimeout:     envDuration("WRITE_TIMEOUT", 10*time.Second),
		PongTimeout:      envDuration("PONG_TIMEOUT", 60*time.Second),
		PingInterval:     envDuration("PING_INTERVAL", 25*time.Second),
		ShutdownGrace:    envDuration("SHUTDOWN_GRACE", 15*time.Second),
		MediaIceMaxAge:   envDuration("ICE_CANDIDATE_MAX_AGE", 60*time.Second),
		JoinRatePerMin:   envInt("JOIN_RATE_PER_MIN", 40),
		ChatRatePerMin:   envInt("CHAT_RATE_PER_MIN", 120),
		ReportRatePerMin: envInt("REPORT_RATE_PER_MIN", 10),
		SignalRatePerMin: envInt("SIGNAL_RATE_PER_MIN", 600),
		MaxChatRunes:     envInt("MAX_CHAT_RUNES", 1000),
		AutoSuspendAfter: envInt("AUTO_SUSPEND_AFTER", 3),
		SuspendDuration:  envDuration("SUSPEND_DURATION", 30*time.Minute),
		ViolationLimit:   envInt("VIOLATION_LIMIT", 5),
	}
	cfg.IceServers = parseIceServers()
	return cfg
}

// AllowsOrigin reports whether a browser origin may open a WebSocket.
//
// Same-origin requests are always accepted. The server also serves the SPA, so
// in a single-binary deployment the page origin *is* the server host: requiring
// an explicit allowlist entry there would reject every connection. That check is
// safe because a browser sets Origin itself and cannot forge a cross-origin one.
//
// An empty origin means a non-browser client (native app, smoke test, curl);
// browsers always send the header, so there is nothing to protect against.
func (c *Config) AllowsOrigin(origin, requestHost string) bool {
	if origin == "" {
		return true
	}
	if host := originHost(origin); host != "" && requestHost != "" && strings.EqualFold(host, requestHost) {
		return true
	}
	for _, allowed := range c.AllowedOrigins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	return false
}

// originHost extracts the host:port from an Origin header value.
func originHost(origin string) string {
	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func parseIceServers() []IceServer {
	servers := []IceServer{}
	if raw := envString("ICE_SERVERS", ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &servers); err != nil {
			slog.Warn("ICE_SERVERS is not valid JSON, ignoring", "err", err)
			servers = nil
		}
	}
	if turnURLs := envList("TURN_URLS", nil); len(turnURLs) > 0 {
		servers = append(servers, IceServer{
			URLs:       turnURLs,
			Username:   envString("TURN_USERNAME", ""),
			Credential: envString("TURN_CREDENTIAL", ""),
		})
	}
	if len(servers) == 0 {
		servers = []IceServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}
	}
	return servers
}

func envString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return fallback
}

func envList(key string, fallback []string) []string {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
