package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A manual loop run returns the error redacted, because the API caller (the orchestrator agent)
// receives its text. The original error stays reachable through errors.Is.
func TestManualLoopErrorReturnedRedacted(t *testing.T) {
	const secret = "sntrys_secret_manual_7788"
	doc := lpDoc(t, lpPath(t), "    env:\n      SENTRY_AUTH_TOKEN: "+secret+"\n", "")
	env := lpNewEngine(t, doc)
	sentinel := errors.New("fresh sentinel for the manual-run test")
	env.obs.err = fmt.Errorf("sentry refused token %s: %w", secret, sentinel)

	err := env.e.RunLoop(env.ctx, loopProdWatch, "demo")
	if err == nil {
		t.Fatal("RunLoop returned nil, want the observer error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("returned error leaks the secret: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "***") {
		t.Fatalf("returned error = %q, want the secret replaced by ***", err.Error())
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is lost the original sentinel through the redacted error: %v", err)
	}
}
