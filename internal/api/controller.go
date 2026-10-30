package api

import (
	"context"
	"time"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
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
	// TaskConversation returns the task agent conversation that taskID belongs to: the root
	// task, follow-up tasks, the agent's runs, messages, and whether the owner can message it.
	TaskConversation(ctx context.Context, taskID string) (domain.TaskConversation, error)
	// AgentConversations lists the task agents the owner can talk to, open ones first.
	AgentConversations(ctx context.Context) ([]domain.AgentConversation, error)
	// SendTaskMessage sends the owner's text to the agent that works on taskID. A live agent that
	// takes input gets it at once; otherwise it is queued and the agent resumes its session with
	// it. ErrConflict, with the reason, when the agent cannot be messaged.
	SendTaskMessage(ctx context.Context, taskID, text string) (domain.ChatMessage, error)
	// FlushTaskMessages starts the follow-up for queued messages now, instead of waiting for the
	// agent's run to succeed. It does nothing when no message is queued.
	FlushTaskMessages(ctx context.Context, taskID string) error
	DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error)

	Loops(ctx context.Context) []LoopView
	// RunLoop runs one loop now; project "" runs it for every project.
	RunLoop(ctx context.Context, name, project string) error

	Config() *config.Config
	ReloadConfig(ctx context.Context) error

	// Paused reports the global pause switch. While paused no run starts and no loop acts.
	Paused() bool
	SetPaused(ctx context.Context, paused bool) error
	// ProjectPausedBy reports why a project is paused: "config" (paused: true in onett.yaml,
	// which the API cannot change), "owner" (paused at runtime through the API) or "".
	ProjectPausedBy(project string) string
	// SetProjectPaused pauses or resumes one project at runtime (persisted across restarts).
	// ErrInvalid for an unknown project; resuming a project paused in config is ErrConflict.
	SetProjectPaused(ctx context.Context, project string, paused bool) error

	// SendChat appends a user message. Idle, it starts the orchestrator's reply, which arrives
	// as chat.delta events and a final chat.message event. While a reply is running the message
	// goes straight to an agent that takes input, or else is queued for the next turn.
	SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error)
	// FlushChat starts a turn from the queued messages now. It does nothing when the queue is
	// empty or a turn is running.
	FlushChat(ctx context.Context, conversationID string) error
	// EditQueuedMessage and CancelQueuedMessage change a message not yet delivered. ErrConflict
	// once it has been; ErrNotFound for an unknown message.
	EditQueuedMessage(ctx context.Context, id, text string) (domain.ChatMessage, error)
	CancelQueuedMessage(ctx context.Context, id string) error

	// AgentPrompt is an agent's instructions: its inline PromptText, else the file or builtin
	// its Prompt names. BuiltinPrompt is the embedded prompt of one built-in agent.
	AgentPrompt(a config.Agent) (string, error)
	BuiltinPrompt(name string) (string, error)
}

type LoopView struct {
	Name      string    `json:"name"`
	Project   string    `json:"project"`
	Enabled   bool      `json:"enabled"`
	Every     string    `json:"every"`
	LastRunAt time.Time `json:"last_run_at"`
	LastError string    `json:"last_error,omitempty"`
}
