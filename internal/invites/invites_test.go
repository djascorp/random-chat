package invites

import (
	"sync"
	"testing"
)

func TestActiveOrCreateIsStableWithoutRotate(t *testing.T) {
	r := NewRegistry()
	first := r.ActiveOrCreate("session-1", false)
	second := r.ActiveOrCreate("session-1", false)
	if first == "" || first != second {
		t.Fatalf("stable token = %q then %q, want the same non-empty token", first, second)
	}
	other := r.ActiveOrCreate("session-2", false)
	if other == first {
		t.Fatal("two sessions must not share a token")
	}
}

func TestRotateRevokesPreviousToken(t *testing.T) {
	r := NewRegistry()
	old := r.ActiveOrCreate("session-1", false)
	fresh := r.ActiveOrCreate("session-1", true)
	if fresh == "" || fresh == old {
		t.Fatalf("rotated token = %q, want a new token different from %q", fresh, old)
	}
	if _, ok := r.Resolve(old); ok {
		t.Fatal("the old token must not resolve after rotation")
	}
	if session, ok := r.Resolve(fresh); !ok || session != "session-1" {
		t.Fatalf("resolve fresh = %q, %v, want session-1, true", session, ok)
	}
}

func TestRevokeBlocksFutureArrivals(t *testing.T) {
	r := NewRegistry()
	token := r.ActiveOrCreate("session-1", false)
	if !r.Revoke("session-1") {
		t.Fatal("revoke of an active link must report true")
	}
	if _, ok := r.Resolve(token); ok {
		t.Fatal("a revoked token must not resolve")
	}
	if r.Revoke("session-1") {
		t.Fatal("a second revoke must report false")
	}
	// A new link can be minted after a revoke.
	if next := r.ActiveOrCreate("session-1", false); next == "" || next == token {
		t.Fatalf("token after revoke = %q, want a fresh token", next)
	}
}

func TestResolveUnknownToken(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Resolve("no-such-token"); ok {
		t.Fatal("an unknown token must not resolve")
	}
	if r.Revoke("no-such-session") {
		t.Fatal("revoking an unknown session must report false")
	}
}

func TestConcurrentEmissionKeepsOneTokenPerSession(t *testing.T) {
	r := NewRegistry()
	const writers = 32
	results := make([]string, writers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = r.ActiveOrCreate("session-1", false)
		}(i)
	}
	wg.Wait()
	for _, token := range results[1:] {
		if token != results[0] {
			t.Fatalf("concurrent emission diverged: %q vs %q", results[0], token)
		}
	}
	if got := r.Count(); got != 1 {
		t.Fatalf("active tokens = %d, want 1", got)
	}
}
