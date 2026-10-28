package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/events"
	"github.com/bobbyrc/foundry/internal/harness"
	hfake "github.com/bobbyrc/foundry/internal/harness/fake"
	"github.com/bobbyrc/foundry/internal/scm"
	scmfake "github.com/bobbyrc/foundry/internal/scm/fake"
	"github.com/bobbyrc/foundry/internal/store"
	"github.com/bobbyrc/foundry/internal/store/sqlite"
	"github.com/bobbyrc/foundry/internal/workspace"
)

// rtOpts configures rtNewEngine. Zero values give one project "demo", limits that suit tests,
// and the fake harness's default script.
type rtOpts struct {
	projects      []string
	script        hfake.Script
	maxAttempts   int
	selfProject   string
	maxConcurrent int
	maxPerProject int
	// wrapStore, when set, wraps the engine's store (the test reads the raw store).
	wrapStore func(store.Store) store.Store
	// projectExtra maps a project name to extra YAML lines for its entry (agents, routing),
	// each indented four spaces.
	projectExtra map[string]string
}

// rtEnv is a real engine wired to an in-memory store, a shared fake SCM and local bare repos.
type rtEnv struct {
	t     *testing.T
	e     *Engine
	st    store.Store
	ctx   context.Context
	scm   *scmfake.Fake
	bare  map[string]string // project name -> bare repo path
	clone map[string]string // project name -> clone path
}

// rtNewEngine builds the engine and starts its scheduler. Cleanup cancels every run and waits
// for them.
func rtNewEngine(t *testing.T, o rtOpts) *rtEnv {
	t.Helper()
	if len(o.projects) == 0 {
		o.projects = []string{"demo"}
	}
	if o.maxAttempts == 0 {
		o.maxAttempts = 3
	}
	if o.maxConcurrent == 0 {
		o.maxConcurrent = 3
	}
	if o.maxPerProject == 0 {
		o.maxPerProject = 1
	}
	dataDir := t.TempDir()
	env := &rtEnv{t: t, bare: map[string]string{}, clone: map[string]string{}}

	var projects strings.Builder
	for _, name := range o.projects {
		env.bare[name] = filepath.Join(t.TempDir(), name+".git")
		env.clone[name] = filepath.Join(t.TempDir(), name+"-clone")
		rtSeedRepo(t, env.bare[name])
		fmt.Fprintf(&projects, "  %s:\n    repo: %s\n    path: %s\n    default_branch: main\n%s",
			name, env.bare[name], env.clone[name], o.projectExtra[name])
	}
	var agents strings.Builder
	for _, a := range []string{"orchestrator", "implementer", "reviewer", "triager", "sre", "improver"} {
		fmt.Fprintf(&agents, "  %s: {harness: fake, prompt: builtin:%s}\n", a, a)
	}
	self := ""
	if o.selfProject != "" {
		self = "self_project: " + o.selfProject + "\n"
	}
	yaml := fmt.Sprintf(`server:
  data_dir: %s
%slimits:
  stall_timeout: 2s
  run_timeout: 20s
  max_attempts: %d
  max_concurrent_runs: %d
  max_runs_per_project: %d
harnesses:
  fake: {type: fake}
  alt: {type: fake}
agents:
%sprojects:
%s`, dataDir, self, o.maxAttempts, o.maxConcurrent, o.maxPerProject, agents.String(), projects.String())
	cfg, err := config.Parse([]byte(yaml), "")
	require.NoError(t, err, yaml)

	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	env.st = st

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	env.scm = scmfake.New()
	demoClone := env.clone[o.projects[0]]
	env.scm.HeadSHA = func(branch string) string { return rtRevParseQuiet(demoClone, "origin/"+branch) }

	var engStore store.Store = st
	if o.wrapStore != nil {
		engStore = o.wrapStore(st)
	}
	e, err := New(Deps{
		Config:     cfg,
		Store:      engStore,
		Bus:        events.NewBus(st, logger),
		Workspaces: workspace.New(filepath.Join(dataDir, "worktrees")),
		Logger:     logger,
		SCMFor:     func(*config.Project) (scm.SCM, error) { return env.scm, nil },
		HarnessFor: func(name string, h config.Harness) (harness.Harness, error) {
			if o.script != nil {
				return hfake.New(o.script), nil
			}
			return harness.New(harness.Config{Name: name, Type: h.Type, Command: h.Command, Args: h.Args, Env: h.Env})
		},
		AgentEnv:      map[string]string{"FOUNDRY_API": "http://127.0.0.1:1", "FOUNDRY_TOKEN": "test"},
		SchedulerTick: 50 * time.Millisecond,
	})
	require.NoError(t, err)
	env.e = e

	ctx, cancel := context.WithCancel(context.Background())
	env.ctx = ctx
	schedDone := make(chan struct{})
	go func() {
		defer close(schedDone)
		e.runScheduler(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		// Wait for the scheduler to stop first: until then it can still start a run, and a run
		// started after the wait below would write into the test's directories during cleanup.
		<-schedDone
		e.cancelAllRuns("test finished")
		e.wg.Wait()
	})
	return env
}

