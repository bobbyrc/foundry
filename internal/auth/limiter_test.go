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

func TestAcquireAllowsExactlyMaxConcurrentAttempts(t *testing.T) {
	const max = 5
	l, _ := newTestLimiter(max, time.Minute)

	var wg sync.WaitGroup
	var start sync.WaitGroup
	start.Add(1)
	var granted int64
	var mu sync.Mutex
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Wait()
			if l.Acquire("alice") {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	start.Done()
	wg.Wait()
	if granted != max {
		t.Fatalf("Acquire granted %d attempts, want exactly %d", granted, max)
	}
	if !l.Locked("alice") {
		t.Fatal("key not locked after max acquired attempts")
	}
}

func TestAcquireRecordsAttemptAsFailure(t *testing.T) {
	l, _ := newTestLimiter(2, time.Minute)
	if !l.Acquire("alice") {
		t.Fatal("first Acquire refused")
	}
	if l.Locked("alice") {
		t.Fatal("locked after one attempt, want unlocked until two")
	}
	if !l.Acquire("alice") {
		t.Fatal("second Acquire refused")
	}
	if !l.Locked("alice") {
		t.Fatal("not locked after two acquired attempts")
	}
	if l.Acquire("alice") {
		t.Fatal("Acquire granted a third attempt on a locked key")
	}
}

func TestLimiterCapPrunesExpiredKeysBeforeEvicting(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	l := newLimiter(2, time.Minute, clock.Now, 2)
	l.Fail("a")
	clock.Advance(2 * time.Minute) // a's only failure is now outside the window
	l.Fail("b")
	l.Fail("c") // the map is full, but a has expired, so it goes instead of a live key

	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.failures["a"]; ok {
		t.Fatal("expired key a still in the map")
	}
	if _, ok := l.failures["b"]; !ok {
		t.Fatal("live key b was evicted while an expired key was available")
	}
	if _, ok := l.failures["c"]; !ok {
		t.Fatal("new key c was not recorded")
	}
}

func TestLimiterCapEvictsKeyWhoseNewestFailureIsOldest(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	l := newLimiter(2, time.Hour, clock.Now, 2)
	l.Fail("a")
	clock.Advance(time.Second)
	l.Fail("b")
	clock.Advance(time.Second)
	l.Fail("a") // a's newest failure is now later than b's
	clock.Advance(time.Second)
	l.Fail("c") // full and nothing has expired: b has the oldest newest failure

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.failures) != 2 {
		t.Fatalf("map has %d keys, want the cap of 2", len(l.failures))
	}
	if _, ok := l.failures["b"]; ok {
		t.Fatal("b (oldest newest failure) was not evicted")
	}
	for _, k := range []string{"a", "c"} {
		if _, ok := l.failures[k]; !ok {
			t.Fatalf("key %q missing after eviction", k)
		}
	}
}

func TestLimiterCapDoesNotEvictForExistingKey(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	l := newLimiter(3, time.Hour, clock.Now, 2)
	l.Fail("a")
	l.Fail("b")
	l.Fail("a") // already known: the map does not grow, so nothing is evicted

	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.failures) != 2 {
		t.Fatalf("map has %d keys, want 2", len(l.failures))
	}
	if len(l.failures["a"]) != 2 {
		t.Fatalf("a has %d failures, want 2", len(l.failures["a"]))
	}
}
