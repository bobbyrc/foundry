package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/policy"
)

// runGit runs git in dir for test setup and assertions, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// setup builds a bare origin with one commit on main (README.md, pkg/a.txt, pkg/b.txt) and
// returns a Repo whose clone does not exist yet, plus a Manager rooted in the test's temp dir.
// Global git config is pointed at an empty file so no identity leaks in from the machine.
func setup(t *testing.T) (Repo, *Manager, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "no-such-gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	runGit(t, root, "init", "--bare", "-b", "main", origin)

	seed := filepath.Join(root, "seed")
	runGit(t, root, "init", "-b", "main", seed)
	writeFile(t, filepath.Join(seed, "README.md"), "hello\n")
	writeFile(t, filepath.Join(seed, "pkg", "a.txt"), numberedLines(10))
	writeFile(t, filepath.Join(seed, "pkg", "b.txt"), numberedLines(10))
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "init")
	runGit(t, seed, "push", origin, "main")

	repo := Repo{
		Name:          "demo",
		Remote:        origin,
		Path:          filepath.Join(root, "clone"),
		DefaultBranch: "main",
	}
	return repo, New(filepath.Join(root, "worktrees")), seed
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func numberedLines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i))
		b.WriteString("\n")
	}
	return b.String()
}

func TestRemoteURL(t *testing.T) {
	cases := map[string]string{
		"owner/name":                 "https://github.com/owner/name.git",
		"/srv/git/thing.git":         "/srv/git/thing.git",
		"file:///srv/git/thing.git":  "file:///srv/git/thing.git",
		"git@github.com:owner/n.git": "git@github.com:owner/n.git",
	}
	for in, want := range cases {
		assert.Equal(t, want, remoteURL(Repo{Remote: in}), in)
	}
}

func TestConcurrentPrepareOnOneClone(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.Prepare(ctx, repo, fmt.Sprintf("foundry/c%d", i), fmt.Sprintf("run%d", i))
			assert.NoError(t, err, "prepare %d", i)
		}()
	}
	wg.Wait()

	for i := range n {
		assert.DirExists(t, filepath.Join(m.Root, repo.Name, fmt.Sprintf("run%d", i)))
	}
}

func TestEnsureCloneFreshAndExisting(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()

	require.NoError(t, m.EnsureClone(ctx, repo))
	assert.DirExists(t, filepath.Join(repo.Path, ".git"))
	seedHead := runGit(t, seed, "rev-parse", "HEAD")
	assert.Equal(t, seedHead, runGit(t, repo.Path, "rev-parse", "origin/main"))

	// A new commit on origin is picked up by the second call, which fetches.
	writeFile(t, filepath.Join(seed, "NEW.md"), "new\n")
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "second")
	runGit(t, seed, "push", repo.Remote, "main")
	newHead := runGit(t, seed, "rev-parse", "HEAD")

	require.NoError(t, m.EnsureClone(ctx, repo))
	assert.Equal(t, newHead, runGit(t, repo.Path, "rev-parse", "origin/main"))
}

func TestPrepareNewBranchAndOutboxExclude(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()

	ws1, err := m.Prepare(ctx, repo, "foundry/a", "run1")
	require.NoError(t, err)
	ws2, err := m.Prepare(ctx, repo, "foundry/b", "run2")
	require.NoError(t, err)

	mainSHA := runGit(t, repo.Path, "rev-parse", "origin/main")
	assert.Equal(t, filepath.Join(m.Root, "demo", "run1"), ws1.Dir)
	assert.Equal(t, "foundry/a", ws1.Branch)
	assert.Equal(t, "origin/main", ws1.BaseRef)
	assert.Equal(t, mainSHA, ws1.StartSHA)
	assert.Equal(t, mainSHA, ws2.StartSHA)
	assert.Equal(t, "foundry/a", runGit(t, ws1.Dir, "rev-parse", "--abbrev-ref", "HEAD"))
	assert.DirExists(t, filepath.Join(ws1.Dir, ".foundry-run"))

	// The exclude line is written once across both Prepare calls.
	exclude, err := os.ReadFile(filepath.Join(repo.Path, ".git", "info", "exclude"))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(exclude), outboxExclude))

	// The outbox is ignored: a file written there does not show in status.
	writeFile(t, filepath.Join(ws1.Dir, ".foundry-run", "result.json"), "{}")
	assert.Empty(t, runGit(t, ws1.Dir, "status", "--porcelain"))
}

