package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/store"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenMemoryIsPrivate(t *testing.T) {
	a := newStore(t)
	b := newStore(t)
	ctx := context.Background()
	_, err := a.CreateTask(ctx, domain.Task{Project: "p", Title: "only in a"})
	require.NoError(t, err)
	list, err := b.ListTasks(ctx, store.TaskFilter{})
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestTaskCRUD(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	created, err := s.CreateTask(ctx, domain.Task{Project: "p", Title: "t1", Kind: domain.KindFeature,
		Status: domain.TaskQueued, Priority: 1, SourceRef: "github_issue:1", Body: "body"})
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	assert.False(t, created.CreatedAt.IsZero())
	assert.False(t, created.UpdatedAt.IsZero())

	got, err := s.GetTask(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "t1", got.Title)
	assert.Equal(t, "body", got.Body)
	assert.True(t, created.CreatedAt.Equal(got.CreatedAt))

	_, err = s.GetTask(ctx, "missing")
	assert.True(t, errors.Is(err, store.ErrNotFound))

	found, err := s.FindTaskBySourceRef(ctx, "p", "github_issue:1")
	require.NoError(t, err)
	assert.Equal(t, created.ID, found.ID)
	_, err = s.FindTaskBySourceRef(ctx, "p", "nope")
	assert.True(t, errors.Is(err, store.ErrNotFound))

	updated, err := s.UpdateTask(ctx, created.ID, func(t *domain.Task) error {
		t.Status = domain.TaskReview
		t.ID = "tampered"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, domain.TaskReview, updated.Status)
	assert.True(t, updated.UpdatedAt.After(created.UpdatedAt) || updated.UpdatedAt.Equal(created.UpdatedAt))

	// The indexed status column follows the update.
	list, err := s.ListTasks(ctx, store.TaskFilter{Statuses: []domain.TaskStatus{domain.TaskReview}})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, created.ID, list[0].ID)

	_, err = s.UpdateTask(ctx, "missing", func(*domain.Task) error { return nil })
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestUpdateTaskFnErrorAborts(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	c, err := s.CreateTask(ctx, domain.Task{Project: "p", Title: "orig", Status: domain.TaskQueued})
	require.NoError(t, err)

	boom := errors.New("boom")
	_, err = s.UpdateTask(ctx, c.ID, func(t *domain.Task) error {
		t.Title = "changed"
		t.Status = domain.TaskDone
		return boom
	})
	require.ErrorIs(t, err, boom)

	got, err := s.GetTask(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, "orig", got.Title)
	assert.Equal(t, domain.TaskQueued, got.Status)
}

func TestListTasksFiltersAndOrder(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	var ids []string
	for i := range 3 {
		c, err := s.CreateTask(ctx, domain.Task{Project: "p", Kind: domain.KindBugfix,
			Status: domain.TaskQueued, ParentID: "parent", Title: fmt.Sprint(i)})
		require.NoError(t, err)
		ids = append(ids, c.ID)
	}
	_, err := s.CreateTask(ctx, domain.Task{Project: "other", Kind: domain.KindFeature, Status: domain.TaskDone})
	require.NoError(t, err)

	all, err := s.ListTasks(ctx, store.TaskFilter{Project: "p"})
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, ids[2], all[0].ID, "newest first")
	assert.Equal(t, ids[0], all[2].ID)

	byKind, err := s.ListTasks(ctx, store.TaskFilter{Kind: domain.KindFeature})
	require.NoError(t, err)
	assert.Len(t, byKind, 1)

	byParent, err := s.ListTasks(ctx, store.TaskFilter{ParentID: "parent"})
	require.NoError(t, err)
	assert.Len(t, byParent, 3)

	limited, err := s.ListTasks(ctx, store.TaskFilter{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, limited, 2)

	empty, err := s.ListTasks(ctx, store.TaskFilter{Project: "nobody"})
	require.NoError(t, err)
	require.NotNil(t, empty)
	assert.Empty(t, empty)
	raw, err := json.Marshal(empty)
	require.NoError(t, err)
	assert.Equal(t, "[]", string(raw))
}

func TestClaimNextTaskOrdering(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	low, err := s.CreateTask(ctx, domain.Task{Project: "a", Status: domain.TaskQueued, Priority: 0})
	require.NoError(t, err)
	time.Sleep(2 * time.Millisecond)
	highOld, err := s.CreateTask(ctx, domain.Task{Project: "a", Status: domain.TaskQueued, Priority: 5})
	require.NoError(t, err)
	time.Sleep(2 * time.Millisecond)
	highNew, err := s.CreateTask(ctx, domain.Task{Project: "b", Status: domain.TaskQueued, Priority: 5})
	require.NoError(t, err)

	c1, err := s.ClaimNextTask(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, highOld.ID, c1.ID, "priority first, then oldest")
	assert.Equal(t, domain.TaskRunning, c1.Status)
	assert.Equal(t, 1, c1.Attempts)

	stored, err := s.GetTask(ctx, c1.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskRunning, stored.Status)
	assert.Equal(t, 1, stored.Attempts)

	c2, err := s.ClaimNextTask(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, highNew.ID, c2.ID)

	c3, err := s.ClaimNextTask(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, low.ID, c3.ID)

	_, err = s.ClaimNextTask(ctx, nil)
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestClaimNextTaskSkipsBusyProjects(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	_, err := s.CreateTask(ctx, domain.Task{Project: "busy", Status: domain.TaskQueued, Priority: 9})
	require.NoError(t, err)
	free, err := s.CreateTask(ctx, domain.Task{Project: "free", Status: domain.TaskQueued, Priority: 1})
	require.NoError(t, err)

	c, err := s.ClaimNextTask(ctx, []string{"busy"})
	require.NoError(t, err)
	assert.Equal(t, free.ID, c.ID)

	_, err = s.ClaimNextTask(ctx, []string{"busy", "free"})
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestClaimNextTaskConcurrent(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for range 5 {
		_, err := s.CreateTask(ctx, domain.Task{Project: "p", Status: domain.TaskQueued})
		require.NoError(t, err)
	}

	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		claimed    = map[string]bool{}
		notFound   atomic.Int32
		unexpected []error
	)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := s.ClaimNextTask(ctx, nil)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				claimed[task.ID] = true
			case errors.Is(err, store.ErrNotFound):
				notFound.Add(1)
			default:
				unexpected = append(unexpected, err)
			}
		}()
	}
	wg.Wait()
	assert.Empty(t, unexpected)
	assert.Len(t, claimed, 5)
	assert.Equal(t, int32(5), notFound.Load())
}

