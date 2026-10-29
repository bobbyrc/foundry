package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

// rtNoChangesScript is a harness run that changes nothing and reports no_changes, the way an
// agent does when it finds the work already on the branch.
func rtNoChangesScript(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
	dir := filepath.Join(spec.Workdir, domain.OutboxDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return harness.Result{IsError: true}, err
	}
	report := `{"status":"no_changes","summary":"the work is already on the branch"}`
	if err := os.WriteFile(filepath.Join(dir, domain.ResultFile), []byte(report), 0o644); err != nil {
		return harness.Result{IsError: true}, err
	}
	return harness.Result{SessionID: "nochanges-" + spec.RunID, FinalText: "the work is already on the branch"}, nil
}

// A branch with commits from an earlier attempt that never got a pull request. Here the earlier
// attempt is simulated by pushing the commit to the bare remote before the task exists, so the
// next run starts from that branch and makes no commits of its own. The run must still publish
// the branch and open the pull request; finishing it as done would strand the work. The contrast
// case, a run with no earlier commits that reports no_changes, finishes done with no pull request:
// see TestRunnerNoMarkerFinishesDone.
func TestRunnerNoChangesOnBranchAheadOfBaseOpensPR(t *testing.T) {
	env := rtNewEngine(t, rtOpts{script: rtNoChangesScript})

	const branch = "onett/stranded-greeting"
	clone := t.TempDir()
	rtGit(t, "", "clone", env.bare["demo"], clone)
	rtGit(t, clone, "checkout", "-b", branch)
	require.NoError(t, os.WriteFile(filepath.Join(clone, "greeting.txt"), []byte("hello\n"), 0o644))
	rtGit(t, clone, "add", "greeting.txt")
	rtGit(t, clone, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "greeting")
	rtGit(t, clone, "push", "origin", branch)
	pushed := rtRevParseQuiet(env.bare["demo"], "refs/heads/"+branch)
	require.NotEmpty(t, pushed)

	task := env.create(domain.Task{Title: "Add greeting", Body: "add the greeting", Branch: branch})

	// Wait for either outcome, so the buggy "done" result fails at once instead of timing out.
	var got domain.Task
	rtWait(t, 20*time.Second, "the task to leave running", func() bool {
		got = env.task(task.ID)
		return got.Status == domain.TaskReview || got.Status == domain.TaskDone || got.Status == domain.TaskBlocked
	})
	require.Equal(t, domain.TaskReview, got.Status, "the stranded branch must be published, not finished: %+v", got)
	require.NotZero(t, got.PRNumber, "a pull request is opened for the branch")
	assert.Equal(t, branch, got.Branch)

	pr, ok := env.scm.PRs[got.PRNumber]
	require.True(t, ok, "fake SCM has the PR")
	assert.Equal(t, branch, pr.HeadBranch)
	assert.Equal(t, pushed, rtRevParseQuiet(env.bare["demo"], "refs/heads/"+branch), "the branch is unchanged on the remote")

	runs := env.runs(task.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, domain.RunSucceeded, runs[0].Status)
}
