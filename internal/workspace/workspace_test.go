package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
