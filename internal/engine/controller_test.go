package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/api"
	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/events"
	"github.com/bobbyrc/foundry/internal/harness"
	"github.com/bobbyrc/foundry/internal/scm"
	scmfake "github.com/bobbyrc/foundry/internal/scm/fake"
	"github.com/bobbyrc/foundry/internal/store"
	"github.com/bobbyrc/foundry/internal/store/sqlite"
	"github.com/bobbyrc/foundry/internal/workspace"
)

// ctYAML is a config every agent can resolve. The repo is an absolute path that does not
// exist, which validation accepts and which these tests never clone.
const ctYAML = `
server:
  data_dir: %s
harnesses:
  claude: {type: fake}
  codex: {type: fake}
  fake: {type: fake}
projects:
  demo:
    repo: /nonexistent/demo.git
`

// ctRig holds the shared collaborators of one test. Engines built from it share the store, so
// a second engine sees what the first one saved.
type ctRig struct {
	t   *testing.T
	st  store.Store
	bus events.Bus
	fk  *scmfake.Fake
	cfg *config.Config
}

func ctNewRig(t *testing.T) *ctRig {
	t.Helper()
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	cfg, err := config.Parse([]byte(fmt.Sprintf(ctYAML, t.TempDir())), filepath.Join(t.TempDir(), "foundry.yaml"))
	require.NoError(t, err)
	return &ctRig{t: t, st: st, bus: events.NewBus(st, nil), fk: scmfake.New(), cfg: cfg}
}

// engine builds an engine on the rig's collaborators.
func (r *ctRig) engine(reload func(string) (*config.Config, error)) *Engine {
	r.t.Helper()
	e, err := New(Deps{
		Config:     r.cfg,
		Store:      r.st,
		Bus:        r.bus,
		Workspaces: workspace.New(r.t.TempDir()),
		SCMFor: func(*config.Project) (scm.SCM, error) {
			return r.fk, nil
		},
		HarnessFor: func(name string, h config.Harness) (harness.Harness, error) {
			return harness.New(harness.Config{Name: name, Type: h.Type})
		},
		ReloadConfig: reload,
	})
	require.NoError(r.t, err)
	return e
}

// ctTask queues a feature task in demo.
func ctTask(t *testing.T, e *Engine, title string) domain.Task {
	t.Helper()
	task, err := e.CreateTask(context.Background(), domain.Task{
		Project: "demo", Kind: domain.KindFeature, Title: title, Body: "original body",
	})
	require.NoError(t, err)
	return task
}

// ctSetTask writes fields directly, for states the controller reaches only through runs.
func ctSetTask(t *testing.T, r *ctRig, id string, fn func(*domain.Task)) {
	t.Helper()
	_, err := r.st.UpdateTask(context.Background(), id, func(task *domain.Task) error {
		fn(task)
		return nil
	})
	require.NoError(t, err)
}

func TestControllerCreateTaskValidation(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)

	_, err := e.CreateTask(ctx, domain.Task{Project: "demo", Kind: "bogus", Title: "x"})
	assert.True(t, errors.Is(err, api.ErrInvalid), "bad kind: %v", err)

	_, err = e.CreateTask(ctx, domain.Task{Project: "nope", Kind: domain.KindFeature, Title: "x"})
	assert.True(t, errors.Is(err, api.ErrInvalid), "unknown project: %v", err)

	_, err = e.CreateTask(ctx, domain.Task{Project: "demo", Kind: domain.KindFeature, Title: "  "})
	assert.True(t, errors.Is(err, api.ErrInvalid), "missing title: %v", err)
}

func TestControllerCreateTaskRoutesAndZeroesClientFields(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)

	task, err := e.CreateTask(ctx, domain.Task{
		ID: "client-id", Project: "demo", Kind: domain.KindBugfix, Title: "fix it", Body: "b",
		Status: domain.TaskRunning, Attempts: 2, Branch: "mine", PRNumber: 7,
		PRURL: "https://x/7", ApprovedSHA: "abc", MergeSHA: "def",
	})
	require.NoError(t, err)

	assert.NotEqual(t, "client-id", task.ID)
	assert.Equal(t, domain.TaskQueued, task.Status)
	assert.Equal(t, 0, task.Attempts)
	assert.Empty(t, task.Branch)
	assert.Zero(t, task.PRNumber)
	assert.Empty(t, task.PRURL)
	assert.Empty(t, task.ApprovedSHA)
	assert.Empty(t, task.MergeSHA)
	assert.Equal(t, "api", task.Source)
	assert.Equal(t, "implementer", task.Role, "bugfix routes to the implementer by default")
	assert.Equal(t, r.cfg.Limits.MaxAttempts, task.MaxAttempts)

	stored, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, task.Role, stored.Role)
}

