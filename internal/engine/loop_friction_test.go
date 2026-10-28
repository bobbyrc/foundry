package engine

import (
	"testing"

	"github.com/bobbyrc/foundry/internal/domain"
)

func lpFriction(t *testing.T, env *lpEnv, title string) domain.Friction {
	t.Helper()
	f, _, err := env.st.UpsertFriction(env.ctx, domain.Friction{
		Project: "demo", RunID: "run-1", Title: title, Body: "The docs are missing.",
		Fingerprint: "fp-" + title, Count: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLoopFrictionFilesOnceAndLinks(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	f := lpFriction(t, env, "Docs for the fake")

	env.run(loopFriction)
	if n := len(env.fake.Issues); n != 1 {
		t.Fatalf("got %d issues, want 1", n)
	}
	issue := env.fake.Issues[1]
	if len(issue.Labels) != 1 || issue.Labels[0] != "friction" {
		t.Fatalf("labels = %v, want [friction]", issue.Labels)
	}
	got, err := env.st.ListFriction(env.ctx, "demo", 10)
	if err != nil || len(got) != 1 || got[0].ID != f.ID || got[0].IssueURL != issue.URL {
		t.Fatalf("friction = %+v (err %v), want IssueURL %q", got, err, issue.URL)
	}

	env.run(loopFriction)
	if n := len(env.fake.Issues); n != 1 {
		t.Fatalf("second run filed again: %d issues", n)
	}
}

func TestLoopFrictionLinksExistingIssueWithSameTitle(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	existing := env.fake.AddIssue("Existing report", "already here")
	lpFriction(t, env, "existing report")

	env.run(loopFriction)

	if n := len(env.fake.Issues); n != 1 {
		t.Fatalf("got %d issues, want the existing one only", n)
	}
	got, err := env.st.ListFriction(env.ctx, "demo", 10)
	if err != nil || len(got) != 1 || got[0].IssueURL != existing.URL {
		t.Fatalf("friction = %+v (err %v), want linked to %s", got, err, existing.URL)
	}
}

func TestLoopFrictionRespectsAutoFileOff(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", "loops:\n  friction:\n    auto_file: false\n"))
	lpFriction(t, env, "Docs for the fake")

	env.run(loopFriction)

	if n := len(env.fake.Issues); n != 0 {
		t.Fatalf("auto_file false still filed %d issues", n)
	}
}
