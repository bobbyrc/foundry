package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/harness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func TestBuildArgsDefaults(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{Prompt: "hi"})
	assert.Equal(t, []string{
		"claude", "-p", "--output-format", "stream-json", "--verbose",
		"--permission-mode", "auto",
	}, args)
}

func TestBuildArgsOptionalFlags(t *testing.T) {
	cfg := harness.Config{Command: []string{"/opt/claude"}, Args: []string{"--cfg-arg"}}
	spec := harness.Spec{
		Instructions:    "be terse",
		Model:           "haiku",
		ResumeSessionID: "sess-1",
		Args:            []string{"--spec-arg"},
	}
	args := buildArgs(cfg, spec)
	assert.Equal(t, []string{
		"/opt/claude", "-p", "--output-format", "stream-json", "--verbose",
		"--append-system-prompt", "be terse",
		"--model", "haiku",
		"--resume", "sess-1",
		"--cfg-arg", "--spec-arg",
		"--permission-mode", "auto",
	}, args)
}

func TestBuildArgsEmptyOptionalsAreOmitted(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{Instructions: "", Model: "", ResumeSessionID: ""})
	assert.Equal(t, -1, indexOf(args, "--append-system-prompt"))
	assert.Equal(t, -1, indexOf(args, "--model"))
	assert.Equal(t, -1, indexOf(args, "--resume"))
}

func TestBuildArgsKeepsExplicitPermissionMode(t *testing.T) {
	cases := map[string]harness.Config{
		"config --permission-mode":  {Args: []string{"--permission-mode", "acceptEdits"}},
		"config --permission-mode=": {Args: []string{"--permission-mode=plan"}},
		"config dangerously-skip":   {Args: []string{"--dangerously-skip-permissions"}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			args := buildArgs(cfg, harness.Spec{})
			assert.Equal(t, -1, indexOf(args, "auto"), "no default injected: %v", args)
		})
	}

	args := buildArgs(harness.Config{}, harness.Spec{Args: []string{"--dangerously-skip-permissions"}})
	assert.Equal(t, -1, indexOf(args, "auto"), "spec permission flag suppresses the default")
	assert.Equal(t, 0, countOf(args, "--permission-mode"))
}

func countOf(args []string, want string) int {
	n := 0
	for _, a := range args {
		if a == want {
			n++
		}
	}
	return n
}

func TestParseSimpleStream(t *testing.T) {
	events, res := replay(t, "testdata/simple.jsonl")

	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, domain.LogSystem, events[0].Kind)
	assert.Equal(t, "session started (model claude-haiku-5-5)", events[0].Text)
	assert.Equal(t, "4ccc4a84-3649-44f2-aeb3-1df30a937b9d", events[0].SessionID)

	var texts []string
	for _, ev := range events {
		if ev.Kind == domain.LogText {
			texts = append(texts, ev.Text)
		}
		assert.NotContains(t, ev.Text, "hook", "hook noise must be dropped")
	}
	assert.Equal(t, []string{"OK"}, texts)

	assert.Equal(t, "OK", res.FinalText)
	assert.Equal(t, "4ccc4a84-3649-44f2-aeb3-1df30a937b9d", res.SessionID)
	assert.InDelta(t, 0.00955102, res.CostUSD, 1e-9)
	assert.Equal(t, int64(2+47175+11382), res.InputTokens, "cache tokens count as input")
	assert.Equal(t, int64(4), res.OutputTokens)
	assert.False(t, res.IsError)
	assert.Empty(t, res.Denials)
}

func TestParseToolUseStream(t *testing.T) {
	events, res := replay(t, "testdata/tool_use.jsonl")

	var toolUse, toolResult []string
	for _, ev := range events {
		switch ev.Kind {
		case domain.LogToolUse:
			toolUse = append(toolUse, ev.Text)
		case domain.LogToolResult:
			toolResult = append(toolResult, ev.Text)
		}
	}
	assert.Equal(t, []string{`Bash: {"command":"echo hi","description":"Print hi via echo"}`}, toolUse)
	assert.Equal(t, []string{"hi"}, toolResult)
	assert.Equal(t, "The command `echo hi` ran and printed `hi`.", res.FinalText)
	assert.False(t, res.IsError)
}

func TestParseDenialExtraction(t *testing.T) {
	events, res := replay(t, "testdata/denial.jsonl")

	assert.Equal(t, []string{`Bash: {"command":"rm -rf /"}`}, res.Denials)
	var denials []harness.Event
	for _, ev := range events {
		if ev.Kind == domain.LogDenial {
			denials = append(denials, ev)
		}
	}
	require.Len(t, denials, 1)
	assert.Equal(t, `Bash: {"command":"rm -rf /"}`, denials[0].Text)
	assert.Equal(t, "I could not run that command because it was denied.", res.FinalText)
	assert.Equal(t, "sess-denied-1", res.SessionID)
}

func TestParseErrorResultSetsIsError(t *testing.T) {
	p := newParser("")
	p.parseLine(`{"type":"system","subtype":"init","session_id":"s1","model":"m"}`)
	events := p.parseLine(`{"type":"result","is_error":true,"result":"rate limited","session_id":"s1","total_cost_usd":0.5,"usage":{"input_tokens":1,"output_tokens":2}}`)

	require.Len(t, events, 1)
	assert.Equal(t, domain.LogError, events[0].Kind)
	res := p.result(0, nil)
	assert.True(t, res.IsError)
	assert.Equal(t, "rate limited", res.FinalText)
	assert.Equal(t, 0.5, res.CostUSD)
	assert.Equal(t, int64(1), res.InputTokens)
	assert.Equal(t, int64(2), res.OutputTokens)
}

