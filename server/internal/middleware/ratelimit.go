package middleware

import (
	"sync"
	"time"
)

// Limiter is a per-key token bucket: each key may burst up to `burst` requests
// and regains tokens at `perMinute` per minute. Idle keys are evicted so the
// map cannot grow without bound.
type Limiter struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	rate      float64 // tokens per second
	burst     float64
	maxKeys   int
	idleAfter time.Duration
	now       func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewLimiter builds a limiter allowing perMinute requests per key per minute
// (with a burst of the same size).
func NewLimiter(perMinute int) *Limiter {
	return &Limiter{
		buckets:   make(map[string]*bucket),
		rate:      float64(perMinute) / 60.0,
		burst:     float64(perMinute),
		maxKeys:   50000,
		idleAfter: 10 * time.Minute,
		now:       time.Now,
	}
}

// Allow consumes a token for key. When denied it reports how long until one is available.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxKeys {
			l.evict(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	} else {
		b.tokens += now.Sub(b.last).Seconds() * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = now
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	return false, wait
}

// evict drops idle buckets; if the map is still full it drops the oldest half.
func (l *Limiter) evict(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.last) > l.idleAfter {
			delete(l.buckets, k)
		}
	}
	if len(l.buckets) < l.maxKeys {
		return
	}
	n := 0
	for k := range l.buckets {
		delete(l.buckets, k)
		if n++; n >= l.maxKeys/2 {
			break
		}
	}
}