// create queues a task through the engine's normal entry point. Empty fields take test defaults.
func (env *rtEnv) create(t domain.Task) domain.Task {
	env.t.Helper()
	if t.Project == "" {
		t.Project = "demo"
	}
	if t.Title == "" {
		t.Title = "Add greeting"
	}
	if t.Body == "" {
		t.Body = "[fake:commit]"
	}
	created, err := env.e.createTask(env.ctx, t)
	require.NoError(env.t, err)
	return created
}

// task reads a task from the store.
func (env *rtEnv) task(id string) domain.Task {
	env.t.Helper()
	got, err := env.st.GetTask(env.ctx, id)
	require.NoError(env.t, err)
	return got
}

// waitStatus waits until task id reaches status and returns it.
func (env *rtEnv) waitStatus(id string, want domain.TaskStatus) domain.Task {
	env.t.Helper()
	var got domain.Task
	rtWait(env.t, 20*time.Second, fmt.Sprintf("task %s to reach %s", id, want), func() bool {
		var err error
		got, err = env.st.GetTask(env.ctx, id)
		return err == nil && got.Status == want
	})
	return got
}

// runs returns the runs of a task, newest first.
func (env *rtEnv) runs(taskID string) []domain.Run {
	env.t.Helper()
	runs, err := env.st.ListRuns(env.ctx, store.RunFilter{TaskID: taskID})
	require.NoError(env.t, err)
	return runs
}

// events returns recent events.
func (env *rtEnv) events() []domain.Event {
	env.t.Helper()
	evs, err := env.st.ListEvents(env.ctx, store.EventFilter{Limit: 1000})
	require.NoError(env.t, err)
	return evs
}

func rtWait(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// rtGit runs git in dir (or the current directory when empty) and returns trimmed stdout.
func rtGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := rtGitRun(dir, args...)
	require.NoError(t, err, "git %v: %s", args, out)
	return out
}

func rtGitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// rtRevParseQuiet resolves ref in dir, or "" when it does not exist. Safe from any goroutine.
func rtRevParseQuiet(dir, ref string) string {
	out, err := rtGitRun(dir, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return out
}

// rtSeedRepo creates a bare repository whose main branch has one commit.
func rtSeedRepo(t *testing.T, bare string) {
	t.Helper()
	rtGit(t, "", "init", "--bare", "-b", "main", bare)
	seed := t.TempDir()
	rtGit(t, "", "clone", bare, seed)
	require.NoError(t, os.WriteFile(filepath.Join(seed, "README.md"), []byte("demo\n"), 0o644))
	rtGit(t, seed, "add", "README.md")
	rtGit(t, seed, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init")
	rtGit(t, seed, "push", "origin", "HEAD:main")
}

func TestRunnerCommitPublishesPR(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Title: "Add greeting", Body: "[fake:commit]"})

	got := env.waitStatus(task.ID, domain.TaskReview)
	require.NotZero(t, got.PRNumber)
	assert.Equal(t, "foundry/"+task.ID[len(task.ID)-8:]+"-add-greeting", got.Branch)

	pr, ok := env.scm.PRs[got.PRNumber]
	require.True(t, ok, "fake SCM has the PR")
	assert.Equal(t, got.Branch, pr.HeadBranch)

	rtGit(t, "", "--git-dir", env.bare["demo"], "rev-parse", "--verify", "refs/heads/"+got.Branch)

	runs := env.runs(task.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, domain.RunSucceeded, runs[0].Status)
	logs, err := env.st.ListRunLogs(env.ctx, runs[0].ID, 0, 100)
	require.NoError(t, err)
	assert.NotEmpty(t, logs, "run log is stored")
}

