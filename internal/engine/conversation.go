package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/store"
)

// agentConversationWindow is how long a closed conversation stays in the list after its last
// activity.
const agentConversationWindow = 7 * 24 * time.Hour

// agentConversationLimit caps the list of task agent conversations.
const agentConversationLimit = 100

// conversationRoot returns the task that starts t's conversation: it follows ParentID while the
// task is a follow-up the owner started from the conversation (Source is SourceConversation).
// Any other task, with or without a parent, is its own root.
func (e *Engine) conversationRoot(ctx context.Context, t domain.Task) (domain.Task, error) {
	seen := map[string]bool{t.ID: true}
	for t.Source == domain.SourceConversation && t.ParentID != "" {
		parent, err := e.d.Store.GetTask(ctx, t.ParentID)
		if err != nil {
			return domain.Task{}, err
		}
		if seen[parent.ID] { // a cycle would loop forever
			break
		}
		seen[parent.ID] = true
		t = parent
	}
	return t, nil
}

// conversationLineage is a root task, its conversation follow-ups, and the agent's runs.
type conversationLineage struct {
	tasks      []domain.Task  // root first, then follow-ups, oldest first
	runs       []domain.Run   // runs of root.Role across tasks, oldest first
	runsByTask map[string]int // number of runs of any role, per task
	live       string         // a lineage run registered as active, or ""
}

// lineage gathers the root's conversation follow-ups (children with Source SourceConversation,
// recursively) and the runs of the root's role across all of them.
func (e *Engine) lineage(ctx context.Context, root domain.Task) (conversationLineage, error) {
	l := conversationLineage{tasks: []domain.Task{root}, runsByTask: map[string]int{}}
	queue := []string{root.ID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		children, err := e.d.Store.ListTasks(ctx, store.TaskFilter{ParentID: id, Limit: 1000})
		if err != nil {
			return l, err
		}
		for _, c := range children {
			if c.Source == domain.SourceConversation {
				l.tasks = append(l.tasks, c)
				queue = append(queue, c.ID)
			}
		}
	}
	sort.SliceStable(l.tasks[1:], func(i, j int) bool {
		a, b := l.tasks[1+i], l.tasks[1+j]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	for _, t := range l.tasks {
		runs, err := e.d.Store.ListRuns(ctx, store.RunFilter{TaskID: t.ID, Limit: 1000})
		if err != nil {
			return l, err
		}
		l.runsByTask[t.ID] = len(runs)
		for _, r := range runs {
			if r.Role == root.Role {
				l.runs = append(l.runs, r)
			}
		}
	}
	sort.SliceStable(l.runs, func(i, j int) bool {
		a, b := l.runs[i], l.runs[j]
		if !a.StartedAt.Equal(b.StartedAt) {
			return a.StartedAt.Before(b.StartedAt)
		}
		return a.ID < b.ID
	})
	e.runsMu.Lock()
	for i := len(l.runs) - 1; i >= 0; i-- { // the newest live run wins
		if _, ok := e.active[l.runs[i].ID]; ok {
			l.live = l.runs[i].ID
			break
		}
	}
	e.runsMu.Unlock()
	return l, nil
}

// resumeSession returns the newest run of the root's agent that left a harness session behind,
// which is the session a follow-up message resumes.
func (e *Engine) resumeSession(ctx context.Context, root domain.Task) (domain.Run, bool) {
	l, err := e.lineage(ctx, root)
	if err != nil {
		return domain.Run{}, false
	}
	return newestSession(l.runs)
}

func newestSession(runs []domain.Run) (domain.Run, bool) {
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].SessionID != "" {
			return runs[i], true
		}
	}
	return domain.Run{}, false
}

