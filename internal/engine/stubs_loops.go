package engine

// Stubs owned by the loops unit (loops.go, loop_*.go). That unit deletes this file once its
// real implementations exist. Signatures are frozen.

import (
	"context"

	"github.com/bobbyrc/foundry/internal/api"
	"github.com/bobbyrc/foundry/internal/domain"
)

// startLoops starts the loop goroutines (tracked by e.wg) and returns.
func (e *Engine) startLoops(ctx context.Context) {}

// kickLoop asks a loop to run soon for one project. Never blocks.
func (e *Engine) kickLoop(name, project string) {}

// mergeTask merges the task's PR, moves the task to merged, emits pr.merged and kicks the
// deploy watch. Used by the shepherd (auto-merge) and by approval decisions.
func (e *Engine) mergeTask(ctx context.Context, t domain.Task) error { panic("unimplemented") }

// Loops implements api.Controller.
func (e *Engine) Loops(ctx context.Context) []api.LoopView { panic("unimplemented") }

// RunLoop implements api.Controller.
func (e *Engine) RunLoop(ctx context.Context, name, project string) error { panic("unimplemented") }