func TestCommitWithAndWithoutChanges(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "foundry/c", "run1")
	require.NoError(t, err)

	committed, err := m.Commit(ctx, ws, "nothing here")
	require.NoError(t, err)
	assert.False(t, committed)
	assert.Equal(t, ws.StartSHA, runGit(t, ws.Dir, "rev-parse", "HEAD"))

	// No user.email is configured (global config is empty), so Foundry's identity is used.
	// The outbox file must stay out of the commit.
	writeFile(t, filepath.Join(ws.Dir, "README.md"), "hello\nworld\n")
	writeFile(t, filepath.Join(ws.Dir, ".foundry-run", "result.json"), "{}")
	committed, err = m.Commit(ctx, ws, "add world")
	require.NoError(t, err)
	assert.True(t, committed)
	assert.Equal(t, "add world", runGit(t, ws.Dir, "log", "-1", "--format=%s"))
	assert.Equal(t, "Foundry <foundry@localhost>", runGit(t, ws.Dir, "log", "-1", "--format=%an <%ae>"))
	assert.Equal(t, "README.md", runGit(t, ws.Dir, "show", "--name-only", "--format=", "HEAD"))

	// With a configured identity, git's configuration wins.
	runGit(t, repo.Path, "config", "user.name", "Dev")
	runGit(t, repo.Path, "config", "user.email", "dev@example.com")
	writeFile(t, filepath.Join(ws.Dir, "README.md"), "hello\nworld\nagain\n")
	committed, err = m.Commit(ctx, ws, "again")
	require.NoError(t, err)
	assert.True(t, committed)
	assert.Equal(t, "Dev <dev@example.com>", runGit(t, ws.Dir, "log", "-1", "--format=%an <%ae>"))
}

func TestPushThenPrepareSameBranchPicksUpCommit(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()

	ws1, err := m.Prepare(ctx, repo, "foundry/p", "run1")
	require.NoError(t, err)
	writeFile(t, filepath.Join(ws1.Dir, "feature.txt"), "feature\n")
	committed, err := m.Commit(ctx, ws1, "add feature")
	require.NoError(t, err)
	require.True(t, committed)
	pushed := runGit(t, ws1.Dir, "rev-parse", "HEAD")
	require.NoError(t, m.Push(ctx, repo, ws1))
	assert.Equal(t, pushed, runGit(t, repo.Path, "rev-parse", "origin/foundry/p"))
	require.NoError(t, m.Remove(ctx, repo, ws1))

	ws2, err := m.Prepare(ctx, repo, "foundry/p", "run2")
	require.NoError(t, err)
	assert.Equal(t, pushed, ws2.StartSHA)
	assert.Equal(t, "foundry/p", runGit(t, ws2.Dir, "rev-parse", "--abbrev-ref", "HEAD"))
	assert.FileExists(t, filepath.Join(ws2.Dir, "feature.txt"))
}

