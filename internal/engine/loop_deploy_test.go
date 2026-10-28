package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/foundry/internal/deploy"
	"github.com/bobbyrc/foundry/internal/domain"
)

// lpMergedTask creates a task that is merged at sha and waiting for a release.
func lpMergedTask(t *testing.T, env *lpEnv, title, sha string) domain.Task {
	t.Helper()
	task, err := env.st.CreateTask(env.ctx, domain.Task{
		Project: "demo", Kind: domain.KindFeature, Role: "implementer", Title: title,
		Status: domain.TaskMerged, MergeSHA: sha, MaxAttempts: 3, Source: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// lpDeployments lists demo's releases, newest first.
func lpDeployments(t *testing.T, env *lpEnv) []domain.Deployment {
	t.Helper()
	out, err := env.st.ListDeployments(env.ctx, "demo", 50)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func lpDeployDoc(t *testing.T, healthURL string) string {
	t.Helper()
	extra := "    deploy:\n      type: test\n      verify_for: 10m\n"
	if healthURL != "" {
		extra += "      health_url: " + healthURL + "\n"
	}
	return lpDoc(t, lpPath(t), extra, "")
}

func TestLoopDeployWithoutDeployerMarksDone(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	merged := lpMergedTask(t, env, "Add notes", "sha-1")

	env.run(loopDeployWatch)

	got := env.task(merged.ID)
	if got.Status != domain.TaskDone {
		t.Fatalf("status = %s, want done", got.Status)
	}
	if len(env.dep.deployed) != 0 {
		t.Fatalf("deployer called without a deploy block: %v", env.dep.deployed)
	}
}

func TestLoopDeployHappyPathVerifiesAndMarksDone(t *testing.T) {
	health := lpNewHealth(t)
	env := lpNewEngine(t, lpDeployDoc(t, health.srv.URL))
	merged := lpMergedTask(t, env, "Add notes", "sha-1")

	env.run(loopDeployWatch) // starts the release
	if got := env.task(merged.ID); got.Status != domain.TaskDeploying {
		t.Fatalf("task = %s, want deploying", got.Status)
	}
	if len(env.dep.deployed) != 1 || env.dep.deployed[0] != "sha-1" {
		t.Fatalf("deployer got %v, want [sha-1]", env.dep.deployed)
	}

	env.dep.status = deploy.Status{State: deploy.StateReady, URL: "https://demo.example"}
	env.run(loopDeployWatch) // provider is ready: verification starts
	rel := lpDeployments(t, env)[0]
	if rel.State != domain.DeployVerifying || rel.VerifyingSince == nil || !rel.VerifyingSince.Equal(env.now) {
		t.Fatalf("release = %s since %v, want verifying from now", rel.State, rel.VerifyingSince)
	}

	env.now = env.now.Add(5 * time.Minute)
	env.run(loopDeployWatch) // inside verify_for
	if rel := lpDeployments(t, env)[0]; rel.State != domain.DeployVerifying {
		t.Fatalf("release = %s at 5m, want still verifying", rel.State)
	}

	env.now = env.now.Add(6 * time.Minute)
	env.run(loopDeployWatch) // past verify_for
	rel = lpDeployments(t, env)[0]
	if rel.State != domain.DeployHealthy {
		t.Fatalf("release = %s at 11m, want healthy", rel.State)
	}
	if got := env.task(merged.ID); got.Status != domain.TaskDone {
		t.Fatalf("task = %s, want done after a healthy release", got.Status)
	}
	if len(env.dep.deployed) != 1 {
		t.Fatalf("a second release started: %v", env.dep.deployed)
	}
}

func TestLoopDeployHealthFailuresRollBack(t *testing.T) {
	health := lpNewHealth(t)
	env := lpNewEngine(t, lpDeployDoc(t, health.srv.URL))
	previous, err := env.st.CreateDeployment(env.ctx, domain.Deployment{
		Project: "demo", Ref: "prev-sha", ProviderID: "prov-old", State: domain.DeployHealthy,
		StartedAt: env.now.Add(-time.Hour), // older than the release under test
	})
	if err != nil {
		t.Fatal(err)
	}
	merged := lpMergedTask(t, env, "Break things", "sha-2")

	env.run(loopDeployWatch)
	env.dep.status = deploy.Status{State: deploy.StateReady}
	env.run(loopDeployWatch)
	health.code.Store(500)

	env.run(loopDeployWatch)
	env.run(loopDeployWatch)
	if rel := lpDeployments(t, env)[0]; rel.State != domain.DeployVerifying {
		t.Fatalf("release = %s after two failed checks, want still verifying", rel.State)
	}

	env.run(loopDeployWatch) // third failure in a row

	rel := lpDeployments(t, env)[0]
	if rel.State != domain.DeployRolledBack {
		t.Fatalf("release = %s, want rolled_back", rel.State)
	}
	if !strings.Contains(rel.Notes, "health check failing") || !strings.Contains(rel.Notes, "rolled back to prev-sh") {
		t.Fatalf("notes = %q, want the health reason and the rollback", rel.Notes)
	}
	if len(env.dep.rolledBack) != 1 || env.dep.rolledBack[0] != [2]string{previous.ProviderID, previous.Ref} {
		t.Fatalf("rollbacks = %v, want the previous healthy release", env.dep.rolledBack)
	}
	got := env.task(merged.ID)
	if got.Status != domain.TaskBlocked || !strings.Contains(got.BlockedReason, "release failed") {
		t.Fatalf("task = %s %q, want blocked by the failed release", got.Status, got.BlockedReason)
	}
	incidents := env.tasks(domain.KindIncident)
	if len(incidents) != 1 || incidents[0].SourceRef != "deploy:"+rel.ID || incidents[0].Priority != 10 {
		t.Fatalf("incidents = %+v, want one incident for the release", incidents)
	}
	if !strings.Contains(got.BlockedReason, incidents[0].ID) {
		t.Fatalf("blocked reason %q does not name incident %s", got.BlockedReason, incidents[0].ID)
	}
}

func TestLoopDeployStartErrorFailsAndOpensIncident(t *testing.T) {
	env := lpNewEngine(t, lpDeployDoc(t, ""))
	merged := lpMergedTask(t, env, "Add notes", "sha-3")
	env.dep.deployErr = errors.New("provider down")

	env.run(loopDeployWatch)

	rel := lpDeployments(t, env)[0]
	if rel.State != domain.DeployFailed || !strings.Contains(rel.Notes, "deploy failed to start: provider down") {
		t.Fatalf("release = %s notes %q, want failed with the start error", rel.State, rel.Notes)
	}
	if len(env.dep.rolledBack) != 0 {
		t.Fatalf("a start failure rolled back: %v", env.dep.rolledBack)
	}
	if got := env.task(merged.ID); got.Status != domain.TaskBlocked {
		t.Fatalf("task = %s, want blocked", got.Status)
	}
	if n := len(env.tasks(domain.KindIncident)); n != 1 {
		t.Fatalf("got %d incidents, want 1", n)
	}
}