func TestControllerCancelTask(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	task := ctTask(t, e, "cancel me")

	pending, err := r.st.CreateApproval(ctx, domain.Approval{
		TaskID: task.ID, Project: "demo", Kind: "merge", Title: "merge", Status: domain.ApprovalPending,
	})
	require.NoError(t, err)

	require.NoError(t, e.CancelTask(ctx, task.ID))
	got, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskCancelled, got.Status)

	a, err := r.st.GetApproval(ctx, pending.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalRejected, a.Status)
	assert.Equal(t, "task cancelled", a.Note)
	assert.NotNil(t, a.DecidedAt)

	err = e.CancelTask(ctx, task.ID)
	assert.True(t, errors.Is(err, api.ErrConflict), "cancelling a terminal task: %v", err)

	err = e.CancelTask(ctx, "no-such-task")
	assert.True(t, errors.Is(err, store.ErrNotFound), "unknown task: %v", err)
}

func TestControllerCancelTaskCancelsLiveRuns(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	task := ctTask(t, e, "running")
	other := ctTask(t, e, "other")

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	otherCtx, cancelOther := context.WithCancel(ctx)
	defer cancelOther()
	e.runsMu.Lock()
	e.active["run-mine"] = &activeRun{runID: "run-mine", taskID: task.ID, cancel: cancelRun}
	e.active["run-other"] = &activeRun{runID: "run-other", taskID: other.ID, cancel: cancelOther}
	e.runsMu.Unlock()

	require.NoError(t, e.CancelTask(ctx, task.ID))
	assert.Error(t, runCtx.Err(), "the task's live run is cancelled")
	assert.NoError(t, otherCtx.Err(), "another task's run is left alone")
	reason, _ := e.active["run-mine"].reason.Load().(string)
	assert.Equal(t, "cancelled by user", reason)
}

func TestControllerRetryTask(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	task := ctTask(t, e, "retry me")

	err := e.RetryTask(ctx, task.ID, "")
	assert.True(t, errors.Is(err, api.ErrConflict), "retry from queued: %v", err)

	ctSetTask(t, r, task.ID, func(x *domain.Task) {
		x.Status, x.Attempts, x.BlockedReason = domain.TaskBlocked, 3, "stalled"
	})
	require.NoError(t, e.RetryTask(ctx, task.ID, "try the other file"))

	got, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskQueued, got.Status)
	assert.Equal(t, 0, got.Attempts)
	assert.Empty(t, got.BlockedReason)
	assert.Equal(t, "original body\n\n## Note from the owner\n\ntry the other file", got.Body)
}

func TestControllerCancelRun(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	task := ctTask(t, e, "a run")

	run, err := r.st.CreateRun(ctx, domain.Run{TaskID: task.ID, Project: "demo", Role: "implementer", Status: domain.RunRunning})
	require.NoError(t, err)
	ctSetTask(t, r, task.ID, func(x *domain.Task) { x.Status = domain.TaskRunning })

	err = e.CancelRun(ctx, run.ID)
	assert.True(t, errors.Is(err, api.ErrConflict), "run not active: %v", err)

	err = e.CancelRun(ctx, "no-such-run")
	assert.True(t, errors.Is(err, store.ErrNotFound), "unknown run: %v", err)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.runsMu.Lock()
	e.active[run.ID] = &activeRun{runID: run.ID, taskID: task.ID, cancel: cancel}
	e.runsMu.Unlock()
	require.NoError(t, e.CancelRun(ctx, run.ID))
	assert.Error(t, runCtx.Err())

	got, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskBlocked, got.Status, "the task waits for the owner under Needs you")
	assert.Equal(t, "run cancelled by owner", got.BlockedReason)
}

func TestControllerDecideApprovalReject(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	task := ctTask(t, e, "merge me")
	ctSetTask(t, r, task.ID, func(x *domain.Task) { x.Status = domain.TaskAwaitingApproval })

	approval, err := r.st.CreateApproval(ctx, domain.Approval{
		TaskID: task.ID, Project: "demo", Kind: "merge", Title: "merge me", Status: domain.ApprovalPending,
	})
	require.NoError(t, err)

	got, err := e.DecideApproval(ctx, approval.ID, false, "too risky")
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalRejected, got.Status)
	assert.Equal(t, "too risky", got.Note)
	assert.NotNil(t, got.DecidedAt)

	stored, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskCancelled, stored.Status)

	_, err = e.DecideApproval(ctx, approval.ID, true, "")
	assert.True(t, errors.Is(err, api.ErrConflict), "decided approval is not pending: %v", err)
}

