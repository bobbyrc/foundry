package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/bobbyrc/onlet/internal/api"
	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/store"
)

// A loop error that echoes a project secret is stored and emitted redacted.
func TestLoopErrorStoredRedacted(t *testing.T) {
	const secret = "sntrys_secret_0123456"
	doc := lpDoc(t, lpPath(t), "    env:\n      SENTRY_AUTH_TOKEN: "+secret+"\n", "")
	env := lpNewEngine(t, doc)
	env.obs.err = errors.New("sentry is down, request with token " + secret + " refused")

	if err := env.e.RunLoop(env.ctx, loopProdWatch, "demo"); err == nil {
		t.Fatal("RunLoop returned nil, want the observer error")
	}

	var row api.LoopView
	for _, r := range env.e.Loops(env.ctx) {
		if r.Name == loopProdWatch && r.Project == "demo" {
			row = r
		}
	}
	if row.LastError == "" {
		t.Fatal("prod_watch row has no LastError")
	}
	if strings.Contains(row.LastError, secret) || !strings.Contains(row.LastError, "***") {
		t.Fatalf("LastError = %q, want the secret replaced", row.LastError)
	}

	evs, err := env.st.ListEvents(env.ctx, store.EventFilter{Project: "demo", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range evs {
		if ev.Type != domain.EvLoopError {
			continue
		}
		found = true
		if strings.Contains(ev.Message, secret) {
			t.Fatalf("loop.error event leaks the secret: %q", ev.Message)
		}
	}
	if !found {
		t.Fatal("no loop.error event was emitted")
	}
}

// The service logs tail is redacted, and a secret cut by the tail's front is not kept as a fragment.
func TestTailBufferDropsPartialFirstLine(t *testing.T) {
	b := &tailBuffer{max: 10}
	_, _ = b.Write([]byte("line1\nline2\nline3\nline4\n"))
	if got := b.String(); got != "[…]\nline4\n" {
		t.Fatalf("tailBuffer = %q, want the partial first line dropped and marked", got)
	}

	whole := &tailBuffer{max: 100}
	_, _ = whole.Write([]byte("a\nb\n"))
	if got := whole.String(); got != "a\nb\n" {
		t.Fatalf("tailBuffer without truncation = %q, want unchanged", got)
	}
}
