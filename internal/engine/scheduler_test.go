package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/store"
)

// Two stalling tasks must never run at the same time under either limit: the per-project limit
// (same project) or the total limit (different projects).
func TestSchedulerLimitsConcurrentRuns(t *testing.T) {
	cases := []struct {
		name          string
		projects      []string
		maxConcurrent int
	}{
		{name: "per project", projects: []string{"demo"}, maxConcurrent: 3},
		{name: "total", projects: []string{"demo", "other"}, maxConcurrent: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			env := rtNewEngine(t, rtOpts{projects: c.projects, maxConcurrent: c.maxConcurrent, maxPerProject: 1})
			var ids []string
			for _, p := range c.projects {
				ids = append(ids, env.create(domain.Task{Project: p, Body: "[fake:stall]"}).ID)
			}

			peak := 0
			deadline := time.Now().Add(20 * time.Second)
			for {
				if _, total := env.e.activeTaskRuns(); total > peak {
					peak = total
				}
				blocked := 0
				for _, id := range ids {
					if env.task(id).Status == domain.TaskBlocked {
						blocked++
					}
				}
				if blocked == len(ids) {
					break
				}
				require.True(t, time.Now().Before(deadline), "tasks did not all block in time")
				time.Sleep(10 * time.Millisecond)
			}
			assert.Equal(t, 1, peak, "at most one run was live at a time")
		})
	}
}

// rtSetBudget replaces the engine's config with a copy whose daily budget is usd, as a config
// reload would.
func rtSetBudget(env *rtEnv, usd float64) {
	env.e.cfgMu.Lock()
	defer env.e.cfgMu.Unlock()
	cfg := *env.e.cfg
	cfg.Limits.DailyBudgetUSD = usd
	env.e.cfg = &cfg
}

// rtBudgetEvents counts the daily budget system events recorded so far.
func rtBudgetEvents(env *rtEnv) int {
	n := 0
	for _, ev := range env.events() {
		if ev.Type == domain.EvSystem && strings.HasPrefix(ev.Message, "daily budget") {
			n++
		}
	}
	return n
}

// rtStoreCost records a finished run with the given cost and start time.
func rtStoreCost(t *testing.T, env *rtEnv, cost float64, started time.Time) {
	t.Helper()
	_, err := env.st.CreateRun(env.ctx, domain.Run{TaskID: "earlier", Project: "demo",
		Status: domain.RunSucceeded, CostUSD: cost, StartedAt: started})
	require.NoError(t, err)
}

// A run today that already cost more than the budget stops new task runs, emits one budget event
// however many passes run, and does not stop once the budget is raised in config.
func TestRtDailyBudgetBlocksClaimsUntilRaised(t *testing.T) {
	t.Parallel()
	env := rtNewEngine(t, rtOpts{})
	rtSetBudget(env, 1.0)
	rtStoreCost(t, env, 1.5, time.Now())
	task := env.create(domain.Task{})

	for i := 0; i < 5; i++ {
		env.e.schedule(env.ctx)
	}
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, domain.TaskQueued, env.task(task.ID).Status, "budget reached: nothing claimed")
	assert.Empty(t, env.runs(task.ID))
	assert.Equal(t, 1, rtBudgetEvents(env), "one budget event across several passes")

	rtSetBudget(env, 5)
	env.e.wakeScheduler()
	rtWait(t, 20*time.Second, "task to be claimed after budget raised", func() bool {
		return len(env.runs(task.ID)) > 0
	})
	assert.Equal(t, 1, rtBudgetEvents(env), "no second event after the budget was raised")
}

// Only runs started since local midnight count: yesterday's cost does not block today.
func TestRtDailyBudgetIgnoresYesterday(t *testing.T) {
	t.Parallel()
	env := rtNewEngine(t, rtOpts{})
	rtSetBudget(env, 1.0)
	now := time.Now()
	yesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 23, 0, 0, 0, now.Location())
	rtStoreCost(t, env, 5, yesterday)
	task := env.create(domain.Task{})

	rtWait(t, 20*time.Second, "task to be claimed", func() bool {
		return len(env.runs(task.ID)) > 0
	})
	assert.Equal(t, 0, rtBudgetEvents(env))
}

// A cost that cannot be read stops scheduling: the budget is a limit, so failing open would
// defeat it.
func TestRtDailyBudgetFailsClosed(t *testing.T) {
	t.Parallel()
	env := rtNewEngine(t, rtOpts{wrapStore: func(s store.Store) store.Store {
		return rtCostErrStore{Store: s}
	}})
	rtSetBudget(env, 1.0)
	task := env.create(domain.Task{})

	env.e.schedule(env.ctx)
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, domain.TaskQueued, env.task(task.ID).Status)
	assert.Empty(t, env.runs(task.ID))
}

// rtCostErrStore is a store whose CostSince always fails.
type rtCostErrStore struct {
	store.Store
}

func (rtCostErrStore) CostSince(context.Context, time.Time) (float64, error) {
	return 0, errors.New("boom")
}

// A project paused by the owner at runtime is skipped while another project's task is claimed,
// and its own task is claimed once the project is resumed.
func TestSchedulerSkipsOwnerPausedProject(t *testing.T) {
	t.Parallel()
	env := rtNewEngine(t, rtOpts{projects: []string{"demo", "other"}})
	require.NoError(t, env.e.SetProjectPaused(env.ctx, "demo", true))
	pausedTask := env.create(domain.Task{Project: "demo"})
	otherTask := env.create(domain.Task{Project: "other"})

	rtWait(t, 20*time.Second, "other project's task to be claimed", func() bool {
		return len(env.runs(otherTask.ID)) > 0
	})
	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, env.runs(pausedTask.ID), "paused project's task was claimed")
	assert.Equal(t, domain.TaskQueued, env.task(pausedTask.ID).Status)

	require.NoError(t, env.e.SetProjectPaused(env.ctx, "demo", false))
	rtWait(t, 20*time.Second, "resumed project's task to be claimed", func() bool {
		return len(env.runs(pausedTask.ID)) > 0
	})
}
