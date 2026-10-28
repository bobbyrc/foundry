package engine

import (
	"strconv"
	"sync"
	"testing"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/scm"
	"github.com/stretchr/testify/require"
)

// lpIssueTask queues an intake task for issue, as intakeLoop does.
func lpIssueTask(t *testing.T, env *lpEnv, issue scm.Issue) domain.Task {
	t.Helper()
	task, err := env.e.createTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindIssue, Title: issue.Title, Body: issue.Body,
		Source: "github_issue", SourceRef: "github_issue:" + strconv.Itoa(issue.Number),
	})
	require.NoError(t, err)
	return task
}

// lpSetStatus moves a task to status through setTask and waits for the issue report it starts.
func lpSetStatus(t *testing.T, env *lpEnv, id string, fn func(*domain.Task)) {
	t.Helper()
	_, err := env.e.setTask(env.ctx, id, "test", func(x *domain.Task) error {
		fn(x)
		return nil
	})
	require.NoError(t, err)
	env.e.wg.Wait()
}

func TestIssueReportDoneWithoutPRCommentsOnce(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	issue := env.fake.AddIssue("Docs are stale", "The README is wrong.")
	task := lpIssueTask(t, env, issue)

	lpSetStatus(t, env, task.ID, func(x *domain.Task) {
		x.Status, x.Summary = domain.TaskDone, "The README already matches the code."
	})

	got := env.fake.Comments[issue.Number]
	require.Len(t, got, 1)
	require.Contains(t, got[0], "without code changes")
	require.Contains(t, got[0], "The README already matches the code.")
	require.Contains(t, got[0], task.ID)

	// The same status again adds nothing.
	lpSetStatus(t, env, task.ID, func(x *domain.Task) { x.Summary = "again" })
	require.Len(t, env.fake.Comments[issue.Number], 1)
}

func TestIssueReportConcurrentSameStatusCommentsOnce(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	issue := env.fake.AddIssue("Racy report", "Two writes of the same status.")
	task, err := env.st.CreateTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindIssue, Title: issue.Title, Body: issue.Body,
		Source: "github_issue", SourceRef: "github_issue:" + strconv.Itoa(issue.Number),
		Status: domain.TaskDone, Summary: "Nothing to change.",
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			env.e.reportToIssue(env.ctx, task)
		}()
	}
	wg.Wait()

	require.Len(t, env.fake.Comments[issue.Number], 1)
}

func TestIssueReportBlockedAfterDone(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	issue := env.fake.AddIssue("Needs a key", "Call the vendor API.")
	task := lpIssueTask(t, env, issue)

	lpSetStatus(t, env, task.ID, func(x *domain.Task) {
		x.Status, x.Summary = domain.TaskDone, "Nothing to change."
	})
	lpSetStatus(t, env, task.ID, func(x *domain.Task) {
		x.Status, x.BlockedReason = domain.TaskBlocked, "The vendor API key is missing."
	})

	got := env.fake.Comments[issue.Number]
	require.Len(t, got, 2)
	require.Contains(t, got[1], "blocked and needs the owner")
	require.Contains(t, got[1], "The vendor API key is missing.")

	// Blocked again is the same status: nothing more.
	lpSetStatus(t, env, task.ID, func(x *domain.Task) {
		x.Status, x.BlockedReason = domain.TaskBlocked, "still missing"
	})
	require.Len(t, env.fake.Comments[issue.Number], 2)
}

func TestIssueReportSkipsTaskWithPR(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	issue := env.fake.AddIssue("Fix the thing", "It is broken.")
	task := lpIssueTask(t, env, issue)

	lpSetStatus(t, env, task.ID, func(x *domain.Task) {
		x.Status, x.PRNumber, x.Summary = domain.TaskDone, 12, "Opened a PR."
	})

	require.Empty(t, env.fake.Comments[issue.Number])
}

func TestIssueReportSkipsNonIssueTask(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	issue := env.fake.AddIssue("Unrelated", "Not a task source.")
	task, err := env.e.createTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindFeature, Title: "chat task", Body: "do it",
		Source: "chat",
	})
	require.NoError(t, err)

	lpSetStatus(t, env, task.ID, func(x *domain.Task) {
		x.Status, x.Summary = domain.TaskDone, "Finished quietly."
	})

	require.Empty(t, env.fake.Comments[issue.Number])
	require.Empty(t, env.fake.Comments)
}
