package exec

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

func pluginCommand(t *testing.T) []string {
	t.Helper()
	script, err := filepath.Abs("testdata/plugin.sh")
	require.NoError(t, err)
	return []string{"sh", script}
}

func newHarness(t *testing.T, cfg harness.Config) harness.Harness {
	t.Helper()
	cfg.Name = "plugin"
	cfg.Type = "exec"
	h, err := harness.New(cfg)
	require.NoError(t, err)
	return h
}

// run starts the plugin and returns every event with the result.
func run(t *testing.T, h harness.Harness, spec harness.Spec) ([]harness.Event, harness.Result) {
	t.Helper()
	sess, err := h.Start(context.Background(), spec)
	require.NoError(t, err)
	var events []harness.Event
	for ev := range sess.Events() {
		events = append(events, ev)
	}
	res, err := sess.Wait()
	require.NoError(t, err)
	return events, res
}

func TestFactoryRequiresCommand(t *testing.T) {
	_, err := harness.New(harness.Config{Name: "empty", Type: "exec"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a command")
}

func TestFullProtocolStream(t *testing.T) {
	h := newHarness(t, harness.Config{Command: pluginCommand(t), Args: []string{"--cfg"}})
	dir := t.TempDir()
	events, res := run(t, h, harness.Spec{Workdir: dir, Prompt: "full task", Args: []string{"--spec"}})

	var kinds []string
	for _, ev := range events {
		kinds = append(kinds, string(ev.Kind)+"|"+ev.Text)
	}
	assert.Equal(t, []string{
		"system|args=--cfg --spec",
		"text|thinking",
		"tool_use|shell: ls",
		"tool_result|a.txt",
		"denial|Bash: rm -rf /",
		"error|soft error",
		"text|plain text line",
		"denial|Bash: rm -rf /",
	}, kinds)

	assert.Equal(t, "all done", res.FinalText)
	assert.Equal(t, "plug-1", res.SessionID)
	assert.InDelta(t, 0.1, res.CostUSD, 1e-9)
	assert.Equal(t, int64(1), res.InputTokens)
	assert.Equal(t, int64(2), res.OutputTokens)
	assert.Equal(t, []string{"Bash: rm -rf /"}, res.Denials)
	assert.False(t, res.IsError)
	assert.Equal(t, 0, res.ExitCode)

	// The session ID is first revealed by the result line, so it is attached to the first event
	// that line produces: its denial.
	for i, ev := range events {
		if i == len(events)-1 {
			assert.Equal(t, "plug-1", ev.SessionID)
		} else {
			assert.Empty(t, ev.SessionID)
		}
	}
}

func TestMissingResultLineUsesLastText(t *testing.T) {
	h := newHarness(t, harness.Config{Command: pluginCommand(t)})
	events, res := run(t, h, harness.Spec{Prompt: "bare"})

	var sessions []string
	for _, ev := range events {
		if ev.SessionID != "" {
			sessions = append(sessions, ev.SessionID)
		}
	}
	assert.Equal(t, []string{"plug-2"}, sessions, "the session ID is attached to the first event that reveals it")
	assert.Equal(t, "last words", res.FinalText)
	assert.Equal(t, "plug-2", res.SessionID)
	assert.False(t, res.IsError, "exit code 0 without a result line is not an error")
}

func TestNonZeroExitWithoutResultIsError(t *testing.T) {
	h := newHarness(t, harness.Config{Command: pluginCommand(t)})
	_, res := run(t, h, harness.Spec{Prompt: "fail"})

	assert.True(t, res.IsError)
	assert.Equal(t, 4, res.ExitCode)
	assert.Equal(t, "about to fail", res.FinalText)
}

func TestEnvironmentAndFilesAreProvided(t *testing.T) {
	h := newHarness(t, harness.Config{
		Command: pluginCommand(t),
		Env:     map[string]string{"FROM_CFG": "1"},
	})
	events, res := run(t, h, harness.Spec{
		Prompt:          "env run",
		Instructions:    "be brief",
		Model:           "some-model",
		ResumeSessionID: "resume-7",
		Env:             map[string]string{"FROM_SPEC": "1"},
	})

	var system []string
	var promptFile, instructionsFile string
	var texts []string
	for _, ev := range events {
		switch ev.Kind {
		case domain.LogSystem:
			system = append(system, ev.Text)
		case domain.LogText:
			texts = append(texts, ev.Text)
		}
	}
	assert.Contains(t, system, "model=some-model resume=resume-7")
	for _, s := range system {
		if p, ok := strings.CutPrefix(s, "prompt_file="); ok {
			promptFile = p
		}
		if p, ok := strings.CutPrefix(s, "instructions_file="); ok {
			instructionsFile = p
		}
	}
	require.NotEmpty(t, promptFile)
	require.NotEmpty(t, instructionsFile)
	assert.Equal(t, []string{"instructions=be brief"}, texts)
	assert.Equal(t, "env ok", res.FinalText)

	// Both temporary files are removed once the run has been waited on.
	_, err := os.Stat(promptFile)
	assert.True(t, os.IsNotExist(err), "prompt file removed: %v", err)
	_, err = os.Stat(instructionsFile)
	assert.True(t, os.IsNotExist(err), "instructions file removed: %v", err)
}

func TestParserNonJSONIsText(t *testing.T) {
	p := newParser()
	events := p.parseLine("hello there")
	require.Len(t, events, 1)
	assert.Equal(t, domain.LogText, events[0].Kind)
	assert.Equal(t, "hello there", events[0].Text)
}

func TestParserUnknownTypeIsSystem(t *testing.T) {
	p := newParser()
	events := p.parseLine(`{"type":"mystery","text":"?"}`)
	require.Len(t, events, 1)
	assert.Equal(t, domain.LogSystem, events[0].Kind)
	assert.Equal(t, "?", events[0].Text)
}

func TestParserResultWithoutDenialsUsesDenialEvents(t *testing.T) {
	p := newParser()
	p.parseLine(`{"type":"denial","text":"Write: /etc/passwd"}`)
	p.parseLine(`{"type":"result","text":"done","is_error":false}`)
	res := p.result(0)
	assert.Equal(t, []string{"Write: /etc/passwd"}, res.Denials)
	assert.Equal(t, "done", res.FinalText)
}
