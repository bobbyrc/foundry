package events

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
	"github.com/bobbyrc/onett/internal/store/sqlite"
)

func newBus(t *testing.T) *MemBus {
	t.Helper()
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return NewBus(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func recv(t *testing.T, ch <-chan domain.Event) domain.Event {
	t.Helper()
	select {
	case e, ok := <-ch:
		require.True(t, ok, "channel closed")
		return e
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timed out waiting for event")
		return domain.Event{}
	}
}

func TestPublishPersistsAndDelivers(t *testing.T) {
	b := newBus(t)
	ctx := context.Background()
	ch, cancel := b.Subscribe()
	defer cancel()

	e, err := b.Publish(ctx, domain.Event{Type: domain.EvTaskCreated, Project: "p", Message: "made"})
	require.NoError(t, err)
	assert.NotZero(t, e.ID)
	assert.False(t, e.Time.IsZero())

	got := recv(t, ch)
	assert.Equal(t, e.ID, got.ID)
	assert.Equal(t, "made", got.Message)

	stored, err := b.st.ListEvents(ctx, storeFilterAll())
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, e.ID, stored[0].ID)
}

func TestLiveEventsAreNotPersisted(t *testing.T) {
	b := newBus(t)
	ctx := context.Background()
	ch, cancel := b.Subscribe()
	defer cancel()

	for _, typ := range []string{domain.EvRunLog, domain.EvChatDelta} {
		e, err := b.Publish(ctx, domain.Event{Type: typ, Message: "live"})
		require.NoError(t, err)
		assert.Zero(t, e.ID)
		got := recv(t, ch)
		assert.Equal(t, typ, got.Type)
		assert.Zero(t, got.ID)
	}

	stored, err := b.st.ListEvents(ctx, storeFilterAll())
	require.NoError(t, err)
	assert.Empty(t, stored)
}

func TestDeliveryOrder(t *testing.T) {
	b := newBus(t)
	ctx := context.Background()
	ch, cancel := b.Subscribe()
	defer cancel()

	for i := range 20 {
		_, err := b.Publish(ctx, domain.Event{Type: domain.EvSystem, Message: fmt.Sprint(i)})
		require.NoError(t, err)
	}
	for i := range 20 {
		assert.Equal(t, fmt.Sprint(i), recv(t, ch).Message)
	}
}

func TestEmitMarshalsData(t *testing.T) {
	b := newBus(t)
	ctx := context.Background()
	ch, cancel := b.Subscribe()
	defer cancel()

	b.Emit(ctx, domain.EvTaskUpdated, "p", "t1", "", "updated", domain.Task{ID: "t1", Title: "x"})
	got := recv(t, ch)
	assert.Equal(t, domain.EvTaskUpdated, got.Type)
	var task domain.Task
	require.NoError(t, json.Unmarshal(got.Data, &task))
	assert.Equal(t, "t1", task.ID)

	b.Emit(ctx, domain.EvSystem, "", "", "", "no payload", nil)
	got = recv(t, ch)
	assert.Empty(t, got.Data)
}

func TestSlowSubscriberDroppedOthersKeepReceiving(t *testing.T) {
	b := newBus(t)
	ctx := context.Background()

	slow, slowCancel := b.Subscribe()
	defer slowCancel()
	fast, fastCancel := b.Subscribe()
	defer fastCancel()

	const total = 300
	var wg sync.WaitGroup
	var count int
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range fast {
			count++
			if count == total {
				return
			}
		}
	}()

	for i := range total {
		_, err := b.Publish(ctx, domain.Event{Type: domain.EvSystem, Message: fmt.Sprint(i)})
		require.NoError(t, err)
	}
	wg.Wait()
	assert.Equal(t, total, count)

	// The slow subscriber kept its buffered events, then its channel is closed.
	drained := 0
	for range slow {
		drained++
	}
	assert.Equal(t, subscriberBuffer, drained)

	// Cancelling a dropped subscriber is a no-op.
	assert.NotPanics(t, slowCancel)
}

func TestCancelIsIdempotent(t *testing.T) {
	b := newBus(t)
	ch, cancel := b.Subscribe()
	cancel()
	cancel()
	_, ok := <-ch
	assert.False(t, ok, "channel closed after cancel")

	// Publishing after cancel must not panic or deliver.
	_, err := b.Publish(context.Background(), domain.Event{Type: domain.EvSystem})
	assert.NoError(t, err)
}

func TestConcurrentPublish(t *testing.T) {
	b := newBus(t)
	ctx := context.Background()
	ch, cancel := b.Subscribe()
	defer cancel()

	const goroutines, perGoroutine = 10, 20
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perGoroutine {
				b.Emit(ctx, domain.EvSystem, "p", "", "", fmt.Sprintf("%d-%d", g, i), nil)
			}
		}()
	}
	wg.Wait()

	seen := map[int64]bool{}
	for range goroutines * perGoroutine {
		e := recv(t, ch)
		assert.False(t, seen[e.ID], "duplicate delivery")
		seen[e.ID] = true
	}
	stored, err := b.st.ListEvents(ctx, storeFilterAll())
	require.NoError(t, err)
	assert.Len(t, stored, goroutines*perGoroutine)
}

// TestConcurrentPublishDeliversInIDOrder checks that persisted events reach a subscriber in
// strictly increasing ID order even when many publishers race. The SSE handler skips any ID at
// or below the last one it sent, so an out-of-order delivery would drop an event.
func TestConcurrentPublishDeliversInIDOrder(t *testing.T) {
	b := newBus(t)
	ctx := context.Background()

	// 50 publishers x 4 events stays under subscriberBuffer, so a slow reader cannot overflow
	// the subscriber and get it dropped.
	const publishers, perPublisher = 50, 4
	const total = publishers * perPublisher
	ch, cancel := b.Subscribe()
	defer cancel()

	var wg sync.WaitGroup
	for g := range publishers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perPublisher {
				_, err := b.Publish(ctx, domain.Event{Type: domain.EvSystem, Message: fmt.Sprintf("%d-%d", g, i)})
				assert.NoError(t, err)
			}
		}()
	}

	var last int64
	for range total {
		e := recv(t, ch)
		require.Greater(t, e.ID, last, "event %q delivered out of ID order", e.Message)
		last = e.ID
	}
	wg.Wait()
}

// storeFilterAll reads every stored event in one call.
func storeFilterAll() store.EventFilter {
	return store.EventFilter{Limit: 10000}
}
