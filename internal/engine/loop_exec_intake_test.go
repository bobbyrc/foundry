package engine

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
)

// lpIntakeScript writes a shell script with body into a temp dir and returns the command that runs
// it.
func lpIntakeScript(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	lpWrite(t, dir, "intake.sh", "#!/bin/sh\n"+body)
	return "sh " + filepath.Join(dir, "intake.sh")
}

// lpIntakeSource renders one entry of a project's intake list. kind and timeout are optional.
func lpIntakeSource(name, command, kind, timeout string) string {
	s := fmt.Sprintf("      - name: %s\n        command: %q\n", name, command)
	if kind != "" {
		s += "        kind: " + kind + "\n"
	}
	if timeout != "" {
		s += "        timeout: " + timeout + "\n"
	}
	return s
}

// lpIntakeDoc renders a config for project demo with the given intake entries.
func lpIntakeDoc(t *testing.T, entries ...string) string {
	t.Helper()
	return lpDoc(t, lpPath(t), "    intake:\n"+strings.Join(entries, ""), "")
}

func TestExecIntakeCreatesTasksOncePerID(t *testing.T) {
	cmd := lpIntakeScript(t, `
echo '{"id":"a","title":"First","body":"details"}'
echo 'not json'
echo '{"id":"b","title":"Second","kind":"bugfix","priority":2,"role":"implementer"}'
echo '{"id":"a","title":"First again"}'
`)
	env := lpNewEngine(t, lpIntakeDoc(t, lpIntakeSource("inbox", cmd, "", "")))

	env.run(loopExecIntake)

	tasks := env.tasks("")
	if len(tasks) != 2 {
		t.Fatalf("first pass: got %d tasks, want 2 (invalid line and duplicate id skipped)", len(tasks))
	}
	byRef := map[string]domain.Task{}
	for _, tk := range tasks {
		byRef[tk.SourceRef] = tk
	}
	a, ok := byRef["intake:inbox:a"]
	if !ok {
		t.Fatalf("no task for intake:inbox:a; refs = %v", byRef)
	}
	if a.Title != "First" || a.Source != "intake" || a.Kind != domain.KindFeature {
		t.Fatalf("task a = title %q source %q kind %s, want First / intake / feature", a.Title, a.Source, a.Kind)
	}
	if !strings.Contains(a.Body, "details") || !strings.Contains(a.Body, "From intake source `inbox` (id `a`).") {
		t.Fatalf("task a body = %q, want the item body and the source footer", a.Body)
	}
	b, ok := byRef["intake:inbox:b"]
	if !ok {
		t.Fatalf("no task for intake:inbox:b; refs = %v", byRef)
	}
	if b.Kind != domain.KindBugfix || b.Priority != 2 || b.Role != "implementer" {
		t.Fatalf("task b = kind %s priority %d role %s, want bugfix / 2 / implementer", b.Kind, b.Priority, b.Role)
	}

	env.run(loopExecIntake)
	if n := len(env.tasks("")); n != 2 {
		t.Fatalf("second pass: got %d tasks, want still 2", n)
	}
}

func TestExecIntakeKindFromLineIsAllowlisted(t *testing.T) {
	cmd := lpIntakeScript(t, `
echo '{"id":"r","title":"Review it","kind":"review"}'
echo '{"id":"i","title":"Incident","kind":"incident"}'
echo '{"id":"x","title":"Issue kind","kind":"issue"}'
echo '{"id":"q","title":"Question","kind":"question"}'
`)
	env := lpNewEngine(t, lpIntakeDoc(t, lpIntakeSource("inbox", cmd, "chore", "")))

	env.run(loopExecIntake)

	want := map[string]domain.TaskKind{
		"intake:inbox:r": domain.KindChore,
		"intake:inbox:i": domain.KindChore,
		"intake:inbox:x": domain.KindChore,
		"intake:inbox:q": domain.KindQuestion,
	}
	got := map[string]domain.TaskKind{}
	for _, tk := range env.tasks("") {
		got[tk.SourceRef] = tk.Kind
	}
	for ref, kind := range want {
		if got[ref] != kind {
			t.Errorf("%s: kind = %q, want %q", ref, got[ref], kind)
		}
	}
}

func TestExecIntakeFailingSourceDoesNotStopOthers(t *testing.T) {
	bad := lpIntakeScript(t, `
echo '{"id":"x","title":"never queued"}'
echo boom >&2
exit 3
`)
	good := lpIntakeScript(t, `echo '{"id":"ok","title":"Good item"}'`)
	env := lpNewEngine(t, lpIntakeDoc(t,
		lpIntakeSource("bad", bad, "", ""),
		lpIntakeSource("good", good, "", ""),
	))

	err := env.e.RunLoop(env.ctx, loopExecIntake, "demo")
	if err == nil {
		t.Fatal("RunLoop returned nil, want the failing source reported")
	}
	if !strings.Contains(err.Error(), "intake bad") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %q, want it to name source bad and its stderr", err)
	}

	tasks := env.tasks("")
	if len(tasks) != 1 || tasks[0].SourceRef != "intake:good:ok" {
		t.Fatalf("tasks = %+v, want only the good source's item", tasks)
	}
}

func TestExecIntakeTimeoutKillsCommand(t *testing.T) {
	env := lpNewEngine(t, lpIntakeDoc(t, lpIntakeSource("slow", "sleep 30 & sleep 30", "", "1s")))

	start := time.Now()
	err := env.e.RunLoop(env.ctx, loopExecIntake, "demo")
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "timed out after 1s") {
		t.Fatalf("RunLoop error = %v, want a timeout error", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("RunLoop took %s, want the 1s timeout to kill the command", elapsed)
	}
}

func TestExecIntakeOutputOverCapIsAnErrorAndKeepsCompleteLines(t *testing.T) {
	// The third line is longer than the cap, so the output is cut in the middle of it.
	cmd := lpIntakeScript(t, `
echo '{"id":"a","title":"First"}'
echo '{"id":"b","title":"Second"}'
echo '{"id":"big","title":"'
head -c 1100000 /dev/zero | tr '\0' 'x'
echo '"}'
`)
	env := lpNewEngine(t, lpIntakeDoc(t, lpIntakeSource("inbox", cmd, "", "")))

	err := env.e.RunLoop(env.ctx, loopExecIntake, "demo")
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("RunLoop error = %v, want an over-cap error", err)
	}

	refs := map[string]bool{}
	for _, tk := range env.tasks("") {
		refs[tk.SourceRef] = true
	}
	if len(refs) != 2 || !refs["intake:inbox:a"] || !refs["intake:inbox:b"] {
		t.Fatalf("task refs = %v, want the two complete lines before the cap", refs)
	}
}
