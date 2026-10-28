package fake

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/harness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runDefault runs DefaultScript and returns the events and the result.
func runDefault(t *testing.T, spec harness.Spec) ([]harness.Event, harness.Result) {
	t.Helper()
	if spec.Workdir == "" {
		spec.Workdir = t.TempDir()
	}
	if spec.RunID == "" {
		spec.RunID = "run-1"
	}
	sess, err := New(DefaultScript).Start(context.Background(), spec)
	require.NoError(t, err)
	var events []harness.Event
	for ev := range sess.Events() {
		events = append(events, ev)
	}
	res, err := sess.Wait()
	require.NoError(t, err)
	return events, res
}

// readResult loads the outbox file the fake wrote.
func readResult(t *testing.T, workdir string) domain.RunResult {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(workdir, domain.OutboxDir, domain.ResultFile))
	require.NoError(t, err)
	var rr domain.RunResult
	require.NoError(t, json.Unmarshal(data, &rr))
	return rr
}

func TestStallEndsOnCancel(t *testing.T) {
	sess, err := New(DefaultScript).Start(context.Background(), harness.Spec{
		Prompt:  "[fake:stall]",
		Workdir: t.TempDir(),
	})
	require.NoError(t, err)

	time.AfterFunc(50*time.Millisecond, sess.Cancel)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for range sess.Events() {
		}
		res, err := sess.Wait()
		assert.NoError(t, err)
		assert.True(t, res.IsError, "a cancelled session is an error")
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled stall did not end")
	}
}

func TestCancelledContextMarksResultAsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	script := func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		emit(harness.Event{Kind: domain.LogText, Text: "working"})
		<-ctx.Done()
		return harness.Result{FinalText: "partial"}, nil
	}
	sess, err := New(script).Start(ctx, harness.Spec{})
	require.NoError(t, err)
	cancel()
	for range sess.Events() {
	}
	res, err := sess.Wait()
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Equal(t, "partial", res.FinalText)
}

func TestDenyReportsDenialAndFails(t *testing.T) {
	events, res := runDefault(t, harness.Spec{Prompt: "please [fake:deny]"})

	require.NotEmpty(t, events)
	assert.Equal(t, domain.LogDenial, events[0].Kind)
	assert.Equal(t, "Bash: rm -rf /", events[0].Text)
	assert.Equal(t, []string{"Bash: rm -rf /"}, res.Denials)
	assert.True(t, res.IsError)
}

func TestFailWritesFailedResult(t *testing.T) {
	spec := harness.Spec{Prompt: "[fake:fail]", Workdir: t.TempDir(), RunID: "r1"}
	_, res := runDefault(t, spec)
	rr := readResult(t, spec.Workdir)
	assert.Equal(t, domain.ResultFailed, rr.Status)
	assert.Equal(t, "fake failure", rr.Summary)
	assert.Equal(t, "fake failure", res.FinalText)
	assert.False(t, res.IsError)
}

func TestBlockWritesBlockedResult(t *testing.T) {
	spec := harness.Spec{Prompt: "[fake:block]", Workdir: t.TempDir(), RunID: "r2"}
	_, res := runDefault(t, spec)
	rr := readResult(t, spec.Workdir)
	assert.Equal(t, domain.ResultBlocked, rr.Status)
	assert.Equal(t, "fake needs a human", rr.BlockedReason)
	assert.Equal(t, "fake needs a human", res.FinalText)
}

func TestReviewerApproves(t *testing.T) {
	spec := harness.Spec{
		Prompt:  "review this",
		Env:     map[string]string{"ONLET_ROLE": "reviewer"},
		Workdir: t.TempDir(),
	}
	_, res := runDefault(t, spec)
	rr := readResult(t, spec.Workdir)
	assert.Equal(t, domain.ResultCompleted, rr.Status)
	assert.Equal(t, "fake review", rr.Summary)
	require.NotNil(t, rr.Review)
	assert.True(t, rr.Review.Approve)
	assert.Empty(t, rr.Review.Findings)
	assert.Equal(t, "fake review", res.FinalText)
}

