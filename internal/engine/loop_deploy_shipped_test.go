package engine

import (
	"testing"
	"time"

	"github.com/bobbyrc/onlet/internal/deploy"
	"github.com/bobbyrc/onlet/internal/domain"
)

// lpShippedCommit commits a file on the seed's main, pushes it to origin, and returns the commit.
func lpShippedCommit(t *testing.T, r lpRepo, file string) string {
	t.Helper()
	lpWrite(t, r.seed, file, file+"\n")
	lpGit(t, r.seed, "add", "-A")
	lpGit(t, r.seed, "commit", "-m", "add "+file)
	lpGit(t, r.seed, "push", "origin", "main")
	return lpRevParse(t, r.seed, "HEAD")
}

// lpBlockedTask creates a task that is blocked with reason at sha, the way a failed release
// leaves it, or any other block.
func lpBlockedTask(t *testing.T, env *lpEnv, title, sha, reason string) domain.Task {
	t.Helper()
	task, err := env.st.CreateTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindFeature, Role: "implementer", Title: title,
		Status: domain.TaskBlocked, BlockedReason: reason, MergeSHA: sha,
		MaxAttempts: 3, Source: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// TestLoopDeployHealthyReleaseResolvesShippedBlockedTasks checks which blocked tasks a later
// healthy release clears. A task blocked by a failed release whose merge commit the release
// contains becomes done. A task whose commit the release does not contain stays blocked, and so
// does a task blocked for another reason, even when its commit is contained.
func TestLoopDeployHealthyReleaseResolvesShippedBlockedTasks(t *testing.T) {
	r := lpNewRepo(t)
	env := lpNewEngine(t, lpDoc(t, r.clone, "    deploy:\n      type: test\n      verify_for: 10m\n", ""))

	earlier := lpShippedCommit(t, r, "earlier.txt")
	r.pushBranch(t, "side", map[string]string{"side.txt": "side\n"})
	sideOnly := lpRevParse(t, r.seed, "side")
	released := lpShippedCommit(t, r, "released.txt")

	shipped := lpBlockedTask(t, env, "Shipped despite failed release", earlier,
		"release failed: health check failing; incident task inc-1")
	notShipped := lpBlockedTask(t, env, "Commit not in release", sideOnly,
		"release failed: health check failing; incident task inc-2")
	otherBlock := lpBlockedTask(t, env, "Blocked for another reason", earlier,
		"gave up after 3 attempts")
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
	got := env.task(shipped.ID)
	if got.Status != domain.TaskDone || got.BlockedReason != "" {
		t.Fatalf("shipped task = %s reason %q, want done with no reason", got.Status, got.BlockedReason)
	}
	got = env.task(notShipped.ID)
	if got.Status != domain.TaskBlocked || got.BlockedReason == "" {
		t.Fatalf("task outside the release = %s reason %q, want still blocked", got.Status, got.BlockedReason)
	}
	got = env.task(otherBlock.ID)
	if got.Status != domain.TaskBlocked || got.BlockedReason != "gave up after 3 attempts" {
		t.Fatalf("task blocked for another reason = %s reason %q, want unchanged", got.Status, got.BlockedReason)
	}
}
