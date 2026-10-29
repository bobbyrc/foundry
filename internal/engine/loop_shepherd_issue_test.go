package engine

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/scm"
)

// lpStagingShepherdEnv is lpShepherdEnv for a project whose default branch is staging, so a
// merged fix does not close its issue on its own.
func lpStagingShepherdEnv(t *testing.T) (*lpEnv, domain.Task) {
	t.Helper()
	repo := lpNewRepo(t)
	lpGit(t, repo.seed, "push", "origin", "main:refs/heads/staging")
	repo.pushBranch(t, lpBranch, lpNotesFile)
	env := lpNewEngine(t, lpDoc(t, repo.clone, "    default_branch: staging\n", ""))
	env.repo = repo
	env.head = lpRevParse(t, repo.seed, lpBranch)
	env.fake.HeadSHA = func(string) string { return env.head }
	return env, lpReviewTask(t, env, lpBranch)
}

// lpLinkIssue makes task an intake task for a new fake issue and returns the issue.
func lpLinkIssue(t *testing.T, env *lpEnv, task domain.Task) scm.Issue {
	t.Helper()
	issue := env.fake.AddIssue("Notes are lost", "Save the notes.")
	lpSetTask(t, env, task.ID, func(x *domain.Task) {
		x.Source = "github_issue"
		x.SourceRef = "github_issue:" + strconv.Itoa(issue.Number)
	})
	return issue
}

func TestLoopShepherdAutoMergeClosesIssueTask(t *testing.T) {
	env, task := lpStagingShepherdEnv(t)
	issue := lpLinkIssue(t, env, task)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })

	env.run(loopPRShepherd)

	if got := env.task(task.ID); got.Status != domain.TaskMerged {
		t.Fatalf("task = %s, want merged", got.Status)
	}
	want := fmt.Sprintf("Fixed by #%d (merged into `staging`).", task.PRNumber)
	if is := env.fake.Issues[issue.Number]; is.State != "closed" {
		t.Fatalf("issue state = %s, want closed", is.State)
	}
	if got := env.fake.Comments[issue.Number]; len(got) != 1 || got[0] != want {
		t.Fatalf("issue comments = %q, want [%q]", got, want)
	}
}

func TestLoopShepherdExternalMergeClosesIssueTask(t *testing.T) {
	env, task := lpStagingShepherdEnv(t)
	issue := lpLinkIssue(t, env, task)
	pr := env.fake.PRs[task.PRNumber]
	pr.State = "merged"
	pr.MergeSHA = "external-sha"

	env.run(loopPRShepherd)

	if got := env.task(task.ID); got.Status != domain.TaskMerged {
		t.Fatalf("task = %s, want merged", got.Status)
	}
	want := fmt.Sprintf("Fixed by #%d (merged into `staging`).", task.PRNumber)
	if is := env.fake.Issues[issue.Number]; is.State != "closed" {
		t.Fatalf("issue state = %s, want closed", is.State)
	}
	if got := env.fake.Comments[issue.Number]; len(got) != 1 || got[0] != want {
		t.Fatalf("issue comments = %q, want [%q]", got, want)
	}
}

func TestLoopShepherdMergeLeavesNonIssueTaskAlone(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	bystander := env.fake.AddIssue("Unrelated", "not linked to any task")
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })

	env.run(loopPRShepherd)

	if got := env.task(task.ID); got.Status != domain.TaskMerged {
		t.Fatalf("task = %s, want merged", got.Status)
	}
	if is := env.fake.Issues[bystander.Number]; is.State != "open" || len(env.fake.Comments[bystander.Number]) != 0 {
		t.Fatalf("unlinked issue touched: state %s comments %q", is.State, env.fake.Comments[bystander.Number])
	}
}

func TestLoopShepherdFailedIssueCloseDoesNotBlockMerge(t *testing.T) {
	env, task := lpStagingShepherdEnv(t)
	lpSetTask(t, env, task.ID, func(x *domain.Task) {
		x.Source = "github_issue"
		x.SourceRef = "github_issue:999" // no such issue: the close fails
		x.ApprovedSHA = env.head
	})

	env.run(loopPRShepherd)

	if got := env.task(task.ID); got.Status != domain.TaskMerged {
		t.Fatalf("task = %s, want merged even though the issue close failed", got.Status)
	}
}