func TestReviewerRejects(t *testing.T) {
	spec := harness.Spec{
		Prompt:  "review this [fake:reject]",
		Env:     map[string]string{"ONLET_ROLE": "reviewer"},
		Workdir: t.TempDir(),
	}
	runDefault(t, spec)
	rr := readResult(t, spec.Workdir)
	require.NotNil(t, rr.Review)
	assert.False(t, rr.Review.Approve)
	require.Len(t, rr.Review.Findings, 1)
	assert.Equal(t, "blocker", rr.Review.Findings[0].Severity)
	assert.Equal(t, "fake finding", rr.Review.Findings[0].Message)
}

func TestNoMarkerIsNoChanges(t *testing.T) {
	spec := harness.Spec{Prompt: "just chat", Workdir: t.TempDir()}
	events, res := runDefault(t, spec)
	rr := readResult(t, spec.Workdir)
	assert.Equal(t, domain.ResultNoChanges, rr.Status)
	assert.Equal(t, "fake: nothing to do", rr.Summary)
	assert.Equal(t, domain.RiskLow, rr.Risk)
	assert.Equal(t, "fake: nothing to do", res.FinalText)
	require.NotEmpty(t, events)
	assert.Equal(t, domain.LogText, events[len(events)-1].Kind)
}

func TestSessionIDAttachedToFirstEvent(t *testing.T) {
	events, res := runDefault(t, harness.Spec{Prompt: "[fake:fail]", RunID: "abc"})
	require.NotEmpty(t, events)
	assert.Equal(t, "fake-abc", events[0].SessionID)
	assert.Equal(t, "fake-abc", res.SessionID)
}

func TestCommitCreatesCommitAndResult(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q")

	spec := harness.Spec{
		Prompt:  "add a greeting [fake:commit]\nwith more detail",
		RunID:   "run-9",
		Workdir: dir,
	}
	_, res := runDefault(t, spec)

	content, err := os.ReadFile(filepath.Join(dir, "fake-run-9.txt"))
	require.NoError(t, err)
	assert.Equal(t, "add a greeting [fake:commit]\n", string(content))
	assert.Equal(t, "fake change", strings.TrimSpace(git(t, dir, "log", "-1", "--format=%s")))
	assert.Equal(t, "Onlet <onlet@localhost>", strings.TrimSpace(git(t, dir, "log", "-1", "--format=%an <%ae>")))
	assert.Contains(t, git(t, dir, "show", "--name-only", "--format=", "HEAD"), "fake-run-9.txt")

	rr := readResult(t, dir)
	assert.Equal(t, domain.ResultCompleted, rr.Status)
	assert.Equal(t, "fake change", rr.Summary)
	assert.Equal(t, domain.RiskLow, rr.Risk)
	assert.Equal(t, "Fake change", rr.PRTitle)
	assert.Equal(t, "Made by the fake harness.", rr.PRBody)
	assert.Equal(t, "fake change", res.FinalText)
}

func TestCommitRiskHighMarker(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	runDefault(t, harness.Spec{Prompt: "risky [fake:commit] [fake:risk-high]", RunID: "r3", Workdir: dir})
	rr := readResult(t, dir)
	assert.Equal(t, domain.RiskHigh, rr.Risk)
}

func TestCustomScriptReceivesSpecAndEmits(t *testing.T) {
	var seen harness.Spec
	script := func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		seen = spec
		emit(harness.Event{Kind: domain.LogToolUse, Text: "custom"})
		return harness.Result{FinalText: "custom done", CostUSD: 1.5}, nil
	}
	sess, err := New(script).Start(context.Background(), harness.Spec{Prompt: "p", Model: "m"})
	require.NoError(t, err)
	var kinds []domain.LogKind
	for ev := range sess.Events() {
		kinds = append(kinds, ev.Kind)
	}
	res, err := sess.Wait()
	require.NoError(t, err)
	assert.Equal(t, []domain.LogKind{domain.LogToolUse}, kinds)
	assert.Equal(t, "p", seen.Prompt)
	assert.Equal(t, "custom done", res.FinalText)
	assert.Equal(t, 1.5, res.CostUSD)
	assert.False(t, res.IsError)
}

func TestWriteRequiresWorkdir(t *testing.T) {
	_, err := DefaultScript(context.Background(), harness.Spec{Prompt: "[fake:fail]", RunID: "x"}, func(harness.Event) {})
	require.Error(t, err)
}

func TestRegisteredAsFake(t *testing.T) {
	h, err := harness.New(harness.Config{Name: "f", Type: "fake"})
	require.NoError(t, err)
	assert.NotNil(t, h)
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@localhost"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}
