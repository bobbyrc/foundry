package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
)

func TestParseTwoResults(t *testing.T) {
	data, err := os.ReadFile("testdata/two_results.jsonl")
	require.NoError(t, err)
	p := newParser("")
	for _, line := range splitLines(string(data)) {
		p.parseLine(line)
	}
	res := p.result(0, nil)
	assert.Equal(t, "TWO", res.FinalText)
	assert.Equal(t, 0.03, res.CostUSD)
	assert.Equal(t, int64(30+1+2), res.InputTokens)
	assert.Equal(t, int64(7), res.OutputTokens)
	assert.False(t, res.IsError)
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func TestClaudeSendDuringRun(t *testing.T) {
	script, err := filepath.Abs("testdata/echo_turns.sh")
	require.NoError(t, err)
	r := &runner{cfg: harness.Config{Command: []string{script}}}
	sess, err := r.Start(context.Background(), harness.Spec{Workdir: t.TempDir(), Prompt: `say "hi"`})
	require.NoError(t, err)
	st, ok := sess.(harness.Steerable)
	require.True(t, ok, "session must implement harness.Steerable")

	var stderr []string
	for ev := range sess.Events() {
		switch ev.Kind {
		case domain.LogStderr:
			stderr = append(stderr, ev.Text)
		}
	}
	res, err := sess.Wait()
	require.NoError(t, err)
	assert.Contains(t, stderr, "arg=--input-format")
	assert.Equal(t, 0, res.ExitCode)
	assert.Equal(t, `say "hi"`, res.FinalText) // quotes survive the JSON round trip
	assert.ErrorIs(t, st.Send("late"), harness.ErrInputClosed)
}
