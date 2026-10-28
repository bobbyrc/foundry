package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
)

// An observer signal whose title, culprit or URL carries a project secret becomes an incident and
// a stored signal without it.
func TestLoopProdSignalTextRedactedBeforeIncident(t *testing.T) {
	const secret = "sntrys_secret_0123456"
	doc := lpDoc(t, lpPath(t), "    env:\n      SENTRY_AUTH_TOKEN: "+secret+"\n", "")
	env := lpNewEngine(t, doc)

	o := lpObservation("error", 5, time.Now())
	o.Title = "TypeError: bad token " + secret
	o.Culprit = "client.go " + secret
	o.URL = "https://sentry.example/1?key=" + secret
	env.obs.obs = []domain.SignalObservation{o}
	env.run(loopProdWatch)

	incidents := env.tasks(domain.KindIncident)
	if len(incidents) != 1 {
		t.Fatalf("got %d incidents, want 1", len(incidents))
	}
	inc := incidents[0]
	if strings.Contains(inc.Title, secret) || !strings.Contains(inc.Title, "***") {
		t.Fatalf("incident title = %q, want the secret replaced", inc.Title)
	}
	if strings.Contains(inc.Body, secret) {
		t.Fatalf("incident body leaks the secret:\n%s", inc.Body)
	}

	sigs, err := env.st.ListSignals(env.ctx, "demo", 10)
	if err != nil || len(sigs) != 1 {
		t.Fatalf("signals = %d (err %v), want 1", len(sigs), err)
	}
	if s := sigs[0]; strings.Contains(s.Title, secret) || strings.Contains(s.Culprit, secret) || strings.Contains(s.URL, secret) {
		t.Fatalf("stored signal leaks the secret: %+v", s)
	}
	if sigs[0].Fingerprint != "fp1" {
		t.Fatalf("fingerprint = %q, want it unchanged", sigs[0].Fingerprint)
	}
}
