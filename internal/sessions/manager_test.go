package sessions

import (
	"testing"

	"random-chat/internal/storage"
)

func TestPromoteSoloToGroupLiftsCap(t *testing.T) {
	m := NewManager(nil, nil)
	s := m.Create([]string{"a", "b"}, storage.SessionSolo, 2)

	if !m.Promote(s.ID) {
		t.Fatal("Promote must switch a solo session")
	}
	got, ok := m.Get(s.ID)
	if !ok || got.Mode != storage.SessionGroup {
		t.Fatalf("mode = %v, want group", got)
	}
	if got.MaxParticipants != 0 {
		t.Fatalf("cap after promote = %d, want 0 (uncapped)", got.MaxParticipants)
	}
	if m.Promote(s.ID) {
		t.Fatal("a second Promote must be a no-op")
	}
	if m.Promote("no-such-session") {
		t.Fatal("Promote of a missing session must report false")
	}
}

func TestSetMaxThenLowerWithoutEject(t *testing.T) {
	m := NewManager(nil, nil)
	s := m.Create([]string{"a", "b", "c"}, storage.SessionGroup, 0)

	if !m.SetMax(s.ID, 2) {
		t.Fatal("SetMax must update a live session")
	}
	got, _ := m.Get(s.ID)
	if got.MaxParticipants != 2 {
		t.Fatalf("cap = %d, want 2", got.MaxParticipants)
	}
	// Lowering the cap below the headcount ejects nobody.
	if len(got.Members()) != 3 {
		t.Fatalf("members = %d, want nobody ejected", len(got.Members()))
	}
	if !m.SetMax(s.ID, 0) {
		t.Fatal("SetMax must lift the cap with 0")
	}
	if m.SetMax("no-such-session", 4) {
		t.Fatal("SetMax of a missing session must report false")
	}
}
