package config

import "testing"

// TestAllowsOriginSameOrigin locks the fix for the single-binary deployment
// bug: when the Go server serves the SPA itself, the page origin is the server
// host, which is not necessarily in ALLOWED_ORIGINS.
func TestAllowsOriginSameOrigin(t *testing.T) {
	// Mirrors a realistic deployment: the SPA served by the server itself, plus
	// the Vite dev server allowed for local development.
	cfg := &Config{AllowedOrigins: []string{"https://app.example.com", "http://localhost:5173"}}

	cases := []struct {
		name    string
		origin  string
		host    string
		allowed bool
	}{
		{"same origin, loopback", "http://localhost:8080", "localhost:8080", true},
		{"same origin, custom port", "http://localhost:3000", "localhost:3000", true},
		{"same origin, public host", "https://chat.example.com", "chat.example.com", true},
		{"same origin, host behind proxy", "https://chat.example.com", "chat.example.com:443", false},
		{"allowlisted dev server", "http://localhost:5173", "localhost:8080", true},
		{"exact allowlist entry", "https://app.example.com", "api.example.com", true},
		{"foreign origin", "https://evil.example", "localhost:8080", false},
		{"foreign origin on an allowed port", "http://localhost:5173.evil.example", "localhost:8080", false},
		{"sibling subdomain", "https://evil.example.com", "chat.example.com", false},
		{"non-browser client", "", "localhost:8080", true},
		{"malformed origin", "not a url", "localhost:8080", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cfg.AllowsOrigin(tc.origin, tc.host); got != tc.allowed {
				t.Fatalf("AllowsOrigin(%q, %q) = %v, want %v", tc.origin, tc.host, got, tc.allowed)
			}
		})
	}
}

func TestAllowsOriginWildcard(t *testing.T) {
	cfg := &Config{AllowedOrigins: []string{"*"}}
	if !cfg.AllowsOrigin("https://anything.example", "localhost:8080") {
		t.Fatal("a wildcard allowlist must accept every origin")
	}
}

func TestAllowsOriginEmptyAllowlistStillAcceptsSameOrigin(t *testing.T) {
	cfg := &Config{}
	if !cfg.AllowsOrigin("http://localhost:8080", "localhost:8080") {
		t.Fatal("same-origin must work even with no configured origins")
	}
	if cfg.AllowsOrigin("https://evil.example", "localhost:8080") {
		t.Fatal("a foreign origin must not be accepted with an empty allowlist")
	}
}
