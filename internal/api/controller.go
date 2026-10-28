package api

import (
	"context"
	"time"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
)

// Controller is everything the API does beyond reading the store. internal/engine implements
// it. Errors wrapping store.ErrNotFound map to 404, ErrInvalid to 400, ErrConflict to 409.
type Controller interface {
	// CreateTask validates the project, fills Role from routing when empty, sets MaxAttempts
	// from limits, queues the task and wakes the scheduler.
	CreateTask(ctx context.Context, t domain.Task) (domain.Task, error)
	CancelTask(ctx context.Context, id string) error
	// RetryTask re-queues a blocked, failed or cancelled task with Attempts reset to 0.
	// A non-empty note is appended to the task body as human guidance.
	RetryTask(ctx context.Context, id, note string) error
	CancelRun(ctx context.Context, id string) error
	DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error)

	Loops(ctx context.Context) []LoopView
	// RunLoop runs one loop now; project "" runs it for every project.
	RunLoop(ctx context.Context, name, project string) error

	Config() *config.Config
	ReloadConfig(ctx context.Context) error

	// Paused reports the global pause switch. While paused no run starts and no loop acts.
	Paused() bool
	SetPaused(ctx context.Context, paused bool) error

	// SendChat appends a user message and starts the orchestrator's reply, which arrives as
	// chat.delta events and a final chat.message event. ErrConflict while a reply is running.
	SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error)
}

type LoopView struct {
	Name      string    `json:"name"`
	Project   string    `json:"project"`
	Enabled   bool      `json:"enabled"`
	Every     string    `json:"every"`
	LastRunAt time.Time `json:"last_run_at"`
	LastError string    `json:"last_error,omitempty"`
}