func TestRunsAndLogs(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	r, err := s.CreateRun(ctx, domain.Run{TaskID: "t1", Project: "p", Status: domain.RunStarting, Harness: "claude"})
	require.NoError(t, err)
	require.NotEmpty(t, r.ID)
	assert.False(t, r.StartedAt.IsZero())

	got, err := s.GetRun(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, "claude", got.Harness)

	_, err = s.GetRun(ctx, "missing")
	assert.True(t, errors.Is(err, store.ErrNotFound))

	upd, err := s.UpdateRun(ctx, r.ID, func(r *domain.Run) error {
		r.Status = domain.RunSucceeded
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, domain.RunSucceeded, upd.Status)

	other, err := s.CreateRun(ctx, domain.Run{TaskID: "t2", Project: "p", Status: domain.RunRunning})
	require.NoError(t, err)

	runsForT1, err := s.ListRuns(ctx, store.RunFilter{TaskID: "t1"})
	require.NoError(t, err)
	require.Len(t, runsForT1, 1)
	assert.Equal(t, r.ID, runsForT1[0].ID)

	running, err := s.ListRuns(ctx, store.RunFilter{Project: "p", Statuses: []domain.RunStatus{domain.RunRunning}})
	require.NoError(t, err)
	require.Len(t, running, 1)
	assert.Equal(t, other.ID, running[0].ID)

	_, err = s.UpdateRun(ctx, "missing", func(*domain.Run) error { return nil })
	assert.True(t, errors.Is(err, store.ErrNotFound))

	// Log sequence continues across appends and is per run.
	first, err := s.AppendRunLogs(ctx, r.ID, []domain.LogLine{
		{Kind: domain.LogText, Text: "a"}, {Kind: domain.LogStderr, Text: "b"},
	})
	require.NoError(t, err)
	require.Len(t, first, 2)
	assert.Equal(t, int64(1), first[0].Seq)
	assert.Equal(t, int64(2), first[1].Seq)
	assert.Equal(t, r.ID, first[0].RunID)
	assert.False(t, first[0].Time.IsZero())

	second, err := s.AppendRunLogs(ctx, r.ID, []domain.LogLine{{Kind: domain.LogToolUse, Text: "c"}})
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, int64(3), second[0].Seq)

	otherLogs, err := s.AppendRunLogs(ctx, other.ID, []domain.LogLine{{Kind: domain.LogText, Text: "x"}})
	require.NoError(t, err)
	assert.Equal(t, int64(1), otherLogs[0].Seq)

	all, err := s.ListRunLogs(ctx, r.ID, 0, 0)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, []string{"a", "b", "c"}, []string{all[0].Text, all[1].Text, all[2].Text})
	assert.Equal(t, domain.LogToolUse, all[2].Kind)

	after, err := s.ListRunLogs(ctx, r.ID, 1, 10)
	require.NoError(t, err)
	require.Len(t, after, 2)
	assert.Equal(t, int64(2), after[0].Seq)

	limited, err := s.ListRunLogs(ctx, r.ID, 0, 1)
	require.NoError(t, err)
	assert.Len(t, limited, 1)

	none, err := s.ListRunLogs(ctx, "nobody", 0, 0)
	require.NoError(t, err)
	require.NotNil(t, none)
	assert.Empty(t, none)
}