func TestRunnerBlockedNotifies(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Body: "[fake:block]"})

	got := env.waitStatus(task.ID, domain.TaskBlocked)
	assert.Equal(t, "agent needs a human: fake needs a human", got.BlockedReason)

	// The status flips before the notification is emitted, so wait for the event.
	rtWait(t, 5*time.Second, "a blocked notification event", func() bool {
		for _, ev := range env.events() {
			if ev.Type == domain.EvNotification && strings.Contains(ev.Message, "Blocked:") {
				return true
			}
		}
		return false
	})
}

func TestRunnerDeniedBlocksWithoutRetry(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Body: "[fake:deny]"})

	got := env.waitStatus(task.ID, domain.TaskBlocked)
	assert.Contains(t, got.BlockedReason, "never retries")
	assert.Contains(t, got.BlockedReason, "rm -rf /")

	runs := env.runs(task.ID)
	require.Len(t, runs, 1, "a denial is never retried")
	assert.Equal(t, domain.RunDenied, runs[0].Status)
}

func TestRunnerFailureRetriesOnceThenBlocks(t *testing.T) {
	env := rtNewEngine(t, rtOpts{maxAttempts: 2})
	task := env.create(domain.Task{Body: "[fake:fail]"})

	got := env.waitStatus(task.ID, domain.TaskBlocked)
	assert.Contains(t, got.BlockedReason, "gave up after 2 attempts")
	assert.Contains(t, got.Body, "## Attempt 1")
	assert.Len(t, env.runs(task.ID), 2)
}

func TestRunnerNoMarkerFinishesDone(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Body: "just answer the question"})

	got := env.waitStatus(task.ID, domain.TaskDone)
	assert.Zero(t, got.PRNumber)
	assert.Empty(t, env.scm.PRs)
	assert.Equal(t, "fake: nothing to do", got.Summary)
}

func TestRunnerReviewApproveRecordsReviewedSHA(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	parent := env.waitStatus(env.create(domain.Task{Body: "[fake:commit]"}).ID, domain.TaskReview)

	review := env.create(domain.Task{
		Kind: domain.KindReview, Role: "reviewer", ParentID: parent.ID, Title: "Review " + parent.Title,
		Branch: parent.Branch, PRNumber: parent.PRNumber, Body: "",
	})
	env.waitStatus(review.ID, domain.TaskDone)

	head := rtRevParseQuiet(env.clone["demo"], "origin/"+parent.Branch)
	require.NotEmpty(t, head)
	rtWait(t, 10*time.Second, "parent approved", func() bool {
		return env.task(parent.ID).ApprovedSHA == head
	})
}

func TestRunnerReviewRejectRequeuesParent(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	parent := env.waitStatus(env.create(domain.Task{Body: "[fake:commit]\n[fake:reject]"}).ID, domain.TaskReview)

	review := env.create(domain.Task{
		Kind: domain.KindReview, Role: "reviewer", ParentID: parent.ID, Title: "Review " + parent.Title,
		Branch: parent.Branch, PRNumber: parent.PRNumber,
	})
	env.waitStatus(review.ID, domain.TaskDone)

	rtWait(t, 10*time.Second, "parent re-queued with findings", func() bool {
		return strings.Contains(env.task(parent.ID).Body, "reviewer requested changes")
	})
}