// TaskConversation returns the owner's view of the task agent that works on taskID: the root
// task, its conversation, every follow-up task and run, and whether the owner can message it.
func (e *Engine) TaskConversation(ctx context.Context, taskID string) (domain.TaskConversation, error) {
	t, err := e.d.Store.GetTask(ctx, taskID)
	if err != nil {
		return domain.TaskConversation{}, err
	}
	root, err := e.conversationRoot(ctx, t)
	if err != nil {
		return domain.TaskConversation{}, err
	}
	l, err := e.lineage(ctx, root)
	if err != nil {
		return domain.TaskConversation{}, err
	}
	out := domain.TaskConversation{
		Task: root, Tasks: l.tasks, Runs: l.runs, Messages: []domain.ChatMessage{}, LiveRunID: l.live,
	}
	if out.Runs == nil {
		out.Runs = []domain.Run{}
	}
	conv, err := e.d.Store.GetConversationByTask(ctx, root.ID)
	switch {
	case err == nil:
		out.Conversation = &conv
		msgs, err := e.d.Store.ListChatMessages(ctx, conv.ID)
		if err != nil {
			return domain.TaskConversation{}, err
		}
		if msgs != nil {
			out.Messages = msgs
		}
	case !errors.Is(err, store.ErrNotFound):
		return domain.TaskConversation{}, err
	}
	out.CanSend, out.Reason = canSend(out.Conversation, l)
	return out, nil
}

// canSend applies the rules in order and stops at the first that matches. The viewer role is
// not considered: the UI shows its own line for viewers.
func canSend(conv *domain.Conversation, l conversationLineage) (bool, string) {
	if conv != nil && conv.ClosedAt != nil {
		return false, fmt.Sprintf("Closed by %s: %s", conv.ClosedBy, conv.ClosedReason)
	}
	if l.live != "" {
		return true, ""
	}
	if newest := l.tasks[len(l.tasks)-1]; newest.Status == domain.TaskQueued && l.runsByTask[newest.ID] == 0 {
		return true, "" // the message queues until the task starts
	}
	if _, ok := newestSession(l.runs); !ok {
		return false, "This agent has no session to continue. Retry the task instead."
	}
	return true, ""
}

// AgentConversations lists the task agents the owner can talk to: root tasks with at least one
// run. Open conversations come first, then ones closed or last active within the past week,
// newest first, at most 100.
func (e *Engine) AgentConversations(ctx context.Context) ([]domain.AgentConversation, error) {
	runs, err := e.d.Store.ListRuns(ctx, store.RunFilter{Limit: 1000})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var open, closed []domain.AgentConversation
	cutoff := time.Now().Add(-agentConversationWindow)
	for _, r := range runs {
		t, err := e.d.Store.GetTask(ctx, r.TaskID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			return nil, err
		}
		root, err := e.conversationRoot(ctx, t)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if seen[root.ID] {
			continue
		}
		seen[root.ID] = true
		l, err := e.lineage(ctx, root)
		if err != nil {
			return nil, err
		}
		row := domain.AgentConversation{
			TaskID: root.ID, Title: root.Title, Project: root.Project, Role: root.Role,
			TaskStatus: root.Status, Live: l.live != "",
		}
		for _, lr := range l.runs {
			for _, ts := range []time.Time{lr.StartedAt, lr.LastOutputAt} {
				row.UpdatedAt = laterOf(row.UpdatedAt, ts)
			}
			if lr.EndedAt != nil {
				row.UpdatedAt = laterOf(row.UpdatedAt, *lr.EndedAt)
			}
		}
		conv, err := e.d.Store.GetConversationByTask(ctx, root.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if err == nil && conv.ClosedAt != nil {
			row.Closed = true
			row.UpdatedAt = laterOf(row.UpdatedAt, *conv.ClosedAt)
		}
		switch {
		case !row.Closed:
			open = append(open, row)
		case row.UpdatedAt.After(cutoff):
			closed = append(closed, row)
		}
	}
	byNewest := func(rows []domain.AgentConversation) {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].UpdatedAt.After(rows[j].UpdatedAt) })
	}
	byNewest(open)
	byNewest(closed)
	out := append(open, closed...)
	if len(out) > agentConversationLimit {
		out = out[:agentConversationLimit]
	}
	if out == nil {
		out = []domain.AgentConversation{}
	}
	return out, nil
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// SendTaskMessage sends the owner's text to the agent of the task's conversation. A live agent
// that takes input gets it at once. Otherwise the message is stored queued and, when no run is
// live, a follow-up is started now; while a run is live the queue waits for that run to succeed.
func (e *Engine) SendTaskMessage(ctx context.Context, taskID, text string) (domain.ChatMessage, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return domain.ChatMessage{}, fmt.Errorf("%w: message is empty", api.ErrInvalid)
	}
	t, err := e.d.Store.GetTask(ctx, taskID)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	root, err := e.conversationRoot(ctx, t)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	conv, l, err := e.sendableConversation(ctx, root)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	msg := domain.ChatMessage{ConversationID: conv.ID, Role: "user", Text: text, Queued: true}
	if l.live != "" {
		switch err := e.deliverToRun(ctx, l.live, text); {
		case err == nil:
			msg.Queued, msg.RunID = false, l.live
		case errors.Is(err, errRunNotLive), errors.Is(err, harness.ErrInputClosed):
			// The agent cannot take it now; it waits in the queue.
		default:
			e.d.Logger.Warn("task chat: deliver to agent", "task", root.ID, "err", err)
		}
	}
	msg, err = e.storeOwnerMessage(ctx, conv, msg)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	if msg.Queued {
		// If the live run ended while the message was being stored, nobody else will pick it up.
		if l.live == "" {
			e.continueConversation(ctx, root.ID)
		} else if now, err := e.lineage(ctx, root); err == nil && now.live == "" {
			e.continueConversation(ctx, root.ID)
		}
	}
	return msg, nil
}