func TestEvents(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	payload := json.RawMessage(`{"k":1}`)
	e1, err := s.AppendEvent(ctx, domain.Event{Type: domain.EvTaskCreated, Project: "p", TaskID: "t1", Message: "one", Data: payload})
	require.NoError(t, err)
	require.NotZero(t, e1.ID)
	assert.False(t, e1.Time.IsZero())

	var ids []int64
	ids = append(ids, e1.ID)
	for i := range 9 {
		typ := domain.EvSystem
		project, task := "p", "t1"
		if i%2 == 0 {
			project, task = "q", "t2"
		}
		e, err := s.AppendEvent(ctx, domain.Event{Type: typ, Project: project, TaskID: task, Message: fmt.Sprint(i)})
		require.NoError(t, err)
		ids = append(ids, e.ID)
	}

	// AfterID 0: newest N events, ascending.
	newest, err := s.ListEvents(ctx, store.EventFilter{Limit: 3})
	require.NoError(t, err)
	require.Len(t, newest, 3)
	assert.Equal(t, ids[7], newest[0].ID)
	assert.Equal(t, ids[9], newest[2].ID)
	assert.Equal(t, "8", newest[2].Message)

	// AfterID > 0: the next events ascending.
	next, err := s.ListEvents(ctx, store.EventFilter{AfterID: ids[3], Limit: 2})
	require.NoError(t, err)
	require.Len(t, next, 2)
	assert.Equal(t, ids[4], next[0].ID)
	assert.Equal(t, ids[5], next[1].ID)

	// Filters apply in both modes.
	byProject, err := s.ListEvents(ctx, store.EventFilter{Project: "q"})
	require.NoError(t, err)
	assert.Len(t, byProject, 5)
	byTaskAfter, err := s.ListEvents(ctx, store.EventFilter{AfterID: 0, TaskID: "t1"})
	require.NoError(t, err)
	assert.Len(t, byTaskAfter, 5)

	// Data round-trips; absent data reads back empty.
	all, err := s.ListEvents(ctx, store.EventFilter{AfterID: 0, Limit: 100})
	require.NoError(t, err)
	require.Len(t, all, 10)
	assert.JSONEq(t, `{"k":1}`, string(all[0].Data))
	assert.Empty(t, all[1].Data)
	assert.Equal(t, "one", all[0].Message)

	none, err := s.ListEvents(ctx, store.EventFilter{Project: "nobody"})
	require.NoError(t, err)
	require.NotNil(t, none)
	assert.Empty(t, none)
}

