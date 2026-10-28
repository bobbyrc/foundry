package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/scm"
	"github.com/bobbyrc/onlet/internal/store"
)

const lpBranch = "onlet/aaaa1111-add-notes"

// lpShepherdEnv returns an engine over a real git remote. The PR head is the real commit of
// lpBranch, so the shepherd's diff of that head works, and it stays put until the test pushes.
func lpShepherdEnv(t *testing.T, files map[string]string) (*lpEnv, domain.Task) {
	t.Helper()
	repo := lpNewRepo(t)
	repo.pushBranch(t, lpBranch, files)
	env := lpNewEngine(t, lpDoc(t, repo.clone, "", ""))
	env.repo = repo
	env.head = lpRevParse(t, repo.seed, lpBranch)
	env.fake.HeadSHA = func(string) string { return env.head }
	task := lpReviewTask(t, env, lpBranch)
	return env, task
}

// addCommit pushes a commit to lpBranch, as a human push would, and makes it the PR head.
func (env *lpEnv) addCommit(t *testing.T, files map[string]string) string {
	t.Helper()
	env.repo.extend(t, lpBranch, files)
	env.head = lpRevParse(t, env.repo.seed, lpBranch)
	return env.head
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
	if want := "review:" + task.ID + ":" + env.head; reviews[0].SourceRef != want || reviews[0].ParentID != task.ID {
		t.Fatalf("review = ref %q parent %q, want ref %q parent %q", reviews[0].SourceRef, reviews[0].ParentID, want, task.ID)
	}
	if len(env.fake.Merged) != 0 {
		t.Fatal("merged before a reviewer approved")
	}

	env.addCommit(t, map[string]string{"more.txt": "two\n"})
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
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })

	env.run(loopPRShepherd)

	got := env.task(task.ID)
	if got.Status != domain.TaskMerged || got.MergeSHA != env.head {
		t.Fatalf("task = %s merge %q, want merged at the judged head", got.Status, got.MergeSHA)
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
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })

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

// A merge that GitHub is still computing is judged on a later pass, never merged now.
func TestLoopShepherdSkipsUnknownMergeability(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	env.fake.PRs[task.PRNumber].Mergeable = "unknown"

	env.run(loopPRShepherd)

	if got := env.task(task.ID); got.Status != domain.TaskReview {
		t.Fatalf("status = %s, want review while mergeability is unknown", got.Status)
	}
	if len(env.fake.Merged) != 0 {
		t.Fatalf("merged while mergeability was unknown: %v", env.fake.Merged)
	}
}

// Awaiting approval with nothing pending means the request is gone; the task is judged again.
func TestLoopShepherdAwaitingWithoutApprovalReturnsToReview(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.Status = domain.TaskAwaitingApproval })

	env.run(loopPRShepherd)

	if got := env.task(task.ID); got.Status != domain.TaskReview {
		t.Fatalf("status = %s, want review when no approval is pending", got.Status)
	}
}

// An approval asked about an older PR head is rejected and the task goes back to review.
func TestLoopShepherdSupersedesApprovalForOldHead(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.Status = domain.TaskAwaitingApproval })
	if _, err := env.st.CreateApproval(env.ctx, domain.Approval{
		TaskID: task.ID, Project: "demo", Kind: "merge", Title: "Merge: Add notes",
		HeadSHA: "sha-0", Status: domain.ApprovalPending,
	}); err != nil {
		t.Fatal(err)
	}

	env.run(loopPRShepherd)

	if got := env.task(task.ID); got.Status != domain.TaskReview {
		t.Fatalf("status = %s, want review after the head changed", got.Status)
	}
	rejected, err := env.st.ListApprovals(env.ctx, domain.ApprovalRejected)
	if err != nil {
		t.Fatal(err)
	}
	if len(rejected) != 1 || rejected[0].Note != "superseded: the pull request changed" {
		t.Fatalf("rejected approvals = %+v, want one superseded by the head change", rejected)
	}
}

