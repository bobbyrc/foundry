package engine

import (
	"testing"

	"github.com/bobbyrc/onlet/internal/domain"
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

const (
	lpHTTPFirst  = "Worker sandbox prevents Go HTTP test listeners"
	lpHTTPSecond = "Worker sandbox denies HTTP test listening sockets"
	lpGoFirst    = "Sandbox blocks required Go verification"
	lpGoSecond   = "Go build module cache is outside the worker writable sandbox"
	lpUnrelated  = "Missing docs for the deploy command"
)

func TestFrictionSimilarityExamples(t *testing.T) {
	score := func(a, b string) float64 {
		return frictionSimilarity(frictionTokens(a), frictionTokens(b))
	}
	t.Logf("HTTP listener pair: %.3f", score(lpHTTPFirst, lpHTTPSecond))
	t.Logf("Go verification pair: %.3f", score(lpGoFirst, lpGoSecond))
	t.Logf("HTTP vs unrelated: %.3f", score(lpHTTPFirst, lpUnrelated))

	if s := score(lpHTTPFirst, lpHTTPSecond); s < frictionMatchThreshold {
		t.Errorf("HTTP listener pair scores %.3f, want >= %.2f", s, frictionMatchThreshold)
	}
	if s := score(lpHTTPFirst, lpUnrelated); s >= frictionMatchThreshold {
		t.Errorf("unrelated title scores %.3f, want < %.2f", s, frictionMatchThreshold)
	}
	if got := frictionTokens("The sandbox is a Go test listener for it"); len(got) != 3 {
		t.Errorf("tokens = %v, want sandbox, test and listen-stem only", got)
	}
}

func TestLoopFrictionCommentsOnSimilarOpenIssue(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	existing := env.fake.AddIssue(lpHTTPSecond, "original report", "friction")
	f := lpFriction(t, env, lpHTTPFirst)

	env.run(loopFriction)

	if n := len(env.fake.Issues); n != 1 {
		t.Fatalf("got %d issues, want the existing one only", n)
	}
	comments := env.fake.Comments[existing.Number]
	want := "Reported again by run `run-1`: " + lpHTTPFirst + "\n\nThe docs are missing."
	if len(comments) != 1 || comments[0] != want {
		t.Fatalf("comments = %q, want [%q]", comments, want)
	}
	got, err := env.st.ListFriction(env.ctx, "demo", 10)
	if err != nil || len(got) != 1 || got[0].ID != f.ID || got[0].IssueURL != existing.URL {
		t.Fatalf("friction = %+v (err %v), want linked to %s", got, err, existing.URL)
	}
}

func TestLoopFrictionFilesNewIssueForUnrelatedTitle(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	existing := env.fake.AddIssue(lpHTTPSecond, "original report", "friction")
	lpFriction(t, env, lpUnrelated)

	env.run(loopFriction)

	if n := len(env.fake.Issues); n != 2 {
		t.Fatalf("got %d issues, want a new one beside the existing", n)
	}
	if len(env.fake.Comments[existing.Number]) != 0 {
		t.Fatalf("unrelated friction commented on %d", existing.Number)
	}
}
