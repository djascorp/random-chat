// Package invites owns the single active invitation link of each live
// session. Every member shares the same token; rotating or revoking it kills
// future arrivals without ejecting anyone already in the call.
//
// The registry is deliberately in memory, like the sessions it points at: a
// token that outlives its session can never grant access because callers
// revalidate the session itself (double barrier).
package invites

import (
	"sort"
	"sync"
	"time"

	"random-chat/internal/storage"
)

const maxTokens = 10000

type record struct {
	sessionID string
	createdAt time.Time
}

// Registry maps opaque tokens to live sessions, one token per session.
type Registry struct {
	mu        sync.Mutex
	tokens    map[string]record
	bySession map[string]string
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		tokens:    make(map[string]record),
		bySession: make(map[string]string),
	}
}

// ActiveOrCreate returns the live token of the session, minting one when
// there is none. With rotate it revokes the previous token first, so at most
// one link stays valid per session.
func (r *Registry) ActiveOrCreate(sessionID string, rotate bool) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !rotate {
		if token, ok := r.bySession[sessionID]; ok {
			if _, live := r.tokens[token]; live {
				return token
			}
			delete(r.bySession, sessionID)
		}
	} else {
		r.revokeLocked(sessionID)
	}

	r.evictOverflowLocked()

	token := storage.NewID()
	// NewID is 128 random bits; a collision would mean a broken RNG.
	for {
		if _, taken := r.tokens[token]; !taken {
			break
		}
		token = storage.NewID()
	}
	r.tokens[token] = record{sessionID: sessionID, createdAt: time.Now().UTC()}
	r.bySession[sessionID] = token
	return token
}

// Resolve maps a token to its session, if the link is still active. Callers
// must revalidate the session itself: a token is only half of the barrier.
func (r *Registry) Resolve(token string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.tokens[token]
	if !ok {
		return "", false
	}
	return rec.sessionID, true
}

// Revoke kills the active link of a session. Members are unaffected: only
// future arrivals are blocked.
func (r *Registry) Revoke(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.revokeLocked(sessionID)
}

func (r *Registry) revokeLocked(sessionID string) bool {
	token, ok := r.bySession[sessionID]
	if !ok {
		return false
	}
	delete(r.bySession, sessionID)
	delete(r.tokens, token)
	return true
}

// Count reports how many links are currently active.
func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.tokens)
}

// evictOverflowLocked drops the oldest links when the registry grows past its
// bound, so a leak of sessions can never grow memory without limit. Called
// with the mutex held.
func (r *Registry) evictOverflowLocked() {
	if len(r.tokens) < maxTokens {
		return
	}
	type aged struct {
		token string
		at    time.Time
	}
	oldest := make([]aged, 0, len(r.tokens))
	for token, rec := range r.tokens {
		oldest = append(oldest, aged{token: token, at: rec.createdAt})
	}
	sort.Slice(oldest, func(i, j int) bool { return oldest[i].at.Before(oldest[j].at) })
	for _, entry := range oldest {
		if len(r.tokens) < maxTokens {
			return
		}
		if rec, ok := r.tokens[entry.token]; ok {
			delete(r.tokens, entry.token)
			if r.bySession[rec.sessionID] == entry.token {
				delete(r.bySession, rec.sessionID)
			}
		}
	}
}
