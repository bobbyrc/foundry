package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bobbyrc/foundry/internal/api"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/notify"
)

// Engine is the API's controller: every owner action goes through these methods.
var _ api.Controller = (*Engine)(nil)

// CreateTask queues a task from an owner or API client. Client-supplied identity and run
// state (ID, status, attempts, branch, PR and SHA fields, timestamps) are discarded.
func (e *Engine) CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	switch t.Kind {
	case "", domain.KindFeature, domain.KindBugfix, domain.KindIssue, domain.KindIncident,
		domain.KindReview, domain.KindChore, domain.KindImprove, domain.KindQuestion:
	default:
		return domain.Task{}, fmt.Errorf("%w: unknown task kind %q", api.ErrInvalid, t.Kind)
	}
	t.ID, t.Status, t.Attempts = "", "", 0
	t.Branch, t.PRNumber, t.PRURL = "", 0, ""
	t.ApprovedSHA, t.MergeSHA = "", ""
	t.CreatedAt, t.UpdatedAt = time.Time{}, time.Time{}
	if t.Source == "" {
		t.Source = "api"
	}
	return e.createTask(ctx, t)
}

// CancelTask cancels a task that is not yet terminal, stops its live runs and rejects its
// pending approvals. The check and the status change happen in one store update.
func (e *Engine) CancelTask(ctx context.Context, id string) error {
	if _, err := e.setTask(ctx, id, "cancelled by owner", func(t *domain.Task) error {
		if t.Status.Terminal() {
			return fmt.Errorf("%w: task is %s", api.ErrConflict, t.Status)
		}
		t.Status = domain.TaskCancelled
		return nil
	}); err != nil {
		return err
	}
	for _, runID := range e.liveRunsOf(id) {
		e.cancelRun(runID, "cancelled by user")
	}
	pending, err := e.d.Store.ListApprovals(ctx, domain.ApprovalPending)
	if err != nil {
		return err
	}
	for _, a := range pending {
		if a.TaskID != id {
			continue
		}
		_, err := e.recordDecision(ctx, a.ID, domain.ApprovalRejected, "task cancelled")
		if err != nil && !errors.Is(err, api.ErrConflict) {
			return err
		}
	}
	return nil
}

// RetryTask re-queues a blocked, failed or cancelled task with its attempts reset. A note is
// appended to the body as guidance for the next agent.
func (e *Engine) RetryTask(ctx context.Context, id, note string) error {
	_, err := e.setTask(ctx, id, "re-queued by owner", func(t *domain.Task) error {
		switch t.Status {
		case domain.TaskBlocked, domain.TaskFailed, domain.TaskCancelled:
		default:
			return fmt.Errorf("%w: task is %s, not blocked, failed or cancelled", api.ErrConflict, t.Status)
		}
		t.Status = domain.TaskQueued
		t.Attempts = 0
		t.BlockedReason = ""
		if note != "" {
			t.Body += "\n\n## Note from the owner\n\n" + note
		}
		return nil
	})
	return err
}

// CancelRun stops one live run. The task is not changed by the run's own outcome, so the
// task is blocked here with the reason "run cancelled by owner", which puts it under "Needs
// you" until the owner cancels or retries it.
func (e *Engine) CancelRun(ctx context.Context, id string) error {
	r, err := e.d.Store.GetRun(ctx, id)
	if err != nil {
		return err
	}
	if !e.cancelRun(id, "cancelled by user") {
		return fmt.Errorf("%w: run is not active", api.ErrConflict)
	}
	if r.TaskID != "" {
		e.blockTask(ctx, r.TaskID, "run cancelled by owner")
	}
	return nil
}

// DecideApproval records the owner's decision. For a merge approval, approving merges the
// task's PR and rejecting cancels the task. Other kinds are only recorded.
func (e *Engine) DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error) {
	status := domain.ApprovalRejected
	if approve {
		status = domain.ApprovalApproved
	}
	a, err := e.recordDecision(ctx, id, status, note)
	if err != nil {
		return domain.Approval{}, err
	}
	if a.Kind != "merge" {
		return a, nil
	}
	t, err := e.d.Store.GetTask(ctx, a.TaskID)
	if err != nil {
		return a, fmt.Errorf("approval %s: task: %w", a.ID, err)
	}
	if !approve {
		reason := "merge rejected by owner"
		if note != "" {
			reason += ": " + note
		}
		if _, err := e.setTask(ctx, t.ID, reason, func(x *domain.Task) error {
			x.Status = domain.TaskCancelled
			return nil
		}); err != nil {
			return a, err
		}
		return a, nil
	}
	if err := e.mergeTask(ctx, t); err != nil {
		e.blockTask(ctx, t.ID, "merge failed after approval: "+err.Error())
		return a, fmt.Errorf("merge failed after approval: %w", err)
	}
	return a, nil
}

