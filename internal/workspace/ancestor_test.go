package workspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsAncestorOrdersCommitsAndRejectsUnknown checks IsAncestor on a two-commit history: an
// earlier commit is an ancestor of a later one but not the reverse, a commit is its own
// ancestor, and a commit the clone does not have is an error rather than false. The clone is
// fetched explicitly first, because IsAncestor does not fetch.
func TestIsAncestorOrdersCommitsAndRejectsUnknown(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	require.NoError(t, m.EnsureClone(ctx, repo))

	a := runGit(t, seed, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(seed, "later.txt"), "later\n")
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "later")
	runGit(t, seed, "push", repo.Remote, "main")
	b := runGit(t, seed, "rev-parse", "HEAD")
	require.NoError(t, m.Fetch(ctx, repo))

	got, err := m.IsAncestor(ctx, repo, a, b)
	require.NoError(t, err)
	assert.True(t, got, "earlier commit should be an ancestor of the later one")

	got, err = m.IsAncestor(ctx, repo, b, a)
	require.NoError(t, err)
	assert.False(t, got, "later commit should not be an ancestor of the earlier one")

	got, err = m.IsAncestor(ctx, repo, a, a)
	require.NoError(t, err)
	assert.True(t, got, "a commit is an ancestor of itself")

	_, err = m.IsAncestor(ctx, repo, "1111111111111111111111111111111111111111", b)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git exit 128")
}
