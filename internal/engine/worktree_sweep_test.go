package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wsNewEngine builds an engine whose project demo is a real local repository, so runs can get
// real git worktrees from the workspace manager.
func wsNewEngine(t *testing.T) *lpEnv {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "demo.git")
	rtSeedRepo(t, bare)
	doc := fmt.Sprintf(`server:
  data_dir: %s
projects:
  demo:
    repo: %s
    path: %s
    default_branch: main
`, t.TempDir(), bare, filepath.Join(t.TempDir(), "demo-clone"))
	return lpNewEngine(t, doc)
}

// wsTask stores a task of demo in status.
func wsTask(t *testing.T, env *lpEnv, status domain.TaskStatus) domain.Task {
	t.Helper()
	task, err := env.st.CreateTask(env.ctx, domain.Task{
		Project: "demo",
		Title:   "sweep fixture",
		Status:  status,
	})
	require.NoError(t, err)
	return task
}

// wsRun stores a run of taskID that started at started. It has ended when ended is true.
func wsRun(t *testing.T, env *lpEnv, taskID string, started time.Time, ended bool) domain.Run {
	t.Helper()
	r := domain.Run{
		TaskID:    taskID,
		Project:   "demo",
		Role:      "implementer",
		Status:    domain.RunRunning,
		StartedAt: started,
	}
	if ended {
		end := started.Add(time.Minute)
		r.Status, r.EndedAt = domain.RunFailed, &end
	}
	run, err := env.st.CreateRun(env.ctx, r)
	require.NoError(t, err)
	return run
}

// wsWorktree gives run runID a real worktree under the project directory and returns its path.
func wsWorktree(t *testing.T, env *lpEnv, runID string) string {
	t.Helper()
	p := env.e.Config().Projects["demo"]
	ws, err := env.e.d.Workspaces.Prepare(env.ctx, env.e.repoFor(p), "sweep-"+runID, runID)
	require.NoError(t, err)
	return ws.Dir
}

// wsExists reports whether path is still on disk.
func wsExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
	return err == nil
}

func TestSweepWorktreesRemovesOnlyLeftovers(t *testing.T) {
	env := wsNewEngine(t)
	p := env.e.Config().Projects["demo"]
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)

	done := wsTask(t, env, domain.TaskDone)
	queued := wsTask(t, env, domain.TaskQueued)
	blocked := wsTask(t, env, domain.TaskBlocked)
	running := wsTask(t, env, domain.TaskRunning)

	doneRun := wsRun(t, env, done.ID, t0, true)
	queuedRun := wsRun(t, env, queued.ID, t0, true)
	blockedOld := wsRun(t, env, blocked.ID, t0, true)
	blockedNew := wsRun(t, env, blocked.ID, t0.Add(time.Hour), true)
	runningRun := wsRun(t, env, running.ID, t0, false)

	doneDir := wsWorktree(t, env, doneRun.ID)
	queuedDir := wsWorktree(t, env, queuedRun.ID)
	blockedOldDir := wsWorktree(t, env, blockedOld.ID)
	blockedNewDir := wsWorktree(t, env, blockedNew.ID)
	runningDir := wsWorktree(t, env, runningRun.ID)
	strayDir := filepath.Join(filepath.Dir(doneDir), "not-a-run")
	require.NoError(t, os.MkdirAll(strayDir, 0o755))

	env.e.sweepWorktrees(env.ctx, p)

	assert.False(t, wsExists(t, doneDir), "ended run of a done task is removed")
	assert.False(t, wsExists(t, queuedDir), "ended run of a re-queued task is removed")
	assert.False(t, wsExists(t, blockedOldDir), "older run of a blocked task is removed")
	assert.True(t, wsExists(t, blockedNewDir), "newest run of a blocked task is kept for a person")
	assert.True(t, wsExists(t, runningDir), "run that has not ended is kept")
	assert.True(t, wsExists(t, strayDir), "directory that is not a known run is kept")
}

func TestSweepWorktreesKeepsWorktreeOfLiveRun(t *testing.T) {
	env := wsNewEngine(t)
	p := env.e.Config().Projects["demo"]
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)

	task := wsTask(t, env, domain.TaskDone)
	run := wsRun(t, env, task.ID, t0, true)
	dir := wsWorktree(t, env, run.ID)

	// A run that is still finishing has its row ended but is still registered as live. Its
	// worktree must survive a sweep started by another run's end.
	env.e.registerRun(run.ID, task.ID, "demo", func() {})
	env.e.sweepWorktrees(env.ctx, p)
	assert.True(t, wsExists(t, dir), "worktree of a registered run is kept")

	env.e.unregisterRun(run.ID)
	env.e.sweepWorktrees(env.ctx, p)
	assert.False(t, wsExists(t, dir), "worktree of a finished run is removed once it is unregistered")
}

func TestSweepWorktreesMissingDirectoryIsNotAnError(t *testing.T) {
	env := wsNewEngine(t)
	// Nothing has ever run in demo, so its directory does not exist. The sweep must return
	// quietly instead of creating or failing on it.
	env.e.sweepWorktrees(env.ctx, env.e.Config().Projects["demo"])

	_, err := os.Stat(filepath.Join(env.e.d.Workspaces.Root, "demo"))
	assert.True(t, os.IsNotExist(err), "sweep does not create the project directory")
}

func TestSweepWorktreesRemovesPlainLeftoverDirectory(t *testing.T) {
	env := wsNewEngine(t)
	task := wsTask(t, env, domain.TaskDone)
	run := wsRun(t, env, task.ID, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC), true)

	// A directory git no longer knows as a worktree: no .git entry, only leftover files.
	dir := filepath.Join(env.e.d.Workspaces.Root, "demo", run.ID)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "node_modules", "x.js"), []byte("x"), 0o644))

	env.e.sweepWorktrees(env.ctx, env.e.Config().Projects["demo"])

	assert.False(t, wsExists(t, dir), "plain leftover directory of an ended run is removed")
}

func TestSweepWorktreesKeepsUnknownDirectoryWithGitEntry(t *testing.T) {
	env := wsNewEngine(t)
	task := wsTask(t, env, domain.TaskDone)
	run := wsRun(t, env, task.ID, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC), true)

	// A .git file that points at a worktree git does not know. The directory must survive:
	// git owns anything that still has a .git entry.
	dir := filepath.Join(env.e.d.Workspaces.Root, "demo", run.ID)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git"),
		[]byte("gitdir: "+filepath.Join(t.TempDir(), "missing")+"\n"), 0o644))

	env.e.sweepWorktrees(env.ctx, env.e.Config().Projects["demo"])

	assert.True(t, wsExists(t, dir), "directory with a .git entry is kept")
}
