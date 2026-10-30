package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
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
