package engine

import (
	"testing"
	"time"

	"github.com/bobbyrc/onlet/internal/deploy"
	"github.com/bobbyrc/onlet/internal/domain"
)

// TestLoopDeployHealthyReleaseKeepsRevertedShippedTaskBlocked checks that a healthy release does
// not mark a task done when its merge commit is contained in the release but a later commit
// reverted it. A `git revert` of the commit is pushed before the release, so the release carries
// the revert too. An unreverted task whose commit the release also contains still becomes done.
func TestLoopDeployHealthyReleaseKeepsRevertedShippedTaskBlocked(t *testing.T) {
	r := lpNewRepo(t)
	env := lpNewEngine(t, lpDoc(t, r.clone, "    deploy:\n      type: test\n      verify_for: 10m\n", ""))

	reverted := lpShippedCommit(t, r, "reverted.txt")
	kept := lpShippedCommit(t, r, "kept.txt")
	lpGit(t, r.seed, "revert", "--no-edit", reverted)
	lpGit(t, r.seed, "push", "origin", "main")
	released := lpShippedCommit(t, r, "released.txt")

	revertedTask := lpBlockedTask(t, env, "Commit reverted later", reverted,
		"release failed: health check failing; incident task inc-1")
	keptTask := lpBlockedTask(t, env, "Commit not reverted", kept,
		"release failed: health check failing; incident task inc-2")
	merged := lpMergedTask(t, env, "Ship released", released)

	env.run(loopDeployWatch) // starts the release of released
	env.dep.status = deploy.Status{State: deploy.StateReady, URL: "https://demo.example"}
	env.run(loopDeployWatch) // provider is ready: verification starts
	env.now = env.now.Add(11 * time.Minute)
	env.run(loopDeployWatch) // past verify_for: the release is healthy
	if rel := lpDeployments(t, env)[0]; rel.State != domain.DeployHealthy {
		t.Fatalf("release = %s, want healthy", rel.State)
	}

	if got := env.task(merged.ID); got.Status != domain.TaskDone {
		t.Fatalf("released task = %s, want done", got.Status)
	}
	got := env.task(keptTask.ID)
	if got.Status != domain.TaskDone || got.BlockedReason != "" {
		t.Fatalf("unreverted shipped task = %s reason %q, want done with no reason", got.Status, got.BlockedReason)
	}
	got = env.task(revertedTask.ID)
	if got.Status != domain.TaskBlocked || got.BlockedReason == "" {
		t.Fatalf("reverted task = %s reason %q, want still blocked", got.Status, got.BlockedReason)
	}
}
