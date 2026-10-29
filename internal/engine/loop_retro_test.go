package engine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// lpRetroTask stores a task with explicit timestamps (the store stamps UpdatedAt on every update,
// so a test cannot age a task through the engine). Unset fields default to demo, a feature and the
// test clock.
func lpRetroTask(t *testing.T, env *lpEnv, task domain.Task) domain.Task {
	t.Helper()
	if task.Project == "" {
		task.Project = "demo"
	}
	if task.Kind == "" {
		task.Kind = domain.KindFeature
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = env.now
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = env.now
	}
	out, err := env.st.CreateTask(env.ctx, task)
	if err != nil {
		t.Fatalf("create task %q: %v", task.Title, err)
	}
	return out
}

// lpRetroTasks lists the retro tasks of demo, which are improve tasks with source retro.
func lpRetroTasks(env *lpEnv) []domain.Task {
	var out []domain.Task
	for _, t := range env.tasks(domain.KindImprove) {
		if t.Source == "retro" {
			out = append(out, t)
		}
	}
	return out
}

func TestLoopRetroFilesOneTaskPerDay(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	lpRetroTask(t, env, domain.Task{
		Title: "Lint gate", Status: domain.TaskBlocked, BlockedReason: "CI failed: lint",
		UpdatedAt: env.now.Add(-time.Hour),
	})
	day := env.now.Local().Format("2006-01-02")

	env.run(loopRetro)
	got := lpRetroTasks(env)
	if len(got) != 1 {
		t.Fatalf("got %d retro tasks, want 1", len(got))
	}
	task := got[0]
	if task.Kind != domain.KindImprove || task.Role != "improver" || task.Priority != -1 {
		t.Fatalf("task = kind %s role %q priority %d, want improve/improver/-1", task.Kind, task.Role, task.Priority)
	}
	if want := "retro:demo:" + day; task.SourceRef != want {
		t.Fatalf("source_ref = %q, want %q", task.SourceRef, want)
	}
	if want := "Retrospective " + day + ": what slowed the factory down"; task.Title != want {
		t.Fatalf("title = %q, want %q", task.Title, want)
	}
	if !strings.HasPrefix(task.Body, retroInstruction+"\n\n") {
		t.Fatalf("body does not open with the instruction:\n%s", task.Body)
	}
	if !strings.Contains(task.Body, "CI failed: lint") {
		t.Fatalf("body misses the blocked reason:\n%s", task.Body)
	}

	env.run(loopRetro)
	env.now = env.now.Add(time.Hour)
	env.run(loopRetro)
	if n := len(lpRetroTasks(env)); n != 1 {
		t.Fatalf("same day runs made %d retro tasks, want 1", n)
	}

	lpRetroTask(t, env, domain.Task{
		Title: "Flaky clock", Status: domain.TaskFailed, UpdatedAt: env.now,
	})
	env.now = env.now.Add(24 * time.Hour)
	env.run(loopRetro)
	if n := len(lpRetroTasks(env)); n != 2 {
		t.Fatalf("next day with new evidence made %d retro tasks, want 2", n)
	}
}

func TestLoopRetroWithoutEvidenceFilesNothing(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	lpRetroTask(t, env, domain.Task{
		Title: "Ancient failure", Status: domain.TaskFailed, UpdatedAt: env.now.Add(-48 * time.Hour),
	})

	env.run(loopRetro)

	if n := len(lpRetroTasks(env)); n != 0 {
		t.Fatalf("no evidence in the window still filed %d retro tasks", n)
	}
}

