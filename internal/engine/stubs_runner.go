package engine

// Stubs owned by the runner unit (scheduler.go, runner.go, supervise.go, prompt.go). That unit
// deletes this file once its real implementations exist. Signatures are frozen.

import (
	"context"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/harness"
)

// runScheduler claims queued tasks and starts runs until ctx is done.
func (e *Engine) runScheduler(ctx context.Context) { <-ctx.Done() }

// registerRun records a live run so it can be cancelled and counted against limits.
// taskID is "" for chat runs, which do not count against limits.
func (e *Engine) registerRun(runID, taskID, project string, cancel context.CancelFunc) *activeRun {
	panic("unimplemented")
}

// unregisterRun forgets a finished run and wakes the scheduler.
func (e *Engine) unregisterRun(runID string) { panic("unimplemented") }

// supervise streams a session into the run's log (store + run.log events), enforces the stall
// and denial rules (cancelling the session and recording the reason on ar), waits for the
// session, and stores the run's final state. onEvent (may be nil) sees every event. It returns
// the harness result, the run's final status and the reason ("" for a normal end).
func (e *Engine) supervise(ctx context.Context, run domain.Run, sess harness.Session, ar *activeRun, onEvent func(harness.Event)) (harness.Result, domain.RunStatus, string) {
	panic("unimplemented")
}

// loadPrompt returns a role prompt: "builtin:<name>" from the embedded prompts directory, or
// the contents of a Markdown file path (read on every call, so edits apply without restart).
func (e *Engine) loadPrompt(ref string) (string, error) { panic("unimplemented") }

// commonPrompt returns prompts/common.md, the rules and report contract every worker gets.
func (e *Engine) commonPrompt() string { panic("unimplemented") }