func TestRunnerFrictionGoesToSelfProject(t *testing.T) {
	script := func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		dir := filepath.Join(spec.Workdir, domain.OutboxDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return harness.Result{IsError: true}, err
		}
		body := `{"status":"no_changes","summary":"nothing","friction":[` +
			`{"title":"Docs are wrong","body":"b","target":"foundry"},` +
			`{"title":"Flaky test","body":"c","target":"project"}]}`
		if err := os.WriteFile(filepath.Join(dir, domain.ResultFile), []byte(body), 0o644); err != nil {
			return harness.Result{IsError: true}, err
		}
		return harness.Result{FinalText: "done"}, nil
	}
	env := rtNewEngine(t, rtOpts{projects: []string{"demo", "factory"}, selfProject: "factory", script: script})
	task := env.create(domain.Task{Body: "anything"})
	env.waitStatus(task.ID, domain.TaskDone)

	factory, err := env.st.ListFriction(env.ctx, "factory", 10)
	require.NoError(t, err)
	require.Len(t, factory, 1)
	assert.Equal(t, "Docs are wrong", factory[0].Title)

	demo, err := env.st.ListFriction(env.ctx, "demo", 10)
	require.NoError(t, err)
	require.Len(t, demo, 1)
	assert.Equal(t, "Flaky test", demo[0].Title)
}

// A run cancelled while its agent is live ends cancelled, and the task is left as it was.
func TestRunnerCancelLeavesTaskAsCancellerSetIt(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Body: "[fake:stall]"})

	var runID string
	rtWait(t, 10*time.Second, "agent running", func() bool {
		runs := env.runs(task.ID)
		if len(runs) == 0 || runs[0].Status != domain.RunRunning {
			return false
		}
		runID = runs[0].ID
		return true
	})
	require.True(t, env.e.cancelRun(runID, "cancelled by user"))

	rtWait(t, 10*time.Second, "run cancelled", func() bool {
		r, err := env.st.GetRun(env.ctx, runID)
		return err == nil && r.Status == domain.RunCancelled
	})
	got := env.task(task.ID)
	assert.Equal(t, domain.TaskRunning, got.Status, "a cancelled run does not touch the task")
	assert.Empty(t, got.BlockedReason)
}

// A cancel that lands during workspace setup is still a cancellation, not a setup failure: the
// task must not be re-queued for another attempt.
func TestRunnerCancelDuringSetupIsNotRetried(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Body: "[fake:stall]"})

	var runID string
	rtWait(t, 10*time.Second, "run created", func() bool {
		runs := env.runs(task.ID)
		if len(runs) == 0 {
			return false
		}
		runID = runs[0].ID
		return env.e.cancelRun(runID, "cancelled by user")
	})

	rtWait(t, 10*time.Second, "run cancelled", func() bool {
		r, err := env.st.GetRun(env.ctx, runID)
		return err == nil && r.Status == domain.RunCancelled
	})
	got := env.task(task.ID)
	assert.Equal(t, domain.TaskRunning, got.Status)
	assert.NotContains(t, got.Body, "## Attempt", "no re-queue")
	assert.Len(t, env.runs(task.ID), 1)
}

func TestRunnerReadResultFallbacks(t *testing.T) {
	dir := t.TempDir()
	outbox := filepath.Join(dir, domain.OutboxDir)
	require.NoError(t, os.MkdirAll(outbox, 0o755))

	missing := readResult(dir, "  final words  ")
	assert.Equal(t, domain.ResultFailed, missing.Status)
	assert.Equal(t, "agent did not write a valid result.json: final words", missing.Summary)

	require.NoError(t, os.WriteFile(filepath.Join(outbox, domain.ResultFile), []byte("{not json"), 0o644))
	assert.Equal(t, domain.ResultFailed, readResult(dir, "").Status)

	require.NoError(t, os.WriteFile(filepath.Join(outbox, domain.ResultFile),
		[]byte(`{"status":"completed","summary":"ok","risk":"extreme"}`), 0o644))
	got := readResult(dir, "")
	assert.Equal(t, domain.ResultCompleted, got.Status)
	assert.Equal(t, domain.RiskUnknown, got.Risk, "unknown risk becomes unrated")
}