func TestDiffNumstatRenameAndBinary(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "foundry/d", "run1")
	require.NoError(t, err)

	// README gains one line; pkg/a.txt is renamed within its directory (brace form); pkg/b.txt
	// is moved to the root (arrow form); a binary file is added.
	writeFile(t, filepath.Join(ws.Dir, "README.md"), "hello\nworld\n")
	runGit(t, ws.Dir, "mv", "pkg/a.txt", "pkg/a2.txt")
	runGit(t, ws.Dir, "mv", "pkg/b.txt", "moved.txt")
	require.NoError(t, os.WriteFile(filepath.Join(ws.Dir, "bin.dat"), []byte{0, 1, 2, 3, 0}, 0o644))
	committed, err := m.Commit(ctx, ws, "reshape")
	require.NoError(t, err)
	require.True(t, committed)
	require.NoError(t, m.Push(ctx, repo, ws))

	changes, err := m.Diff(ctx, repo, "main", "foundry/d")
	require.NoError(t, err)
	got := map[string]policy.FileChange{}
	for _, c := range changes {
		got[c.Path] = c
	}
	assert.Equal(t, map[string]policy.FileChange{
		"README.md":  {Path: "README.md", Added: 1, Deleted: 0},
		"pkg/a2.txt": {Path: "pkg/a2.txt", Added: 0, Deleted: 0},
		"moved.txt":  {Path: "moved.txt", Added: 0, Deleted: 0},
		"bin.dat":    {Path: "bin.dat", Added: 0, Deleted: 0},
	}, got)
	assert.Len(t, changes, 4)
}

// A commit SHA as head judges that commit, even after the branch has moved on.
func TestDiffPinsToCommitSHA(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "foundry/pin", "run1")
	require.NoError(t, err)

	writeFile(t, filepath.Join(ws.Dir, "first.txt"), "one\n")
	committed, err := m.Commit(ctx, ws, "first")
	require.NoError(t, err)
	require.True(t, committed)
	require.NoError(t, m.Push(ctx, repo, ws))
	judged := strings.TrimSpace(runGit(t, ws.Dir, "rev-parse", "HEAD"))

	// A push after the judgement adds a second file to the branch.
	writeFile(t, filepath.Join(ws.Dir, "second.txt"), "two\n")
	committed, err = m.Commit(ctx, ws, "second")
	require.NoError(t, err)
	require.True(t, committed)
	// Plain git: Push's lease is the SHA Prepare saw, so a second Push from the same workspace is refused.
	runGit(t, ws.Dir, "push", "origin", "foundry/pin")

	byCommit, err := m.Diff(ctx, repo, "main", judged)
	require.NoError(t, err)
	assert.Equal(t, []policy.FileChange{{Path: "first.txt", Added: 1}}, byCommit)

	byBranch, err := m.Diff(ctx, repo, "main", "foundry/pin")
	require.NoError(t, err)
	assert.Len(t, byBranch, 2, "the branch name follows the newest push")
}

func TestDiffNoChangesReturnsEmptySlice(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "foundry/e", "run1")
	require.NoError(t, err)
	require.NoError(t, m.Push(ctx, repo, ws))

	changes, err := m.Diff(ctx, repo, "main", "foundry/e")
	require.NoError(t, err)
	assert.NotNil(t, changes)
	assert.Empty(t, changes)
}

func TestNewPath(t *testing.T) {
	assert.Equal(t, "pkg/a2.txt", newPath("pkg/{a.txt => a2.txt}"))
	assert.Equal(t, "moved.txt", newPath("pkg/b.txt => moved.txt"))
	assert.Equal(t, "lib/new/f.go", newPath("lib/{old => new}/f.go"))
	assert.Equal(t, "sub/f.go", newPath("{ => sub}/f.go"))
	assert.Equal(t, "plain/path.go", newPath("plain/path.go"))
}

func TestRemove(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "foundry/r", "run1")
	require.NoError(t, err)

	require.NoError(t, m.Remove(ctx, repo, ws))
	assert.NoDirExists(t, ws.Dir)
	assert.NotContains(t, runGit(t, repo.Path, "worktree", "list"), ws.Dir)

	// Removing an already-gone worktree is not an error.
	require.NoError(t, m.Remove(ctx, repo, ws))
}

