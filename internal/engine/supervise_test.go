package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/harness"
	hfake "github.com/bobbyrc/foundry/internal/harness/fake"
)

func TestSuperviseStallBlocksTask(t *testing.T) {
	env := rtNewEngine(t, rtOpts{}) // stall_timeout is 2s
	start := time.Now()
	task := env.create(domain.Task{Body: "[fake:stall]"})

	got := env.waitStatus(task.ID, domain.TaskBlocked)
	elapsed := time.Since(start)
	assert.Contains(t, got.BlockedReason, "stalled: no output for 2s")
	assert.Less(t, elapsed, 6*time.Second, "stall detected near stall_timeout, took %s", elapsed)

	runs := env.runs(task.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, domain.RunStalled, runs[0].Status)
	assert.Contains(t, runs[0].Error, "stalled")
}

func costScript(res harness.Result) hfake.Script {
	return func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		return res, nil
	}
}

func TestSuperviseEstimatesCostFromTokens(t *testing.T) {
	env := rtNewEngine(t, rtOpts{
		fakePrice: "input_per_mtok: 2, output_per_mtok: 10, cached_input_per_mtok: 0.5",
		script:    costScript(harness.Result{InputTokens: 1_000_000, CachedInputTokens: 400_000, OutputTokens: 100_000}),
	})
	task := env.create(domain.Task{Body: "estimate"})
	runs := env.firstEndedRun(task.ID)
	assert.True(t, runs[0].CostEstimated)
	assert.InDelta(t, 600_000*2/1e6+400_000*0.5/1e6+100_000*10/1e6, runs[0].CostUSD, 1e-9)
}

func TestSuperviseKeepsReportedCost(t *testing.T) {
	env := rtNewEngine(t, rtOpts{
		fakePrice: "input_per_mtok: 2, output_per_mtok: 10",
		script:    costScript(harness.Result{CostUSD: 1.5, InputTokens: 1_000_000, OutputTokens: 1_000_000}),
	})
	task := env.create(domain.Task{Body: "reported"})
	runs := env.firstEndedRun(task.ID)
	assert.False(t, runs[0].CostEstimated)
	assert.Equal(t, 1.5, runs[0].CostUSD)
}

func TestSuperviseNoPriceNoEstimate(t *testing.T) {
	env := rtNewEngine(t, rtOpts{script: costScript(harness.Result{InputTokens: 1_000_000})})
	task := env.create(domain.Task{Body: "unpriced"})
	runs := env.firstEndedRun(task.ID)
	assert.False(t, runs[0].CostEstimated)
	assert.Zero(t, runs[0].CostUSD)
}

// firstEndedRun waits for one of the task's runs to finish and returns it as a one-run slice. It
// returns the run it saw ended rather than listing again: a retry can start a newer run between
// two listings, and runs are listed newest first.
func (env *rtEnv) firstEndedRun(taskID string) []domain.Run {
	env.t.Helper()
	var ended domain.Run
	require.Eventually(env.t, func() bool {
		for _, r := range env.runs(taskID) {
			if r.EndedAt != nil {
				ended = r
				return true
			}
		}
		return false
	}, 10*time.Second, 20*time.Millisecond)
	return []domain.Run{ended}
}
