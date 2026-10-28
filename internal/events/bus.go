package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/store"
)

// subscriberBuffer is how many events a subscriber may fall behind before it is dropped.
const subscriberBuffer = 256

// MemBus is a Bus that persists events through a store.Store and fans them out to in-process
// subscribers.
type MemBus struct {
	st     store.Store
	logger *slog.Logger

	mu   sync.Mutex
	subs map[chan domain.Event]struct{}
}

var _ Bus = (*MemBus)(nil)

// NewBus returns a MemBus that persists to st. A nil logger uses slog's default.
func NewBus(st store.Store, logger *slog.Logger) *MemBus {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemBus{st: st, logger: logger, subs: make(map[chan domain.Event]struct{})}
}

// isLive reports whether events of typ are fanned out without being persisted.
func isLive(typ string) bool {
	return typ == domain.EvRunLog || typ == domain.EvChatDelta
}

// Publish persists e unless it is live-only, then delivers it to every subscriber. A subscriber
// whose buffer is full is dropped: its channel is closed and it is removed.
func (b *MemBus) Publish(ctx context.Context, e domain.Event) (domain.Event, error) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	if !isLive(e.Type) {
		saved, err := b.st.AppendEvent(ctx, e)
		if err != nil {
			return domain.Event{}, fmt.Errorf("publish %s: %w", e.Type, err)
		}
		e = saved
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
			close(ch)
			delete(b.subs, ch)
		}
	}
	return e, nil
}

// Emit marshals v into Data and publishes the event. Errors are logged, not returned.
func (b *MemBus) Emit(ctx context.Context, typ, project, taskID, runID, message string, v any) {
	var data json.RawMessage
	if v != nil {
		raw, err := json.Marshal(v)
		if err != nil {
			b.logger.Error("emit: marshal event data", "type", typ, "err", err)
			return
		}
		data = raw
	}
	e := domain.Event{
		Type:    typ,
		Project: project,
		TaskID:  taskID,
		RunID:   runID,
		Message: message,
		Data:    data,
	}
	if _, err := b.Publish(ctx, e); err != nil {
		b.logger.Error("emit event", "type", typ, "err", err)
	}
}

// Subscribe returns a channel of events published after this call and a cancel func. Cancel is
// idempotent and safe after the subscriber was dropped for falling behind.
func (b *MemBus) Subscribe() (<-chan domain.Event, func()) {
	ch := make(chan domain.Event, subscriberBuffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
	}
	return ch, cancel
}