// An approval for the current head keeps the task waiting.
func TestLoopShepherdApprovalForCurrentHeadKeepsWaiting(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.Status = domain.TaskAwaitingApproval })
	if _, err := env.st.CreateApproval(env.ctx, domain.Approval{
		TaskID: task.ID, Project: "demo", Kind: "merge", Title: "Merge: Add notes",
		HeadSHA: env.head, Status: domain.ApprovalPending,
	}); err != nil {
		t.Fatal(err)
	}

	env.run(loopPRShepherd)

	if got := env.task(task.ID); got.Status != domain.TaskAwaitingApproval {
		t.Fatalf("status = %s, want awaiting_approval for the current head", got.Status)
	}
	pending, err := env.st.ListApprovals(env.ctx, domain.ApprovalPending)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending approvals = %d (err %v), want 1", len(pending), err)
	}
}

// The approval records the PR head it was asked about.
func TestLoopShepherdApprovalRecordsHeadSHA(t *testing.T) {
	env, task := lpShepherdEnv(t, map[string]string{"migrations/0001_init.sql": "create table t();\n"})
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })

	env.run(loopPRShepherd)

	pending, err := env.st.ListApprovals(env.ctx, domain.ApprovalPending)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending approvals = %d (err %v), want 1", len(pending), err)
	}
	if pending[0].TaskID != task.ID || pending[0].HeadSHA != env.head {
		t.Fatalf("approval task %s head %q, want task %s head of the PR", pending[0].TaskID, pending[0].HeadSHA, task.ID)
	}
}

// mergeTask refuses a task that is not waiting for a merge and merges nothing.
func TestMergeTaskRefusesTaskNotWaiting(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.Status = domain.TaskDone })

	err := env.e.mergeTask(env.ctx, task.ID, env.head)
	if err == nil || !strings.Contains(err.Error(), "task is done") {
		t.Fatalf("mergeTask error = %v, want 'task is done'", err)
	}
	if len(env.fake.Merged) != 0 {
		t.Fatalf("merged a task that was done: %v", env.fake.Merged)
	}
}

// mergeTask merges only the head it was given; a head that moved is not merged.
func TestMergeTaskMergesOnlyGivenHead(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.fake.HeadSHA = func(string) string { return "sha-2" }

	if err := env.e.mergeTask(env.ctx, task.ID, env.head); err == nil {
		t.Fatal("mergeTask merged a head that is no longer the PR head")
	}
	if len(env.fake.Merged) != 0 {
		t.Fatalf("merged despite a moved head: %v", env.fake.Merged)
	}
	if got := env.task(task.ID); got.Status != domain.TaskReview {
		t.Fatalf("status = %s, want review after a refused merge", got.Status)
	}
}

// An approved task in awaiting_approval merges at the approved head and is marked merged.
func TestMergeTaskMergesAwaitingApproval(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.Status = domain.TaskAwaitingApproval })

	if err := env.e.mergeTask(env.ctx, task.ID, env.head); err != nil {
		t.Fatalf("mergeTask: %v", err)
	}
	got := env.task(task.ID)
	if got.Status != domain.TaskMerged || got.MergeSHA != env.head {
		t.Fatalf("task = %s merge %q, want merged at the judged head", got.Status, got.MergeSHA)
	}
	if n := len(env.e.loopKickChan(loopDeployWatch)); n != 1 {
		t.Fatalf("deploy watch kicks = %d, want 1", n)
	}
}

// A push between the shepherd's evaluation and the merge makes MergePR refuse the judged head.
// That is not a failure: the task stays in review so the next pass judges the new head.
func TestLoopShepherdHeadMovedBeforeMergeStaysInReview(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	// The first read (the shepherd's evaluation) sees the old head; every later read sees a push.
	first := env.head
	reads := 0
	second := env.addCommit(t, map[string]string{"moved.txt": "pushed later\n"})
	env.fake.HeadSHA = func(string) string {
		reads++
		if reads == 1 {
			return first
		}
		return second
	}

	env.run(loopPRShepherd)

	got := env.task(task.ID)
	if got.Status != domain.TaskReview || got.BlockedReason != "" {
		t.Fatalf("task = %s %q, want review and not blocked after a push", got.Status, got.BlockedReason)
	}
	if len(env.fake.Merged) != 0 {
		t.Fatalf("merged despite the moved head: %v", env.fake.Merged)
	}
}

