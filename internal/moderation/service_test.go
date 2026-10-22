package moderation

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsBurstThenThrottles(t *testing.T) {
	limiter := NewRateLimiter(60, 3) // 1 token per second, burst of 3
	limiter.now = func() time.Time { return time.Unix(0, 0) }

	for i := 0; i < 3; i++ {
		if !limiter.Allow("peer") {
			t.Fatalf("request %d should be allowed within the burst", i+1)
		}
	}
	if limiter.Allow("peer") {
		t.Fatal("fourth request must be throttled")
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	current := time.Unix(0, 0)
	limiter := NewRateLimiter(60, 2) // 1 token per second
	limiter.now = func() time.Time { return current }

	if !limiter.Allow("peer") || !limiter.Allow("peer") {
		t.Fatal("burst of 2 should be allowed")
	}
	if limiter.Allow("peer") {
		t.Fatal("budget exhausted, request must be throttled")
	}

	current = current.Add(1100 * time.Millisecond)
	if !limiter.Allow("peer") {
		t.Fatal("a token should have been refilled after one second")
	}
}

func TestRateLimiterKeysAreIndependent(t *testing.T) {
	limiter := NewRateLimiter(60, 1)
	limiter.now = func() time.Time { return time.Unix(0, 0) }

	if !limiter.Allow("a") {
		t.Fatal("first key should be allowed")
	}
	if !limiter.Allow("b") {
		t.Fatal("second key must have its own budget")
	}
	if limiter.Allow("a") {
		t.Fatal("first key budget should be exhausted")
	}
}

func TestRateLimiterSweepDropsIdleBuckets(t *testing.T) {
	current := time.Unix(0, 0)
	limiter := NewRateLimiter(60, 1)
	limiter.now = func() time.Time { return current }

	limiter.Allow("stale")
	current = current.Add(2 * time.Hour)
	limiter.Sweep(10 * time.Minute)

	limiter.mu.Lock()
	_, present := limiter.buckets["stale"]
	limiter.mu.Unlock()
	if present {
		t.Fatal("idle bucket should have been swept")
	}
}

func TestValidCategory(t *testing.T) {
	for _, valid := range []string{"nudity", "harassment", "hate", "violence", "spam", "minor", "other"} {
		if !ValidCategory(valid) {
			t.Fatalf("category %q should be valid", valid)
		}
	}
	// Matching is case insensitive so the client can send any casing.
	for _, mixed := range []string{"Nudity", "HARASSMENT", "Other"} {
		if !ValidCategory(mixed) {
			t.Fatalf("category %q should be accepted case-insensitively", mixed)
		}
	}
	for _, invalid := range []string{"", "nuke", "  ", "under-age-ish"} {
		if ValidCategory(invalid) {
			t.Fatalf("category %q should be rejected", invalid)
		}
	}
}

func TestBlockIsSymmetricInMemory(t *testing.T) {
	service := &Service{
		blocked: make(map[string]map[string]struct{}),
	}
	if service.IsBlocked("a", "b") {
		t.Fatal("nothing is blocked yet")
	}
	if err := service.Block("a", "b"); err != nil {
		t.Fatalf("block failed: %v", err)
	}
	if !service.IsBlocked("a", "b") {
		t.Fatal("block should apply in the direction it was created")
	}
	if !service.IsBlocked("b", "a") {
		t.Fatal("block must be symmetric")
	}
	if service.IsBlocked("a", "c") {
		t.Fatal("unrelated peers must not be blocked")
	}
}

func TestIPSuspensionAndViolationEscalation(t *testing.T) {
	service := &Service{
		ipSuspended:  make(map[string]time.Time),
		violations:   make(map[string]int),
		violationMax: 3,
		suspendFor:   time.Minute,
	}

	if _, suspended := service.IPSuspendedUntil("1.2.3.4"); suspended {
		t.Fatal("ip should not be suspended yet")
	}
	if service.RegisterViolation("1.2.3.4") {
		t.Fatal("first violation must not suspend")
	}
	if service.RegisterViolation("1.2.3.4") {
		t.Fatal("second violation must not suspend")
	}
	if !service.RegisterViolation("1.2.3.4") {
		t.Fatal("third violation must trigger the suspension")
	}
	if _, suspended := service.IPSuspendedUntil("1.2.3.4"); !suspended {
		t.Fatal("ip should now be suspended")
	}
}
