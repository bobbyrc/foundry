package engine

import (
	"testing"
)

func TestLoopFrictionFiledIsOpen(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	lpFriction(t, env, "Docs for the fake")

	env.run(loopFriction)

	got, err := env.st.ListFriction(env.ctx, "demo", 10)
	if err != nil || len(got) != 1 || got[0].IssueState != "open" {
		t.Fatalf("friction = %+v (err %v), want IssueState open", got, err)
	}
}

func TestLoopFrictionStateFollowsClosedIssue(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	lpFriction(t, env, "Docs for the fake")
	env.run(loopFriction)

	issue := env.fake.Issues[1]
	if err := env.fake.CloseIssue(env.ctx, issue.Number, "fixed"); err != nil {
		t.Fatal(err)
	}
	env.run(loopFriction)

	got, err := env.st.ListFriction(env.ctx, "demo", 10)
	if err != nil || len(got) != 1 || got[0].IssueState != "closed" {
		t.Fatalf("friction = %+v (err %v), want IssueState closed", got, err)
	}
	if n := len(env.fake.Issues); n != 1 {
		t.Fatalf("second run filed again: %d issues", n)
	}
}

func TestLoopFrictionLinkedToClosedIssueIsClosed(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	existing := env.fake.AddIssue("Existing report", "already fixed", "friction")
	if err := env.fake.CloseIssue(env.ctx, existing.Number, "fixed"); err != nil {
		t.Fatal(err)
	}
	lpFriction(t, env, "existing report")

	env.run(loopFriction)

	got, err := env.st.ListFriction(env.ctx, "demo", 10)
	if err != nil || len(got) != 1 || got[0].IssueURL != existing.URL || got[0].IssueState != "closed" {
		t.Fatalf("friction = %+v (err %v), want linked to %s and closed", got, err, existing.URL)
	}
}
