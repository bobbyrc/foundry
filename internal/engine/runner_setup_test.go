package engine

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/harness"
)

// A failing setup command fails the attempt before the agent starts: the task is re-queued with
// the command's output, and the harness is never started.
func TestRunnerFailingSetupRequeuesBeforeAgentStarts(t *testing.T) {
	var starts atomic.Int32
	script := func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		starts.Add(1)
		return harness.Result{FinalText: "done"}, nil
	}
	env := rtNewEngine(t, rtOpts{maxAttempts: 2, script: script, projectExtra: map[string]string{
		"demo": `    setup: [{name: prep, run: "echo boom; exit 3"}]` + "\n",
	}})
	task := env.create(domain.Task{Body: "[fake:commit]"})

	got := env.waitStatus(task.ID, domain.TaskBlocked)
	assert.Contains(t, got.BlockedReason, "setup failed: prep")
	assert.Contains(t, got.BlockedReason, "gave up after 2 attempts")
	assert.Contains(t, got.Body, "## Attempt 1: setup failed: prep", "the first failure re-queued the task")
	assert.Contains(t, got.Body, "boom", "the setup command's output reaches the next attempt")
	assert.Zero(t, starts.Load(), "the agent never starts when setup fails")
	assert.Empty(t, env.scm.PRs, "no pull request is opened for a failed setup")
	rtWaitEvent(t, env, task.ID, "setup prep failed")
}

// Setup that passes lets the agent run and the task publish as usual.
func TestRunnerPassingSetupPublishes(t *testing.T) {
	env := rtNewEngine(t, rtOpts{projectExtra: map[string]string{
		"demo": `    setup: [{name: prep, run: "test -f README.md"}]` + "\n",
	}})
	task := env.create(domain.Task{Body: "[fake:commit]"})

	got := env.waitStatus(task.ID, domain.TaskReview)
	assert.NotZero(t, got.PRNumber, "a passing setup lets the agent run and the change publish")
}

// Setup and checks run with the agent's environment: a foundry-only variable is not visible to
// them, and the variables the agent gets (FOUNDRY_TASK_ID) are.
func TestRunnerSetupAndChecksUseAgentEnvironment(t *testing.T) {
	t.Setenv("FOUNDRY_CHECK_TEST_SECRET", "s3cr3t-value")
	env := rtNewEngine(t, rtOpts{projectExtra: map[string]string{
		"demo": `    setup: [{name: env, run: "test -z \"$FOUNDRY_CHECK_TEST_SECRET\" && test \"$FOUNDRY_TASK_ID\" != \"\""}]` + "\n" +
			`    checks: [{name: env, run: "test -z \"$FOUNDRY_CHECK_TEST_SECRET\" && test \"$FOUNDRY_TASK_ID\" != \"\""}]` + "\n",
	}})
	task := env.create(domain.Task{Body: "[fake:commit]"})

	got := env.waitStatus(task.ID, domain.TaskReview)
	assert.NotZero(t, got.PRNumber, "setup and checks see the agent's environment, not foundry's secret")
}