func TestPrepareSameBranchTwiceWithoutRemove(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()

	// A failed run keeps its worktree, so the retry for the same branch must still set up.
	ws1, err := m.Prepare(ctx, repo, "foundry/retry", "run1")
	require.NoError(t, err)
	ws2, err := m.Prepare(ctx, repo, "foundry/retry", "run2")
	require.NoError(t, err)
	assert.NoDirExists(t, ws1.Dir, "the stale worktree is removed")
	assert.Equal(t, "foundry/retry", runGit(t, ws2.Dir, "rev-parse", "--abbrev-ref", "HEAD"))
}

// conflictingWorkspace returns a worktree on foundry/m whose README.md conflicts with origin/main.
func conflictingWorkspace(t *testing.T) (Repo, *Manager, Workspace) {
	t.Helper()
	repo, m, seed := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "foundry/m", "run1")
	require.NoError(t, err)

	writeFile(t, filepath.Join(ws.Dir, "README.md"), "branch side\n")
	committed, err := m.Commit(ctx, ws, "branch edit")
	require.NoError(t, err)
	require.True(t, committed)

	writeFile(t, filepath.Join(seed, "README.md"), "main side\n")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-am", "main edit")
	runGit(t, seed, "push", repo.Remote, "main")
	return repo, m, ws
}

func TestMergeBaseCleanMerge(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "foundry/clean", "run1")
	require.NoError(t, err)

	writeFile(t, filepath.Join(seed, "NEW.md"), "new\n")
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "new")
	runGit(t, seed, "push", repo.Remote, "main")

	conflicts, err := m.MergeBase(ctx, repo, ws)
	require.NoError(t, err)
	assert.Empty(t, conflicts)
	assert.FileExists(t, filepath.Join(ws.Dir, "NEW.md"))
}