func TestLoopRetroEvidenceSections(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	since := env.now.Add(-24 * time.Hour)
	longReason := strings.Repeat("x", 400)

	lpRetroTask(t, env, domain.Task{
		Title: "Long blocker", Status: domain.TaskBlocked, BlockedReason: longReason,
		UpdatedAt: env.now.Add(-time.Hour),
	})
	lpRetroTask(t, env, domain.Task{
		Title: "Old blocker", Status: domain.TaskBlocked, BlockedReason: "stale",
		UpdatedAt: since.Add(-time.Hour),
	})
	lpRetroTask(t, env, domain.Task{
		Title: "Cache fix", Status: domain.TaskQueued,
		Body:      "Do the thing.\n\n## Attempt 1: CI failed: lint\n\nlog excerpt\n",
		UpdatedAt: env.now.Add(-2 * time.Hour),
	})
	lpRetroTask(t, env, domain.Task{
		Title: "Incident: boom", Kind: domain.KindIncident, Status: domain.TaskDone,
		CreatedAt: env.now.Add(-time.Hour),
	})
	parent := lpRetroTask(t, env, domain.Task{
		Title: "Add cache", Status: domain.TaskQueued,
		Body:      "Build it.\n\n## Attempt 1: reviewer requested changes\n\n- nil check missing\n",
		UpdatedAt: env.now.Add(-3 * time.Hour),
	})
	lpRetroTask(t, env, domain.Task{
		Title: "Review: Add cache", Kind: domain.KindReview, Status: domain.TaskDone,
		ParentID: parent.ID, UpdatedAt: env.now.Add(-3 * time.Hour),
	})
	lpFriction(t, env, "Docs for the fake")

	evidence, err := env.e.retroEvidence(env.ctx, "demo", since)
	if err != nil {
		t.Fatalf("retro evidence: %v", err)
	}
	for _, want := range []string{
		"## Blocked or failed tasks",
		"- Long blocker (id ",
		"blocked_reason: " + longReason[:retroReasonLimit] + "...",
		"## Re-queued tasks",
		"- Cache fix (id ",
		"Attempt 1: CI failed: lint",
		"## Friction",
		"- Docs for the fake (count 2, issue none)",
		"## Incidents",
		"- Incident: boom (status done)",
		"## Review findings sent back",
		"- Add cache (id " + parent.ID + ")",
	} {
		if !strings.Contains(evidence, want) {
			t.Errorf("evidence misses %q:\n%s", want, evidence)
		}
	}
	if strings.Contains(evidence, "Old blocker") || strings.Contains(evidence, "stale") {
		t.Errorf("evidence includes a task from before the window:\n%s", evidence)
	}
	if strings.Contains(evidence, longReason) {
		t.Errorf("evidence includes the full blocked reason, want it clipped")
	}
}

func TestLoopRetroCapsEachSection(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	for i := range retroSectionLimit + 5 {
		lpRetroTask(t, env, domain.Task{
			Title: fmt.Sprintf("Failure %02d", i), Status: domain.TaskFailed,
			UpdatedAt: env.now.Add(-time.Duration(i+1) * time.Minute),
		})
	}

	evidence, err := env.e.retroEvidence(env.ctx, "demo", env.now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("retro evidence: %v", err)
	}
	if n := strings.Count(evidence, "(id "); n != retroSectionLimit {
		t.Fatalf("section lists %d items, want %d", n, retroSectionLimit)
	}
	if !strings.Contains(evidence, "Failure 00") || strings.Contains(evidence, fmt.Sprintf("Failure %02d", retroSectionLimit+4)) {
		t.Fatalf("cap kept the wrong items (want newest):\n%s", evidence)
	}
}

