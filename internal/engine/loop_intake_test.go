package engine

import (
	"strings"
	"testing"

	"github.com/bobbyrc/foundry/internal/domain"
)

func TestLoopIntakeQueuesEachIssueOnce(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	issue := env.fake.AddIssue("Crash on start", "stack trace here", "foundry")
	env.fake.AddIssue("Unlabelled", "not for foundry", "bug")

	env.run(loopIssueIntake)

	tasks := env.tasks("")
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1 (only the labelled issue)", len(tasks))
	}
	got := tasks[0]
	if got.Kind != domain.KindIssue || got.Source != "github_issue" || got.SourceRef != "github_issue:1" {
		t.Fatalf("task = kind %s source %s ref %s, want issue / github_issue / github_issue:1", got.Kind, got.Source, got.SourceRef)
	}
	if got.Title != "Crash on start" || !strings.Contains(got.Body, "stack trace here") ||
		!strings.Contains(got.Body, "GitHub issue #1: "+issue.URL) {
		t.Fatalf("task body/title not carried over: %q / %q", got.Title, got.Body)
	}
	if n := len(env.fake.Comments[issue.Number]); n != 1 {
		t.Fatalf("got %d comments on the issue, want 1", n)
	}
	if !strings.Contains(env.fake.Comments[issue.Number][0], got.ID) {
		t.Fatalf("comment does not name task %s: %q", got.ID, env.fake.Comments[issue.Number][0])
	}

	env.run(loopIssueIntake)
	if n := len(env.tasks("")); n != 1 {
		t.Fatalf("second pass made %d tasks, want still 1", n)
	}
	if n := len(env.fake.Comments[issue.Number]); n != 1 {
		t.Fatalf("second pass commented again: %d comments", n)
	}
}
