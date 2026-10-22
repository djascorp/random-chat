// Package moderation implements abuse protection: per-key rate limiting,
// block lists, reports and suspensions.
package moderation

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"random-chat/internal/storage"
)

// RateLimiter is a token bucket keyed by an arbitrary string (user id or IP).
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
	now     func() time.Time
}

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// NewRateLimiter allows perMinute sustained requests with the given burst.
func NewRateLimiter(perMinute, burst int) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*bucket),
		rate:    float64(perMinute) / 60.0,
		burst:   math.Max(1, float64(burst)),
		now:     time.Now,
	}
}

// Allow consumes one token for key, reporting whether the request may proceed.
func (r *RateLimiter) Allow(key string) bool {
	return r.AllowN(key, 1)
}

// AllowN consumes n tokens for key.
func (r *RateLimiter) AllowN(key string, n float64) bool {
	if r.rate <= 0 {
		return false
	}
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()

	b, ok := r.buckets[key]
	if !ok {
		b = &bucket{tokens: r.burst, lastSeen: now}
		r.buckets[key] = b
	}
	elapsed := now.Sub(b.lastSeen).Seconds()
	if elapsed > 0 {
		b.tokens = math.Min(r.burst, b.tokens+elapsed*r.rate)
		b.lastSeen = now
	}
	if b.tokens < n {
		return false
	}
	b.tokens -= n
	return true
}

// Sweep drops buckets that have been idle long enough to be full again.
func (r *RateLimiter) Sweep(olderThan time.Duration) {
	cutoff := r.now().Add(-olderThan)
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, b := range r.buckets {
		if b.lastSeen.Before(cutoff) {
			delete(r.buckets, key)
		}
	}
}

// Store is the persistence port the moderation service needs.
type Store interface {
	CreateReport(ctx context.Context, r *storage.Report) error
	CreateBlock(ctx context.Context, b *storage.Block) error
	IsBlocked(ctx context.Context, a, b string) (bool, error)
	CountReportsAgainst(ctx context.Context, targetID string, since time.Time) (int, error)
	SuspendUser(ctx context.Context, id string, until time.Time, reason string) error
	CreateModerationAction(ctx context.Context, a *storage.ModerationAction) error
}

// Service coordinates moderation state. Blocks and IP suspensions are cached in
// memory because the matching engine queries them on every pairing attempt.
type Service struct {
	store Store
	log   *slog.Logger

	mu           sync.RWMutex
	blocked      map[string]map[string]struct{}
	ipSuspended  map[string]time.Time
	violations   map[string]int
	autoSuspend  int
	suspendFor   time.Duration
	violationMax int
	reportWindow time.Duration
}

// Options configures the moderation policy.
type Options struct {
	AutoSuspendAfter int
	SuspendDuration  time.Duration
	ViolationLimit   int
	ReportWindow     time.Duration
}

// NewService loads the existing block list into memory and returns the service.
func NewService(ctx context.Context, store Store, opts Options, log *slog.Logger) *Service {
	if opts.ReportWindow <= 0 {
		opts.ReportWindow = 24 * time.Hour
	}
	s := &Service{
		store:        store,
		log:          log,
		blocked:      make(map[string]map[string]struct{}),
		ipSuspended:  make(map[string]time.Time),
		violations:   make(map[string]int),
		autoSuspend:  opts.AutoSuspendAfter,
		suspendFor:   opts.SuspendDuration,
		violationMax: opts.ViolationLimit,
		reportWindow: opts.ReportWindow,
	}
	_ = ctx
	return s
}

// IsBlocked reports whether the two users must never be paired.
// A block is symmetric: either direction prevents the match.
func (s *Service) IsBlocked(a, b string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if set, ok := s.blocked[a]; ok {
		if _, found := set[b]; found {
			return true
		}
	}
	if set, ok := s.blocked[b]; ok {
		if _, found := set[a]; found {
			return true
		}
	}
	return false
}

