package engine

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/stretchr/testify/require"
)

// TestIssueReportLockIsPerTask checks that holding one task's report lock does not block the
// report for another task. The lock for task A is taken by hand, as a slow GitHub call in
// reportToIssue would hold it. The report for task B must still finish and comment while A's
// lock is held, and A's lock is released afterwards.
func TestIssueReportLockIsPerTask(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	issueA := env.fake.AddIssue("Slow report", "Its comment is held.")
	issueB := env.fake.AddIssue("Fast report", "Its comment goes through.")
	taskA, err := env.st.CreateTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindIssue, Title: issueA.Title, Body: issueA.Body,
		Source: "github_issue", SourceRef: "github_issue:" + strconv.Itoa(issueA.Number),
		Status: domain.TaskDone, Summary: "Held.",
	})
	require.NoError(t, err)
	taskB, err := env.st.CreateTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindIssue, Title: issueB.Title, Body: issueB.Body,
		Source: "github_issue", SourceRef: "github_issue:" + strconv.Itoa(issueB.Number),
		Status: domain.TaskDone, Summary: "Free.",
	})
	require.NoError(t, err)

	lockA, _ := env.e.issueReportLocks.LoadOrStore(taskA.ID, &sync.Mutex{})
	lockA.(*sync.Mutex).Lock()
	defer lockA.(*sync.Mutex).Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		env.e.reportToIssue(env.ctx, taskB)
	}()
	// A report that waited for A's lock would never close done, so the wait is bounded.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("report for task B waited on task A's lock")
	}

	require.Len(t, env.fake.Comments[issueB.Number], 1, "task B is reported while task A's lock is held")
	require.Empty(t, env.fake.Comments[issueA.Number], "task A is not reported while its lock is held")
}
