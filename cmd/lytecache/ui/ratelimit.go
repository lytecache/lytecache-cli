package ui

import (
	"sync"
	"time"
)

// loginRateLimiter tracks failed /login attempts per client IP, applying
// exponential backoff after a few failures. In-memory, no external
// dependency -- this is a local admin tool, not a fleet of servers that
// need a shared limiter.
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempts
}

type loginAttempts struct {
	count       int
	lockUntil   time.Time
	lastAttempt time.Time
}

// failuresBeforeBackoff is how many failed attempts are free before
// backoff kicks in -- matches the spec's "~5 failures".
const failuresBeforeBackoff = 5

// maxBackoff caps the exponential backoff so a forgotten lockout can't
// grow unboundedly large.
const maxBackoff = 30 * time.Second

// staleAttemptTTL is how long an IP's attempt record survives with no new
// failures before being forgotten -- bounds memory for a long-running
// process fielding attempts from many distinct IPs over time.
const staleAttemptTTL = 1 * time.Hour

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{attempts: make(map[string]*loginAttempts)}
}

// allow reports whether ip may attempt a login right now, and if not, how
// long until it may.
func (rl *loginRateLimiter) allow(ip string) (ok bool, retryAfter time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	a := rl.attempts[ip]
	if a == nil {
		return true, 0
	}
	if now := time.Now(); now.Before(a.lockUntil) {
		return false, a.lockUntil.Sub(now)
	}
	return true, 0
}

// recordFailure counts a failed attempt from ip, escalating the backoff
// once failuresBeforeBackoff is exceeded: 1s, 2s, 4s, ... capped at
// maxBackoff.
func (rl *loginRateLimiter) recordFailure(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.gcLocked()

	a := rl.attempts[ip]
	if a == nil {
		a = &loginAttempts{}
		rl.attempts[ip] = a
	}
	a.count++
	a.lastAttempt = time.Now()
	if a.count > failuresBeforeBackoff {
		shift := a.count - failuresBeforeBackoff - 1
		backoff := time.Second << shift
		if backoff > maxBackoff || backoff <= 0 { // shift overflow guards against a very long attack
			backoff = maxBackoff
		}
		a.lockUntil = time.Now().Add(backoff)
	}
}

// recordSuccess clears ip's attempt history after a successful login.
func (rl *loginRateLimiter) recordSuccess(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, ip)
}

func (rl *loginRateLimiter) gcLocked() {
	cutoff := time.Now().Add(-staleAttemptTTL)
	for ip, a := range rl.attempts {
		if a.lastAttempt.Before(cutoff) {
			delete(rl.attempts, ip)
		}
	}
}
