// Package workspace gives each agent run its own git worktree on a repository's local clone, and
// does the git work around it: commit, push, diff stats and cleanup. Foundry pushes; agents
// never get push credentials.
package workspace

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bobbyrc/foundry/internal/policy"
)

// Repo is a project's repository and its local clone.
type Repo struct {
	Name          string // project name
	Remote        string // "owner/name" (GitHub) or an absolute path / file:// URL to a git repo (tests, local-only projects)
	Path          string // local clone
	DefaultBranch string
}

// Workspace is one run's worktree.
type Workspace struct {
	Dir      string // the worktree
	Branch   string
	BaseRef  string // "origin/<DefaultBranch>"
	StartSHA string // HEAD when Prepare returned
}

// Manager creates, commits, pushes and removes worktrees under Root.
type Manager struct {
	Root string // where worktrees live, e.g. <data_dir>/worktrees
}

// outboxExclude is the info/exclude pattern that keeps an agent's .foundry-run/ outbox out of
// commits. It is anchored to the worktree root.
const outboxExclude = "/.foundry-run/"

// New returns a Manager whose worktrees live under root.
func New(root string) *Manager {
	return &Manager{Root: root}
}

// remoteURL turns Repo.Remote into something git can clone. A bare "owner/name" is a GitHub
// repository; anything that looks like a path or URL is used as-is.
func remoteURL(r Repo) string {
	s := r.Remote
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "git@") ||
		strings.Contains(s, "://") {
		return s
	}
	return "https://github.com/" + s + ".git"
}

// EnsureClone makes sure r.Path is a clone of the remote and fetches origin, pruning deleted
// branches.
func (m *Manager) EnsureClone(ctx context.Context, r Repo) error {
	_, err := os.Stat(filepath.Join(r.Path, ".git"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(r.Path), 0o755); err != nil {
			return fmt.Errorf("create parent of clone %s: %w", r.Path, err)
		}
		if _, err := git(ctx, "", "clone", remoteURL(r), r.Path); err != nil {
			return fmt.Errorf("clone %s: %w", r.Name, err)
		}
	case err != nil:
		return fmt.Errorf("stat clone %s: %w", r.Path, err)
	}
	if _, err := git(ctx, r.Path, "fetch", "origin", "--prune"); err != nil {
		return fmt.Errorf("fetch %s: %w", r.Name, err)
	}
	return nil
}

// Prepare creates the worktree for one run on branch. If origin already has the branch (a PR
// being re-worked), the worktree starts from its tip; otherwise it starts a new branch from
// origin's default branch.
func (m *Manager) Prepare(ctx context.Context, r Repo, branch, runID string) (Workspace, error) {
	if err := m.EnsureClone(ctx, r); err != nil {
		return Workspace{}, err
	}
	dir := filepath.Join(m.Root, r.Name, runID)
	baseRef := "origin/" + r.DefaultBranch

	var start string
	if _, err := git(ctx, r.Path, "rev-parse", "--verify", "--quiet",
		"refs/remotes/origin/"+branch); err == nil {
		start = "origin/" + branch
		_, err = git(ctx, r.Path, "worktree", "add", "-B", branch, dir, start)
		if err != nil {
			return Workspace{}, fmt.Errorf("add worktree for %s: %w", branch, err)
		}
	} else {
		flag := "-b"
		if _, err := git(ctx, r.Path, "rev-parse", "--verify", "--quiet",
			"refs/heads/"+branch); err == nil {
			flag = "-B" // a stale local branch of that name; it was never pushed
		}
		start = baseRef
		if _, err := git(ctx, r.Path, "worktree", "add", flag, branch, dir, start); err != nil {
			return Workspace{}, fmt.Errorf("add worktree for %s: %w", branch, err)
		}
	}

	if err := excludeOutbox(ctx, r.Path); err != nil {
		return Workspace{}, err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".foundry-run"), 0o755); err != nil {
		return Workspace{}, fmt.Errorf("create outbox in %s: %w", dir, err)
	}

	ws := Workspace{Dir: dir, Branch: branch, BaseRef: baseRef}
	sha, err := m.HeadSHA(ctx, ws)
	if err != nil {
		return Workspace{}, err
	}
	ws.StartSHA = sha
	return ws, nil
}

