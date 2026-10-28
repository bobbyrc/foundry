package engine

import "testing"

func TestRedactSecretsRedactsURLPasswordOnItsOwn(t *testing.T) {
	env := []string{"DATABASE_URL=postgres://u:pa55word@db/app"}
	if got := redactSecrets("auth failed for pa55word", env); got != "auth failed for ***" {
		t.Fatalf("redactSecrets = %q, want the bare password replaced", got)
	}
}

func TestRedactSecretsRedactsQueryEscapedURLPassword(t *testing.T) {
	// The password decodes to "a+b&c=d"; its query-escaped form is what a tool may print.
	env := []string{"DATABASE_URL=postgres://u:a%2Bb%26c%3Dd@db/app"}
	if got := redactSecrets("dsn a+b&c=d and a%2Bb%26c%3Dd", env); got != "dsn *** and ***" {
		t.Fatalf("redactSecrets = %q, want both forms replaced", got)
	}
}

func TestRedactSecretsLeavesShortURLPasswordAlone(t *testing.T) {
	// "pass1" is 5 bytes, below the 6-byte password minimum: only the whole URL is replaced.
	env := []string{"DATABASE_URL=postgres://u:pass1@db/app"}
	if got := redactSecrets("user pass1 at postgres://u:pass1@db/app", env); got != "user pass1 at ***" {
		t.Fatalf("redactSecrets = %q, want only the URL replaced", got)
	}
}
