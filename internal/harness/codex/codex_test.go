package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/harness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countOf(args []string, want string) int {
	n := 0
	for _, a := range args {
		if a == want {
			n++
		}
	}
	return n
}

func TestBuildArgsNewSession(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{Workdir: "/work/tree", Prompt: "hi"})
	assert.Equal(t, []string{
		"codex", "exec", "--json", "--skip-git-repo-check", "-C", "/work/tree",
		"--approve-for-me", "-",
	}, args)
}

func TestBuildArgsNewSessionWithOptions(t *testing.T) {
	cfg := harness.Config{Command: []string{"/opt/codex"}, Args: []string{"--cfg-arg"}}
	spec := harness.Spec{Workdir: "/w", Model: "gpt-x", Args: []string{"--spec-arg"}}
	args := buildArgs(cfg, spec)
	assert.Equal(t, []string{
		"/opt/codex", "exec", "--json", "--skip-git-repo-check", "-C", "/w",
		"--model", "gpt-x", "--cfg-arg", "--spec-arg",
		"--approve-for-me", "-",
	}, args)
}

func TestBuildArgsResumeSession(t *testing.T) {
	spec := harness.Spec{Workdir: "/w", ResumeSessionID: "thread-1", Model: "m"}
	args := buildArgs(harness.Config{}, spec)
	assert.Equal(t, []string{
		"codex", "exec", "resume", "thread-1", "--json", "--skip-git-repo-check",
		"--model", "m", "-",
	}, args, "resume takes no -C and no sandbox flags")
}

func TestBuildArgsSandboxFlagSuppressesDefault(t *testing.T) {
	for _, flag := range []string{"--sandbox", "-s", "--approve-for-me", "--full-auto",
		"--dangerously-bypass-approvals-and-sandbox"} {
		args := buildArgs(harness.Config{Args: []string{flag}}, harness.Spec{Workdir: "/w"})
		assert.Equal(t, 1, countOf(args, flag), "flag %s", flag)
		if flag != "--approve-for-me" {
			assert.Equal(t, 0, countOf(args, "--approve-for-me"), "flag %s", flag)
		}
	}
	args := buildArgs(harness.Config{}, harness.Spec{Args: []string{"--sandbox", "read-only"}})
	assert.Equal(t, 1, countOf(args, "--sandbox"))
	assert.NotContains(t, args, "--approve-for-me")
}

func TestBuildArgsNeverPairsSandboxWithApproveForMe(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{Workdir: "/w"})
	assert.Equal(t, 0, countOf(args, "--sandbox"), "codex rejects --sandbox with --approve-for-me")
	assert.Contains(t, args, "--approve-for-me")
}

func TestStdinTextAddsInstructions(t *testing.T) {
	assert.Equal(t, "just the prompt", stdinText(harness.Spec{Prompt: "just the prompt"}))
	assert.Equal(t, "rules\n\n---\n\ntask", stdinText(harness.Spec{Instructions: "rules", Prompt: "task"}))
}

func TestParseSimpleStream(t *testing.T) {
	events, res := replay(t, "testdata/simple.jsonl", 0)

	require.NotEmpty(t, events)
	assert.Equal(t, "01a18082-90f2-7d03-818d-d3226e06531a", events[0].SessionID)
	assert.Equal(t, "01a18082-90f2-7d03-818d-d3226e06531a", res.SessionID)
	assert.Equal(t, "OK", res.FinalText)
	assert.Equal(t, int64(24138), res.InputTokens)
	assert.Equal(t, int64(5), res.OutputTokens)
	assert.Zero(t, res.CostUSD, "codex reports no USD cost")
	assert.False(t, res.IsError)
	assert.Empty(t, res.Denials)
}

func TestParseCommandStream(t *testing.T) {
	events, res := replay(t, "testdata/command.jsonl", 0)

	var got []string
	for _, ev := range events {
		got = append(got, string(ev.Kind)+"|"+ev.Text)
	}
	assert.Contains(t, got, "tool_use|shell: /bin/zsh -lc 'echo hi > out.txt .'")
	assert.Contains(t, got, "tool_result|exit 0")
	assert.Equal(t, "DONE", res.FinalText)
	assert.Equal(t, int64(65961), res.InputTokens)
}