// ReloadConfig re-reads the config file the engine was loaded from and swaps it in. A file
// that fails to load or validate is reported as api.ErrInvalid and the current config stays.
func (e *Engine) ReloadConfig(ctx context.Context) error {
	if e.d.ReloadConfig == nil {
		return fmt.Errorf("%w: reload not supported", api.ErrInvalid)
	}
	path := e.Config().Path
	cfg, err := e.d.ReloadConfig(path)
	if err != nil {
		return fmt.Errorf("%w: %v", api.ErrInvalid, err)
	}
	e.cfgMu.Lock()
	e.cfg = cfg
	e.cfgMu.Unlock()
	e.d.Bus.Emit(ctx, domain.EvSystem, "", "", "", "config reloaded from "+path, nil)
	e.wakeScheduler()
	return nil
}

// SetPaused persists the pause switch. Pausing does not cancel running runs; they finish and
// nothing new starts.
func (e *Engine) SetPaused(ctx context.Context, paused bool) error {
	cursor := ""
	title := "factory resumed"
	if paused {
		cursor = "paused"
		title = "factory paused — running agents finish, nothing new starts"
	}
	if err := e.d.Store.SaveLoopState(ctx, domain.LoopState{Name: controlLoop, Cursor: cursor}); err != nil {
		return err
	}
	e.paused.Store(paused)
	e.d.Bus.Emit(ctx, domain.EvSystem, "", "", "", title, nil)
	e.notify(ctx, notify.PriorityNormal, title, "", "/")
	if !paused {
		e.wakeScheduler()
	}
	return nil
}

// SendChat stores the owner's message and starts the orchestrator's reply in the background.
// The conversation is marked busy in the same store update that checks it, so two sends
// cannot both start a reply.
func (e *Engine) SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return domain.ChatMessage{}, fmt.Errorf("%w: message is empty", api.ErrInvalid)
	}
	conv, err := e.d.Store.UpdateConversation(ctx, conversationID, func(c *domain.Conversation) error {
		if c.Busy {
			return fmt.Errorf("%w: the orchestrator is still replying", api.ErrConflict)
		}
		c.Busy = true
		if c.Title == "" {
			c.Title = firstRunes(text, 60)
		}
		return nil
	})
	if err != nil {
		return domain.ChatMessage{}, err
	}
	msg, err := e.d.Store.AppendChatMessage(ctx, domain.ChatMessage{
		ConversationID: conv.ID, Role: "user", Text: text,
	})
	if err != nil {
		e.releaseConversation(ctx, conv.ID)
		return domain.ChatMessage{}, err
	}
	e.d.Bus.Emit(ctx, domain.EvChatMessage, conv.Project, "", "", "message from owner", msg)

	// The reply outlives the request, so it does not use the request's context.
	timeout := e.Config().Limits.RunTimeout.Duration
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		runCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		e.chatReply(runCtx, conv, text)
	}()
	return msg, nil
}

// recordDecision moves a pending approval to status and emits the decision. A decision on an
// approval that is no longer pending is api.ErrConflict.
func (e *Engine) recordDecision(ctx context.Context, id string, status domain.ApprovalStatus, note string) (domain.Approval, error) {
	a, err := e.d.Store.UpdateApproval(ctx, id, func(a *domain.Approval) error {
		if a.Status != domain.ApprovalPending {
			return fmt.Errorf("%w: approval is already %s", api.ErrConflict, a.Status)
		}
		a.Status = status
		a.Note = note
		now := e.d.Now().UTC()
		a.DecidedAt = &now
		return nil
	})
	if err != nil {
		return domain.Approval{}, err
	}
	e.d.Bus.Emit(ctx, domain.EvApprovalDecided, a.Project, a.TaskID, "", a.Title+": "+string(status), a)
	return a, nil
}

// liveRunsOf returns the IDs of the live runs of a task.
func (e *Engine) liveRunsOf(taskID string) []string {
	e.runsMu.Lock()
	defer e.runsMu.Unlock()
	var ids []string
	for id, r := range e.active {
		if r.taskID == taskID {
			ids = append(ids, id)
		}
	}
	return ids
}

// releaseConversation clears the busy flag after a reply could not start.
func (e *Engine) releaseConversation(ctx context.Context, id string) {
	if _, err := e.d.Store.UpdateConversation(ctx, id, func(c *domain.Conversation) error {
		c.Busy = false
		return nil
	}); err != nil {
		e.d.Logger.Error("chat: release conversation", "conversation", id, "err", err)
	}
}

// firstRunes returns at most the first n runes of s.
func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
