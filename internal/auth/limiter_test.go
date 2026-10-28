package auth

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a clock the test moves by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestLimiter(max int, window time.Duration) (*Limiter, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	return NewLimiter(max, window, clock.Now), clock
}

func TestLimiterLocksAtMaxFailures(t *testing.T) {
	l, _ := newTestLimiter(3, time.Minute)
	for i := 0; i < 2; i++ {
		l.Fail("alice")
		if l.Locked("alice") {
			t.Fatalf("locked after %d failures, want unlocked until 3", i+1)
		}
	}
	l.Fail("alice")
	if !l.Locked("alice") {
		t.Fatal("not locked after 3 failures")
	}
}

func TestLimiterUnlocksAfterWindow(t *testing.T) {
	l, clock := newTestLimiter(2, time.Minute)
	l.Fail("alice")
	clock.Advance(30 * time.Second)
	l.Fail("alice")
	if !l.Locked("alice") {
		t.Fatal("not locked at 2 failures inside the window")
	}
	// The first failure is now 60s old, so it leaves the window and only one failure counts.
	clock.Advance(30 * time.Second)
	if l.Locked("alice") {
		t.Fatal("still locked after the first failure left the window")
	}
	clock.Advance(30 * time.Second)
	if l.Locked("alice") {
		t.Fatal("still locked after both failures left the window")
	}
}

func TestLimiterResetClearsKey(t *testing.T) {
	l, _ := newTestLimiter(2, time.Minute)
	l.Fail("alice")
	l.Fail("alice")
	if !l.Locked("alice") {
		t.Fatal("not locked before Reset")
	}
	l.Reset("alice")
	if l.Locked("alice") {
		t.Fatal("still locked after Reset")
	}
}

func TestLimiterKeysAreIndependent(t *testing.T) {
	l, _ := newTestLimiter(2, time.Minute)
	l.Fail("alice")
	l.Fail("alice")
	if l.Locked("bob") {
		t.Fatal("bob locked by alice's failures")
	}
	if !l.Locked("alice") {
		t.Fatal("alice not locked")
	}
	l.Reset("bob")
	if !l.Locked("alice") {
		t.Fatal("resetting bob unlocked alice")
	}
}

func TestLimiterDropsEmptyKeys(t *testing.T) {
	l, clock := newTestLimiter(2, time.Minute)
	l.Fail("alice")
	clock.Advance(2 * time.Minute)
	l.Locked("alice") // touching the key after the window must drop it
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.failures["alice"]; ok {
		t.Fatal("key with no failures left is still in the map")
	}
}