func TestParseFileChangeStream(t *testing.T) {
	events, res := replay(t, "testdata/file_change.jsonl", 0)

	var edits, results []string
	for _, ev := range events {
		switch ev.Kind {
		case domain.LogToolUse:
			edits = append(edits, ev.Text)
		case domain.LogToolResult:
			results = append(results, ev.Text)
		}
	}
	require.Len(t, edits, 2, "one edit and one shell command")
	assert.True(t, strings.HasPrefix(edits[0], "edit: "))
	assert.True(t, strings.HasSuffix(edits[0], "/hello.txt"))
	assert.Equal(t, []string{"exit 0\nhi\n"}, results)
	assert.Equal(t, "DONE", res.FinalText)
}

func TestParseErrorStream(t *testing.T) {
	events, res := replay(t, "testdata/error.jsonl", 1)

	var errs, system []string
	for _, ev := range events {
		switch ev.Kind {
		case domain.LogError:
			errs = append(errs, ev.Text)
		case domain.LogSystem:
			system = append(system, ev.Text)
		}
	}
	assert.Equal(t, []string{
		"The 'not-a-real-model-xyz' model is not supported when using Codex with a ChatGPT account.",
		"The 'not-a-real-model-xyz' model is not supported when using Codex with a ChatGPT account.",
	}, errs, "top-level error and turn.failed")
	assert.Contains(t, strings.Join(system, "\n"), "Model metadata for")
	assert.True(t, res.IsError)
	assert.Equal(t, 1, res.ExitCode)
}

func TestParseResumeStream(t *testing.T) {
	events, res := replay(t, "testdata/resume.jsonl", 0)
	require.NotEmpty(t, events)
	assert.Equal(t, "01a18082-90f2-7d03-818d-d3226e06531a", res.SessionID)
	assert.Equal(t, "AGAIN", res.FinalText)
	assert.False(t, res.IsError)
}

func TestReadOnlyRefusalIsNotStructuredDenial(t *testing.T) {
	// codex exec emits no event for a sandbox refusal: the command never appears as an item and
	// the model just reports the failure in prose. So foundry cannot report a structured denial.
	events, res := replay(t, "testdata/readonly_no_command.jsonl", 0)
	for _, ev := range events {
		assert.NotEqual(t, domain.LogDenial, ev.Kind)
		assert.NotEqual(t, domain.LogToolUse, ev.Kind)
	}
	assert.Empty(t, res.Denials)
	assert.Contains(t, res.FinalText, "operation not permitted")
}

func TestUnknownLinesAreSystemEvents(t *testing.T) {
	p := newParser()
	events := p.parseLine("garbage")
	require.Len(t, events, 1)
	assert.Equal(t, domain.LogSystem, events[0].Kind)
	events = p.parseLine(`{"type":"brand.new","x":1}`)
	require.Len(t, events, 1)
	assert.Equal(t, domain.LogSystem, events[0].Kind)
}

func TestStartRunsCodexWithStdinPrompt(t *testing.T) {
	fixture, err := filepath.Abs("testdata/simple.jsonl")
	require.NoError(t, err)
	script, err := filepath.Abs("testdata/fake_codex.sh")
	require.NoError(t, err)

	r := &runner{cfg: harness.Config{Command: []string{"sh", script, fixture}}}
	sess, err := r.Start(context.Background(), harness.Spec{
		Workdir:      t.TempDir(),
		Prompt:       "Reply with exactly: OK",
		Instructions: "rules here",
	})
	require.NoError(t, err)
	var stderr []string
	for ev := range sess.Events() {
		if ev.Kind == domain.LogStderr {
			stderr = append(stderr, ev.Text)
		}
	}
	res, err := sess.Wait()
	require.NoError(t, err)

	assert.Equal(t, "OK", res.FinalText)
	assert.Contains(t, stderr, "stdin=rules here")
	assert.Contains(t, stderr, "arg=exec")
	assert.Contains(t, stderr, "arg=-")
}

func TestStartReportsFailureWithStderr(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "die.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\necho 'unknown flag' >&2\nexit 2\n"), 0o755))

	r := &runner{cfg: harness.Config{Command: []string{"sh", script}}}
	sess, err := r.Start(context.Background(), harness.Spec{Workdir: dir, Prompt: "x"})
	require.NoError(t, err)
	for range sess.Events() {
	}
	res, err := sess.Wait()
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Equal(t, 2, res.ExitCode)
	assert.Equal(t, "unknown flag", res.FinalText)
}

// replay feeds a fixture through the parser and returns the events and the result for exitCode.
func replay(t *testing.T, path string, exitCode int) ([]harness.Event, harness.Result) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	p := newParser()
	var events []harness.Event
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		events = append(events, p.parseLine(line)...)
	}
	return events, p.result(exitCode, nil)
}
