package harness

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func textParser(line string) []Event {
	return []Event{{Kind: domain.LogText, Text: line}}
}

func drain(s Session) []Event {
	var out []Event
	for ev := range s.Events() {
		out = append(out, ev)
	}
	return out
}

func startSh(t *testing.T, script string, extra ProcSpec) *Proc {
	t.Helper()
	extra.Command = []string{"sh", "-c", script}
	if extra.ParseLine == nil {
		extra.ParseLine = textParser
	}
	p, err := StartProcess(context.Background(), extra)
	require.NoError(t, err)
	return p
}

func TestStartProcessRejectsEmptyCommand(t *testing.T) {
	_, err := StartProcess(context.Background(), ProcSpec{})
	require.Error(t, err)
}

func TestStartProcessParsesStdoutAndCapturesStderr(t *testing.T) {
	p := startSh(t, "echo one; echo two; echo oops >&2", ProcSpec{})
	events := drain(p)
	res, err := p.Wait()
	require.NoError(t, err)

	var texts []string
	var stderr []string
	for _, ev := range events {
		require.False(t, ev.Time.IsZero(), "events get a timestamp")
		if ev.Kind == domain.LogStderr {
			stderr = append(stderr, ev.Text)
		} else {
			texts = append(texts, ev.Text)
		}
	}
	assert.Equal(t, []string{"one", "two"}, texts)
	assert.Equal(t, []string{"oops"}, stderr)
	assert.Equal(t, 0, res.ExitCode)
	assert.False(t, res.IsError)
	assert.Equal(t, []string{"oops"}, p.StderrTail())
}

func TestExitCodeReachesResult(t *testing.T) {
	p := startSh(t, "echo x; exit 3", ProcSpec{})
	drain(p)
	res, err := p.Wait()
	require.NoError(t, err)
	assert.Equal(t, 3, res.ExitCode)
	assert.True(t, res.IsError)
}

func TestOnExitCalledOnceAndResultCached(t *testing.T) {
	var calls atomic.Int32
	p := startSh(t, "echo hi; exit 5", ProcSpec{
		OnExit: func(code int, waitErr error) (Result, error) {
			calls.Add(1)
			return Result{FinalText: "final", ExitCode: 99}, nil
		},
	})
	drain(p)

	var wg = make(chan Result, 4)
	for i := 0; i < 4; i++ {
		go func() {
			res, err := p.Wait()
			assert.NoError(t, err)
			wg <- res
		}()
	}
	for i := 0; i < 4; i++ {
		res := <-wg
		assert.Equal(t, "final", res.FinalText)
		assert.Equal(t, 5, res.ExitCode, "ExitCode is always the process exit code")
	}
	_, _ = p.Wait()
	assert.Equal(t, int32(1), calls.Load())
}

func TestOnExitSeesAllParsedLines(t *testing.T) {
	var last string
	p := startSh(t, "echo a; echo b; echo c", ProcSpec{
		ParseLine: func(line string) []Event {
			last = line
			return nil
		},
		OnExit: func(int, error) (Result, error) {
			return Result{FinalText: last}, nil
		},
	})
	drain(p)
	res, err := p.Wait()
	require.NoError(t, err)
	assert.Equal(t, "c", res.FinalText)
}

func TestWaitWithoutDrainingWhenOutputFits(t *testing.T) {
	p := startSh(t, "echo a; echo b", ProcSpec{})
	res, err := p.Wait()
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Len(t, drain(p), 2)
}

func TestStdinIsWrittenAndClosed(t *testing.T) {
	p, err := StartProcess(context.Background(), ProcSpec{
		Command:   []string{"cat"},
		Stdin:     "from stdin",
		ParseLine: textParser,
	})
	require.NoError(t, err)
	events := drain(p)
	require.Len(t, events, 1)
	assert.Equal(t, "from stdin", events[0].Text)
}

func TestEnvAndDirAreApplied(t *testing.T) {
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	p := startSh(t, `echo "$FOO"; pwd -P`, ProcSpec{
		Dir: dir,
		Env: map[string]string{"FOO": "bar"},
	})
	events := drain(p)
	require.Len(t, events, 2)
	assert.Equal(t, "bar", events[0].Text)
	assert.Equal(t, resolved, events[1].Text)
}

