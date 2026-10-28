package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onlet/internal/config"
	"github.com/bobbyrc/onlet/internal/deploy"
	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/events"
	"github.com/bobbyrc/onlet/internal/harness"
	_ "github.com/bobbyrc/onlet/internal/harness/fake" // registers the "fake" harness type
	"github.com/bobbyrc/onlet/internal/observe"
	"github.com/bobbyrc/onlet/internal/scm"
	scmfake "github.com/bobbyrc/onlet/internal/scm/fake"
	"github.com/bobbyrc/onlet/internal/store"
	"github.com/bobbyrc/onlet/internal/store/sqlite"
	"github.com/bobbyrc/onlet/internal/workspace"
)

// e2eConfigYAML is the onlet.yaml every end-to-end test starts from. Arguments: data dir,
// bare repo path, clone path, and extra top-level YAML (column zero, may be empty).
const e2eConfigYAML = `server:
  data_dir: %s
limits:
  stall_timeout: 5s
  run_timeout: 30s
harnesses:
  fake: {type: fake}
agents:
  orchestrator: {harness: fake, prompt: builtin:orchestrator}
  implementer: {harness: fake, prompt: builtin:implementer}
  reviewer: {harness: fake, prompt: builtin:reviewer}
  triager: {harness: fake, prompt: builtin:triager}
  sre: {harness: fake, prompt: builtin:sre}
  improver: {harness: fake, prompt: builtin:improver}
projects:
  demo:
    repo: %s
    path: %s
    default_branch: main
    deploy: {type: noop, verify_for: 1s}
loops:
  pr_shepherd: {every: 1s}
  deploy_watch: {every: 1s}
  prod_watch: {every: 1s}
  issue_intake: {enabled: false, every: 1s}
  friction: {enabled: false, every: 1s}
%s`

// e2eProject is the only project in the e2e config.
const e2eProject = "demo"

// e2eEnv is a real engine with its scheduler and loops running, wired to an in-memory store, a
// local bare repository, a fake SCM and the fake harness.
type e2eEnv struct {
	t   *testing.T
	ctx context.Context
	e   *Engine
	st  store.Store
	scm *scmfake.Fake
}

// e2eNewEngine builds the engine, starts Run in a goroutine, and stops it on cleanup.
func e2eNewEngine(t *testing.T, yamlExtra string) *e2eEnv {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	clone := filepath.Join(root, "clone")
	dataDir := filepath.Join(root, "data")
	e2eSeedRepo(t, bare)

	doc := fmt.Sprintf(e2eConfigYAML, dataDir, bare, clone, yamlExtra)
	cfg, err := config.Parse([]byte(doc), "")
	require.NoError(t, err, doc)

	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fakeSCM := scmfake.New()
	fakeSCM.HeadSHA = e2eHeadSHA(clone)

	e, err := New(Deps{
		Config:     cfg,
		Store:      st,
		Bus:        events.NewBus(st, logger),
		Workspaces: workspace.New(filepath.Join(dataDir, "worktrees")),
		Logger:     logger,
		SCMFor:     func(*config.Project) (scm.SCM, error) { return fakeSCM, nil },
		HarnessFor: func(name string, h config.Harness) (harness.Harness, error) {
			return harness.New(harness.Config{Name: name, Type: h.Type})
		},
		DeployerFor: func(p *config.Project) (deploy.Deployer, error) {
			if p.Deploy == nil {
				return nil, fmt.Errorf("project %s has no deploy block", p.Name)
			}
			return deploy.New(deploy.Config{Type: p.Deploy.Type, Options: p.Deploy.Options})
		},
		ObserversFor:  func(*config.Project) (map[string]observe.Observer, error) { return nil, nil },
		SchedulerTick: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("engine Run returned an error: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Errorf("engine did not stop within 30s of cancel")
		}
	})

	return &e2eEnv{t: t, ctx: context.Background(), e: e, st: st, scm: fakeSCM}
}

// e2eGit runs git in dir (or the current directory when dir is "") with a fixed identity and
// fails the test on error.
func e2eGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=E2E", "GIT_AUTHOR_EMAIL=e2e@example.com",
		"GIT_COMMITTER_NAME=E2E", "GIT_COMMITTER_EMAIL=e2e@example.com")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

