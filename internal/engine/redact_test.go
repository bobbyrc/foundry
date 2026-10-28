package engine

import "testing"

func TestRedactSecretsReplacesOnlySecretValues(t *testing.T) {
	env := []string{"GH_TOKEN=ghp_abcdefghijkl", "DB_PASSWORD=hunter2hunter2", "SHORT_KEY=abc", "HOME=/Users/x"}
	in := "gh ghp_abcdefghijkl, db hunter2hunter2, short abc, home /Users/x"
	want := "gh ***, db ***, short abc, home /Users/x"
	if got := redactSecrets(in, env); got != want {
		t.Fatalf("redactSecrets = %q, want %q", got, want)
	}
}

func TestRedactSecretsLongestValueFirst(t *testing.T) {
	env := []string{"A_TOKEN=secretvalue1", "B_TOKEN=secretvalue1-extended"}
	if got := redactSecrets("secretvalue1-extended", env); got != "***" {
		t.Fatalf("redactSecrets = %q, want the whole value replaced", got)
	}
}

func TestRedactSecretsMatchesWholeNameSegments(t *testing.T) {
	env := []string{
		"GIT_AUTHOR_NAME=Someone Longname",
		"MONKEY_BARS=bananabanana",
		"DATABASE_URL=postgres://u:pw123456@db.local/app",
		"OPENAI_API_KEY=sk-1234567890",
	}
	in := "Someone Longname bananabanana postgres://u:pw123456@db.local/app sk-1234567890"
	want := "Someone Longname bananabanana *** ***"
	if got := redactSecrets(in, env); got != want {
		t.Fatalf("redactSecrets = %q, want %q", got, want)
	}
}