// excludeOutbox adds outboxExclude to the repository's shared info/exclude once. The file lives
// in the common git dir, so every worktree of this clone honours it.
func excludeOutbox(ctx context.Context, clonePath string) error {
	common, err := git(ctx, clonePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(clonePath, common)
	}
	infoDir := filepath.Join(common, "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", infoDir, err)
	}
	path := filepath.Join(infoDir, "exclude")

	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(existing)))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == outboxExclude {
			return nil
		}
	}
	prefix := ""
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		prefix = "\n"
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(prefix + outboxExclude + "\n"); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Commit stages everything in the worktree and commits it. It returns false, with no commit,
// when nothing changed. The commit uses Foundry's identity only when git has no user.email
// configured.
func (m *Manager) Commit(ctx context.Context, ws Workspace, message string) (bool, error) {
	if _, err := git(ctx, ws.Dir, "add", "-A"); err != nil {
		return false, err
	}
	code, err := gitExitCode(ctx, ws.Dir, "diff", "--cached", "--quiet")
	if err != nil {
		return false, err
	}
	if code == 0 {
		return false, nil
	}

	var args []string
	if email, err := git(ctx, ws.Dir, "config", "user.email"); err != nil || email == "" {
		args = append(args, "-c", "user.name=Foundry", "-c", "user.email=foundry@localhost")
	}
	args = append(args, "commit", "-m", message)
	if _, err := git(ctx, ws.Dir, args...); err != nil {
		return false, err
	}
	return true, nil
}

// HeadSHA returns the commit checked out in the worktree.
func (m *Manager) HeadSHA(ctx context.Context, ws Workspace) (string, error) {
	sha, err := git(ctx, ws.Dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read HEAD of %s: %w", ws.Dir, err)
	}
	return sha, nil
}

// Push pushes the worktree's branch to origin. It uses --force-with-lease because agents may
// rebase their own branch.
func (m *Manager) Push(ctx context.Context, r Repo, ws Workspace) error {
	if _, err := git(ctx, ws.Dir, "push", "--force-with-lease", "-u", "origin", ws.Branch); err != nil {
		return fmt.Errorf("push %s: %w", ws.Branch, err)
	}
	return nil
}

// Diff returns per-file line counts between origin/base and origin/head. Both are branch names
// without the "origin/" prefix.
func (m *Manager) Diff(ctx context.Context, r Repo, base, head string) ([]policy.FileChange, error) {
	if _, err := git(ctx, r.Path, "fetch", "origin", "--prune"); err != nil {
		return nil, fmt.Errorf("fetch %s: %w", r.Name, err)
	}
	out, err := git(ctx, r.Path, "diff", "--numstat",
		"origin/"+base+"...origin/"+head)
	if err != nil {
		return nil, fmt.Errorf("diff %s...%s: %w", base, head, err)
	}

	changes := []policy.FileChange{}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("parse numstat line %q", line)
		}
		added, err := numstatCount(fields[0])
		if err != nil {
			return nil, fmt.Errorf("parse numstat line %q: %w", line, err)
		}
		deleted, err := numstatCount(fields[1])
		if err != nil {
			return nil, fmt.Errorf("parse numstat line %q: %w", line, err)
		}
		changes = append(changes, policy.FileChange{
			Path:    newPath(fields[2]),
			Added:   added,
			Deleted: deleted,
		})
	}
	return changes, nil
}

// numstatCount reads one count column; git prints "-" for binary files, which count as zero.
func numstatCount(s string) (int, error) {
	if s == "-" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

// newPath resolves a rename as numstat prints it to the file's new path. It handles both
// "old => new" and "dir/{old => new}/file" forms.
func newPath(p string) string {
	open := strings.Index(p, "{")
	closeIdx := strings.Index(p, "}")
	if open >= 0 && closeIdx > open {
		inner := p[open+1 : closeIdx]
		if parts := strings.SplitN(inner, " => ", 2); len(parts) == 2 {
			joined := p[:open] + parts[1] + p[closeIdx+1:]
			return strings.ReplaceAll(joined, "//", "/")
		}
	}
	if _, after, ok := strings.Cut(p, " => "); ok {
		return after
	}
	return p
}

// Remove deletes the worktree and prunes git's record of it. A worktree that is already gone is
// not an error. The branch is kept, since it may back an open PR.
func (m *Manager) Remove(ctx context.Context, r Repo, ws Workspace) error {
	if _, err := os.Stat(ws.Dir); err == nil {
		if _, err := git(ctx, r.Path, "worktree", "remove", "--force", ws.Dir); err != nil {
			return fmt.Errorf("remove worktree %s: %w", ws.Dir, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat worktree %s: %w", ws.Dir, err)
	}
	if _, err := git(ctx, r.Path, "worktree", "prune"); err != nil {
		return fmt.Errorf("prune worktrees of %s: %w", r.Name, err)
	}
	return nil
}
