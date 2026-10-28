package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/foundry/internal/config"
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
	if !strings.Contains(rel.Notes, "health check failing at "+health.srv.URL+": 3 consecutive failed checks") || !strings.Contains(rel.Notes, "rolled back to prev-sh") {
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

// A release still building past the project's deploy timeout fails, without a rollback.
func TestLoopDeployFailsStuckBuildAfterTimeout(t *testing.T) {
	env := lpNewEngine(t, lpDeployDoc(t, ""))
	merged := lpMergedTask(t, env, "Slow build", "sha-9")
	if _, err := env.st.CreateDeployment(env.ctx, domain.Deployment{
		Project: "demo", TaskID: merged.ID, Ref: "sha-9", ProviderID: "prov-x",
		State: domain.DeployDeploying, StartedAt: env.now.Add(-31 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	env.run(loopDeployWatch)

	rel := lpDeployments(t, env)[0]
	if rel.State != domain.DeployFailed || !strings.Contains(rel.Notes, "deploy still not ready after 30m0s") {
		t.Fatalf("release = %s notes %q, want failed after the 30m timeout", rel.State, rel.Notes)
	}
	if len(env.dep.rolledBack) != 0 {
		t.Fatalf("a stuck build rolled back: %v", env.dep.rolledBack)
	}
	if got := env.task(merged.ID); got.Status != domain.TaskBlocked {
		t.Fatalf("task = %s, want blocked by the failed release", got.Status)
	}
}

// The project's own timeout is used when set.
func TestLoopDeployUsesConfiguredTimeout(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "    deploy:\n      type: test\n      timeout: 10m\n", ""))
	if _, err := env.st.CreateDeployment(env.ctx, domain.Deployment{
		Project: "demo", Ref: "sha-8", ProviderID: "prov-y",
		State: domain.DeployDeploying, StartedAt: env.now.Add(-11 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	env.run(loopDeployWatch)

	if rel := lpDeployments(t, env)[0]; rel.State != domain.DeployFailed || !strings.Contains(rel.Notes, "after 10m0s") {
		t.Fatalf("release = %s notes %q, want failed after the 10m timeout", rel.State, rel.Notes)
	}
}

// A build inside the timeout keeps waiting.
func TestLoopDeployWaitsInsideTimeout(t *testing.T) {
	env := lpNewEngine(t, lpDeployDoc(t, ""))
	if _, err := env.st.CreateDeployment(env.ctx, domain.Deployment{
		Project: "demo", Ref: "sha-7", ProviderID: "prov-z",
		State: domain.DeployDeploying, StartedAt: env.now.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	env.run(loopDeployWatch)

	if rel := lpDeployments(t, env)[0]; rel.State != domain.DeployDeploying {
		t.Fatalf("release = %s at 5m, want still deploying", rel.State)
	}
}

// lpOutputDeployer is lpDeployer with a canned deploy output on every handle.
type lpOutputDeployer struct {
	*lpDeployer
	output string
}

func (d lpOutputDeployer) Deploy(ctx context.Context, ref string) (deploy.Handle, error) {
	h, err := d.lpDeployer.Deploy(ctx, ref)
	h.Output = d.output
	return h, err
}

// lpWithOutput makes the env's deployer print output on every release.
func lpWithOutput(env *lpEnv, output string) {
	env.e.d.DeployerFor = func(*config.Project) (deploy.Deployer, error) {
		return lpOutputDeployer{lpDeployer: env.dep, output: output}, nil
	}
}

// lpFailReleaseWithOutput starts a release, then makes the provider report it failed, so the
// release fails through failRelease with the evidence gathered.
func lpFailReleaseWithOutput(t *testing.T, env *lpEnv) domain.Deployment {
	t.Helper()
	lpMergedTask(t, env, "Break things", "sha-ev")
	env.run(loopDeployWatch) // starts the release
	env.dep.status = deploy.Status{State: deploy.StateError, Message: "build exploded"}
	env.run(loopDeployWatch) // provider reports the build failed
	rel := lpDeployments(t, env)[0]
	if rel.State != domain.DeployFailed {
		t.Fatalf("release = %s, want failed", rel.State)
	}
	return rel
}

func TestLoopDeployStoresDeployOutputOnRelease(t *testing.T) {
	env := lpNewEngine(t, lpDeployDoc(t, ""))
	lpWithOutput(env, "uploading\n{\"id\":\"prov-1\"}\n")
	lpMergedTask(t, env, "Add notes", "sha-out")

	env.run(loopDeployWatch)

	rel := lpDeployments(t, env)[0]
	if want := "== deploy output ==\nuploading\n{\"id\":\"prov-1\"}\n"; rel.Log != want {
		t.Fatalf("log = %q, want %q", rel.Log, want)
	}
}

func TestLoopDeployFailureIncidentHasOutputAndServiceLogs(t *testing.T) {
	doc := lpDoc(t, t.TempDir(), "    deploy:\n      type: test\n      logs: echo service-line-1; echo service-line-2\n", "")
	env := lpNewEngine(t, doc)
	lpWithOutput(env, "compiling app\nlinking app\n")

	rel := lpFailReleaseWithOutput(t, env)

	for _, want := range []string{
		"== deploy output ==\ncompiling app\nlinking app\n",
		"== service logs (echo service-line-1; echo service-line-2) ==\nservice-line-1\nservice-line-2",
	} {
		if !strings.Contains(rel.Log, want) {
			t.Fatalf("release log %q does not contain %q", rel.Log, want)
		}
	}
	incidents := env.tasks(domain.KindIncident)
	if len(incidents) != 1 {
		t.Fatalf("got %d incidents, want 1", len(incidents))
	}
	body := incidents[0].Body
	if !strings.Contains(body, "\n## Evidence\n\n```\n== deploy output ==\ncompiling app\nlinking app\n") ||
		!strings.Contains(body, "service-line-2\n```\n") {
		t.Fatalf("incident body has no evidence section with both outputs:\n%s", body)
	}
}

func TestLoopDeployServiceLogsFailureIsReported(t *testing.T) {
	doc := lpDoc(t, t.TempDir(), "    deploy:\n      type: test\n      logs: echo partial-logs; echo nope >&2; exit 4\n", "")
	env := lpNewEngine(t, doc)

	rel := lpFailReleaseWithOutput(t, env)

	if rel.State != domain.DeployFailed || !strings.Contains(rel.Notes, "build exploded") {
		t.Fatalf("release = %s notes %q, want failed with the provider's reason", rel.State, rel.Notes)
	}
	if !strings.Contains(rel.Log, "partial-logs\nnope\nlogs command failed: exit status 4") {
		t.Fatalf("release log %q does not report the logs failure", rel.Log)
	}
	incidents := env.tasks(domain.KindIncident)
	if len(incidents) != 1 || !strings.Contains(incidents[0].Body, "logs command failed: exit status 4") {
		t.Fatalf("incident evidence does not report the logs failure: %+v", incidents)
	}
	if got := env.task(lpDeployTaskID(t, env)); got.Status != domain.TaskBlocked {
		t.Fatalf("task = %s, want blocked by the failed release", got.Status)
	}
}

func TestLoopDeployServiceLogsTimeoutIsReported(t *testing.T) {
	old := serviceLogsTimeout
	serviceLogsTimeout = 200 * time.Millisecond
	t.Cleanup(func() { serviceLogsTimeout = old })

	doc := lpDoc(t, t.TempDir(), "    deploy:\n      type: test\n      logs: sleep 30\n", "")
	env := lpNewEngine(t, doc)

	start := time.Now()
	rel := lpFailReleaseWithOutput(t, env)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("release handling took %s, the logs command was not bounded", elapsed)
	}
	if !strings.Contains(rel.Log, "logs command failed: timed out after 200ms") {
		t.Fatalf("release log %q does not report the timeout", rel.Log)
	}
}

func TestLoopDeployServiceLogsRunInProjectDirWithProjectEnv(t *testing.T) {
	dir := t.TempDir()
	doc := lpDoc(t, dir, "    env:\n      LP_LOG_ENV: from-project\n    deploy:\n      type: test\n      logs: pwd -P; echo \"$LP_LOG_ENV\"\n", "")
	env := lpNewEngine(t, doc)

	rel := lpFailReleaseWithOutput(t, env)

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(rel.Log, resolved+"\nfrom-project") {
		t.Fatalf("release log %q, want the logs run in %s with the project env", rel.Log, resolved)
	}
}

func TestLoopDeployNoLogsCommandAddsNoEvidence(t *testing.T) {
	env := lpNewEngine(t, lpDeployDoc(t, ""))
	env.dep.status = deploy.Status{State: deploy.StateError, Message: "boom"}
	lpMergedTask(t, env, "Plain", "sha-plain")
	env.run(loopDeployWatch)
	env.run(loopDeployWatch)

	if rel := lpDeployments(t, env)[0]; rel.Log != "" {
		t.Fatalf("log = %q, want empty for a noop-style output and no logs command", rel.Log)
	}
	if body := env.tasks(domain.KindIncident)[0].Body; strings.Contains(body, "## Evidence") {
		t.Fatalf("incident has an evidence section with nothing to show:\n%s", body)
	}
}

func TestEvidenceSectionKeepsNewestOutput(t *testing.T) {
	long := strings.Repeat("a", 20<<10) + "NEWEST"
	section := evidenceSection(long)
	if len(section) > evidenceCap+64 {
		t.Fatalf("section is %d bytes, want about %d", len(section), evidenceCap)
	}
	if !strings.Contains(section, "earlier output truncated") || !strings.HasSuffix(strings.TrimSuffix(section, "```\n"), "NEWEST\n") {
		t.Fatalf("section did not keep the newest output: ...%s", section[len(section)-40:])
	}

	fenced := evidenceSection("before\n```\ninside\n````\nafter")
	if !strings.Contains(fenced, "\n`````\n") {
		t.Fatalf("fence does not outlast the backticks in the evidence:\n%s", fenced)
	}
}

// lpDeployTaskID returns the merged task's ID for the one release in env.
func lpDeployTaskID(t *testing.T, env *lpEnv) string {
	t.Helper()
	rel := lpDeployments(t, env)[0]
	if rel.TaskID == "" {
		t.Fatalf("release %s has no task", rel.ID)
	}
	return rel.TaskID
}

func TestLoopDeployHealthFailureReasonNamesURLCountAndElapsed(t *testing.T) {
	health := lpNewHealth(t)
	env := lpNewEngine(t, lpDeployDoc(t, health.srv.URL))
	lpMergedTask(t, env, "Break things", "sha-2")

	env.run(loopDeployWatch)
	env.dep.status = deploy.Status{State: deploy.StateReady}
	env.run(loopDeployWatch) // verifying from env.now
	health.code.Store(500)

	for range deployHealthAttempts {
		env.now = env.now.Add(30 * time.Second)
		env.run(loopDeployWatch)
	}

	rel := lpDeployments(t, env)[0]
	want := fmt.Sprintf("health check failing at %s: %d consecutive failed checks, 1m30s after verification started: ",
		health.srv.URL, deployHealthAttempts)
	if !strings.Contains(rel.Notes, want) {
		t.Fatalf("release = %s, notes = %q, want notes containing %q", rel.State, rel.Notes, want)
	}
}