func TestRunnerFrictionFingerprintNormalises(t *testing.T) {
	assert.Equal(t, frictionFingerprint("Docs  are WRONG "), frictionFingerprint("docs are wrong"))
	assert.Len(t, frictionFingerprint("x"), 16)
}

// rtFailFirstRunUpdate fails its first UpdateRun call, to show that a failed run-row update
// does not disturb the run being recorded.
type rtFailFirstRunUpdate struct {
	store.Store
	failed atomic.Bool
}

func (s *rtFailFirstRunUpdate) UpdateRun(ctx context.Context, id string, fn func(*domain.Run) error) (domain.Run, error) {
	if s.failed.CompareAndSwap(false, true) {
		return domain.Run{}, errors.New("injected run update failure")
	}
	return s.Store.UpdateRun(ctx, id, fn)
}

func TestRunnerFailedRunUpdateKeepsRun(t *testing.T) {
	env := rtNewEngine(t, rtOpts{wrapStore: func(s store.Store) store.Store {
		return &rtFailFirstRunUpdate{Store: s}
	}})
	task := env.create(domain.Task{Title: "Add greeting", Body: "[fake:commit]"})
	env.waitStatus(task.ID, domain.TaskReview)

	runs := env.runs(task.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, domain.RunSucceeded, runs[0].Status, "the run is still recorded after a failed update")
}

// rtConflictScript runs the fake's normal behaviour, except when the prompt carries merge
// conflicts: then it records the prompt and reports no changes, leaving the markers in place.
func rtConflictScript(prompts chan<- string) hfake.Script {
	return func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		if !strings.Contains(spec.Prompt, "## Merge conflicts to resolve") {
			return hfake.DefaultScript(ctx, spec, emit)
		}
		select {
		case prompts <- spec.Prompt:
		default:
		}
		dir := filepath.Join(spec.Workdir, domain.OutboxDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return harness.Result{IsError: true}, err
		}
		report := `{"status":"no_changes","summary":"left the conflicts in place"}`
		if err := os.WriteFile(filepath.Join(dir, domain.ResultFile), []byte(report), 0o644); err != nil {
			return harness.Result{IsError: true}, err
		}
		return harness.Result{SessionID: "conflict-" + spec.RunID, FinalText: "left the conflicts in place"}, nil
	}
}

func TestRunnerMergesBaseIntoPRBranchAndReportsConflicts(t *testing.T) {
	prompts := make(chan string, 10)
	env := rtNewEngine(t, rtOpts{script: rtConflictScript(prompts)})
	task := env.create(domain.Task{Title: "Add greeting", Body: "[fake:commit]"})
	got := env.waitStatus(task.ID, domain.TaskReview)
	require.NotZero(t, got.PRNumber)
	run := env.runs(task.ID)[0]

	// Main gets a file of the same name as the one the branch's run added, with other content.
	clone := t.TempDir()
	rtGit(t, "", "clone", env.bare["demo"], clone)
	file := "fake-" + run.ID + ".txt"
	require.NoError(t, os.WriteFile(filepath.Join(clone, file), []byte("main side\n"), 0o644))
	rtGit(t, clone, "add", file)
	rtGit(t, clone, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "clash")
	rtGit(t, clone, "push", "origin", "HEAD:main")

	// Re-queue the task: the next run merges main in and must be told about the conflict.
	_, err := env.st.UpdateTask(env.ctx, task.ID, func(x *domain.Task) error {
		x.Status = domain.TaskQueued
		return nil
	})
	require.NoError(t, err)

	select {
	case prompt := <-prompts:
		assert.Contains(t, prompt, "## Merge conflicts to resolve")
		assert.Contains(t, prompt, "- "+file)
	case <-time.After(20 * time.Second):
		t.Fatal("no run received the merge conflict prompt")
	}

	blocked := env.waitStatus(task.ID, domain.TaskBlocked)
	assert.Contains(t, blocked.BlockedReason, "unresolved merge conflicts")
}

