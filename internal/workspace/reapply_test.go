package workspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRevertedInIgnoresRevertedRevert checks that a revert which is itself reverted (a "Reapply")
// no longer counts: commit A, then revert A (R1) is reverted, so A is in effect again. Before R2
// lands, R1 reverts A and A is reverted. Each answer comes after an explicit Fetch.
func TestRevertedInIgnoresRevertedRevert(t *testing.T) {
	repo, m, seed := setup(t)
	ctx := context.Background()
	require.NoError(t, m.EnsureClone(ctx, repo))
	commit := func(msg string) {
		t.Helper()
		runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "commit", "-m", msg)
		runGit(t, seed, "push", repo.Remote, "main")
	}

	writeFile(t, filepath.Join(seed, "feature.txt"), "feature\n")
	runGit(t, seed, "add", "-A")
	commit("feature")
	a := runGit(t, seed, "rev-parse", "HEAD")

	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "revert", "--no-edit", a)
	runGit(t, seed, "push", repo.Remote, "main")
	r1 := runGit(t, seed, "rev-parse", "HEAD")
	require.NoError(t, m.Fetch(ctx, repo))

	got, err := m.RevertedIn(ctx, repo, a, "origin/main")
	require.NoError(t, err)
	assert.True(t, got, "A is reverted by R1 while R1 stands")

	runGit(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.com", "revert", "--no-edit", r1)
	runGit(t, seed, "push", repo.Remote, "main")
	require.NoError(t, m.Fetch(ctx, repo))

	got, err = m.RevertedIn(ctx, repo, a, "origin/main")
	require.NoError(t, err)
	assert.False(t, got, "A is back in effect once R1 is reverted by R2")
}
