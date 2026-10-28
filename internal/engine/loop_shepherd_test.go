package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/scm"
)

const lpBranch = "foundry/aaaa1111-add-notes"

// lpShepherdEnv returns an engine over a real git remote, with a head SHA that is stable until
// the test changes it.
func lpShepherdEnv(t *testing.T, files map[string]string) (*lpEnv, domain.Task) {
	t.Helper()
	repo := lpNewRepo(t)
	repo.pushBranch(t, lpBranch, files)
	env := lpNewEngine(t, lpDoc(t, repo.clone, "", ""))
	env.fake.HeadSHA = func(string) string { return "sha-1" }
	task := lpReviewTask(t, env, lpBranch)
	return env, task
}

// lpReviewTask opens a PR for branch and creates a low-risk task in review for it.
func lpReviewTask(t *testing.T, env *lpEnv, branch string) domain.Task {
	t.Helper()
	pr, err := env.fake.OpenPR(env.ctx, scm.PRSpec{Head: branch, Base: "main", Title: "Add notes"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := env.st.CreateTask(env.ctx, domain.Task{
		Project:     "demo",
		Kind:        domain.KindFeature,
		Role:        "implementer",
		Title:       "Add notes",
		Status:      domain.TaskReview,
		Branch:      branch,
		PRNumber:    pr.Number,
		PRURL:       pr.URL,
		Risk:        domain.RiskLow,
		MaxAttempts: 3,
		Source:      "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// lpSetTask edits a task directly in the store, for setting up a state the loop would reach.
func lpSetTask(t *testing.T, env *lpEnv, id string, fn func(*domain.Task)) {
	t.Helper()
	if _, err := env.st.UpdateTask(env.ctx, id, func(x *domain.Task) error {
		fn(x)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

var lpNotesFile = map[string]string{"notes.txt": "one\n"}

func TestLoopShepherdWaitsWhileCIPending(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.fake.SetCI(task.PRNumber, scm.CIPending)

	env.run(loopPRShepherd)

	if got := env.task(task.ID); got.Status != domain.TaskReview {
		t.Fatalf("status = %s, want review while CI is pending", got.Status)
	}
	if n := len(env.tasks(domain.KindReview)); n != 0 {
		t.Fatalf("review run created while CI pending: %d", n)
	}
	if len(env.fake.Merged) != 0 {
		t.Fatalf("merged while CI pending: %v", env.fake.Merged)
	}
}

func TestLoopShepherdRequeuesFailedCIWithLog(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.fake.SetCI(task.PRNumber, scm.CIFailure, "unit-tests")

	env.run(loopPRShepherd)

	got := env.task(task.ID)
	if got.Status != domain.TaskQueued {
		t.Fatalf("status = %s, want queued for another attempt", got.Status)
	}
	if !strings.Contains(got.Body, "## Attempt 0: CI failed: unit-tests") {
		t.Fatalf("body lacks the attempt note: %q", got.Body)
	}
	if !strings.Contains(got.Body, fmt.Sprintf("fake CI log for #%d", task.PRNumber)) {
		t.Fatalf("body lacks the CI log: %q", got.Body)
	}
}

func TestLoopShepherdRequeuesConflict(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.fake.PRs[task.PRNumber].Mergeable = "conflicting"

	env.run(loopPRShepherd)

	got := env.task(task.ID)
	if got.Status != domain.TaskQueued {
		t.Fatalf("status = %s, want queued", got.Status)
	}
	if !strings.Contains(got.Body, "merge conflict with main") {
		t.Fatalf("body lacks the conflict note: %q", got.Body)
	}
}

func TestLoopShepherdReviewCreatedOncePerHeadSHA(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)

	env.run(loopPRShepherd)
	env.run(loopPRShepherd)
	reviews := env.tasks(domain.KindReview)
	if len(reviews) != 1 {
		t.Fatalf("got %d review tasks after two passes, want 1", len(reviews))
	}
	if want := "review:" + task.ID + ":sha-1"; reviews[0].SourceRef != want || reviews[0].ParentID != task.ID {
		t.Fatalf("review = ref %q parent %q, want ref %q parent %q", reviews[0].SourceRef, reviews[0].ParentID, want, task.ID)
	}
	if len(env.fake.Merged) != 0 {
		t.Fatal("merged before a reviewer approved")
	}

	env.fake.HeadSHA = func(string) string { return "sha-2" }
	env.run(loopPRShepherd)
	if n := len(env.tasks(domain.KindReview)); n != 2 {
		t.Fatalf("got %d review tasks after a new push, want 2", n)
	}
}

func TestLoopShepherdBlocksWhenReviewFails(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.run(loopPRShepherd)
	review := env.tasks(domain.KindReview)[0]
	lpSetTask(t, env, review.ID, func(x *domain.Task) {
		x.Status = domain.TaskBlocked
		x.BlockedReason = "agent denied a tool"
	})

	env.run(loopPRShepherd)

	got := env.task(task.ID)
	if got.Status != domain.TaskBlocked || !strings.Contains(got.BlockedReason, "review could not complete: agent denied a tool") {
		t.Fatalf("task = %s %q, want blocked with the review's reason", got.Status, got.BlockedReason)
	}
}

func TestLoopShepherdAutoMergesApprovedLowRisk(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = "sha-1" })

	env.run(loopPRShepherd)

	got := env.task(task.ID)
	if got.Status != domain.TaskMerged || got.MergeSHA != "sha-1" {
		t.Fatalf("task = %s merge %q, want merged at sha-1", got.Status, got.MergeSHA)
	}
	if len(env.fake.Merged) != 1 || env.fake.Merged[0] != task.PRNumber {
		t.Fatalf("fake merged %v, want [%d]", env.fake.Merged, task.PRNumber)
	}
	if n := len(env.e.loopKickChan(loopDeployWatch)); n != 1 {
		t.Fatalf("deploy watch kicks = %d, want 1", n)
	}
}

func TestLoopShepherdHighRiskNeedsApprovalOnce(t *testing.T) {
	env, task := lpShepherdEnv(t, map[string]string{"migrations/0001_init.sql": "create table t();\n"})
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = "sha-1" })

	env.run(loopPRShepherd)
	env.run(loopPRShepherd)

	pending, err := env.st.ListApprovals(env.ctx, domain.ApprovalPending)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TaskID != task.ID || pending[0].Kind != "merge" {
		t.Fatalf("pending approvals = %+v, want one merge approval for the task", pending)
	}
	if !strings.Contains(pending[0].Reason, "database schema or migration") {
		t.Fatalf("approval reason lacks the policy reason: %q", pending[0].Reason)
	}
	got := env.task(task.ID)
	if got.Status != domain.TaskAwaitingApproval || got.Risk != domain.RiskHigh {
		t.Fatalf("task = %s risk %s, want awaiting_approval at high", got.Status, got.Risk)
	}
	if len(env.fake.Merged) != 0 {
		t.Fatal("merged without approval")
	}
}

func TestLoopShepherdSeesExternalMerge(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	pr := env.fake.PRs[task.PRNumber]
	pr.State = "merged"
	pr.MergeSHA = "external-sha"

	env.run(loopPRShepherd)

	got := env.task(task.ID)
	if got.Status != domain.TaskMerged || got.MergeSHA != "external-sha" {
		t.Fatalf("task = %s merge %q, want merged at external-sha", got.Status, got.MergeSHA)
	}
	if n := len(env.e.loopKickChan(loopDeployWatch)); n != 1 {
		t.Fatalf("deploy watch kicks = %d, want 1", n)
	}
}

func TestLoopShepherdCancelsClosedPR(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.fake.PRs[task.PRNumber].State = "closed"

	env.run(loopPRShepherd)

	if got := env.task(task.ID); got.Status != domain.TaskCancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}
}

func TestLoopShepherdWithoutPRBlocks(t *testing.T) {
	env, _ := lpShepherdEnv(t, lpNotesFile)
	orphan, err := env.st.CreateTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindFeature, Role: "implementer", Title: "No PR",
		Status: domain.TaskReview, MaxAttempts: 3, Source: "api",
	})
	if err != nil {
		t.Fatal(err)
	}

	env.run(loopPRShepherd)

	got := env.task(orphan.ID)
	if got.Status != domain.TaskBlocked || got.BlockedReason != "in review without a pull request" {
		t.Fatalf("task = %s %q, want blocked for having no PR", got.Status, got.BlockedReason)
	}
}