func TestGarbledLinesBecomeSystemEvents(t *testing.T) {
	p := newParser("")
	long := strings.Repeat("x", 1000)

	events := p.parseLine("not json at all")
	require.Len(t, events, 1)
	assert.Equal(t, domain.LogSystem, events[0].Kind)
	assert.Equal(t, "not json at all", events[0].Text)

	events = p.parseLine(long)
	require.Len(t, events, 1)
	assert.Equal(t, domain.LogSystem, events[0].Kind)
	assert.LessOrEqual(t, len(events[0].Text), maxRawChars+3)

	events = p.parseLine(`{"type":"something_new","x":1}`)
	require.Len(t, events, 1)
	assert.Equal(t, domain.LogSystem, events[0].Kind)
}

func TestHookAndRateLimitLinesAreIgnored(t *testing.T) {
	p := newParser("")
	assert.Empty(t, p.parseLine(`{"type":"system","subtype":"hook_response","stdout":"{}"}`))
	assert.Empty(t, p.parseLine(`{"type":"rate_limit_event","rate_limit_info":{}}`))
}

func TestResultWithoutResultLineOnFailure(t *testing.T) {
	p := newParser("")
	p.parseLine(`{"type":"assistant","message":{"content":[{"type":"text","text":"partial"}]}}`)

	res := p.result(1, []string{"fatal: login expired", "try again"})
	assert.True(t, res.IsError)
	assert.Equal(t, 1, res.ExitCode)
	assert.Equal(t, "fatal: login expired\ntry again", res.FinalText)
}

func TestResultWithoutResultLineOnSuccess(t *testing.T) {
	p := newParser("")
	p.parseLine(`{"type":"assistant","message":{"content":[{"type":"text","text":"last words"}]}}`)

	res := p.result(0, nil)
	assert.False(t, res.IsError)
	assert.Equal(t, "last words", res.FinalText)
}

func TestToolResultErrorAndTruncation(t *testing.T) {
	p := newParser("")
	big := strings.Repeat("y", 5000)
	line := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t","is_error":true,"content":"` + big + `"}]}}`
	events := p.parseLine(line)
	require.Len(t, events, 1)
	assert.Equal(t, domain.LogToolResult, events[0].Kind)
	assert.True(t, strings.HasPrefix(events[0].Text, "error: "))
	assert.LessOrEqual(t, len(events[0].Text), len("error: ")+maxResultChars+3)
}

func TestStartRunsCommandWithPromptOnStdin(t *testing.T) {
	fixture, err := filepath.Abs("testdata/simple.jsonl")
	require.NoError(t, err)
	script, err := filepath.Abs("testdata/fake_claude.sh")
	require.NoError(t, err)

	r := &runner{cfg: harness.Config{Command: []string{"sh", script, fixture}}}
	sess, err := r.Start(context.Background(), harness.Spec{
		Workdir:      t.TempDir(),
		Prompt:       "Reply with exactly: OK",
		Instructions: "be brief",
		Model:        "haiku",
		Env:          map[string]string{"ONLET_TEST": "1"},
	})
	require.NoError(t, err)

	var stderr []string
	var sawText bool
	for ev := range sess.Events() {
		switch ev.Kind {
		case domain.LogStderr:
			stderr = append(stderr, ev.Text)
		case domain.LogText:
			sawText = true
		}
	}
	res, err := sess.Wait()
	require.NoError(t, err)

	assert.True(t, sawText)
	assert.Equal(t, "OK", res.FinalText)
	assert.Contains(t, stderr, "stdin=Reply with exactly: OK")
	assert.Contains(t, stderr, "arg=--append-system-prompt")
	assert.Contains(t, stderr, "arg=be brief")
	assert.Contains(t, stderr, "arg=--model")
	assert.Contains(t, stderr, "arg=--permission-mode")
	assert.Contains(t, stderr, "arg=auto")
}

func TestStartPassesEnvAndWorkdir(t *testing.T) {
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	script := filepath.Join(dir, "env.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\necho \"env=$FROM_CFG/$FROM_SPEC\"\npwd -P\n"), 0o755))

	r := &runner{cfg: harness.Config{
		Command: []string{"sh", script},
		Env:     map[string]string{"FROM_CFG": "cfg", "FROM_SPEC": "cfg"},
	}}
	sess, err := r.Start(context.Background(), harness.Spec{
		Workdir: dir,
		Env:     map[string]string{"FROM_SPEC": "spec"},
	})
	require.NoError(t, err)
	var texts []string
	for ev := range sess.Events() {
		texts = append(texts, ev.Text)
	}
	_, err = sess.Wait()
	require.NoError(t, err)
	assert.Contains(t, texts, "env=cfg/spec", "spec env wins over config env")
	assert.Contains(t, texts, resolved, "the process runs in Spec.Workdir")
}

// replay feeds a fixture through the parser and returns the events and the final result.
func replay(t *testing.T, path string) ([]harness.Event, harness.Result) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	p := newParser("")
	var events []harness.Event
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		events = append(events, p.parseLine(line)...)
	}
	return events, p.result(0, nil)
}
