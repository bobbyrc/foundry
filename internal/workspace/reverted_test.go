package workspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRevertedInFindsPushedRevert checks RevertedIn on a commit that is later reverted on origin:
// it is false while nothing reverts the commit, true once a `git revert` of it is pushed, and
// false for a commit that no revert names. Each answer comes after an explicit Fetch, since
// RevertedIn reads the clone as it is.
func TestRevertedInFindsPushedRevert(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	require.NoError(t, m.EnsureClone(ctx, repo))

	writeFile(t, filepath.Join(seed, "feature.txt"), "feature\n")
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "feature")
	runGit(t, seed, "push", repo.Remote, "main")
	feature := runGit(t, seed, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(seed, "other.txt"), "other\n")
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", "other")
	runGit(t, seed, "push", repo.Remote, "main")
	other := runGit(t, seed, "rev-parse", "HEAD")
	require.NoError(t, m.Fetch(ctx, repo))

	got, err := m.RevertedIn(ctx, repo, feature, "origin/main")
	require.NoError(t, err)
	assert.False(t, got, "a commit no revert names is not reverted")

	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "revert", "--no-edit", feature)
	runGit(t, seed, "push", repo.Remote, "main")
	require.NoError(t, m.Fetch(ctx, repo))

	got, err = m.RevertedIn(ctx, repo, feature, "origin/main")
	require.NoError(t, err)
	assert.True(t, got, "a pushed git revert of the commit is found")

	got, err = m.RevertedIn(ctx, repo, other, "origin/main")
	require.NoError(t, err)
	assert.False(t, got, "a revert of one commit does not mark another as reverted")
}
