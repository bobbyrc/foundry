package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/notify"
	"github.com/bobbyrc/foundry/internal/store"
)

// prodRegressionGrace is how long after a task finished a signal must still be seen before it
// counts as a regression rather than the tail of the old problem.
const prodRegressionGrace = 10 * time.Minute

// prodLookback is how far back the first poll looks.
const prodLookback = 24 * time.Hour

// signalLevelRank orders error levels: debug and info 0, warning 1, error 2, fatal 3. Unknown
// levels count as error, so an unfamiliar level is not silently ignored.
func signalLevelRank(level string) int {
	switch level {
	case "debug", "info":
		return 0
	case "warning":
		return 1
	case "error":
		return 2
	case "fatal":
		return 3
	default:
		return 2
	}
}

// incidentFinished reports whether an incident task is over. A blocked incident is still open:
// a human has to act on it, so a new signal must not open a second task for it.
func incidentFinished(s domain.TaskStatus) bool {
	return s == domain.TaskDone || s == domain.TaskCancelled || s == domain.TaskFailed
}

// prodLoop polls every observer for the project and opens an incident task for each signal
// that reaches the threshold. The cursor advances only when every observer answered, so a
// failing observer does not lose its window.
func (e *Engine) prodLoop(ctx context.Context, p *config.Project) error {
	if e.d.ObserversFor == nil {
		return nil
	}
	observers, err := e.d.ObserversFor(p)
	if err != nil {
		return fmt.Errorf("prod watch: observers: %w", err)
	}
	if len(observers) == 0 {
		return nil
	}

	pollStart := e.d.Now().UTC()
	st, err := e.d.Store.GetLoopState(ctx, loopProdWatch, p.Name)
	if err != nil {
		return fmt.Errorf("prod watch: state: %w", err)
	}
	since := pollStart.Add(-prodLookback)
	if st.Cursor != "" {
		if t, err := time.Parse(time.RFC3339, st.Cursor); err == nil {
			since = t
		}
	}

	settings := e.Config().LoopsFor(p.Name).ProdWatch
	minRank := signalLevelRank(settings.MinLevel)

	names := make([]string, 0, len(observers))
	for name := range observers {
		names = append(names, name)
	}
	sort.Strings(names)

	var errs []error
	for _, name := range names {
		observations, err := observers[name].Poll(ctx, since)
		if err != nil {
			errs = append(errs, fmt.Errorf("observer %s: %w", name, err))
			continue
		}
		for _, o := range observations {
			if err := e.handleObservation(ctx, p, name, o, minRank, settings.MinCount); err != nil {
				errs = append(errs, fmt.Errorf("observer %s: %w", name, err))
			}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	st, err = e.d.Store.GetLoopState(ctx, loopProdWatch, p.Name)
	if err != nil {
		return fmt.Errorf("prod watch: state: %w", err)
	}
	st.Cursor = pollStart.Format(time.RFC3339)
	return e.d.Store.SaveLoopState(ctx, st)
}

// handleObservation records one observation as a signal and opens an incident task when the
// signal qualifies and has no open task. A task that finished before the signal was last seen
// is reopened as a regression.
func (e *Engine) handleObservation(ctx context.Context, p *config.Project, source string, o domain.SignalObservation, minRank int, minCount int64) error {
	s, isNew, err := e.d.Store.UpsertSignal(ctx, domain.Signal{Project: p.Name, Source: source, SignalObservation: o})
	if err != nil {
		return err
	}
	if isNew {
		e.d.Bus.Emit(ctx, domain.EvSignalNew, p.Name, "", "", "new signal: "+s.Title, s)
	}
	if signalLevelRank(s.Level) < minRank || s.Count < minCount {
		return nil
	}

	ref := "signal:" + source + ":" + s.Fingerprint
	title := "Incident: " + s.Title
	existing, err := e.d.Store.FindTaskBySourceRef(ctx, p.Name, ref)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// No task yet: open one below.
	case err != nil:
		return err
	case !incidentFinished(existing.Status):
		return nil
	case s.LastSeen.After(existing.UpdatedAt.Add(prodRegressionGrace)):
		title = "Regressed: " + title
	default:
		return nil
	}

	t, err := e.createTask(ctx, domain.Task{
		Project: p.Name,
		Kind:    domain.KindIncident,
		Title:   title,
		Body: fmt.Sprintf("Level: %s\nCount: %d\nCulprit: %s\nURL: %s\nFirst seen: %s\nLast seen: %s\nRelease: %s\n",
			s.Level, s.Count, s.Culprit, s.URL, s.FirstSeen.Format(time.RFC3339), s.LastSeen.Format(time.RFC3339), s.Release),
		Source:    "signal",
		SourceRef: ref,
		Priority:  10,
	})
	if err != nil {
		return err
	}
	if _, err := e.d.Store.UpdateSignal(ctx, s.ID, func(x *domain.Signal) error {
		x.TaskID = t.ID
		return nil
	}); err != nil {
		return err
	}
	e.notify(ctx, notify.PriorityNormal, "Production issue: "+s.Title, s.URL, "/tasks/"+t.ID)
	return nil
}