func TestApprovals(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	a, err := s.CreateApproval(ctx, domain.Approval{TaskID: "t1", Project: "p", Kind: "merge",
		Status: domain.ApprovalPending})
	require.NoError(t, err)
	require.NotEmpty(t, a.ID)
	_, err = s.CreateApproval(ctx, domain.Approval{TaskID: "t2", Status: domain.ApprovalApproved})
	require.NoError(t, err)

	got, err := s.GetApproval(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, "merge", got.Kind)

	_, err = s.GetApproval(ctx, "missing")
	assert.True(t, errors.Is(err, store.ErrNotFound))

	pending, err := s.ListApprovals(ctx, domain.ApprovalPending)
	require.NoError(t, err)
	require.Len(t, pending, 1)

	all, err := s.ListApprovals(ctx, "")
	require.NoError(t, err)
	assert.Len(t, all, 2)

	now := time.Now().UTC()
	upd, err := s.UpdateApproval(ctx, a.ID, func(ap *domain.Approval) error {
		ap.Status = domain.ApprovalApproved
		ap.DecidedAt = &now
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalApproved, upd.Status)

	pending, err = s.ListApprovals(ctx, domain.ApprovalPending)
	require.NoError(t, err)
	assert.Empty(t, pending)
	require.NotNil(t, pending)

	_, err = s.UpdateApproval(ctx, "missing", func(*domain.Approval) error { return nil })
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestDeployments(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	_, err := s.LastHealthyDeployment(ctx, "p")
	assert.True(t, errors.Is(err, store.ErrNotFound))

	old, err := s.CreateDeployment(ctx, domain.Deployment{Project: "p", Ref: "a", State: domain.DeployHealthy})
	require.NoError(t, err)
	time.Sleep(2 * time.Millisecond)
	newest, err := s.CreateDeployment(ctx, domain.Deployment{Project: "p", Ref: "b", State: domain.DeployFailed})
	require.NoError(t, err)
	time.Sleep(2 * time.Millisecond)
	_, err = s.CreateDeployment(ctx, domain.Deployment{Project: "other", Ref: "c", State: domain.DeployHealthy})
	require.NoError(t, err)

	healthy, err := s.LastHealthyDeployment(ctx, "p")
	require.NoError(t, err)
	assert.Equal(t, old.ID, healthy.ID)

	list, err := s.ListDeployments(ctx, "p", 0)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, newest.ID, list[0].ID)

	everyone, err := s.ListDeployments(ctx, "", 0)
	require.NoError(t, err)
	assert.Len(t, everyone, 3)

	upd, err := s.UpdateDeployment(ctx, newest.ID, func(d *domain.Deployment) error {
		d.State = domain.DeployHealthy
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, domain.DeployHealthy, upd.State)
	healthy, err = s.LastHealthyDeployment(ctx, "p")
	require.NoError(t, err)
	assert.Equal(t, newest.ID, healthy.ID)

	got, err := s.GetDeployment(ctx, old.ID)
	require.NoError(t, err)
	assert.Equal(t, "a", got.Ref)
	_, err = s.GetDeployment(ctx, "missing")
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestUpsertSignal(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	obs := domain.SignalObservation{Fingerprint: "fp1", Title: "boom", Level: "error", Count: 1}

	first, isNew, err := s.UpsertSignal(ctx, domain.Signal{Project: "p", Source: "sentry", SignalObservation: obs})
	require.NoError(t, err)
	assert.True(t, isNew)
	require.NotEmpty(t, first.ID)
	assert.False(t, first.CreatedAt.IsZero())

	obs.Count = 7
	obs.Title = "boom again"
	obs.Culprit = "pkg.fn"
	second, isNew, err := s.UpsertSignal(ctx, domain.Signal{ID: "ignored", Project: "p", Source: "sentry", SignalObservation: obs})
	require.NoError(t, err)
	assert.False(t, isNew)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, int64(7), second.Count)
	assert.Equal(t, "boom again", second.Title)
	assert.Equal(t, "pkg.fn", second.Culprit)
	assert.True(t, first.CreatedAt.Equal(second.CreatedAt))

	// Same fingerprint under another source is a different signal.
	_, isNew, err = s.UpsertSignal(ctx, domain.Signal{Project: "p", Source: "other", SignalObservation: obs})
	require.NoError(t, err)
	assert.True(t, isNew)

	list, err := s.ListSignals(ctx, "p", 0)
	require.NoError(t, err)
	assert.Len(t, list, 2)

	upd, err := s.UpdateSignal(ctx, first.ID, func(sg *domain.Signal) error {
		sg.TaskID = "task-1"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "task-1", upd.TaskID)
	_, err = s.UpdateSignal(ctx, "missing", func(*domain.Signal) error { return nil })
	assert.True(t, errors.Is(err, store.ErrNotFound))

	none, err := s.ListSignals(ctx, "nobody", 0)
	require.NoError(t, err)
	require.NotNil(t, none)
	assert.Empty(t, none)
}

func TestUpsertFriction(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	first, isNew, err := s.UpsertFriction(ctx, domain.Friction{Project: "p", Fingerprint: "f", Title: "slow", Count: 0})
	require.NoError(t, err)
	assert.True(t, isNew)
	assert.Equal(t, 1, first.Count, "count floors at 1")

	_, err = s.UpdateFriction(ctx, first.ID, func(f *domain.Friction) error {
		f.IssueURL = "https://example.test/issues/1"
		return nil
	})
	require.NoError(t, err)

	second, isNew, err := s.UpsertFriction(ctx, domain.Friction{Project: "p", Fingerprint: "f", Title: "slow"})
	require.NoError(t, err)
	assert.False(t, isNew)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, 2, second.Count)
	assert.Equal(t, "https://example.test/issues/1", second.IssueURL)

	list, err := s.ListFriction(ctx, "p", 0)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, 2, list[0].Count)

	empty, err := s.ListFriction(ctx, "nobody", 0)
	require.NoError(t, err)
	assert.NotNil(t, empty)
	assert.Empty(t, empty)

	upd, err := s.UpdateFriction(ctx, first.ID, func(f *domain.Friction) error {
		f.Title = "renamed"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "renamed", upd.Title)
	_, err = s.UpdateFriction(ctx, "missing", func(*domain.Friction) error { return nil })
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestConversationsAndMessages(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	c1, err := s.CreateConversation(ctx, domain.Conversation{Title: "one", Project: "p"})
	require.NoError(t, err)
	time.Sleep(2 * time.Millisecond)
	c2, err := s.CreateConversation(ctx, domain.Conversation{Title: "two"})
	require.NoError(t, err)

	got, err := s.GetConversation(ctx, c1.ID)
	require.NoError(t, err)
	assert.Equal(t, "one", got.Title)
	_, err = s.GetConversation(ctx, "missing")
	assert.True(t, errors.Is(err, store.ErrNotFound))

	upd, err := s.UpdateConversation(ctx, c1.ID, func(c *domain.Conversation) error {
		c.Busy = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, upd.Busy)
	_, err = s.UpdateConversation(ctx, "missing", func(*domain.Conversation) error { return nil })
	assert.True(t, errors.Is(err, store.ErrNotFound))

	// Updating c1 makes it the most recently updated.
	list, err := s.ListConversations(ctx, 0)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, c1.ID, list[0].ID)
	assert.Equal(t, c2.ID, list[1].ID)

	m1, err := s.AppendChatMessage(ctx, domain.ChatMessage{ConversationID: c1.ID, Role: "user", Text: "hi"})
	require.NoError(t, err)
	require.NotEmpty(t, m1.ID)
	m2, err := s.AppendChatMessage(ctx, domain.ChatMessage{ConversationID: c1.ID, Role: "assistant", Text: "hello"})
	require.NoError(t, err)
	_, err = s.AppendChatMessage(ctx, domain.ChatMessage{ConversationID: c2.ID, Role: "user", Text: "elsewhere"})
	require.NoError(t, err)

	msgs, err := s.ListChatMessages(ctx, c1.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	assert.Equal(t, m1.ID, msgs[0].ID, "ascending")
	assert.Equal(t, m2.ID, msgs[1].ID)

	none, err := s.ListChatMessages(ctx, "nobody")
	require.NoError(t, err)
	require.NotNil(t, none)
	assert.Empty(t, none)
}

func TestLoopState(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	zero, err := s.GetLoopState(ctx, "shepherd", "p")
	require.NoError(t, err)
	assert.Equal(t, domain.LoopState{Name: "shepherd", Project: "p"}, zero)

	now := time.Now().UTC()
	require.NoError(t, s.SaveLoopState(ctx, domain.LoopState{Name: "shepherd", Project: "p", LastRunAt: now, Cursor: "c1"}))
	require.NoError(t, s.SaveLoopState(ctx, domain.LoopState{Name: "shepherd", Project: "p", Cursor: "c2"}))
	require.NoError(t, s.SaveLoopState(ctx, domain.LoopState{Name: "global", Project: ""}))

	got, err := s.GetLoopState(ctx, "shepherd", "p")
	require.NoError(t, err)
	assert.Equal(t, "c2", got.Cursor)

	list, err := s.ListLoopStates(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "global", list[0].Name)
	assert.Equal(t, "shepherd", list[1].Name)
}

func TestReopenFileKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foundry.db")
	ctx := context.Background()

	s, err := Open(path)
	require.NoError(t, err)
	c, err := s.CreateTask(ctx, domain.Task{Project: "p", Title: "persisted", Status: domain.TaskQueued})
	require.NoError(t, err)
	require.NoError(t, s.Close())

	s2, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	got, err := s2.GetTask(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, "persisted", got.Title)

	// Migrations are not re-applied on reopen.
	var n int
	require.NoError(t, s2.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n))
	assert.Equal(t, len(migrations), n)
}

func TestCostSince(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	cutoff := time.Now().Add(-time.Hour)

	for _, r := range []domain.Run{
		{TaskID: "before", Project: "p", CostUSD: 10, StartedAt: cutoff.Add(-time.Second)},
		{TaskID: "at", Project: "p", CostUSD: 1.5, StartedAt: cutoff},
		{TaskID: "after", Project: "p", CostUSD: 2.25, StartedAt: cutoff.Add(time.Second)},
		{TaskID: "free", Project: "p", StartedAt: cutoff.Add(time.Minute)}, // no reported cost
	} {
		_, err := s.CreateRun(ctx, r)
		require.NoError(t, err)
	}

	got, err := s.CostSince(ctx, cutoff)
	require.NoError(t, err)
	assert.InDelta(t, 3.75, got, 1e-9, "runs at or after the cutoff only")

	none, err := s.CostSince(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 0.0, none, "no runs since: zero, not an error")
}

func TestListEventsFromStartPages(t *testing.T) {
	s, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	const total = 1200
	for i := range total {
		_, err := s.AppendEvent(ctx, domain.Event{Type: domain.EvSystem, Message: fmt.Sprint(i)})
		require.NoError(t, err)
	}

	// Without FromStart, AfterID 0 is still the newest page.
	newest, err := s.ListEvents(ctx, store.EventFilter{Limit: 500})
	require.NoError(t, err)
	require.Len(t, newest, 500)
	assert.Equal(t, "700", newest[0].Message)

	var got []domain.Event
	var cursor int64
	for {
		page, err := s.ListEvents(ctx, store.EventFilter{AfterID: cursor, FromStart: true, Limit: 500})
		require.NoError(t, err)
		got = append(got, page...)
		if len(page) < 500 {
			break
		}
		cursor = page[len(page)-1].ID
	}
	require.Len(t, got, total)
	for i, e := range got {
		assert.Equal(t, fmt.Sprint(i), e.Message)
	}
}
