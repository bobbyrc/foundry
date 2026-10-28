package engine

import (
	"context"
	"time"
)

// housekeepEvery is how often housekeep runs while the engine is up.
const housekeepEvery = 24 * time.Hour

// housekeepLoop runs housekeep at start and then once a day until ctx is done.
func (e *Engine) housekeepLoop(ctx context.Context) {
	ticker := time.NewTicker(housekeepEvery)
	defer ticker.Stop()
	for {
		e.housekeep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// housekeep deletes event and run log rows older than the server's retention windows. A window
// of zero is never set, because ApplyDefaults fills it, so every window is applied. Problems are
// logged and never returned: a failed prune must not stop the engine, and the next run retries.
func (e *Engine) housekeep(ctx context.Context) {
	srv := e.Config().Server
	now := e.d.Now()
	if srv.RetainEvents.Duration > 0 {
		before := now.Add(-srv.RetainEvents.Duration)
		n, err := e.d.Store.PruneEvents(ctx, before)
		switch {
		case err != nil:
			e.d.Logger.Warn("prune events", "before", before, "err", err)
		case n > 0:
			e.d.Logger.Info("pruned old events", "deleted", n, "before", before)
		}
	}
	if srv.RetainRunLogs.Duration > 0 {
		before := now.Add(-srv.RetainRunLogs.Duration)
		n, err := e.d.Store.PruneRunLogs(ctx, before)
		switch {
		case err != nil:
			e.d.Logger.Warn("prune run logs", "before", before, "err", err)
		case n > 0:
			e.d.Logger.Info("pruned old run logs", "deleted", n, "before", before)
		}
	}
}
