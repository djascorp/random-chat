package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"random-chat/internal/config"
	"random-chat/internal/invites"
	"random-chat/internal/metrics"
	"random-chat/internal/sessions"
	"random-chat/internal/storage"
)

// TestInvitePreview exposes the room shape without leaking identifiers.
func TestInvitePreview(t *testing.T) {
	registry := invites.NewRegistry()
	manager := sessions.NewManager(storage.NewMemory(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	session := manager.Create([]string{"a", "b"}, storage.SessionGroup, 5)
	token := registry.ActiveOrCreate(session.ID, false)

	router := NewRouter(Options{
		Config:   &config.Config{WebDir: "web/dist"},
		Sessions: manager,
		Metrics:  metrics.New(),
		Invites:  registry,
		Logger:   slog.Default(),
	})

	preview := httptest.NewRecorder()
	router.ServeHTTP(preview, httptest.NewRequest("GET", "/api/invites/"+token, nil))
	if preview.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", preview.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(preview.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if body["mode"] != "group" || body["members"] != float64(2) || body["max_participants"] != float64(5) {
		t.Fatalf("preview = %v, want mode/members/cap only", body)
	}
	for key := range body {
		switch key {
		case "mode", "members", "max_participants":
		default:
			t.Fatalf("preview leaks %q", key)
		}
	}
	if cache := preview.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cache)
	}
}

// TestInvitePreviewDeadLinks distinguishes unknown tokens from ended rooms.
func TestInvitePreviewDeadLinks(t *testing.T) {
	registry := invites.NewRegistry()
	manager := sessions.NewManager(storage.NewMemory(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	session := manager.Create([]string{"a", "b"}, storage.SessionSolo, 2)
	token := registry.ActiveOrCreate(session.ID, false)

	router := NewRouter(Options{
		Config:   &config.Config{WebDir: "web/dist"},
		Sessions: manager,
		Metrics:  metrics.New(),
		Invites:  registry,
		Logger:   slog.Default(),
	})

	unknown := httptest.NewRecorder()
	router.ServeHTTP(unknown, httptest.NewRequest("GET", "/api/invites/does-not-exist", nil))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown token status = %d, want 404", unknown.Code)
	}

	manager.End(session.ID, storage.SessionEnded, "left")
	ended := httptest.NewRecorder()
	router.ServeHTTP(ended, httptest.NewRequest("GET", "/api/invites/"+token, nil))
	if ended.Code != http.StatusNotFound {
		t.Fatalf("ended session status = %d, want 404", ended.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(ended.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body["error"] != "session_ended" {
		t.Fatalf("error = %q, want session_ended", body["error"])
	}
}