func TestRunnerPublishStopsWhenTaskMovedOn(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	bare := filepath.Join(t.TempDir(), "demo.git")
	rtSeedRepo(t, bare)
	r.cfg.Projects["demo"].Repo = bare
	r.cfg.Projects["demo"].Path = filepath.Join(t.TempDir(), "clone")
	e := r.engine(nil)
	p := e.Config().Projects["demo"]

	task := ctTask(t, e, "cancelled mid-run")
	ctSetTask(t, r, task.ID, func(x *domain.Task) { x.Status = domain.TaskCancelled })

	ws, err := e.d.Workspaces.Prepare(ctx, e.repoFor(p), "foundry/late-publish", "run-late")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(ws.Dir, "late.txt"), []byte("late\n"), 0o644))
	rtGit(t, ws.Dir, "add", "late.txt")
	rtGit(t, ws.Dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "late")

	assert.False(t, e.publish(ctx, task, p, ws, domain.RunResult{}), "publish refuses to run for a cancelled task")
	assert.Empty(t, rtRevParseQuiet(bare, "refs/heads/"+ws.Branch), "nothing is pushed")
	assert.Empty(t, r.fk.PRs, "no pull request is opened")

	got, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskCancelled, got.Status)
}

func TestRunnerLateReviewDoesNotApproveCancelledTask(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)

	parent := ctTask(t, e, "feature")
	ctSetTask(t, r, parent.ID, func(x *domain.Task) { x.Status = domain.TaskCancelled })
	review, err := e.createTask(ctx, domain.Task{
		Project: "demo", Kind: domain.KindReview, ParentID: parent.ID, Title: "review of feature",
	})
	require.NoError(t, err)
	ctSetTask(t, r, review.ID, func(x *domain.Task) { x.Status = domain.TaskRunning })

	rt, err := r.st.GetTask(ctx, review.ID)
	require.NoError(t, err)
	pt, err := r.st.GetTask(ctx, parent.ID)
	require.NoError(t, err)
	e.applyReview(ctx, rt, &pt, workspace.Workspace{StartSHA: "reviewed-sha"}, domain.RunResult{
		Status: domain.ResultCompleted, Review: &domain.ReviewVerdict{Approve: true},
	})

	gotParent, err := r.st.GetTask(ctx, parent.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskCancelled, gotParent.Status)
	assert.Empty(t, gotParent.ApprovedSHA, "a cancelled task is not approved by a late review")

	gotReview, err := r.st.GetTask(ctx, review.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskDone, gotReview.Status)
}

func TestRunnerStartTaskDuringShutdownRecordsCancelledRun(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	task := ctTask(t, e, "claimed at shutdown")
	ctSetTask(t, r, task.ID, func(x *domain.Task) { x.Status = domain.TaskRunning })

	e.lifeMu.Lock()
	e.stopped = true
	e.lifeMu.Unlock()
	e.startTask(ctx, task)

	got, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskRunning, got.Status, "recover re-queues the task on restart")

	runs, err := r.st.ListRuns(ctx, store.RunFilter{TaskID: task.ID})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, domain.RunCancelled, runs[0].Status)
	assert.Equal(t, errStopped.Error(), runs[0].Error)
}

