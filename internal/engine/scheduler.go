package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/notify"
	"github.com/bobbyrc/foundry/internal/store"
)

// budgetAlerted maps each *Engine to the local date (YYYY-MM-DD) on which its daily budget alert
// was last emitted, so the alert fires once per day rather than on every scheduling pass.
var budgetAlerted sync.Map

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
	if cfg.Limits.DailyBudgetUSD > 0 && !e.withinDailyBudget(ctx, cfg.Limits.DailyBudgetUSD) {
		return
	}
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

// withinDailyBudget reports whether new task runs may start: the cost of runs started since local
// midnight is below budget. It fails closed: if the cost cannot be read, nothing starts. The first
// time the budget is reached on a local date, it records a system event and sends a high-priority
// notification. Runs already in progress are not affected.
func (e *Engine) withinDailyBudget(ctx context.Context, budget float64) bool {
	now := e.d.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	cost, err := e.d.Store.CostSince(ctx, midnight)
	if err != nil {
		e.d.Logger.Error("daily budget: cannot read today's cost; claiming nothing", slog.Any("err", err))
		return false
	}
	if cost < budget {
		return true
	}
	day := midnight.Format("2006-01-02")
	if prev, _ := budgetAlerted.Swap(e, day); prev != day {
		msg := fmt.Sprintf("daily budget of $%.2f reached ($%.2f spent today); no new runs until midnight — raise limits.daily_budget_usd to continue", budget, cost)
		e.d.Bus.Emit(ctx, domain.EvSystem, "", "", "", msg, nil)
		e.notify(ctx, notify.PriorityHigh, "Daily budget reached", msg, "/")
	}
	return false
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
