package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

// tcTask stores a task directly, so tests control source, parent and status.
func tcTask(t *testing.T, r *ctRig, tk domain.Task) domain.Task {
	t.Helper()
	if tk.Project == "" {
		tk.Project = "demo"
	}
	if tk.Kind == "" {
		tk.Kind = domain.KindFeature
	}
	if tk.Status == "" {
		tk.Status = domain.TaskDone
	}
	if tk.Source == "" {
		tk.Source = "api"
	}
	if tk.Role == "" {
		tk.Role = "implementer"
	}
	out, err := r.st.CreateTask(context.Background(), tk)
	require.NoError(t, err)
	return out
}

func tcRun(t *testing.T, r *ctRig, taskID, session string, started time.Time) domain.Run {
	t.Helper()
	run, err := r.st.CreateRun(context.Background(), domain.Run{
		TaskID: taskID, Project: "demo", Role: "implementer", Harness: "claude",
		Status: domain.RunSucceeded, SessionID: session, StartedAt: started,
	})
	require.NoError(t, err)
	return run
}

func TestTaskConversationLineage(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	base := time.Now().Add(-time.Hour)

	root := tcTask(t, r, domain.Task{Title: "root"})
	child := tcTask(t, r, domain.Task{Title: "follow-up", ParentID: root.ID, Source: domain.SourceConversation})
	// A child that is not a conversation follow-up stays out of the lineage.
	tcTask(t, r, domain.Task{Title: "subtask", ParentID: root.ID})
	r1 := tcRun(t, r, root.ID, "s1", base)
	r2 := tcRun(t, r, root.ID, "s2", base.Add(time.Minute))
	r3 := tcRun(t, r, child.ID, "s3", base.Add(2*time.Minute))

	tc, err := e.TaskConversation(ctx, root.ID)
	require.NoError(t, err)
	assert.Equal(t, root.ID, tc.Task.ID)
	require.Len(t, tc.Tasks, 2)
	assert.Equal(t, []string{root.ID, child.ID}, []string{tc.Tasks[0].ID, tc.Tasks[1].ID})
	require.Len(t, tc.Runs, 3)
	assert.Equal(t, []string{r1.ID, r2.ID, r3.ID}, []string{tc.Runs[0].ID, tc.Runs[1].ID, tc.Runs[2].ID})
	assert.NotNil(t, tc.Messages)

	fromChild, err := e.TaskConversation(ctx, child.ID)
	require.NoError(t, err)
	assert.Equal(t, root.ID, fromChild.Task.ID)
	assert.Len(t, fromChild.Runs, 3)

	_, err = e.TaskConversation(ctx, "missing")
	require.Error(t, err)
}

func TestTaskConversationCanSendReasons(t *testing.T) {
	ctx := context.Background()
	const noSession = "This agent has no session to continue. Retry the task instead."
	base := time.Now().Add(-time.Hour)

	t.Run("closed wins over everything", func(t *testing.T) {
		r := ctNewRig(t)
		e := r.engine(nil)
		root := tcTask(t, r, domain.Task{Title: "x"})
		run := tcRun(t, r, root.ID, "s", base)
		now := time.Now()
		_, err := r.st.CreateConversation(ctx, domain.Conversation{
			TaskID: root.ID, ClosedAt: &now, ClosedBy: "agent", ClosedReason: "all done",
		})
		require.NoError(t, err)
		e.registerRun(run.ID, root.ID, "demo", func() {})
		tc, err := e.TaskConversation(ctx, root.ID)
		require.NoError(t, err)
		assert.False(t, tc.CanSend)
		assert.Equal(t, "Closed by agent: all done", tc.Reason)
	})

	t.Run("live run", func(t *testing.T) {
		r := ctNewRig(t)
		e := r.engine(nil)
		root := tcTask(t, r, domain.Task{Title: "x"})
		run := tcRun(t, r, root.ID, "", base) // no session yet, but live
		e.registerRun(run.ID, root.ID, "demo", func() {})
		tc, err := e.TaskConversation(ctx, root.ID)
		require.NoError(t, err)
		assert.True(t, tc.CanSend)
		assert.Equal(t, run.ID, tc.LiveRunID)
		assert.Empty(t, tc.Reason)
	})

	t.Run("newest task queued with no runs", func(t *testing.T) {
		r := ctNewRig(t)
		e := r.engine(nil)
		root := tcTask(t, r, domain.Task{Title: "x"})
		tcRun(t, r, root.ID, "", base)
		tcTask(t, r, domain.Task{Title: "next", ParentID: root.ID, Source: domain.SourceConversation, Status: domain.TaskQueued})
		tc, err := e.TaskConversation(ctx, root.ID)
		require.NoError(t, err)
		assert.True(t, tc.CanSend)
		assert.Empty(t, tc.LiveRunID)
	})

	t.Run("no session", func(t *testing.T) {
		r := ctNewRig(t)
		e := r.engine(nil)
		root := tcTask(t, r, domain.Task{Title: "x"})
		tcRun(t, r, root.ID, "", base)
		tc, err := e.TaskConversation(ctx, root.ID)
		require.NoError(t, err)
		assert.False(t, tc.CanSend)
		assert.Equal(t, noSession, tc.Reason)
	})

	t.Run("a session to resume", func(t *testing.T) {
		r := ctNewRig(t)
		e := r.engine(nil)
		root := tcTask(t, r, domain.Task{Title: "x"})
		tcRun(t, r, root.ID, "s", base)
		tc, err := e.TaskConversation(ctx, root.ID)
		require.NoError(t, err)
		assert.True(t, tc.CanSend)
		assert.Empty(t, tc.Reason)
	})
}