func TestLongLinesAreNotSplit(t *testing.T) {
	const n = 200_000
	p := startSh(t, "head -c 200000 /dev/zero | tr '\\0' a; echo", ProcSpec{})
	events := drain(p)
	require.Len(t, events, 1)
	assert.Len(t, events[0].Text, n)
}

func TestUnterminatedLastLineIsParsed(t *testing.T) {
	p := startSh(t, "printf tail", ProcSpec{})
	events := drain(p)
	require.Len(t, events, 1)
	assert.Equal(t, "tail", events[0].Text)
}

func TestCancelKillsProcess(t *testing.T) {
	p := startSh(t, "sleep 60", ProcSpec{})
	start := time.Now()
	p.Cancel()
	p.Cancel() // idempotent
	drain(p)
	res, err := p.Wait()
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Less(t, time.Since(start), procKillGrace, "SIGTERM should be enough for sleep")
}

func TestCancelKillsChildrenInProcessGroup(t *testing.T) {
	p := startSh(t, "sleep 60 & wait", ProcSpec{})
	time.Sleep(200 * time.Millisecond) // let the child start
	start := time.Now()
	p.Cancel()
	drain(p)
	_, err := p.Wait()
	require.NoError(t, err)
	assert.Less(t, time.Since(start), procKillGrace, "the background sleep must die with the group")
}

func TestContextCancellationCancelsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p, err := StartProcess(ctx, ProcSpec{
		Command:   []string{"sh", "-c", "sleep 60"},
		ParseLine: textParser,
	})
	require.NoError(t, err)
	cancel()
	done := make(chan struct{})
	go func() {
		drain(p)
		_, _ = p.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(procKillGrace):
		t.Fatal("context cancellation did not stop the process")
	}
}

func TestOnExitErrorIsReturned(t *testing.T) {
	want := errors.New("boom")
	p := startSh(t, "true", ProcSpec{
		OnExit: func(int, error) (Result, error) { return Result{}, want },
	})
	drain(p)
	_, err := p.Wait()
	assert.ErrorIs(t, err, want)
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", Truncate("abc", 3))
	assert.Equal(t, "ab...", Truncate("abcdef", 2))
	// "é" is two bytes; cutting at 1 must not split it.
	assert.Equal(t, "...", Truncate("é", 1))
}

func TestCompactJSON(t *testing.T) {
	assert.Equal(t, `{"a":1}`, CompactJSON([]byte("{\n  \"a\": 1\n}"), 100))
	assert.Equal(t, `{"a":1...`, CompactJSON([]byte(`{"a": 1}`), 6))
	assert.Equal(t, "not json", CompactJSON([]byte("not json"), 100))
}

func TestBackgroundChildDoesNotKeepWaitOpen(t *testing.T) {
	p := startSh(t, "sleep 25 & echo $!; echo done", ProcSpec{})
	events := drain(p)
	require.Len(t, events, 2, "the child's pid, then done")
	pid, err := strconv.Atoi(events[0].Text)
	require.NoError(t, err)

	start := time.Now()
	res, err := p.Wait()
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 3*time.Second, "a background child must not hold Wait open")
	assert.Equal(t, 0, res.ExitCode)
	assert.Equal(t, "done", events[1].Text)

	gone := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			gone = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	assert.True(t, gone, "the background sleep must be killed after the leader exits")
}

func TestAgentEnvWithholdsForeignCredentials(t *testing.T) {
	t.Setenv("GH_TOKEN", "gh-secret")
	p := startSh(t, "env", ProcSpec{})
	events := drain(p)
	_, err := p.Wait()
	require.NoError(t, err)
	for _, ev := range events {
		assert.False(t, strings.HasPrefix(ev.Text, "GH_TOKEN="), "agent saw GH_TOKEN")
	}
}

func TestInheritEnvPassesNamedVariable(t *testing.T) {
	t.Setenv("FOUNDRY_TEST_INHERIT", "passed")
	p := startSh(t, "env", ProcSpec{InheritEnv: []string{"FOUNDRY_TEST_INHERIT"}})
	events := drain(p)
	_, err := p.Wait()
	require.NoError(t, err)
	var found bool
	for _, ev := range events {
		if ev.Text == "FOUNDRY_TEST_INHERIT=passed" {
			found = true
		}
	}
	assert.True(t, found, "a name listed in InheritEnv must reach the child")
}
