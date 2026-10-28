package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// Fanout sends each notification to every configured notifier whose minimum priority it meets.
type Fanout struct {
	targets []fanoutTarget
	logger  *slog.Logger
}

type fanoutTarget struct {
	typ    string
	min    Priority
	notifs Notifier
}

var _ Notifier = (*Fanout)(nil)

// NewFanout builds every notifier in cfgs. An empty list is valid and yields a no-op Fanout. A nil
// logger uses slog.Default(). An unknown type or an invalid min_priority is an error.
func NewFanout(cfgs []Config, logger *slog.Logger) (*Fanout, error) {
	if logger == nil {
		logger = slog.Default()
	}
	f := &Fanout{logger: logger}
	for i, cfg := range cfgs {
		minPri := effectivePriority(cfg.MinPriority)
		if !validPriority(minPri) {
			return nil, fmt.Errorf("notify: notifier %d (%s): unknown min_priority %q", i, cfg.Type, cfg.MinPriority)
		}
		n, err := New(cfg)
		if err != nil {
			return nil, fmt.Errorf("notify: notifier %d (%s): %w", i, cfg.Type, err)
		}
		f.targets = append(f.targets, fanoutTarget{typ: cfg.Type, min: minPri, notifs: n})
	}
	return f, nil
}

// Notify sends n to each eligible notifier concurrently and waits for all of them. A failure is
// logged as a warning and returned, joined with any others. Notifiers whose minimum priority is
// above n's priority are skipped without error.
func (f *Fanout) Notify(ctx context.Context, n Notification) error {
	rank := priorityRank(n.Priority)
	errs := make([]error, len(f.targets))

	var wg sync.WaitGroup
	for i, t := range f.targets {
		if priorityRank(t.min) > rank {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := t.notifs.Notify(ctx, n); err != nil {
				f.logger.Warn("notification failed", "type", t.typ, "title", n.Title, "err", err)
				errs[i] = fmt.Errorf("%s: %w", t.typ, err)
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// effectivePriority returns p, or PriorityNormal when p is empty.
func effectivePriority(p Priority) Priority {
	if p == "" {
		return PriorityNormal
	}
	return p
}

// priorityRank orders priorities: low is 0, normal is 1 and high is 2. An empty priority ranks as
// normal.
func priorityRank(p Priority) int {
	switch effectivePriority(p) {
	case PriorityLow:
		return 0
	case PriorityHigh:
		return 2
	default:
		return 1
	}
}

// validPriority reports whether p is one of the defined priorities.
func validPriority(p Priority) bool {
	switch p {
	case PriorityLow, PriorityNormal, PriorityHigh:
		return true
	}
	return false
}