func TestResumeSessionNewestLineageRun(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	base := time.Now().Add(-time.Hour)
	root := tcTask(t, r, domain.Task{Title: "x"})
	tcRun(t, r, root.ID, "old", base)
	want := tcRun(t, r, root.ID, "new", base.Add(time.Minute))
	tcRun(t, r, root.ID, "", base.Add(2*time.Minute))
	// A run of another role does not carry the agent's session.
	other, err := r.st.CreateRun(ctx, domain.Run{TaskID: root.ID, Project: "demo", Role: "reviewer",
		Status: domain.RunSucceeded, SessionID: "rev", StartedAt: base.Add(3 * time.Minute)})
	require.NoError(t, err)
	_ = other

	got, ok := e.resumeSession(ctx, root)
	require.True(t, ok)
	assert.Equal(t, want.ID, got.ID)

	empty := tcTask(t, r, domain.Task{Title: "empty"})
	_, ok = e.resumeSession(ctx, empty)
	assert.False(t, ok)
}

func TestAgentConversationsOrder(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	now := time.Now()

	noRuns := tcTask(t, r, domain.Task{Title: "no runs"})
	_ = noRuns
	older := tcTask(t, r, domain.Task{Title: "open older"})
	tcRun(t, r, older.ID, "s", now.Add(-3*time.Hour))
	newer := tcTask(t, r, domain.Task{Title: "open newer"})
	tcRun(t, r, newer.ID, "s", now.Add(-time.Hour))
	closedRecent := tcTask(t, r, domain.Task{Title: "closed recent"})
	tcRun(t, r, closedRecent.ID, "s", now.Add(-2*time.Hour))
	closedOld := tcTask(t, r, domain.Task{Title: "closed old"})
	tcRun(t, r, closedOld.ID, "s", now.Add(-30*24*time.Hour))
	// A follow-up task folds into its root's row.
	fu := tcTask(t, r, domain.Task{Title: "fu", ParentID: older.ID, Source: domain.SourceConversation})
	tcRun(t, r, fu.ID, "s", now.Add(-2*time.Hour))

	recent := now.Add(-time.Hour)
	_, err := r.st.CreateConversation(ctx, domain.Conversation{TaskID: closedRecent.ID, ClosedAt: &recent, ClosedBy: "agent"})
	require.NoError(t, err)
	old := now.Add(-20 * 24 * time.Hour)
	_, err = r.st.CreateConversation(ctx, domain.Conversation{TaskID: closedOld.ID, ClosedAt: &old, ClosedBy: "agent"})
	require.NoError(t, err)

	live, err := r.st.CreateRun(ctx, domain.Run{TaskID: newer.ID, Project: "demo", Role: "implementer", Status: domain.RunRunning, StartedAt: now})
	require.NoError(t, err)
	e.registerRun(live.ID, newer.ID, "demo", func() {})

	rows, err := e.AgentConversations(ctx)
	require.NoError(t, err)
	var titles []string
	for _, row := range rows {
		titles = append(titles, row.Title)
	}
	assert.Equal(t, []string{"open newer", "open older", "closed recent"}, titles)
	assert.True(t, rows[0].Live)
	assert.False(t, rows[0].Closed)
	assert.False(t, rows[1].Live)
	assert.True(t, rows[2].Closed)
	assert.Equal(t, "implementer", rows[0].Role)
}