// A merge refused at the head the shepherd judged is a real failure and still blocks the task.
func TestLoopShepherdMergeFailureAtSameHeadBlocks(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	env.fake.MergeErr = errors.New("branch protection rejected the merge")

	env.run(loopPRShepherd)

	got := env.task(task.ID)
	if got.Status != domain.TaskBlocked || !strings.Contains(got.BlockedReason, "merge failed: ") ||
		!strings.Contains(got.BlockedReason, "branch protection rejected the merge") {
		t.Fatalf("task = %s %q, want blocked with the merge failure", got.Status, got.BlockedReason)
	}
	if len(env.fake.Merged) != 0 {
		t.Fatalf("merged despite the merge error: %v", env.fake.Merged)
	}
}

// A head whose CI reports no checks waits for the ci_grace period after the shepherd first sees
// it, so checks that register a few seconds late are not missed. Nothing merges inside it.
func TestLoopShepherdWaitsForCIRegistrationAfterPush(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.fake.SetCI(task.PRNumber, scm.CINone)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	first := env.now

	env.run(loopPRShepherd)
	if got := env.task(task.ID); got.Status != domain.TaskReview || got.CIHeadSHA != env.head {
		t.Fatalf("status = %s ci head %q, want review with the head recorded", got.Status, got.CIHeadSHA)
	}
	if len(env.fake.Merged) != 0 {
		t.Fatalf("merged within the CI grace period: %v", env.fake.Merged)
	}

	env.now = first.Add(2 * time.Minute)
	env.run(loopPRShepherd)
	if len(env.fake.Merged) != 0 {
		t.Fatalf("merged 2m after the head was seen, inside the 3m grace: %v", env.fake.Merged)
	}

	env.now = first.Add(4 * time.Minute)
	env.run(loopPRShepherd)
	if got := env.task(task.ID); got.Status != domain.TaskMerged {
		t.Fatalf("status = %s after the grace, want merged", got.Status)
	}
}

// A PR that never registers checks is judged as having no CI once the grace period has passed.
func TestLoopShepherdNoCIPassesAfterGrace(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.fake.SetCI(task.PRNumber, scm.CINone)
	first := env.now

	env.run(loopPRShepherd)
	if n := len(env.tasks(domain.KindReview)); n != 0 {
		t.Fatalf("got %d review tasks inside the grace period, want 0", n)
	}

	env.now = first.Add(4 * time.Minute)
	env.run(loopPRShepherd)
	if n := len(env.tasks(domain.KindReview)); n != 1 {
		t.Fatalf("got %d review tasks after the grace, want 1: a repo without CI must pass the gate", n)
	}
}

// A push by a human is a new head, and it gets the same CI grace as onlet's own push. The old
// head's grace does not carry over, so the new head waits again.
func TestLoopShepherdHumanPushRestartsCIGrace(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	env.fake.SetCI(task.PRNumber, scm.CINone)
	first := env.now
	env.run(loopPRShepherd)
	env.now = first.Add(4 * time.Minute)
	env.run(loopPRShepherd)
	if n := len(env.tasks(domain.KindReview)); n != 1 {
		t.Fatalf("got %d review tasks before the human push, want 1", n)
	}

	// The human pushes a new head, long after the first grace has passed.
	pushed := env.now
	newHead := env.addCommit(t, map[string]string{"human.txt": "fix\n"})
	env.run(loopPRShepherd)
	got := env.task(task.ID)
	if got.CIHeadSHA != newHead || got.CIHeadSeenAt == nil || !got.CIHeadSeenAt.Equal(pushed) {
		t.Fatalf("ci head %q seen %v, want the new head seen now", got.CIHeadSHA, got.CIHeadSeenAt)
	}

	env.now = pushed.Add(2 * time.Minute)
	env.run(loopPRShepherd)
	if n := len(env.tasks(domain.KindReview)); n != 1 {
		t.Fatalf("got %d review tasks 2m after the human push, want 1: the new head waits for the grace", n)
	}

	env.now = pushed.Add(4 * time.Minute)
	env.run(loopPRShepherd)
	if n := len(env.tasks(domain.KindReview)); n != 2 {
		t.Fatalf("got %d review tasks after the new head's grace, want 2", n)
	}
}

