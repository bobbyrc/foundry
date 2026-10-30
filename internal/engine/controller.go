package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/notify"
	"github.com/bobbyrc/onett/internal/store"
)

// Engine is the API's controller: every owner action goes through these methods.
var _ api.Controller = (*Engine)(nil)

// CreateTask queues a task from an owner or API client. Client-supplied identity and run
// state (ID, status, attempts, branch, PR and SHA fields, risk, timestamps) are discarded.
// Review tasks are created by onett when a pull request is ready, never by a client.
func (e *Engine) CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	switch t.Kind {
	case "", domain.KindFeature, domain.KindBugfix, domain.KindIssue, domain.KindIncident,
		domain.KindChore, domain.KindImprove, domain.KindQuestion:
	case domain.KindReview:
		return domain.Task{}, fmt.Errorf("%w: review tasks are created by onett", api.ErrInvalid)
	default:
		return domain.Task{}, fmt.Errorf("%w: unknown task kind %q", api.ErrInvalid, t.Kind)
	}
	t.ID, t.Status, t.Attempts = "", "", 0
	t.Branch, t.PRNumber, t.PRURL = "", 0, ""
	t.ApprovedSHA, t.MergeSHA = "", ""
	t.Risk, t.RiskReasons = domain.RiskUnknown, nil
	t.CreatedAt, t.UpdatedAt = time.Time{}, time.Time{}
	if t.Source == "" {
		t.Source = "api"
	}
	return e.createTask(ctx, t)
}

// cancellableStatuses are the statuses a cancel may move a task out of: every status except
// done, failed and cancelled.
var cancellableStatuses = []domain.TaskStatus{
	domain.TaskQueued, domain.TaskRunning, domain.TaskReview, domain.TaskAwaitingApproval,
	domain.TaskMerged, domain.TaskDeploying, domain.TaskBlocked,
}

// CancelTask cancels a task that is not done, failed or already cancelled (a blocked task may be
// cancelled). It also cancels the task's open children, such as an in-flight review, and stops
// their runs. The status check and the change happen in one store update.
func (e *Engine) CancelTask(ctx context.Context, id string) error {
	if _, err := e.setTask(ctx, id, "cancelled by owner", func(t *domain.Task) error {
		switch t.Status {
		case domain.TaskDone, domain.TaskFailed, domain.TaskCancelled:
			return fmt.Errorf("%w: task is %s", api.ErrConflict, t.Status)
		}
		t.Status = domain.TaskCancelled
		return nil
	}); err != nil {
		return err
	}
	return e.cancelTree(ctx, id)
}

// cancelTree stops everything under task id after it was cancelled: its live runs stop and its
// pending approvals are rejected, then each child is cancelled the same way, recursively. A
// child already in a terminal state is left as it is.
func (e *Engine) cancelTree(ctx context.Context, id string) error {
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

	children, err := e.d.Store.ListTasks(ctx, store.TaskFilter{ParentID: id, Limit: 10000})
	if err != nil {
		return err
	}
	for _, c := range children {
		if _, _, err := e.setTaskFrom(ctx, c.ID, cancellableStatuses, "cancelled with its parent task",
			func(x *domain.Task) error {
				x.Status = domain.TaskCancelled
				return nil
			}); err != nil {
			return err
		}
		if err := e.cancelTree(ctx, c.ID); err != nil {
			return err
		}
	}
	return nil
}

// RetryTask re-queues a blocked, failed or cancelled task with its attempts reset. A note is
// appended to the body as guidance for the next agent. Reviews of the old attempt that have not
// finished are cancelled, so the new head gets a review of its own.
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
	if err != nil {
		return err
	}
	return e.cancelStaleReviews(ctx, id)
}

