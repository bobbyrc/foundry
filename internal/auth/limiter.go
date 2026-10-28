package auth

import (
	"sync"
	"time"
)

// maxKeys caps how many keys a Limiter tracks. Every failed sign-in can add a key, and the names
// come from the people who type them, so without a cap a flood of made-up names would grow the
// map without limit.
const maxKeys = 10_000

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
	// keyCap is the most keys the map may hold; maxKeys in production.
	keyCap int

	mu       sync.Mutex
	failures map[string][]time.Time
}

// NewLimiter returns a Limiter that locks a key after max failures within window. now is the
// clock; pass time.Now in production and a fake clock in tests. max should be at least 1.
func NewLimiter(max int, window time.Duration, now func() time.Time) *Limiter {
	return newLimiter(max, window, now, maxKeys)
}

// newLimiter is NewLimiter with the key cap given, so tests can use a small one.
func newLimiter(max int, window time.Duration, now func() time.Time, keyCap int) *Limiter {
	return &Limiter{
		max:      max,
		window:   window,
		now:      now,
		keyCap:   keyCap,
		failures: make(map[string][]time.Time),
	}
}

// Locked reports whether key has reached max failures within the last window.
func (l *Limiter) Locked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key)) >= l.max
}

// Acquire is the check and the count in one step: it reports whether key may attempt a sign-in,
// and if so records the attempt as a failure at the current time. Callers record a success with
// Reset; a failed attempt is already counted. Checking with Locked and then calling Fail would let
// parallel requests all pass the check before any of them is counted.
func (l *Limiter) Acquire(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.prune(key)) >= l.max {
		return false
	}
	l.record(key)
	return true
}

// Fail records one failed sign-in for key at the current time.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.record(key)
}

// Reset forgets every failure for key, for use after a successful sign-in.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

// record appends a failure for key at the current time. A new key is added only after making
// room, so the map never holds more than keyCap keys. Callers must hold l.mu.
func (l *Limiter) record(key string) {
	if _, known := l.failures[key]; !known && len(l.failures) >= l.keyCap {
		l.makeRoom()
	}
	l.failures[key] = append(l.prune(key), l.now())
}

// makeRoom frees one slot in a full map. It first drops every expired failure, which also removes
// keys with none left. If the map is still full, it evicts the key whose newest failure is the
// oldest: that key has been quiet the longest. Callers must hold l.mu.
func (l *Limiter) makeRoom() {
	for key := range l.failures {
		l.prune(key)
	}
	if len(l.failures) < l.keyCap {
		return
	}
	var oldestKey string
	var oldest time.Time
	first := true
	for key, times := range l.failures {
		newest := times[len(times)-1]
		if first || newest.Before(oldest) {
			oldestKey, oldest, first = key, newest, false
		}
	}
	delete(l.failures, oldestKey)
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