// With require_checks on, a head with no checks never passes the CI gate: it waits, then the task
// is blocked after 30 minutes on that head.
func TestLoopShepherdRequireChecksNeverMergesWithoutChecks(t *testing.T) {
	repo := lpNewRepo(t)
	repo.pushBranch(t, lpBranch, lpNotesFile)
	doc := lpDoc(t, repo.clone, "", "policies:\n  default:\n    require_checks: true\n")
	env := lpNewEngine(t, doc)
	env.repo = repo
	env.head = lpRevParse(t, repo.seed, lpBranch)
	env.fake.HeadSHA = func(string) string { return env.head }
	task := lpReviewTask(t, env, lpBranch)
	env.fake.SetCI(task.PRNumber, scm.CINone)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	first := env.now

	for _, wait := range []time.Duration{0, 4 * time.Minute, 20 * time.Minute} {
		env.now = first.Add(wait)
		env.run(loopPRShepherd)
		if len(env.fake.Merged) != 0 {
			t.Fatalf("merged at %v with no checks and require_checks on: %v", wait, env.fake.Merged)
		}
		if got := env.task(task.ID); got.Status != domain.TaskReview {
			t.Fatalf("status = %s at %v, want review while waiting for checks", got.Status, wait)
		}
	}

	env.now = first.Add(31 * time.Minute)
	env.run(loopPRShepherd)
	got := env.task(task.ID)
	want := "no CI checks reported for " + env.head + " (require_checks is on)"
	if got.Status != domain.TaskBlocked || got.BlockedReason != want {
		t.Fatalf("task = %s %q, want blocked with %q", got.Status, got.BlockedReason, want)
	}
	if len(env.fake.Merged) != 0 {
		t.Fatalf("merged after the block: %v", env.fake.Merged)
	}
}

// A PR head that is empty cannot be pinned to anything, so the shepherd skips the task: it does
// not approve the head, diff it or merge it.
func TestLoopShepherdSkipsEmptyHead(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.ApprovedSHA = env.head })
	env.fake.HeadSHA = func(string) string { return "" }

	env.run(loopPRShepherd)

	got := env.task(task.ID)
	if got.Status != domain.TaskReview || got.BlockedReason != "" {
		t.Fatalf("task = %s %q, want review and not blocked on an empty head", got.Status, got.BlockedReason)
	}
	if len(env.fake.Merged) != 0 {
		t.Fatalf("merged an unpinned head: %v", env.fake.Merged)
	}
	if got.CIHeadSHA != "" {
		t.Fatalf("recorded an empty head as seen: %q", got.CIHeadSHA)
	}
}

// A review that was cancelled or failed is not a verdict on the head: the next pass starts a new
// one. A review that is blocked still blocks the task.
func TestLoopShepherdRetriesCancelledOrFailedReview(t *testing.T) {
	for _, status := range []domain.TaskStatus{domain.TaskCancelled, domain.TaskFailed} {
		t.Run(string(status), func(t *testing.T) {
			env, task := lpShepherdEnv(t, lpNotesFile)
			env.run(loopPRShepherd)
			review := env.tasks(domain.KindReview)[0]
			lpSetTask(t, env, review.ID, func(x *domain.Task) { x.Status = status })

			env.run(loopPRShepherd)

			reviews := env.tasks(domain.KindReview)
			if len(reviews) != 2 {
				t.Fatalf("got %d reviews after a %s one, want a new review", len(reviews), status)
			}
			if got := env.task(task.ID); got.Status != domain.TaskReview {
				t.Fatalf("task = %s, want review while the new reviewer runs", got.Status)
			}
		})
	}
}

// A change of risk that the policy explains with no reasons reads "risk low", not "risk low: ".
func TestLoopShepherdPolicyEventWithoutReasons(t *testing.T) {
	env, task := lpShepherdEnv(t, lpNotesFile)
	lpSetTask(t, env, task.ID, func(x *domain.Task) { x.RiskReasons = []string{"stale"} })

	env.run(loopPRShepherd)

	evs, err := env.st.ListEvents(env.ctx, store.EventFilter{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Type == domain.EvPolicyDecision && ev.Message == "risk low" {
			return
		}
	}
	t.Fatal("no policy.decision event with message \"risk low\"")
}
