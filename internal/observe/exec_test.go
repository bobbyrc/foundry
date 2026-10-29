package observe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestExec(t *testing.T, opts map[string]any, env map[string]string) Observer {
	t.Helper()
	o, err := New(Config{Name: "probe", Type: "exec", Options: opts, Env: env})
	require.NoError(t, err)
	return o
}

func TestExecJSONArray(t *testing.T) {
	o := newTestExec(t, map[string]any{
		"command": `printf '%s' '[{"fingerprint":"a","title":"boom","level":"error","count":3},{"fingerprint":"","title":"dropped"}]'`,
	}, nil)
	obs, err := o.Poll(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "a", obs[0].Fingerprint)
	assert.Equal(t, "boom", obs[0].Title)
	assert.Equal(t, "error", obs[0].Level)
	assert.Equal(t, int64(3), obs[0].Count)
}

func TestExecJSONLines(t *testing.T) {
	o := newTestExec(t, map[string]any{
		"command": `printf '%s\n' '{"fingerprint":"a","title":"one"}' '' '{"fingerprint":"b","title":"two"}' '{"fingerprint":"","title":"dropped"}'`,
	}, nil)
	obs, err := o.Poll(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, obs, 2)
	assert.Equal(t, "one", obs[0].Title)
	assert.Equal(t, "two", obs[1].Title)
}

func TestExecEmptyOutput(t *testing.T) {
	o := newTestExec(t, map[string]any{"command": `true`}, nil)
	obs, err := o.Poll(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestExecReceivesSinceAndEnv(t *testing.T) {
	since := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	o := newTestExec(t, map[string]any{
		"command": `printf '[{"fingerprint":"%s","title":"%s"}]' "$ONETT_SINCE" "$ONETT_TEST_CFG"`,
	}, map[string]string{"ONETT_TEST_CFG": "from-cfg"})
	obs, err := o.Poll(context.Background(), since)
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "2026-01-02T03:04:05Z", obs[0].Fingerprint)
	assert.Equal(t, "from-cfg", obs[0].Title)
}

func TestExecNonZeroExit(t *testing.T) {
	o := newTestExec(t, map[string]any{"command": `echo oops >&2; exit 2`}, nil)
	_, err := o.Poll(context.Background(), time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "oops")
	assert.Contains(t, err.Error(), "exit status 2")
}

func TestExecInvalidOutput(t *testing.T) {
	arr := newTestExec(t, map[string]any{"command": `echo '[not json'`}, nil)
	_, err := arr.Poll(context.Background(), time.Now())
	require.Error(t, err)

	lines := newTestExec(t, map[string]any{"command": `printf '%s\n' '{"fingerprint":"a"}' 'garbage'`}, nil)
	_, err = lines.Poll(context.Background(), time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 2")
}

func TestExecTimeout(t *testing.T) {
	o := newTestExec(t, map[string]any{"command": `sleep 10`, "timeout": "200ms"}, nil)
	start := time.Now()
	_, err := o.Poll(context.Background(), time.Now())
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestExecTimeoutKillsProcessGroup(t *testing.T) {
	// The forked sleep must die with its shell, or the pipe stays open and Poll hangs.
	o := newTestExec(t, map[string]any{"command": `sleep 30 & sleep 30`, "timeout": "200ms"}, nil)
	start := time.Now()
	_, err := o.Poll(context.Background(), time.Now())
	require.Error(t, err)
	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestExecFactoryRequiresCommand(t *testing.T) {
	_, err := New(Config{Name: "probe", Type: "exec", Options: map[string]any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"command" is required`)
}

// killLeftoverChildOnCleanup kills the background child whose PID a command wrote to pidFile. A
// failed command leaves its child running (sysproc.Wait kills only after success), so the test
// does the cleanup.
func killLeftoverChildOnCleanup(t *testing.T, pidFile string) {
	t.Helper()
	t.Cleanup(func() {
		raw, err := os.ReadFile(pidFile)
		if err != nil {
			return
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
}

// assertLeftoverChildGone reads the PID that a command wrote to pidFile and waits for that process
// to be killed.
func assertLeftoverChildGone(t *testing.T, pidFile string) {
	t.Helper()
	raw, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("leftover background child %d still alive", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestExecSucceedsWithBackgroundChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	o := newTestExec(t, map[string]any{
		"command": `sleep 30 & echo $! > ` + pidFile + `; printf '%s' '[{"fingerprint":"a","title":"ok"}]'`,
	}, nil)
	start := time.Now()
	obs, err := o.Poll(context.Background(), time.Now())
	require.NoError(t, err, "a 0 exit with a leftover child must not be a failed poll")
	assert.Less(t, time.Since(start), 3*time.Second)
	require.Len(t, obs, 1)
	assert.Equal(t, "ok", obs[0].Title)
	assertLeftoverChildGone(t, pidFile)
}

func TestExecNonZeroExitWithBackgroundChildIsError(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	o := newTestExec(t, map[string]any{
		"command": `sleep 30 & echo $! > ` + pidFile + `; echo ok; exit 3`,
	}, nil)
	_, err := o.Poll(context.Background(), time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit status 3")
	assert.Contains(t, err.Error(), "ok")
	killLeftoverChildOnCleanup(t, pidFile)
}
