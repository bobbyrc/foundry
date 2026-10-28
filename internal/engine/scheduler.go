package engine

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/bobbyrc/foundry/internal/store"
)

// runScheduler claims queued tasks and starts runs until ctx is done. It looks for work on every
// SchedulerTick and whenever woken (a task was queued, a run ended).
func (e *Engine) runScheduler(ctx context.Context) {
	tick := time.NewTicker(e.d.SchedulerTick)
	defer tick.Stop()
	for {
		e.schedule(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-e.wake:
		}
	}
}

// schedule starts as many task runs as the limits allow. Projects already at
// Limits.MaxRunsPerProject and paused projects are skipped by the store's claim.
func (e *Engine) schedule(ctx context.Context) {
	if e.Paused() {
		return
	}
	cfg := e.Config()
	for ctx.Err() == nil {
		perProject, total := e.activeTaskRuns()
		if total >= cfg.Limits.MaxConcurrentRuns {
			return
		}
		busy := []string{}
		for name, n := range perProject {
			if n >= cfg.Limits.MaxRunsPerProject {
				busy = append(busy, name)
			}
		}
		for name, p := range cfg.Projects {
			if p.Paused {
				busy = append(busy, name)
			}
		}
		t, err := e.d.Store.ClaimNextTask(ctx, busy)
		if errors.Is(err, store.ErrNotFound) {
			return
		}
		if err != nil {
			e.d.Logger.Error("claim next task", slog.Any("err", err))
			return
		}
		e.startTask(ctx, t)
	}
}

// activeTaskRuns counts live runs that belong to tasks, per project and in total. Chat runs are
// not counted against the limits.
func (e *Engine) activeTaskRuns() (map[string]int, int) {
	e.runsMu.Lock()
	defer e.runsMu.Unlock()
	perProject := map[string]int{}
	total := 0
	for _, r := range e.active {
		if r.taskID == "" {
			continue
		}
		perProject[r.project]++
		total++
	}
	return perProject, total
}

// registerRun records a live run so it can be cancelled and counted against limits.
// taskID is "" for chat runs, which do not count against limits.
func (e *Engine) registerRun(runID, taskID, project string, cancel context.CancelFunc) *activeRun {
	ar := &activeRun{runID: runID, taskID: taskID, project: project, cancel: cancel}
	e.runsMu.Lock()
	e.active[runID] = ar
	e.runsMu.Unlock()
	return ar
}

// unregisterRun forgets a finished run and wakes the scheduler.
func (e *Engine) unregisterRun(runID string) {
	e.runsMu.Lock()
	delete(e.active, runID)
	e.runsMu.Unlock()
	e.wakeScheduler()
}