func TestLoopRetroRunsOnlyWhereEnabled(t *testing.T) {
	doc := fmt.Sprintf(`server:
  data_dir: %s
self_project: demo
projects:
  demo:
    repo: bobbyrc/demo
    path: %s
  other:
    repo: bobbyrc/other
    path: %s
`, t.TempDir(), lpPath(t), lpPath(t))
	env := lpNewEngine(t, doc)
	lpRetroTask(t, env, domain.Task{Title: "Broken", Status: domain.TaskFailed, UpdatedAt: env.now})
	lpRetroTask(t, env, domain.Task{Project: "other", Title: "Broken", Status: domain.TaskFailed, UpdatedAt: env.now})

	if err := env.e.runLoopOnce(env.ctx, loopRetro, "other", false); err != nil {
		t.Fatalf("scheduled run for other: %v", err)
	}
	if err := env.e.runLoopOnce(env.ctx, loopRetro, "demo", false); err != nil {
		t.Fatalf("scheduled run for demo: %v", err)
	}

	if n := len(lpRetroTasks(env)); n != 1 {
		t.Fatalf("demo has %d retro tasks, want 1 (other is off by default)", n)
	}
	other, err := env.st.ListTasks(env.ctx, store.TaskFilter{Project: "other", Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range other {
		if task.Source == "retro" {
			t.Fatalf("retro ran for other, which is not the self project: %+v", task)
		}
	}
}

func TestLoopRetroEnabledDefaultsToSelfProjectInListing(t *testing.T) {
	doc := fmt.Sprintf(`server:
  data_dir: %s
self_project: demo
projects:
  demo:
    repo: bobbyrc/demo
    path: %s
  other:
    repo: bobbyrc/other
    path: %s
  forced:
    repo: bobbyrc/forced
    path: %s
    loops:
      retro:
        enabled: true
`, t.TempDir(), lpPath(t), lpPath(t), lpPath(t))
	env := lpNewEngine(t, doc)

	enabled := map[string]bool{}
	for _, r := range env.e.Loops(env.ctx) {
		if r.Name == loopRetro {
			enabled[r.Project] = r.Enabled
			if r.Every != "24h0m0s" {
				t.Errorf("retro/%s every = %q, want 24h0m0s", r.Project, r.Every)
			}
		}
	}
	want := map[string]bool{"demo": true, "other": false, "forced": true}
	for project, on := range want {
		if enabled[project] != on {
			t.Errorf("retro/%s enabled = %v, want %v", project, enabled[project], on)
		}
	}
}

func TestRetroOneLineClipsAndCollapses(t *testing.T) {
	if got := retroOneLine("  a\n\tb  c ", 300); got != "a b c" {
		t.Fatalf("collapsed = %q, want %q", got, "a b c")
	}
	long := strings.Repeat("é", 10)
	if got := retroOneLine(long, 4); got != "éééé..." {
		t.Fatalf("clipped = %q, want four runes and an ellipsis", got)
	}
}

// A retrospective's own failures (the retro task, its follow-ups and their reviews) are not
// evidence, so they cannot make the next day's retrospective due.
func TestLoopRetroExcludesItsOwnWork(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	since := env.now.Add(-24 * time.Hour)
	retro := lpRetroTask(t, env, domain.Task{
		Title: "Retrospective: last day", Kind: domain.KindImprove, Source: "retro",
		Status: domain.TaskBlocked, BlockedReason: "agent denied a tool", UpdatedAt: env.now.Add(-time.Hour),
	})
	followup := lpRetroTask(t, env, domain.Task{
		Title: "Fix the retro prompt", Source: "followup", ParentID: retro.ID,
		Status: domain.TaskFailed, BlockedReason: "tests failed", UpdatedAt: env.now.Add(-time.Hour),
	})
	lpRetroTask(t, env, domain.Task{
		Title: "Review: Fix the retro prompt", Kind: domain.KindReview, Source: "shepherd",
		Status: domain.TaskDone, ParentID: followup.ID, UpdatedAt: env.now.Add(-time.Hour),
	})

	evidence, err := env.e.retroEvidence(env.ctx, "demo", since)
	if err != nil {
		t.Fatalf("retro evidence: %v", err)
	}
	if evidence != "" {
		t.Fatalf("retro's own work is evidence for the next retro:\n%s", evidence)
	}

	// Ordinary work still counts.
	lpRetroTask(t, env, domain.Task{
		Title: "Lint gate", Status: domain.TaskBlocked, BlockedReason: "CI failed: lint",
		UpdatedAt: env.now.Add(-time.Hour),
	})
	evidence, err = env.e.retroEvidence(env.ctx, "demo", since)
	if err != nil {
		t.Fatalf("retro evidence: %v", err)
	}
	if !strings.Contains(evidence, "- Lint gate (id ") || strings.Contains(evidence, "retro prompt") {
		t.Fatalf("evidence = %q, want the ordinary blocked task and not the retro's own work", evidence)
	}
}

// A parent whose review was sent back several times is named once, from its newest review.
func TestLoopRetroReviewFindingsOncePerParent(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	since := env.now.Add(-24 * time.Hour)
	parent := lpRetroTask(t, env, domain.Task{
		Title: "Add cache", Status: domain.TaskQueued,
		Body:      "Build it.\n\nreviewer requested changes: nil check missing\n",
		UpdatedAt: env.now.Add(-3 * time.Hour),
	})
	for i, age := range []time.Duration{5 * time.Hour, 2 * time.Hour, 4 * time.Hour} {
		lpRetroTask(t, env, domain.Task{
			Title: fmt.Sprintf("Review %d: Add cache", i), Kind: domain.KindReview, Status: domain.TaskDone,
			ParentID: parent.ID, UpdatedAt: env.now.Add(-age),
		})
	}

	evidence, err := env.e.retroEvidence(env.ctx, "demo", since)
	if err != nil {
		t.Fatalf("retro evidence: %v", err)
	}
	if n := strings.Count(evidence, "- Add cache (id "+parent.ID+")"); n != 1 {
		t.Fatalf("parent named %d times, want once:\n%s", n, evidence)
	}
}