func TestMergeBaseReportsConflictsAndLeavesMerge(t *testing.T) {
	repo, m, ws := conflictingWorkspace(t)
	ctx := context.Background()

	conflicts, err := m.MergeBase(ctx, repo, ws)
	require.NoError(t, err)
	assert.Equal(t, []string{"README.md"}, conflicts)
	_, err = gitExitCodeForTest(ws.Dir, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	assert.NoError(t, err, "the conflicted merge stays in progress for the agent")
}

func TestMergeBaseOtherFailureAbortsAndReturnsError(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "foundry/dirty", "run1")
	require.NoError(t, err)

	writeFile(t, filepath.Join(seed, "pkg", "a.txt"), "upstream\n")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-am", "upstream")
	runGit(t, seed, "push", repo.Remote, "main")
	// An uncommitted edit to the same file makes git refuse the merge before any conflict exists.
	writeFile(t, filepath.Join(ws.Dir, "pkg", "a.txt"), "local edit\n")

	conflicts, err := m.MergeBase(ctx, repo, ws)
	require.Error(t, err)
	assert.Nil(t, conflicts)
	_, err = gitExitCodeForTest(ws.Dir, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	assert.Error(t, err, "no merge is left in progress")
}

func TestCommitRefusesLeftoverConflictMarkers(t *testing.T) {
	repo, m, ws := conflictingWorkspace(t)
	ctx := context.Background()
	_, err := m.MergeBase(ctx, repo, ws)
	require.NoError(t, err)
	before := runGit(t, ws.Dir, "rev-parse", "HEAD")

	committed, err := m.Commit(ctx, ws, "conclude merge")
	require.ErrorIs(t, err, ErrUnresolvedConflicts)
	assert.False(t, committed)
	assert.Contains(t, err.Error(), "README.md:")
	assert.Equal(t, before, runGit(t, ws.Dir, "rev-parse", "HEAD"), "nothing was committed")
}

func TestCommitConcludesResolvedMerge(t *testing.T) {
	repo, m, ws := conflictingWorkspace(t)
	ctx := context.Background()
	_, err := m.MergeBase(ctx, repo, ws)
	require.NoError(t, err)

	writeFile(t, filepath.Join(ws.Dir, "README.md"), "resolved\n")
	committed, err := m.Commit(ctx, ws, "merge main")
	require.NoError(t, err)
	assert.True(t, committed)
	_, err = gitExitCodeForTest(ws.Dir, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	assert.Error(t, err, "the commit concludes the merge")
	parents := strings.Fields(runGit(t, ws.Dir, "rev-list", "--parents", "-n", "1", "HEAD"))
	assert.Len(t, parents, 3, "merge commit has two parents")
}

func TestCommitIgnoresWhitespaceProblems(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "foundry/ws", "run1")
	require.NoError(t, err)

	writeFile(t, filepath.Join(ws.Dir, "README.md"), "hello   \n")
	committed, err := m.Commit(ctx, ws, "trailing space")
	require.NoError(t, err)
	assert.True(t, committed)
}

// gitExitCodeForTest runs git in dir and returns an error when it exits non-zero.
func gitExitCodeForTest(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// humanPush clones origin into a fresh directory, commits a file on branch as a human would, and
// pushes it. It returns the new commit.
func humanPush(t *testing.T, repo Repo, branch, file string) string {
	t.Helper()
	human := filepath.Join(t.TempDir(), "human")
	runGit(t, filepath.Dir(human), "clone", repo.Remote, human)
	runGit(t, human, "checkout", "-B", branch, "origin/main")
	writeFile(t, filepath.Join(human, file), "written by a human\n")
	runGit(t, human, "add", "-A")
	runGit(t, human, "-c", "user.name=Human", "-c", "user.email=human@example.com", "commit", "-m", "human work")
	runGit(t, human, "push", "origin", branch)
	return runGit(t, human, "rev-parse", "HEAD")
}

// A human pushes to an existing PR branch after Prepare, and a background fetch moves
// origin/<branch>. Foundry's push must not overwrite the human's commit.
func TestPushRefusesWhenRemoteBranchMovedAfterPrepare(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	runGit(t, seed, "push", repo.Remote, "main:refs/heads/foundry/pr")

	ws, err := m.Prepare(ctx, repo, "foundry/pr", "run1")
	require.NoError(t, err)
	assert.Equal(t, runGit(t, repo.Remote, "rev-parse", "refs/heads/foundry/pr"), ws.RemoteSHA)

	humanSHA := humanPush(t, repo, "foundry/pr", "human.txt")
	require.NoError(t, m.EnsureClone(ctx, repo)) // the background fetch that moves origin/foundry/pr
	require.Equal(t, humanSHA, runGit(t, repo.Path, "rev-parse", "origin/foundry/pr"))

	writeFile(t, filepath.Join(ws.Dir, "agent.txt"), "agent work\n")
	committed, err := m.Commit(ctx, ws, "agent work")
	require.NoError(t, err)
	require.True(t, committed)

	err = m.Push(ctx, repo, ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rejected")
	assert.Equal(t, humanSHA, runGit(t, repo.Remote, "rev-parse", "refs/heads/foundry/pr"),
		"the human's commit is still on origin")
}

// Prepare finds no foundry/new on origin, someone creates it, and a background fetch sees it.
// Foundry's push must fail rather than overwrite it.
func TestPushRefusesWhenBranchCreatedRemotelyAfterPrepare(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()

	ws, err := m.Prepare(ctx, repo, "foundry/new", "run1")
	require.NoError(t, err)
	assert.Empty(t, ws.RemoteSHA)

	humanSHA := humanPush(t, repo, "foundry/new", "human.txt")
	require.NoError(t, m.EnsureClone(ctx, repo))

	writeFile(t, filepath.Join(ws.Dir, "agent.txt"), "agent work\n")
	committed, err := m.Commit(ctx, ws, "agent work")
	require.NoError(t, err)
	require.True(t, committed)

	err = m.Push(ctx, repo, ws)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rejected")
	assert.Equal(t, humanSHA, runGit(t, repo.Remote, "rev-parse", "refs/heads/foundry/new"),
		"the human's branch is still on origin")
}

// A worktree on a branch the remote does not move is pushed as before.
func TestPushSucceedsWhenRemoteUnchangedSincePrepare(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	runGit(t, seed, "push", repo.Remote, "main:refs/heads/foundry/pr")

	ws, err := m.Prepare(ctx, repo, "foundry/pr", "run1")
	require.NoError(t, err)
	writeFile(t, filepath.Join(ws.Dir, "agent.txt"), "agent work\n")
	_, err = m.Commit(ctx, ws, "agent work")
	require.NoError(t, err)
	require.NoError(t, m.Push(ctx, repo, ws))
	assert.Equal(t, runGit(t, ws.Dir, "rev-parse", "HEAD"), runGit(t, repo.Remote, "rev-parse", "refs/heads/foundry/pr"))
}

// Hooks in the clone's shared hooks dir must not run for foundry's git commands.
func TestGitCommandsDoNotRunHooks(t *testing.T) {
	repo, m, _ := setup(t)
	ctx := context.Background()
	ws, err := m.Prepare(ctx, repo, "foundry/hooks", "run1")
	require.NoError(t, err)

	marker := filepath.Join(t.TempDir(), "hook-ran")
	hooks := filepath.Join(repo.Path, ".git", "hooks")
	require.NoError(t, os.MkdirAll(hooks, 0o755))
	for _, name := range []string{"pre-commit", "pre-push"} {
		script := "#!/bin/sh\ntouch " + marker + "-" + name + "\nexit 1\n"
		require.NoError(t, os.WriteFile(filepath.Join(hooks, name), []byte(script), 0o755))
	}

	writeFile(t, filepath.Join(ws.Dir, "README.md"), "hello\nworld\n")
	committed, err := m.Commit(ctx, ws, "add world")
	require.NoError(t, err)
	assert.True(t, committed)
	assert.NoFileExists(t, marker+"-pre-commit")

	require.NoError(t, m.Push(ctx, repo, ws))
	assert.NoFileExists(t, marker+"-pre-push")
}

// The clone's own checkout follows the project's default branch, not the remote's HEAD, and
// moves forward with origin on later fetches.
func TestEnsureCloneChecksOutDefaultBranch(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()

	runGit(t, seed, "checkout", "-b", "dev")
	writeFile(t, filepath.Join(seed, "DEV.md"), "dev\n")
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "dev")
	runGit(t, seed, "push", repo.Remote, "dev")
	repo.DefaultBranch = "dev"

	require.NoError(t, m.EnsureClone(ctx, repo))
	assert.FileExists(t, filepath.Join(repo.Path, "DEV.md"))

	writeFile(t, filepath.Join(seed, "DEV2.md"), "dev again\n")
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "dev2")
	runGit(t, seed, "push", repo.Remote, "dev")

	require.NoError(t, m.EnsureClone(ctx, repo))
	assert.FileExists(t, filepath.Join(repo.Path, "DEV2.md"))
	assert.Equal(t, runGit(t, repo.Path, "rev-parse", "origin/dev"), runGit(t, repo.Path, "rev-parse", "HEAD"))
}

// A clone with local modifications keeps its checkout; EnsureClone does not touch the files.
func TestEnsureCloneLeavesDirtyCheckoutAlone(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()

	runGit(t, seed, "checkout", "-b", "dev")
	writeFile(t, filepath.Join(seed, "DEV.md"), "dev\n")
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "dev")
	runGit(t, seed, "push", repo.Remote, "dev")
	repo.DefaultBranch = "dev"

	require.NoError(t, m.EnsureClone(ctx, repo))
	writeFile(t, filepath.Join(repo.Path, "README.md"), "local edit\n")

	require.NoError(t, m.EnsureClone(ctx, repo))
	got, err := os.ReadFile(filepath.Join(repo.Path, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, "local edit\n", string(got))
}
