// Package store persists foundry's runtime state. store/sqlite implements Store.
package store

import (
	"context"
	"errors"

	"github.com/bobbyrc/foundry/internal/domain"
)

// ErrNotFound is returned (wrapped is fine) when a lookup by ID or key finds nothing.
var ErrNotFound = errors.New("not found")

type TaskFilter struct {
	Project  string
	Statuses []domain.TaskStatus // empty = all
	Kind     domain.TaskKind
	ParentID string
	Limit    int // 0 = 200
}

type RunFilter struct {
	TaskID   string
	Project  string
	Statuses []domain.RunStatus
	Limit    int // 0 = 200
}

type EventFilter struct {
	AfterID int64
	Project string
	TaskID  string
	Limit   int // 0 = 500
}

// Store is safe for concurrent use. List methods return newest first unless noted.
// Create methods assign ID (UUIDv7) and timestamps when they are empty and return the stored
// value. Update methods load the row, call fn, and save the result in one transaction; returning
// an error from fn aborts the update.
type Store interface {
	Close() error

	CreateTask(ctx context.Context, t domain.Task) (domain.Task, error)
	GetTask(ctx context.Context, id string) (domain.Task, error)
	// FindTaskBySourceRef returns the newest task in project with that source_ref.
	FindTaskBySourceRef(ctx context.Context, project, sourceRef string) (domain.Task, error)
	ListTasks(ctx context.Context, f TaskFilter) ([]domain.Task, error)
	UpdateTask(ctx context.Context, id string, fn func(*domain.Task) error) (domain.Task, error)
	// ClaimNextTask atomically moves the highest-priority, oldest queued task whose project is
	// not in busyProjects to running, increments Attempts, and returns it. ErrNotFound when none.
	ClaimNextTask(ctx context.Context, busyProjects []string) (domain.Task, error)

	CreateRun(ctx context.Context, r domain.Run) (domain.Run, error)
	GetRun(ctx context.Context, id string) (domain.Run, error)
	ListRuns(ctx context.Context, f RunFilter) ([]domain.Run, error)
	UpdateRun(ctx context.Context, id string, fn func(*domain.Run) error) (domain.Run, error)
	// AppendRunLogs assigns Seq to each line (continuing the run's sequence) and stores them.
	AppendRunLogs(ctx context.Context, runID string, lines []domain.LogLine) ([]domain.LogLine, error)
	// ListRunLogs returns lines with Seq > afterSeq in ascending order.
	ListRunLogs(ctx context.Context, runID string, afterSeq int64, limit int) ([]domain.LogLine, error)

	// AppendEvent assigns ID and Time (if zero) and stores the event.
	AppendEvent(ctx context.Context, e domain.Event) (domain.Event, error)
	// ListEvents returns events with ID > AfterID; ascending when AfterID > 0, else the newest
	// Limit events in ascending order.
	ListEvents(ctx context.Context, f EventFilter) ([]domain.Event, error)

	CreateApproval(ctx context.Context, a domain.Approval) (domain.Approval, error)
	GetApproval(ctx context.Context, id string) (domain.Approval, error)
	ListApprovals(ctx context.Context, status domain.ApprovalStatus) ([]domain.Approval, error)
	UpdateApproval(ctx context.Context, id string, fn func(*domain.Approval) error) (domain.Approval, error)

	CreateDeployment(ctx context.Context, d domain.Deployment) (domain.Deployment, error)
	GetDeployment(ctx context.Context, id string) (domain.Deployment, error)
	ListDeployments(ctx context.Context, project string, limit int) ([]domain.Deployment, error)
	UpdateDeployment(ctx context.Context, id string, fn func(*domain.Deployment) error) (domain.Deployment, error)
	// LastHealthyDeployment returns the newest deployment of project in state healthy.
	LastHealthyDeployment(ctx context.Context, project string) (domain.Deployment, error)

	// UpsertSignal inserts a new signal or updates count/last_seen/title/url/level of the one
	// with the same (project, source, fingerprint). isNew reports an insert.
	UpsertSignal(ctx context.Context, s domain.Signal) (out domain.Signal, isNew bool, err error)
	ListSignals(ctx context.Context, project string, limit int) ([]domain.Signal, error)
	UpdateSignal(ctx context.Context, id string, fn func(*domain.Signal) error) (domain.Signal, error)

	// UpsertFriction inserts or increments Count of the friction with the same
	// (project, fingerprint). isNew reports an insert.
	UpsertFriction(ctx context.Context, f domain.Friction) (out domain.Friction, isNew bool, err error)
	ListFriction(ctx context.Context, project string, limit int) ([]domain.Friction, error)
	UpdateFriction(ctx context.Context, id string, fn func(*domain.Friction) error) (domain.Friction, error)

	CreateConversation(ctx context.Context, c domain.Conversation) (domain.Conversation, error)
	GetConversation(ctx context.Context, id string) (domain.Conversation, error)
	ListConversations(ctx context.Context, limit int) ([]domain.Conversation, error)
	UpdateConversation(ctx context.Context, id string, fn func(*domain.Conversation) error) (domain.Conversation, error)
	AppendChatMessage(ctx context.Context, m domain.ChatMessage) (domain.ChatMessage, error)
	// ListChatMessages returns a conversation's messages in ascending order.
	ListChatMessages(ctx context.Context, conversationID string) ([]domain.ChatMessage, error)

	// GetLoopState returns a zero LoopState (Name and Project set) when none is stored.
	GetLoopState(ctx context.Context, name, project string) (domain.LoopState, error)
	SaveLoopState(ctx context.Context, s domain.LoopState) error
	ListLoopStates(ctx context.Context) ([]domain.LoopState, error)
}