func TestControllerDecideApprovalRecordsOtherKinds(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	task := ctTask(t, e, "unblock me")

	approval, err := r.st.CreateApproval(ctx, domain.Approval{
		TaskID: task.ID, Project: "demo", Kind: "unblock", Title: "unblock", Status: domain.ApprovalPending,
	})
	require.NoError(t, err)

	got, err := e.DecideApproval(ctx, approval.ID, true, "go")
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalApproved, got.Status)

	stored, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskQueued, stored.Status, "only merge approvals act on the task")
}

func TestControllerDecideApprovalUnknown(t *testing.T) {
	r := ctNewRig(t)
	e := r.engine(nil)
	_, err := e.DecideApproval(context.Background(), "no-such-approval", true, "")
	assert.True(t, errors.Is(err, store.ErrNotFound), "unknown approval: %v", err)
}

func TestControllerSetPausedPersists(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	assert.False(t, e.Paused())

	require.NoError(t, e.SetPaused(ctx, true))
	assert.True(t, e.Paused())

	// A new engine on the same store, as after a restart, restores the switch.
	restarted := r.engine(nil)
	require.NoError(t, restarted.recover(ctx))
	assert.True(t, restarted.Paused())

	require.NoError(t, restarted.SetPaused(ctx, false))
	again := r.engine(nil)
	require.NoError(t, again.recover(ctx))
	assert.False(t, again.Paused())
}

func TestControllerReloadConfig(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)

	var gotPath string
	reload := func(path string) (*config.Config, error) {
		gotPath = path
		cfg, err := config.Parse([]byte(fmt.Sprintf(ctYAML, t.TempDir())+"limits:\n  max_attempts: 9\n"), path)
		return cfg, err
	}
	e := r.engine(reload)
	require.NoError(t, e.ReloadConfig(ctx))
	assert.Equal(t, 9, e.Config().Limits.MaxAttempts)
	assert.Equal(t, r.cfg.Path, gotPath)

	bad := r.engine(func(string) (*config.Config, error) { return nil, errors.New("yaml: line 3") })
	err := bad.ReloadConfig(ctx)
	assert.True(t, errors.Is(err, api.ErrInvalid), "load error: %v", err)
	assert.Equal(t, r.cfg.Limits.MaxAttempts, bad.Config().Limits.MaxAttempts, "current config kept")

	none := r.engine(nil)
	err = none.ReloadConfig(ctx)
	assert.True(t, errors.Is(err, api.ErrInvalid), "no reload func: %v", err)
}

func TestControllerSendChatValidation(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)

	_, err := e.SendChat(ctx, "no-such-conversation", "   ")
	assert.True(t, errors.Is(err, api.ErrInvalid), "empty text: %v", err)

	_, err = e.SendChat(ctx, "no-such-conversation", "hello")
	assert.True(t, errors.Is(err, store.ErrNotFound), "unknown conversation: %v", err)
}

func TestControllerDecideApprovalMergeApprove(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	task := ctTask(t, e, "ship it")

	pr, err := r.fk.OpenPR(ctx, scm.PRSpec{Head: task.Branch, Base: "main", Title: "ship it"})
	require.NoError(t, err)
	ctSetTask(t, r, task.ID, func(x *domain.Task) {
		x.Status, x.PRNumber = domain.TaskAwaitingApproval, pr.Number
	})
	approval, err := r.st.CreateApproval(ctx, domain.Approval{
		TaskID: task.ID, Project: "demo", Kind: "merge", Title: "ship it", Status: domain.ApprovalPending,
	})
	require.NoError(t, err)

	got, err := e.DecideApproval(ctx, approval.ID, true, "")
	require.NoError(t, err)
	assert.Equal(t, domain.ApprovalApproved, got.Status)

	merged, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskMerged, merged.Status)
	assert.NotEmpty(t, merged.MergeSHA)
}

func TestControllerDecideApprovalMergeFailureBlocksTask(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	e := r.engine(nil)
	task := ctTask(t, e, "conflicting")

	pr, err := r.fk.OpenPR(ctx, scm.PRSpec{Head: task.Branch, Base: "main", Title: "conflicting"})
	require.NoError(t, err)
	ctSetTask(t, r, task.ID, func(x *domain.Task) {
		x.Status, x.PRNumber = domain.TaskAwaitingApproval, pr.Number
	})
	r.fk.MergeErr = errors.New("merge conflict")
	approval, err := r.st.CreateApproval(ctx, domain.Approval{
		TaskID: task.ID, Project: "demo", Kind: "merge", Title: "conflicting", Status: domain.ApprovalPending,
	})
	require.NoError(t, err)

	got, err := e.DecideApproval(ctx, approval.ID, true, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "merge failed after approval")
	assert.Equal(t, domain.ApprovalApproved, got.Status, "the decision itself is kept")

	blocked, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskBlocked, blocked.Status)
	assert.Contains(t, blocked.BlockedReason, "merge failed after approval")
}
