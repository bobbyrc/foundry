package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
)

func lpObservation(level string, count int64, lastSeen time.Time) domain.SignalObservation {
	return domain.SignalObservation{
		Fingerprint: "fp1",
		Title:       "TypeError: boom",
		Level:       level,
		Count:       count,
		URL:         "https://sentry.example/1",
		FirstSeen:   lastSeen.Add(-time.Hour),
		LastSeen:    lastSeen,
	}
}

func TestLoopProdOpensOneIncidentPerSignal(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	env.obs.obs = []domain.SignalObservation{lpObservation("error", 5, time.Now())}

	env.run(loopProdWatch)
	incidents := env.tasks(domain.KindIncident)
	if len(incidents) != 1 {
		t.Fatalf("got %d incidents, want 1", len(incidents))
	}
	inc := incidents[0]
	if inc.Title != "Incident: TypeError: boom" || inc.SourceRef != "signal:sentry:fp1" || inc.Priority != 10 {
		t.Fatalf("incident = %q ref %q priority %d", inc.Title, inc.SourceRef, inc.Priority)
	}
	sigs, err := env.st.ListSignals(env.ctx, "demo", 10)
	if err != nil || len(sigs) != 1 || sigs[0].TaskID != inc.ID {
		t.Fatalf("signals = %+v (err %v), want the signal linked to incident %s", sigs, err, inc.ID)
	}

	env.run(loopProdWatch) // same observation again
	if n := len(env.tasks(domain.KindIncident)); n != 1 {
		t.Fatalf("repeat poll made %d incidents, want still 1", n)
	}
}

func TestLoopProdIgnoresBelowThreshold(t *testing.T) {
	doc := lpDoc(t, lpPath(t), "", "loops:\n  prod_watch:\n    min_count: 10\n")
	env := lpNewEngine(t, doc)

	env.obs.obs = []domain.SignalObservation{lpObservation("warning", 50, time.Now())}
	env.run(loopProdWatch)
	env.obs.obs = []domain.SignalObservation{lpObservation("error", 5, time.Now())}
	env.run(loopProdWatch)

	if n := len(env.tasks(domain.KindIncident)); n != 0 {
		t.Fatalf("got %d incidents below threshold, want 0", n)
	}
	sigs, err := env.st.ListSignals(env.ctx, "demo", 10)
	if err != nil || len(sigs) != 1 {
		t.Fatalf("signals = %d (err %v), want the error recorded but not opened", len(sigs), err)
	}
}

func TestLoopProdReopensAsRegression(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	env.obs.obs = []domain.SignalObservation{lpObservation("error", 5, time.Now())}
	env.run(loopProdWatch)
	first := env.tasks(domain.KindIncident)[0]
	if _, err := env.e.setTask(env.ctx, first.ID, "fixed", func(x *domain.Task) error {
		x.Status = domain.TaskDone
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Still firing before the fix was merged plus grace: no new task.
	env.obs.obs = []domain.SignalObservation{lpObservation("error", 6, time.Now().Add(-time.Hour))}
	env.run(loopProdWatch)
	if n := len(env.tasks(domain.KindIncident)); n != 1 {
		t.Fatalf("old tail reopened the incident: %d incidents", n)
	}

	// Seen again well after the task finished: regression.
	env.obs.obs = []domain.SignalObservation{lpObservation("error", 7, time.Now().Add(time.Hour))}
	env.run(loopProdWatch)
	incidents := env.tasks(domain.KindIncident)
	if len(incidents) != 2 {
		t.Fatalf("got %d incidents after a regression, want 2", len(incidents))
	}
	if !strings.HasPrefix(incidents[0].Title, "Regressed: ") {
		t.Fatalf("newest incident title = %q, want a Regressed: prefix", incidents[0].Title)
	}
}

func TestLoopProdCursorAdvances(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	env.run(loopProdWatch)
	st, err := env.st.GetLoopState(env.ctx, loopProdWatch, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if st.Cursor != env.now.Format(time.RFC3339) {
		t.Fatalf("cursor = %q, want %q", st.Cursor, env.now.Format(time.RFC3339))
	}
}

// A blocked incident is still open: a later signal must not open a second task for it.
func TestLoopProdBlockedIncidentStaysOpen(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	env.obs.obs = []domain.SignalObservation{lpObservation("error", 5, time.Now())}
	env.run(loopProdWatch)
	inc := env.tasks(domain.KindIncident)[0]
	lpSetTask(t, env, inc.ID, func(x *domain.Task) { x.Status = domain.TaskBlocked })

	env.obs.obs = []domain.SignalObservation{lpObservation("error", 7, time.Now().Add(time.Hour))}
	env.run(loopProdWatch)

	if n := len(env.tasks(domain.KindIncident)); n != 1 {
		t.Fatalf("got %d incidents while the first is blocked, want 1", n)
	}
}
