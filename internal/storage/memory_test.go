package storage

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// The in-memory store is used by every deployment without DATABASE_URL, so it
// must not grow without bound over a long run.
func TestMemoryStoreBoundsUsers(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	now := time.Now().UTC()

	// A sanction must outlive eviction: dropping it would send an abusive
	// visitor straight back into the matching queue.
	if err := store.SuspendUser(ctx, "banned-visitor", now.Add(time.Hour), "test"); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	for i := 0; i < memoryRetention+1500; i++ {
		user := User{
			ID:         fmt.Sprintf("user-%05d", i),
			Status:     UserActive,
			CreatedAt:  now,
			LastSeenAt: now.Add(time.Duration(i) * time.Millisecond),
		}
		if err := store.UpsertUser(ctx, user); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}

	store.mu.RLock()
	count := len(store.users)
	store.mu.RUnlock()

	if count > memoryRetention {
		t.Fatalf("users retained = %d, want at most %d", count, memoryRetention)
	}
	if _, err := store.GetUser(ctx, "banned-visitor"); err != nil {
		t.Fatalf("a suspended user was evicted: %v", err)
	}
}

func TestMemoryStoreBoundsSessions(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	now := time.Now().UTC()

	const total = memoryRetention + 1500
	for i := 0; i < total; i++ {
		session := &Session{
			ID:           fmt.Sprintf("session-%05d", i),
			Participants: []string{"peer-a", "peer-b"},
			Mode:         SessionSolo,
			StartedAt:    now.Add(time.Duration(i) * time.Millisecond),
			Status:       SessionActive,
		}
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if err := store.EndSession(ctx, session.ID, now, SessionEnded, "test"); err != nil {
			t.Fatalf("end %d: %v", i, err)
		}
	}

	store.mu.RLock()
	count := len(store.sessions)
	_, newestKept := store.sessions[fmt.Sprintf("session-%05d", total-1)]
	_, oldestKept := store.sessions["session-00000"]
	store.mu.RUnlock()

	if count > memoryRetention {
		t.Fatalf("sessions retained = %d, want at most %d", count, memoryRetention)
	}
	if !newestKept {
		t.Fatal("the newest session was evicted instead of the oldest")
	}
	if oldestKept {
		t.Fatal("the oldest finished session should have been evicted first")
	}
}

// A live session is still referenced by the matching engine, so trimming must
// never touch it — even when that means briefly exceeding the cap.
func TestMemoryStoreNeverEvictsLiveSessions(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	now := time.Now().UTC()

	const total = memoryRetention + 200
	for i := 0; i < total; i++ {
		session := &Session{
			ID:           fmt.Sprintf("live-%05d", i),
			Participants: []string{"peer-a", "peer-b"},
			Mode:         SessionSolo,
			StartedAt:    now,
			Status:       SessionActive,
		}
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	store.mu.RLock()
	count := len(store.sessions)
	store.mu.RUnlock()

	if count != total {
		t.Fatalf("live sessions retained = %d, want all %d", count, total)
	}
}