// cancelStaleReviews cancels the review children of task id that have not finished: a review
// that is queued, running or blocked judges an attempt that is being replaced. A review that
// ended (done) stands as it was. A review's run is stopped as well, so two runs never share a
// branch.
func (e *Engine) cancelStaleReviews(ctx context.Context, id string) error {
	children, err := e.d.Store.ListTasks(ctx, store.TaskFilter{ParentID: id, Limit: 10000})
	if err != nil {
		return err
	}
	for _, c := range children {
		if c.Kind != domain.KindReview {
			continue
		}
		if _, _, err := e.setTaskFrom(ctx, c.ID,
			[]domain.TaskStatus{domain.TaskQueued, domain.TaskRunning, domain.TaskBlocked},
			"cancelled: the task was retried", func(x *domain.Task) error {
				x.Status = domain.TaskCancelled
				return nil
			}); err != nil {
			return err
		}
		if err := e.cancelTree(ctx, c.ID); err != nil {
			return err
		}
	}
	return nil
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

// DecideApproval records the owner's decision. For a merge approval the task must be awaiting
// approval; approving merges the task's PR at the head the approval was requested for, and
// rejecting cancels the task. The decision is recorded only once the checks pass, so a refused
// approval stays pending. Other kinds are only recorded.
func (e *Engine) DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error) {
	status := domain.ApprovalRejected
	if approve {
		status = domain.ApprovalApproved
	}
	cur, err := e.d.Store.GetApproval(ctx, id)
	if err != nil {
		return domain.Approval{}, err
	}
	if cur.Kind != "merge" {
		return e.recordDecision(ctx, id, status, note)
	}
	if cur.Status != domain.ApprovalPending {
		return domain.Approval{}, fmt.Errorf("%w: approval is already %s", api.ErrConflict, cur.Status)
	}
	t, err := e.d.Store.GetTask(ctx, cur.TaskID)
	if err != nil {
		return domain.Approval{}, fmt.Errorf("approval %s: task: %w", cur.ID, err)
	}
	if t.Status != domain.TaskAwaitingApproval {
		return domain.Approval{}, fmt.Errorf("%w: task is %s, not awaiting approval", api.ErrConflict, t.Status)
	}

	if !approve {
		a, err := e.recordDecision(ctx, id, status, note)
		if err != nil {
			return a, err
		}
		reason := "merge rejected by owner"
		if note != "" {
			reason += ": " + note
		}
		if _, _, err := e.setTaskFrom(ctx, t.ID, []domain.TaskStatus{domain.TaskAwaitingApproval}, reason,
			func(x *domain.Task) error {
				x.Status = domain.TaskCancelled
				return nil
			}); err != nil {
			return a, err
		}
		return a, nil
	}

	if cur.HeadSHA == "" {
		// Without the head the approval cannot say what it covers, so it must not merge. The
		// approval stays pending; the task goes back to review, where the shepherd re-requests it.
		const msg = "approval has no head commit; onett will re-check it"
		if _, _, err := e.setTaskFrom(ctx, t.ID, []domain.TaskStatus{domain.TaskAwaitingApproval}, msg,
			func(x *domain.Task) error {
				x.Status = domain.TaskReview
				return nil
			}); err != nil {
			return cur, err
		}
		return cur, fmt.Errorf("%w: %s", api.ErrConflict, msg)
	}

	p, err := e.project(t.Project)
	if err != nil {
		return domain.Approval{}, err
	}
	s, err := e.d.SCMFor(p)
	if err != nil {
		return domain.Approval{}, fmt.Errorf("scm: %w", err)
	}
	pr, err := s.GetPR(ctx, t.PRNumber)
	if err != nil {
		return domain.Approval{}, fmt.Errorf("read PR #%d: %w", t.PRNumber, err)
	}
	if pr.HeadSHA != cur.HeadSHA {
		// The reviewed code is no longer what would merge: the approval does not cover it.
		const msg = "the pull request changed after this approval was requested; onett will re-check it"
		a, err := e.recordDecision(ctx, id, domain.ApprovalRejected, msg)
		if err != nil {
			return a, err
		}
		if _, _, err := e.setTaskFrom(ctx, t.ID, []domain.TaskStatus{domain.TaskAwaitingApproval}, msg,
			func(x *domain.Task) error {
				x.Status = domain.TaskReview
				return nil
			}); err != nil {
			return a, err
		}
		return a, fmt.Errorf("%w: %s", api.ErrConflict, msg)
	}

	a, err := e.recordDecision(ctx, id, domain.ApprovalApproved, note)
	if err != nil {
		return a, err
	}
	if err := e.mergeTask(ctx, t.ID, cur.HeadSHA); err != nil {
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

// SetProjectPaused pauses or resumes one project at runtime, persisted across restarts. Pausing
// does not cancel running runs; nothing new starts for the project. A project paused in
// onett.yaml cannot be resumed here: the file is the source of that pause.
func (e *Engine) SetProjectPaused(ctx context.Context, name string, paused bool) error {
	if e.Config().Projects[name] == nil {
		return fmt.Errorf("%w: unknown project %q", api.ErrInvalid, name)
	}
	if !paused && e.Config().Projects[name].Paused {
		return fmt.Errorf("%w: project %q is paused in onett.yaml; edit the file to resume", api.ErrConflict, name)
	}
	cursor := ""
	msg := fmt.Sprintf("project %s resumed", name)
	if paused {
		cursor = "paused"
		msg = fmt.Sprintf("project %s paused by owner — queued work will not start", name)
	}
	if err := e.d.Store.SaveLoopState(ctx, domain.LoopState{Name: controlLoop, Project: name, Cursor: cursor}); err != nil {
		return err
	}
	e.projMu.Lock()
	e.projPaused[name] = paused
	e.projMu.Unlock()
	e.d.Bus.Emit(ctx, domain.EvSystem, name, "", "", msg, nil)
	if !paused {
		e.wakeScheduler()
	}
	return nil
}

// SendChat stores the owner's message. While the orchestrator is replying, a live agent that
// takes input gets the message at once; otherwise it is queued and goes out with the next turn.
// When idle it starts a turn in the background, carrying any queued messages too. The busy flag
// is set in the same store update that checks it, so two sends cannot both start a turn.
func (e *Engine) SendChat(ctx context.Context, conversationID, text string) (domain.ChatMessage, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return domain.ChatMessage{}, fmt.Errorf("%w: message is empty", api.ErrInvalid)
	}
	cur, err := e.d.Store.GetConversation(ctx, conversationID)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	if cur.Busy && cur.ActiveRunID != "" {
		switch err := e.deliverToRun(ctx, cur.ActiveRunID, text); {
		case err == nil:
			return e.storeOwnerMessage(ctx, cur, domain.ChatMessage{
				ConversationID: cur.ID, Role: "user", Text: text, RunID: cur.ActiveRunID,
			})
		case errors.Is(err, errRunNotLive), errors.Is(err, harness.ErrInputClosed):
			// The agent cannot take it now; it waits in the queue.
		default:
			e.d.Logger.Warn("chat: deliver to agent", "conversation", cur.ID, "err", err)
		}
	}

	wasBusy := false
	conv, err := e.d.Store.UpdateConversation(ctx, conversationID, func(c *domain.Conversation) error {
		wasBusy = c.Busy
		c.Busy = true
		if c.Title == "" {
			c.Title = firstRunes(text, 60)
		}
		return nil
	})
	if err != nil {
		return domain.ChatMessage{}, err
	}
	if wasBusy {
		msg, err := e.storeOwnerMessage(ctx, conv, domain.ChatMessage{
			ConversationID: conv.ID, Role: "user", Text: text, Queued: true,
		})
		if err != nil {
			return domain.ChatMessage{}, err
		}
		// The turn may have ended between the check and the store; nobody would send it then.
		if now, err := e.d.Store.GetConversation(ctx, conv.ID); err == nil && !now.Busy {
			if err := e.FlushChat(ctx, conv.ID); err != nil && !errors.Is(err, errStopped) {
				e.d.Logger.Error("chat: send queued messages", "conversation", conv.ID, "err", err)
			}
		}
		return msg, nil
	}

	// Idle: this turn takes the queue plus the new message. Claiming happens first, so an edit
	// or cancel of a queued message either lands before (and is respected) or conflicts.
	queued, err := e.claimQueued(ctx, conv.ID)
	if err != nil {
		e.releaseConversation(ctx, conv.ID)
		return domain.ChatMessage{}, err
	}
	msg, err := e.storeOwnerMessage(ctx, conv, domain.ChatMessage{
		ConversationID: conv.ID, Role: "user", Text: text,
	})
	if err != nil {
		e.restoreQueued(ctx, queued)
		e.releaseConversation(ctx, conv.ID)
		return domain.ChatMessage{}, err
	}
	texts, ids := messageTexts(queued)
	texts, ids = append(texts, text), append(ids, msg.ID)
	if err := e.startTurn(ctx, conv, texts, ids); err != nil {
		e.restoreQueued(ctx, queued)
		return domain.ChatMessage{}, err
	}
	return msg, nil
}

// storeOwnerMessage appends an owner message and announces it.
func (e *Engine) storeOwnerMessage(ctx context.Context, conv domain.Conversation, m domain.ChatMessage) (domain.ChatMessage, error) {
	msg, err := e.d.Store.AppendChatMessage(ctx, m)
	if err != nil {
		if m.RunID == "" && !m.Queued {
			e.releaseConversation(ctx, conv.ID)
		}
		return domain.ChatMessage{}, err
	}
	e.d.Bus.Emit(ctx, domain.EvChatMessage, conv.Project, "", msg.RunID, "message from owner", msg)
	return msg, nil
}

// claimQueued takes the conversation's queued owner messages, oldest first, so they can no
// longer be edited or cancelled. The caller holds the busy flag.
func (e *Engine) claimQueued(ctx context.Context, conversationID string) ([]domain.ChatMessage, error) {
	all, err := e.d.Store.ListChatMessages(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	var claimed []domain.ChatMessage
	for _, m := range all {
		if !m.Queued {
			continue
		}
		got, err := e.d.Store.UpdateChatMessage(ctx, m.ID, func(m *domain.ChatMessage) error {
			m.Queued = false
			return nil
		})
		if errors.Is(err, store.ErrNotFound) {
			continue // cancelled meanwhile
		}
		if err != nil {
			e.restoreQueued(ctx, claimed)
			return nil, err
		}
		claimed = append(claimed, got)
	}
	return claimed, nil
}

// restoreQueued puts claimed messages back in the queue after their turn could not start.
func (e *Engine) restoreQueued(ctx context.Context, msgs []domain.ChatMessage) {
	for _, m := range msgs {
		if _, err := e.d.Store.UpdateChatMessage(context.WithoutCancel(ctx), m.ID, func(m *domain.ChatMessage) error {
			m.Queued = true
			return nil
		}); err != nil && !errors.Is(err, store.ErrNotFound) {
			e.d.Logger.Error("chat: requeue message", "message", m.ID, "err", err)
		}
	}
}

func messageTexts(msgs []domain.ChatMessage) (texts, ids []string) {
	for _, m := range msgs {
		texts = append(texts, m.Text)
		ids = append(ids, m.ID)
	}
	return texts, ids
}

// startTurn runs one orchestrator turn in the background with the given owner message texts
// joined as its prompt. The conversation is already busy. On failure to start it clears the flag.
func (e *Engine) startTurn(ctx context.Context, conv domain.Conversation, texts, ids []string) error {
	prompt := strings.Join(texts, "\n\n")
	// The reply outlives the request, so it does not use the request's context. It derives from
	// Run's context instead, so shutdown cancels it.
	timeout := e.Config().Limits.RunTimeout.Duration
	base := e.replyBase()
	started := e.goTracked(func() {
		runCtx, cancel := context.WithTimeout(base, timeout)
		defer cancel()
		e.chatReply(runCtx, conv, prompt, ids)
	})
	if !started {
		e.releaseConversation(context.WithoutCancel(ctx), conv.ID)
		return errStopped
	}
	return nil
}

// FlushChat starts a turn from the queued messages now, instead of waiting for the current
// turn to succeed. It does nothing when the queue is empty or a turn is already running.
func (e *Engine) FlushChat(ctx context.Context, conversationID string) error {
	cur, err := e.d.Store.GetConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	if cur.Busy {
		return nil
	}
	won := false
	conv, err := e.d.Store.UpdateConversation(ctx, conversationID, func(c *domain.Conversation) error {
		if !c.Busy {
			c.Busy, won = true, true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !won {
		return nil
	}
	queued, err := e.claimQueued(ctx, conv.ID)
	if err != nil {
		e.releaseConversation(ctx, conv.ID)
		return err
	}
	if len(queued) == 0 {
		e.releaseConversation(ctx, conv.ID)
		return nil
	}
	texts, ids := messageTexts(queued)
	if err := e.startTurn(ctx, conv, texts, ids); err != nil {
		e.restoreQueued(ctx, queued)
		return err
	}
	return nil
}

// EditQueuedMessage changes the text of a message that has not been delivered yet.
// api.ErrConflict once it has.
func (e *Engine) EditQueuedMessage(ctx context.Context, id, text string) (domain.ChatMessage, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return domain.ChatMessage{}, fmt.Errorf("%w: message is empty", api.ErrInvalid)
	}
	msg, err := e.d.Store.UpdateChatMessage(ctx, id, func(m *domain.ChatMessage) error {
		if !m.Queued {
			return fmt.Errorf("%w: the message was already delivered", api.ErrConflict)
		}
		m.Text = text
		return nil
	})
	if err != nil {
		return domain.ChatMessage{}, err
	}
	e.emitMessage(ctx, msg)
	return msg, nil
}

// CancelQueuedMessage removes a message that has not been delivered yet. api.ErrConflict once
// it has.
func (e *Engine) CancelQueuedMessage(ctx context.Context, id string) error {
	var msg domain.ChatMessage
	if _, err := e.d.Store.UpdateChatMessage(ctx, id, func(m *domain.ChatMessage) error {
		if !m.Queued {
			return fmt.Errorf("%w: the message was already delivered", api.ErrConflict)
		}
		msg = *m
		return nil
	}); err != nil {
		return err
	}
	if err := e.d.Store.DeleteChatMessage(ctx, id); err != nil {
		return err
	}
	e.emitMessage(ctx, msg)
	return nil
}

// emitMessage announces a changed chat message; the UI refetches the conversation on it.
func (e *Engine) emitMessage(ctx context.Context, msg domain.ChatMessage) {
	proj := ""
	if conv, err := e.d.Store.GetConversation(ctx, msg.ConversationID); err == nil {
		proj = conv.Project
	}
	e.d.Bus.Emit(ctx, domain.EvChatMessage, proj, "", msg.RunID, "chat message changed", msg)
}

// replyBase is the context chat replies derive from: Run's context once Run has started, so
// shutdown cancels in-flight replies, and Background before that.
func (e *Engine) replyBase() context.Context {
	e.lifeMu.Lock()
	defer e.lifeMu.Unlock()
	if e.baseCtx == nil {
		return context.Background()
	}
	return e.baseCtx
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
		c.ActiveRunID = ""
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
