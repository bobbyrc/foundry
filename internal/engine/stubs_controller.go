package engine

// Stubs owned by the controller unit (controller.go, chat.go). That unit deletes this file once
// its real implementations exist. Signatures are frozen by api.Controller.

import (
	"context"

	"github.com/bobbyrc/foundry/internal/domain"
)

func (e *Engine) CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	panic("unimplemented")
}
func (e *Engine) CancelTask(ctx context.Context, id string) error      { panic("unimplemented") }
func (e *Engine) RetryTask(ctx context.Context, id, note string) error { panic("unimplemented") }
func (e *Engine) CancelRun(ctx context.Context, id string) error       { panic("unimplemented") }
func (e *Engine) ReloadConfig(ctx context.Context) error               { panic("unimplemented") }
func (e *Engine) SetPaused(ctx context.Context, paused bool) error     { panic("unimplemented") }
func (e *Engine) DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error) {
	panic("unimplemented")
}
func (e *Engine) SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error) {
	panic("unimplemented")
}
