// Package fake is a deterministic harness for engine tests. It runs a Script in-process instead of
// starting a CLI. Registered as type "fake", it uses DefaultScript, whose behaviour is chosen by
// markers in the prompt.
package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/harness"
)

func init() {
	harness.Register("fake", func(cfg harness.Config) (harness.Harness, error) {
		return New(DefaultScript), nil
	})
}

// Script stands in for an agent run. It emits events, may write files into spec.Workdir, and
// returns its result. It must return once ctx is done.
type Script func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error)

// New returns a harness whose sessions run s.
func New(s Script) harness.Harness {
	return &runner{script: s}
}

type runner struct {
	script Script
}

// Start runs the script in a goroutine. Events is closed when the script returns.
func (r *runner) Start(ctx context.Context, spec harness.Spec) (harness.Session, error) {
	sctx, cancel := context.WithCancel(ctx)
	s := &session{
		ctx:    sctx,
		cancel: cancel,
		events: make(chan harness.Event, 1024),
		done:   make(chan struct{}),
	}
	go func() {
		defer close(s.done)
		res, err := r.script(sctx, spec, s.emit)
		close(s.events)
		if sctx.Err() != nil || err != nil {
			res.IsError = true
		}
		s.res, s.err = res, err
	}()
	return s, nil
}

type session struct {
	ctx    context.Context
	cancel context.CancelFunc
	events chan harness.Event
	done   chan struct{}
	res    harness.Result
	err    error
}

// emit delivers an event unless the session was cancelled. The script must not be blocked by a
// consumer that has stopped reading.
func (s *session) emit(ev harness.Event) {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	select {
	case s.events <- ev:
	case <-s.ctx.Done():
	}
}

func (s *session) Events() <-chan harness.Event {
	return s.events
}

// Wait returns the script's result once it has returned. It is IsError when the session was
// cancelled.
func (s *session) Wait() (harness.Result, error) {
	<-s.done
	return s.res, s.err
}

func (s *session) Cancel() {
	s.cancel()
}

// DefaultScript chooses its behaviour from markers in spec.Prompt, checked in this order:
//
//	[fake:stall]        emit nothing and block until cancelled
//	[fake:deny]         report a denied tool call and fail
//	[fake:fail]         write a failed result.json
//	[fake:block]        write a blocked result.json
//	FOUNDRY_ROLE=reviewer  write a review verdict; [fake:reject] rejects it
//	[fake:commit]       commit a new file on the branch and write a completed result.json;
//	                    [fake:risk-high] rates the change high risk
//	(none of the above) write a no_changes result.json
//
// Any result.json is written to Workdir/.foundry-run/result.json.
func DefaultScript(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
	prompt := spec.Prompt
	sessionID := "fake-" + spec.RunID
	first := true
	say := func(kind domain.LogKind, text string) {
		ev := harness.Event{Kind: kind, Text: text}
		if first {
			ev.SessionID = sessionID
			first = false
		}
		emit(ev)
	}

	switch {
	case strings.Contains(prompt, "[fake:stall]"):
		<-ctx.Done()
		return harness.Result{SessionID: sessionID, IsError: true}, nil

	case strings.Contains(prompt, "[fake:deny]"):
		const denial = "Bash: rm -rf /"
		say(domain.LogDenial, denial)
		return harness.Result{SessionID: sessionID, Denials: []string{denial}, IsError: true}, nil

	case strings.Contains(prompt, "[fake:fail]"):
		rr := domain.RunResult{Status: domain.ResultFailed, Summary: "fake failure"}
		return report(spec, sessionID, say, rr, "wrote failed result")

	case strings.Contains(prompt, "[fake:block]"):
		rr := domain.RunResult{Status: domain.ResultBlocked, BlockedReason: "fake needs a human"}
		return report(spec, sessionID, say, rr, "wrote blocked result")

	case spec.Env["FOUNDRY_ROLE"] == "reviewer":
		reject := strings.Contains(prompt, "[fake:reject]")
		verdict := &domain.ReviewVerdict{Approve: !reject}
		if reject {
			verdict.Findings = []domain.ReviewFinding{{Severity: "blocker", Message: "fake finding"}}
		}
		rr := domain.RunResult{Status: domain.ResultCompleted, Summary: "fake review", Review: verdict}
		return report(spec, sessionID, say, rr, "wrote review verdict")

	case strings.Contains(prompt, "[fake:commit]"):
		risk := domain.RiskLow
		if strings.Contains(prompt, "[fake:risk-high]") {
			risk = domain.RiskHigh
		}
		file := fmt.Sprintf("fake-%s.txt", spec.RunID)
		firstLine := strings.SplitN(prompt, "\n", 2)[0]
		if err := commitFile(ctx, spec.Workdir, file, firstLine+"\n"); err != nil {
			return harness.Result{SessionID: sessionID, IsError: true}, err
		}
		rr := domain.RunResult{
			Status:  domain.ResultCompleted,
			Summary: "fake change",
			Risk:    risk,
			PRTitle: "Fake change",
			PRBody:  "Made by the fake harness.",
		}
		return report(spec, sessionID, say, rr, "committed "+file)

	default:
		rr := domain.RunResult{Status: domain.ResultNoChanges, Summary: "fake: nothing to do", Risk: domain.RiskLow}
		return report(spec, sessionID, say, rr, "nothing to do")
	}
}

// report writes rr to the outbox, emits a text event describing what was done, and returns the
// matching harness result.
func report(spec harness.Spec, sessionID string, say func(domain.LogKind, string), rr domain.RunResult, did string) (harness.Result, error) {
	if err := writeResult(spec.Workdir, rr); err != nil {
		return harness.Result{SessionID: sessionID, IsError: true}, err
	}
	say(domain.LogText, "fake: "+did)
	final := rr.Summary
	if rr.Status == domain.ResultBlocked {
		final = rr.BlockedReason
	}
	return harness.Result{SessionID: sessionID, FinalText: final}, nil
}

// writeResult writes rr to Workdir/.foundry-run/result.json.
func writeResult(workdir string, rr domain.RunResult) error {
	if workdir == "" {
		return fmt.Errorf("fake harness: Spec.Workdir is required to write %s", domain.ResultFile)
	}
	dir := filepath.Join(workdir, domain.OutboxDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("fake harness: create outbox: %w", err)
	}
	data, err := json.MarshalIndent(rr, "", "  ")
	if err != nil {
		return fmt.Errorf("fake harness: marshal result: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, domain.ResultFile), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("fake harness: write result: %w", err)
	}
	return nil
}

// commitFile writes file in dir and commits it with a fixed identity.
func commitFile(ctx context.Context, dir, file, content string) error {
	if dir == "" {
		return fmt.Errorf("fake harness: Spec.Workdir is required to commit")
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		return fmt.Errorf("fake harness: write %s: %w", file, err)
	}
	steps := [][]string{
		{"add", "--", file},
		{"-c", "user.name=Foundry", "-c", "user.email=foundry@localhost", "commit", "-m", "fake change"},
	}
	for _, args := range steps {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("fake harness: git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
