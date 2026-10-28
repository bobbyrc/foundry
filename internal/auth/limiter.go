package auth

import (
	"sync"
	"time"
)

// Limiter counts failed sign-ins per key (a username) over a sliding window, so that repeated
// guesses at one password lock that username for a while. Safe for concurrent use.
//
// The limit is per key, not per client address, so it cannot be bypassed by changing address;
// it does mean someone can lock out a known username for one window. The window is short
// enough that the owner can wait it out.
type Limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	failures map[string][]time.Time
}

// NewLimiter returns a Limiter that locks a key after max failures within window. now is the
// clock; pass time.Now in production and a fake clock in tests. max should be at least 1.
func NewLimiter(max int, window time.Duration, now func() time.Time) *Limiter {
	return &Limiter{
		max:      max,
		window:   window,
		now:      now,
		failures: make(map[string][]time.Time),
	}
}

// Locked reports whether key has reached max failures within the last window.
func (l *Limiter) Locked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key)) >= l.max
}

// Fail records one failed sign-in for key at the current time.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	times := append(l.prune(key), l.now())
	l.failures[key] = times
}

// Reset forgets every failure for key, for use after a successful sign-in.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

// prune drops failures older than the window and removes the key once none are left, so the
// map does not grow with every username ever tried. Callers must hold l.mu.
func (l *Limiter) prune(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	kept := l.failures[key][:0]
	for _, t := range l.failures[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = kept
	return kept
}