// e2eSeedRepo creates a bare repository whose main branch has one commit (README.md).
func e2eSeedRepo(t *testing.T, bare string) {
	t.Helper()
	e2eGit(t, "", "init", "--bare", "-b", "main", bare)
	seed := t.TempDir()
	e2eGit(t, "", "clone", bare, seed)
	require.NoError(t, os.WriteFile(filepath.Join(seed, "README.md"), []byte("demo\n"), 0o644))
	e2eGit(t, seed, "add", "README.md")
	e2eGit(t, seed, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-m", "init")
	e2eGit(t, seed, "push", "origin", "HEAD:main")
}

// e2eHeadSHA returns the fake SCM's head-commit hook. It fetches the clone and reads
// origin/<branch>, so the PR head the shepherd sees is the commit the agents pushed.
func e2eHeadSHA(clone string) func(string) string {
	return func(branch string) string {
		if _, err := os.Stat(clone); err != nil {
			return "" // no run has cloned the repository yet
		}
		_ = exec.Command("git", "-C", clone, "fetch", "-q", "origin").Run()
		out, err := exec.Command("git", "-C", clone, "rev-parse", "--verify", "--quiet", "origin/"+branch).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
}

// e2eCreate queues a task in project demo through the engine's controller API.
func e2eCreate(v *e2eEnv, t domain.Task) domain.Task {
	v.t.Helper()
	t.Project = e2eProject
	t.Kind = domain.KindFeature
	got, err := v.e.CreateTask(v.ctx, t)
	require.NoError(v.t, err)
	return got
}

// e2eTask reads one task from the store.
func e2eTask(v *e2eEnv, id string) domain.Task {
	v.t.Helper()
	got, err := v.st.GetTask(v.ctx, id)
	require.NoError(v.t, err)
	return got
}

// e2eChildren returns the child tasks of parentID with the given kind.
func e2eChildren(v *e2eEnv, parentID string, kind domain.TaskKind) []domain.Task {
	v.t.Helper()
	out, err := v.st.ListTasks(v.ctx, store.TaskFilter{Project: e2eProject, ParentID: parentID, Kind: kind, Limit: 1000})
	require.NoError(v.t, err)
	return out
}

// e2eRuns returns the runs of one task, newest first.
func e2eRuns(v *e2eEnv, taskID string) []domain.Run {
	v.t.Helper()
	out, err := v.st.ListRuns(v.ctx, store.RunFilter{TaskID: taskID, Limit: 1000})
	require.NoError(v.t, err)
	return out
}

// e2eEvents returns the events of one task, oldest first.
func e2eEvents(v *e2eEnv, taskID string) []domain.Event {
	v.t.Helper()
	out, err := v.st.ListEvents(v.ctx, store.EventFilter{TaskID: taskID, Limit: 500})
	require.NoError(v.t, err)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// e2eDeployment returns the newest deployment of taskID, or ok=false.
func e2eDeployment(v *e2eEnv, taskID string) (domain.Deployment, bool) {
	v.t.Helper()
	list, err := v.st.ListDeployments(v.ctx, e2eProject, 100)
	require.NoError(v.t, err)
	for _, d := range list {
		if d.TaskID == taskID {
			return d, true
		}
	}
	return domain.Deployment{}, false
}

// e2ePendingApprovals returns the pending approvals of taskID.
func e2ePendingApprovals(v *e2eEnv, taskID string) []domain.Approval {
	v.t.Helper()
	list, err := v.st.ListApprovals(v.ctx, domain.ApprovalPending)
	require.NoError(v.t, err)
	var out []domain.Approval
	for _, a := range list {
		if a.TaskID == taskID {
			out = append(out, a)
		}
	}
	return out
}

// e2eWait polls cond every 100ms until it holds. On timeout it fails the test with a dump of
// every task and the last 30 events, so the stall can be read from the failure output.
func e2eWait(v *e2eEnv, what string, timeout time.Duration, cond func() bool) {
	v.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			v.t.Fatalf("timed out after %s waiting for: %s\n%s", timeout, what, e2eDump(v))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// e2eDump describes every task in project demo, its runs, the last 30 events, and deployments.
func e2eDump(v *e2eEnv) string {
	var b strings.Builder
	tasks, err := v.st.ListTasks(v.ctx, store.TaskFilter{Project: e2eProject, Limit: 1000})
	if err != nil {
		return "dump: list tasks: " + err.Error()
	}
	fmt.Fprintf(&b, "tasks (%d):\n", len(tasks))
	for _, t := range tasks {
		var runs []string
		for _, r := range e2eRuns(v, t.ID) {
			runs = append(runs, fmt.Sprintf("%s:%s", r.Role, r.Status))
		}
		fmt.Fprintf(&b, "  id=%s kind=%s status=%s blocked_reason=%q pr=%d parent=%s source_ref=%q attempts=%d/%d risk=%s runs=[%s] title=%q\n",
			t.ID, t.Kind, t.Status, t.BlockedReason, t.PRNumber, t.ParentID, t.SourceRef,
			t.Attempts, t.MaxAttempts, t.Risk, strings.Join(runs, " "), t.Title)
	}
	if deps, err := v.st.ListDeployments(v.ctx, e2eProject, 20); err == nil {
		fmt.Fprintf(&b, "deployments (%d):\n", len(deps))
		for _, d := range deps {
			fmt.Fprintf(&b, "  id=%s task=%s ref=%s state=%s\n", d.ID, d.TaskID, d.Ref, d.State)
		}
	}
	evs, err := v.st.ListEvents(v.ctx, store.EventFilter{Limit: 500})
	if err != nil {
		return b.String() + "dump: list events: " + err.Error() + "\n"
	}
	sort.Slice(evs, func(i, j int) bool { return evs[i].ID < evs[j].ID })
	if len(evs) > 30 {
		evs = evs[len(evs)-30:]
	}
	fmt.Fprintf(&b, "last %d events (oldest first):\n", len(evs))
	for _, ev := range evs {
		fmt.Fprintf(&b, "  #%d %s task=%s run=%s: %s\n", ev.ID, ev.Type, ev.TaskID, ev.RunID, ev.Message)
	}
	return b.String()
}

// e2eEventIndex returns the position of the first event of typ in events, or -1.
func e2eEventIndex(events []domain.Event, typ string) int {
	for i, ev := range events {
		if ev.Type == typ {
			return i
		}
	}
	return -1
}

func TestE2ELowRiskAutoMergesAndDeploys(t *testing.T) {
	v := e2eNewEngine(t, "")
	task := e2eCreate(v, domain.Task{Title: "add fake file", Body: "[fake:commit]"})

	e2eWait(v, "task "+task.ID+" to reach done", 60*time.Second, func() bool {
		return e2eTask(v, task.ID).Status == domain.TaskDone
	})

	got := e2eTask(v, task.ID)
	reviews := e2eChildren(v, task.ID, domain.KindReview)
	require.Len(t, reviews, 1, "one review child task")
	assert.Equal(t, domain.TaskDone, reviews[0].Status, "review task is done")

	require.NotZero(t, got.PRNumber, "task has a PR")
	pr, err := v.scm.GetPR(v.ctx, got.PRNumber)
	require.NoError(t, err)
	assert.Equal(t, "merged", pr.State, "fake SCM merged the PR")
	assert.NotEmpty(t, got.MergeSHA, "task.MergeSHA is set")
	assert.Equal(t, domain.RiskLow, got.Risk, "task risk is low")

	dep, ok := e2eDeployment(v, task.ID)
	require.True(t, ok, "a deployment exists for the task")
	assert.Equal(t, domain.DeployHealthy, dep.State, "deployment is healthy")

	evs := e2eEvents(v, task.ID)
	opened := e2eEventIndex(evs, domain.EvPROpened)
	merged := e2eEventIndex(evs, domain.EvPRMerged)
	require.NotEqual(t, -1, opened, "pr.opened event for the task")
	require.NotEqual(t, -1, merged, "pr.merged event for the task")
	assert.Less(t, opened, merged, "pr.opened comes before pr.merged")
}

func TestE2EHighRiskNeedsApproval(t *testing.T) {
	v := e2eNewEngine(t, "")
	task := e2eCreate(v, domain.Task{Title: "risky change", Body: "[fake:commit] [fake:risk-high]"})

	var approval domain.Approval
	e2eWait(v, "task "+task.ID+" awaiting approval with a pending approval", 60*time.Second, func() bool {
		if e2eTask(v, task.ID).Status != domain.TaskAwaitingApproval {
			return false
		}
		pending := e2ePendingApprovals(v, task.ID)
		if len(pending) == 0 {
			return false
		}
		approval = pending[0]
		return true
	})

	waiting := e2eTask(v, task.ID)
	require.NotZero(t, waiting.PRNumber, "task has a PR")
	pr, err := v.scm.GetPR(v.ctx, waiting.PRNumber)
	require.NoError(t, err)
	assert.NotEqual(t, "merged", pr.State, "PR must not merge before approval")
	assert.Equal(t, domain.RiskHigh, waiting.Risk, "task risk is high")

	_, err = v.e.DecideApproval(v.ctx, approval.ID, true, "ok")
	require.NoError(t, err)

	e2eWait(v, "task "+task.ID+" to reach done after approval", 60*time.Second, func() bool {
		return e2eTask(v, task.ID).Status == domain.TaskDone
	})

	got := e2eTask(v, task.ID)
	pr, err = v.scm.GetPR(v.ctx, got.PRNumber)
	require.NoError(t, err)
	assert.Equal(t, "merged", pr.State, "PR merged after approval")

	dep, ok := e2eDeployment(v, task.ID)
	require.True(t, ok, "a deployment exists for the task")
	assert.Equal(t, domain.DeployHealthy, dep.State, "deployment is healthy")
}

func TestE2EReviewerRejectThenBlocks(t *testing.T) {
	v := e2eNewEngine(t, "")
	task := e2eCreate(v, domain.Task{
		Title:       "rejected change",
		Body:        "[fake:commit] [fake:reject]",
		MaxAttempts: 2,
	})

	e2eWait(v, "task "+task.ID+" to be blocked", 90*time.Second, func() bool {
		return e2eTask(v, task.ID).Status == domain.TaskBlocked
	})

	got := e2eTask(v, task.ID)
	assert.Contains(t, got.BlockedReason, "gave up after 2 attempts", "blocked reason")
	assert.Contains(t, got.BlockedReason, "reviewer requested changes", "blocked reason")
	assert.Contains(t, got.Body, "## Attempt 1: reviewer requested changes",
		"the first rejection is appended to the body for the next implementer run")
	assert.Equal(t, 2, got.Attempts, "implementer attempts")

	var implRuns, reviewRuns int
	for _, r := range e2eRuns(v, task.ID) {
		if r.Role == "implementer" {
			implRuns++
		}
	}
	assert.Equal(t, 2, implRuns, "implementer runs on the task")

	reviews := e2eChildren(v, task.ID, domain.KindReview)
	for _, rv := range reviews {
		reviewRuns += len(e2eRuns(v, rv.ID))
		assert.Equal(t, domain.TaskDone, rv.Status, "review task %s is done", rv.ID)
	}
	assert.Len(t, reviews, 2, "one review per implementer attempt")
	assert.Equal(t, 2, reviewRuns, "reviewer runs")

	require.NotZero(t, got.PRNumber, "task has a PR")
	pr, err := v.scm.GetPR(v.ctx, got.PRNumber)
	require.NoError(t, err)
	assert.NotEqual(t, "merged", pr.State, "rejected PR must never merge")
}

func TestE2EPauseStopsScheduling(t *testing.T) {
	v := e2eNewEngine(t, "")
	require.NoError(t, v.e.SetPaused(v.ctx, true))

	task := e2eCreate(v, domain.Task{Title: "paused work", Body: "[fake:commit]"})

	time.Sleep(2 * time.Second)
	got := e2eTask(v, task.ID)
	assert.Equal(t, domain.TaskQueued, got.Status, "task stays queued while paused")
	assert.Empty(t, e2eRuns(v, task.ID), "no runs start while paused")

	require.NoError(t, v.e.SetPaused(v.ctx, false))

	e2eWait(v, "task "+task.ID+" to leave queued after resume", 60*time.Second, func() bool {
		return e2eTask(v, task.ID).Status != domain.TaskQueued
	})
}
