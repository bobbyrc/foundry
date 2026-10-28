package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/harness"
)

// logFlushLines and logFlushEvery bound how long a log line waits before it is stored and
// published: whichever comes first.
const (
	logFlushLines = 50
	logFlushEvery = 300 * time.Millisecond
)

// supervise streams a session into the run's log (store + run.log events), enforces the stall
// and denial rules (cancelling the session and recording the reason on ar), waits for the
// session, and stores the run's final state. onEvent (may be nil) sees every event. It returns
// the harness result, the run's final status and the reason ("" for a normal end).
//
// Denials and stalls cancel the run's context (ar.cancel), which kills the session. The reason
// is recorded on ar first, so the status below reflects why the run ended.
func (e *Engine) supervise(ctx context.Context, run domain.Run, sess harness.Session, ar *activeRun, onEvent func(harness.Event)) (harness.Result, domain.RunStatus, string) {
	bg := context.WithoutCancel(ctx)
	limits := e.Config().Limits
	stall := limits.StallTimeout.Duration

	var lastOutput atomic.Int64 // unix nanos of the last event, or the start time
	lastOutput.Store(e.d.Now().UnixNano())
	done := make(chan struct{})

	// Kill the session when the run's context ends (timeout, cancellation, stall, denial).
	go func() {
		select {
		case <-ctx.Done():
			sess.Cancel()
		case <-done:
		}
	}()

	// Stall watchdog: no output for stall means the run is stuck.
	if stall > 0 {
		interval := min(time.Second, stall/4)
		go func() {
			tick := time.NewTicker(interval)
			defer tick.Stop()
			for {
				select {
				case <-done:
					return
				case <-ctx.Done():
					return
				case <-tick.C:
					idle := e.d.Now().Sub(time.Unix(0, lastOutput.Load()))
					if idle > stall {
						ar.reason.CompareAndSwap(nil, fmt.Sprintf("stalled: no output for %s", stall))
						ar.cancel()
						return
					}
				}
			}
		}()
	}

	flush := func(lines []domain.LogLine) {
		if len(lines) == 0 {
			return
		}
		stored, err := e.d.Store.AppendRunLogs(bg, run.ID, lines)
		if err != nil {
			e.d.Logger.Error("store run log", "run", run.ID, "err", err)
			return
		}
		e.d.Bus.Emit(bg, domain.EvRunLog, run.Project, run.TaskID, run.ID, "", stored)
	}

	ticker := time.NewTicker(logFlushEvery)
	defer ticker.Stop()
	events := sess.Events()
	var batch []domain.LogLine
	savedSession := false
	for events != nil {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			now := e.d.Now()
			lastOutput.Store(now.UnixNano())
			if ev.SessionID != "" && !savedSession {
				savedSession = true
				e.updateRun(bg, run.ID, func(r *domain.Run) error {
					r.SessionID = ev.SessionID
					return nil
				})
			}
			if ev.Kind == domain.LogDenial {
				ar.reason.CompareAndSwap(nil, "denied: "+ev.Text)
				ar.cancel()
			}
			at := ev.Time
			if at.IsZero() {
				at = now
			}
			batch = append(batch, domain.LogLine{RunID: run.ID, Time: at, Kind: ev.Kind, Text: ev.Text})
			if onEvent != nil {
				onEvent(ev)
			}
			if len(batch) >= logFlushLines {
				flush(batch)
				batch = nil
			}
		case <-ticker.C:
			flush(batch)
			batch = nil
		}
	}
	flush(batch)

	res, err := sess.Wait()
	close(done)

	reason := reasonOf(ar)
	if reason == "" && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		reason = fmt.Sprintf("timed out after %s", limits.RunTimeout.Duration)
	}

	status := domain.RunSucceeded
	switch {
	case strings.HasPrefix(reason, "denied"):
		status = domain.RunDenied
	case strings.HasPrefix(reason, "stalled"):
		status = domain.RunStalled
	case strings.HasPrefix(reason, "timed out"):
		status = domain.RunTimedOut
	case reason != "":
		status = domain.RunCancelled
	case len(res.Denials) > 0:
		status = domain.RunDenied
		reason = "denied: " + strings.Join(res.Denials, "; ")
	case err != nil || res.IsError:
		status = domain.RunFailed
	}

	var errMsg string
	switch {
	case status != domain.RunFailed:
		errMsg = reason
	case err != nil:
		errMsg = err.Error()
	case res.FinalText != "":
		errMsg = "agent ended with an error: " + lastChars(res.FinalText, 1000)
	default:
		errMsg = fmt.Sprintf("agent exited with code %d", res.ExitCode)
	}

	endedAt := e.d.Now().UTC()
	lastOut := time.Unix(0, lastOutput.Load()).UTC()
	e.updateRun(bg, run.ID, func(r *domain.Run) error {
		r.Status = status
		r.EndedAt = &endedAt
		r.ExitCode = res.ExitCode
		r.CostUSD = res.CostUSD
		r.CostEstimated = false
		if p := e.Config().Harness[run.Harness].Price; res.CostUSD == 0 && p != nil {
			if est := p.Estimate(res.InputTokens, res.CachedInputTokens, res.OutputTokens); est > 0 {
				r.CostUSD, r.CostEstimated = est, true
			}
		}
		r.InputTokens = res.InputTokens
		r.OutputTokens = res.OutputTokens
		if res.SessionID != "" {
			r.SessionID = res.SessionID
		}
		r.Error = errMsg
		r.LastOutputAt = lastOut
		return nil
	})
	return res, status, reason
}