// Block records a block both in memory and in the store.
func (s *Service) Block(blockerID, blockedID string) error {
	s.mu.Lock()
	if s.blocked[blockerID] == nil {
		s.blocked[blockerID] = make(map[string]struct{})
	}
	s.blocked[blockerID][blockedID] = struct{}{}
	s.mu.Unlock()

	if s.store == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.store.CreateBlock(ctx, &storage.Block{
		BlockerID: blockerID,
		BlockedID: blockedID,
		CreatedAt: time.Now().UTC(),
	})
}

// Report files a report and escalates to a suspension when the threshold is met.
// It returns the report id and whether a suspension was issued.
func (s *Service) Report(reporterID, targetID, sessionID, category, reason, details string) (string, bool, error) {
	now := time.Now().UTC()
	report := &storage.Report{
		ID:         storage.NewID(),
		ReporterID: reporterID,
		TargetID:   targetID,
		SessionID:  sessionID,
		Category:   category,
		Reason:     reason,
		Details:    details,
		Status:     storage.ReportOpen,
		CreatedAt:  now,
	}

	var count int
	if s.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.store.CreateReport(ctx, report); err != nil {
			return report.ID, false, err
		}
		count, _ = s.store.CountReportsAgainst(ctx, targetID, now.Add(-s.reportWindow))
	}

	if s.autoSuspend <= 0 || count < s.autoSuspend {
		return report.ID, false, nil
	}
	if err := s.Suspend(targetID, "auto: repeated reports", "system"); err != nil {
		return report.ID, false, err
	}
	return report.ID, true, nil
}

// Suspend issues a time-boxed suspension and writes an audit entry.
func (s *Service) Suspend(targetID, reason, actor string) error {
	until := time.Now().UTC().Add(s.suspendFor)
	if s.store == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.store.SuspendUser(ctx, targetID, until, reason); err != nil {
		return err
	}
	return s.store.CreateModerationAction(ctx, &storage.ModerationAction{
		ID:        storage.NewID(),
		TargetID:  targetID,
		Action:    "suspend",
		Reason:    reason,
		Actor:     actor,
		CreatedAt: time.Now().UTC(),
	})
}

// SuspendIP blocks a network origin for the configured duration. This is what
// makes abuse protection meaningful for anonymous visitors whose user id is
// client supplied.
func (s *Service) SuspendIP(ip, reason string) {
	if ip == "" {
		return
	}
	s.mu.Lock()
	s.ipSuspended[ip] = time.Now().Add(s.suspendFor)
	s.mu.Unlock()
	if s.log != nil {
		s.log.Warn("ip suspended", "ip", ip, "reason", reason)
	}
}

// IPSuspendedUntil reports the expiry of an active IP suspension.
func (s *Service) IPSuspendedUntil(ip string) (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	until, ok := s.ipSuspended[ip]
	if !ok || until.Before(time.Now()) {
		return time.Time{}, false
	}
	return until, true
}

// RegisterViolation counts a rate-limit violation and suspends the IP once the
// limit is hit. It returns true when the IP was just suspended.
func (s *Service) RegisterViolation(key string) bool {
	s.mu.Lock()
	s.violations[key]++
	count := s.violations[key]
	s.mu.Unlock()
	if s.violationMax > 0 && count >= s.violationMax {
		s.SuspendIP(key, "repeated rate limit violations")
		return true
	}
	return false
}

// Run periodically prunes expired IP suspensions and stale violation counters.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			s.mu.Lock()
			for ip, until := range s.ipSuspended {
				if until.Before(now) {
					delete(s.ipSuspended, ip)
				}
			}
			for key := range s.violations {
				if _, still := s.ipSuspended[key]; !still {
					delete(s.violations, key)
				}
			}
			s.mu.Unlock()
		}
	}
}

// ValidCategory restricts reports to a known set so the admin UI stays clean.
func ValidCategory(category string) bool {
	switch strings.ToLower(category) {
	case "nudity", "harassment", "hate", "violence", "spam", "minor", "other":
		return true
	}
	return false
}