// sendableConversation returns the root's conversation (created on the first message) and its
// lineage, or an api.ErrConflict carrying the reason the agent cannot be messaged.
func (e *Engine) sendableConversation(ctx context.Context, root domain.Task) (domain.Conversation, conversationLineage, error) {
	e.convMu.Lock()
	defer e.convMu.Unlock()
	l, err := e.lineage(ctx, root)
	if err != nil {
		return domain.Conversation{}, l, err
	}
	conv, err := e.d.Store.GetConversationByTask(ctx, root.ID)
	var cur *domain.Conversation
	switch {
	case err == nil:
		cur = &conv
	case !errors.Is(err, store.ErrNotFound):
		return domain.Conversation{}, l, err
	}
	if ok, reason := canSend(cur, l); !ok {
		return domain.Conversation{}, l, fmt.Errorf("%w: %s", api.ErrConflict, reason)
	}
	if cur == nil {
		conv, err = e.d.Store.CreateConversation(ctx, domain.Conversation{
			TaskID: root.ID, Title: root.Title, Project: root.Project,
		})
		if err != nil {
			return domain.Conversation{}, l, err
		}
	}
	return conv, l, nil
}

// FlushTaskMessages starts the follow-up for the queued messages now. This is the only way
// queued messages move after a run that was denied, stalled, timed out or failed.
func (e *Engine) FlushTaskMessages(ctx context.Context, taskID string) error {
	t, err := e.d.Store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	root, err := e.conversationRoot(ctx, t)
	if err != nil {
		return err
	}
	conv, err := e.d.Store.GetConversationByTask(ctx, root.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	l, err := e.lineage(ctx, root)
	if err != nil {
		return err
	}
	if ok, reason := canSend(&conv, l); !ok {
		return fmt.Errorf("%w: %s", api.ErrConflict, reason)
	}
	e.continueConversation(ctx, root.ID)
	return nil
}

// continueConversation starts the follow-up for a conversation's queued messages when the rules
// allow it, and otherwise does nothing: no queued message, a closed conversation, or a newest
// task that is queued or running (the scheduler, or the end of that run, picks the messages up).
// A task that has not reached a PR is re-queued, so the agent resumes where it stopped. One
// whose work was merged, or is deploying, gets a conversation child task with its own branch.
// Callers decide when it is allowed to run: never after a denied, stalled, timed out, failed or
// cancelled run.
func (e *Engine) continueConversation(ctx context.Context, rootID string) {
	e.convMu.Lock()
	defer e.convMu.Unlock()
	root, err := e.d.Store.GetTask(ctx, rootID)
	if err != nil {
		e.d.Logger.Error("continue conversation: read task", "task", rootID, "err", err)
		return
	}
	conv, err := e.d.Store.GetConversationByTask(ctx, rootID)
	if err != nil || conv.ClosedAt != nil {
		return
	}
	msgs, err := e.d.Store.ListChatMessages(ctx, conv.ID)
	if err != nil || !slices.ContainsFunc(msgs, func(m domain.ChatMessage) bool { return m.Queued }) {
		return
	}
	l, err := e.lineage(ctx, root)
	if err != nil {
		e.d.Logger.Error("continue conversation: lineage", "task", rootID, "err", err)
		return
	}
	if l.live != "" {
		return
	}
	newest := l.tasks[len(l.tasks)-1]
	switch {
	case newest.Status == domain.TaskMerged, newest.Status == domain.TaskDeploying,
		newest.Status == domain.TaskDone && newest.PRNumber != 0:
		e.startFollowUp(ctx, root, newest)
	case slices.Contains([]domain.TaskStatus{domain.TaskBlocked, domain.TaskFailed, domain.TaskReview,
		domain.TaskAwaitingApproval, domain.TaskDone}, newest.Status):
		_, applied, err := e.setTaskFrom(ctx, newest.ID, []domain.TaskStatus{newest.Status},
			"re-queued by an owner message", func(x *domain.Task) error {
				x.Status = domain.TaskQueued
				x.Attempts = 0
				x.BlockedReason = ""
				return nil
			})
		if err != nil {
			e.d.Logger.Error("continue conversation: re-queue", "task", newest.ID, "err", err)
			return
		}
		if applied {
			if err := e.cancelStaleReviews(ctx, newest.ID); err != nil {
				e.d.Logger.Error("continue conversation: cancel stale reviews", "task", newest.ID, "err", err)
			}
		}
	}
}

// startFollowUp creates the child task that continues a conversation whose last task shipped.
func (e *Engine) startFollowUp(ctx context.Context, root, newest domain.Task) {
	if _, err := e.createTask(ctx, domain.Task{
		Project:  root.Project,
		Kind:     root.Kind,
		Role:     root.Role,
		Title:    "Follow-up: " + root.Title,
		Body:     root.Body,
		Priority: root.Priority,
		Source:   domain.SourceConversation,
		ParentID: newest.ID,
	}); err != nil {
		e.d.Logger.Error("continue conversation: create follow-up", "task", root.ID, "err", err)
	}
}

// claimOwnerMessages takes the queued owner messages of t's conversation for the run that is
// about to start. They come back oldest first; the caller either marks them delivered with
// deliverClaimed or hands their IDs to restoreQueued.
func (e *Engine) claimOwnerMessages(ctx context.Context, t domain.Task) (domain.Task, []domain.ChatMessage) {
	root, err := e.conversationRoot(ctx, t)
	if err != nil {
		return t, nil
	}
	conv, err := e.d.Store.GetConversationByTask(ctx, root.ID)
	if err != nil {
		return root, nil
	}
	claimed, err := e.claimQueued(ctx, conv.ID)
	if err != nil {
		e.d.Logger.Error("claim owner messages", "task", root.ID, "err", err)
		return root, nil
	}
	return root, claimed
}

// deliverClaimed records the run that took the claimed messages.
func (e *Engine) deliverClaimed(ctx context.Context, claimed []domain.ChatMessage, runID string) {
	for _, m := range claimed {
		got, err := e.d.Store.UpdateChatMessage(ctx, m.ID, func(m *domain.ChatMessage) error {
			m.RunID = runID
			return nil
		})
		if err != nil {
			e.d.Logger.Error("mark owner message delivered", "message", m.ID, "err", err)
			continue
		}
		e.emitMessage(ctx, got)
	}
}
