package server

import (
	"sync"
	"time"
)

// attemptLimiter slows password guessing: after 5 consecutive failures every further
// failure doubles the wait (1s, 2s, 4s ... capped at 5 minutes). Success resets it.
// The server is loopback-only, but any local process could otherwise guess at bcrypt speed.
type attemptLimiter struct {
	mu       sync.Mutex
	failures int
	until    time.Time
	now      func() time.Time
}

const (
	freeAttempts = 5
	maxBackoff   = 5 * time.Minute
)

func newAttemptLimiter() *attemptLimiter { return &attemptLimiter{now: time.Now} }

// blockedFor returns how long callers must wait before another attempt (0 = go ahead).
func (l *attemptLimiter) blockedFor() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if d := l.until.Sub(l.now()); d > 0 {
		return d
	}
	return 0
}

func (l *attemptLimiter) failed() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures++
	if l.failures < freeAttempts {
		return
	}
	shift := l.failures - freeAttempts
	if shift > 10 {
		shift = 10
	}
	d := time.Second << shift
	if d > maxBackoff {
		d = maxBackoff
	}
	l.until = l.now().Add(d)
}

func (l *attemptLimiter) succeeded() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures = 0
	l.until = time.Time{}
}