func TestRunnerStartTaskFailureLandsAfterCancel(t *testing.T) {
	r := ctNewRig(t)
	e := r.engine(nil)
	task := ctTask(t, e, "unresolvable agent")
	ctSetTask(t, r, task.ID, func(x *domain.Task) {
		x.Status = domain.TaskRunning
		x.Role = "no-such-agent"
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.startTask(ctx, domain.Task{ID: task.ID, Project: "demo", Role: "no-such-agent", Title: task.Title, Kind: task.Kind})

	got, err := r.st.GetTask(context.Background(), task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskBlocked, got.Status, "the block lands even though the request was cancelled")
	assert.Contains(t, got.BlockedReason, "could not resolve agent")
}

func TestRunnerProjectAgentOverrideRunsOnItsHarness(t *testing.T) {
	env := rtNewEngine(t, rtOpts{projectExtra: map[string]string{
		"demo": "    agents: {implementer: {harness: alt}}\n",
	}})
	task := env.create(domain.Task{Title: "Add greeting", Body: "[fake:commit]"})

	env.waitStatus(task.ID, domain.TaskReview)
	runs := env.runs(task.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, "implementer", runs[0].Role)
	assert.Equal(t, "alt", runs[0].Harness, "the project's implementer override picks the harness")
}

func TestCreateTaskRoutesByProjectRouting(t *testing.T) {
	env := rtNewEngine(t, rtOpts{projectExtra: map[string]string{
		"demo": "    routing: {chore: reviewer}\n",
	}})

	routed := env.create(domain.Task{Kind: domain.KindChore, Title: "Tidy"})
	assert.Equal(t, "reviewer", routed.Role, "the project's routing wins over the global one")
	assert.Equal(t, "reviewer", env.task(routed.ID).Role)

	global := env.create(domain.Task{Kind: domain.KindBugfix, Title: "Fix it"})
	assert.Equal(t, "implementer", global.Role, "kinds the project does not route use the global routing")
}

// Publishing records when foundry pushed the branch, for the CI grace period.
func TestRunnerPublishRecordsPushTime(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Title: "Add greeting", Body: "[fake:commit]"})

	got := env.waitStatus(task.ID, domain.TaskReview)
	require.NotNil(t, got.PushedAt)
	assert.WithinDuration(t, time.Now(), *got.PushedAt, time.Minute)
}

// rtBodyScript runs the fake's normal behaviour, then replaces the pr_body in its report with
// the current value of *body.
func rtBodyScript(body *atomic.Pointer[string]) hfake.Script {
	return func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		res, err := hfake.DefaultScript(ctx, spec, emit)
		if err != nil {
			return res, err
		}
		path := filepath.Join(spec.Workdir, domain.OutboxDir, domain.ResultFile)
		data, err := os.ReadFile(path)
		if err != nil {
			return res, err
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return res, err
		}
		m["pr_body"] = *body.Load()
		data, err = json.Marshal(m)
		if err != nil {
			return res, err
		}
		return res, os.WriteFile(path, data, 0o644)
	}
}

// rerun queues a task that is in review again and returns once its second run has published.
func (env *rtEnv) rerun(id string) domain.Task {
	env.t.Helper()
	_, err := env.st.UpdateTask(env.ctx, id, func(x *domain.Task) error {
		x.Status = domain.TaskQueued
		return nil
	})
	require.NoError(env.t, err)
	require.Eventually(env.t, func() bool {
		runs := env.runs(id)
		return len(runs) == 2 && runs[1].Status == domain.RunSucceeded &&
			env.task(id).Status == domain.TaskReview
	}, 20*time.Second, 20*time.Millisecond)
	return env.task(id)
}

func TestRunnerRerunRefreshesPRDescription(t *testing.T) {
	var body atomic.Pointer[string]
	first, second := "first body", "second body"
	body.Store(&first)
	env := rtNewEngine(t, rtOpts{script: rtBodyScript(&body)})
	task := env.create(domain.Task{Title: "Add greeting", Body: "[fake:commit]"})
	got := env.waitStatus(task.ID, domain.TaskReview)
	assert.Zero(t, env.scm.BodyUpdates[got.PRNumber], "a first run opens the PR with its body, no edit")

	body.Store(&second)
	again := env.rerun(task.ID)
	assert.Equal(t, got.PRNumber, again.PRNumber)
	assert.Equal(t, 1, env.scm.BodyUpdates[got.PRNumber])
	assert.Contains(t, env.scm.PRBodies[got.PRNumber], "second body")
}

func TestRunnerRerunWithEmptyPRBodyKeepsDescription(t *testing.T) {
	var body atomic.Pointer[string]
	first, empty := "first body", ""
	body.Store(&first)
	env := rtNewEngine(t, rtOpts{script: rtBodyScript(&body)})
	task := env.create(domain.Task{Title: "Add greeting", Body: "[fake:commit]"})
	got := env.waitStatus(task.ID, domain.TaskReview)

	body.Store(&empty)
	env.rerun(task.ID)
	assert.Zero(t, env.scm.BodyUpdates[got.PRNumber])
}
