package notify

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

// newExec builds an exec notifier through the registry with the given options.
func newExec(opts map[string]any) (Notifier, error) {
	return New(Config{Type: "exec", Options: opts})
}

func TestExecNotifyPassesEnvironment(t *testing.T) {
	out := filepath.Join(t.TempDir(), "env.txt")
	command := `printf '%s|%s|%s|%s' "$ONETT_NOTIFY_TITLE" "$ONETT_NOTIFY_BODY" "$ONETT_NOTIFY_PRIORITY" "$ONETT_NOTIFY_URL" > '` + out + `'`

	n, err := New(Config{Type: "exec", Options: map[string]any{"command": command}})
	require.NoError(t, err)

	err = n.Notify(context.Background(), Notification{
		Title:    "Deploy",
		Body:     "v1 is live",
		Priority: PriorityHigh,
		URL:      "https://onett.local/x",
	})
	require.NoError(t, err)

	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "Deploy|v1 is live|high|https://onett.local/x", string(got))
}

func TestExecNotifyDefaultsPriorityToNormal(t *testing.T) {
	out := filepath.Join(t.TempDir(), "prio.txt")
	command := `printf '%s' "$ONETT_NOTIFY_PRIORITY" > '` + out + `'`

	n, err := newExec(map[string]any{"command": command})
	require.NoError(t, err)
	require.NoError(t, n.Notify(context.Background(), Notification{Title: "t"}))

	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "normal", string(got))
}

func TestExecTimeoutKillsProcessGroup(t *testing.T) {
	old := execTimeout
	execTimeout = 200 * time.Millisecond
	t.Cleanup(func() { execTimeout = old })

	// The forked sleep must die with its shell, or the pipe stays open and Notify hangs.
	n, err := newExec(map[string]any{"command": "sleep 30 & sleep 30"})
	require.NoError(t, err)
	start := time.Now()
	err = n.Notify(context.Background(), Notification{Title: "t"})
	require.Error(t, err)
	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestExecNotifyNonZeroExitIsError(t *testing.T) {
	n, err := newExec(map[string]any{"command": "echo kaboom >&2; exit 3"})
	require.NoError(t, err)

	err = n.Notify(context.Background(), Notification{Title: "t"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kaboom")
	assert.Contains(t, err.Error(), "exit status 3")
}

func TestExecOptionValidation(t *testing.T) {
	_, err := New(Config{Type: "exec", Options: map[string]any{}})
	require.Error(t, err)

	_, err = New(Config{Type: "exec", Options: map[string]any{"command": ""}})
	require.Error(t, err)
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

func TestExecNotifySucceedsWithBackgroundChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	n, err := newExec(map[string]any{"command": "sleep 30 & echo $! > " + pidFile + "; echo ok"})
	require.NoError(t, err)
	start := time.Now()
	err = n.Notify(context.Background(), Notification{Title: "t"})
	require.NoError(t, err, "a 0 exit with a leftover child must not be a failed notification")
	assert.Less(t, time.Since(start), 3*time.Second)
	assertLeftoverChildGone(t, pidFile)
}

func TestExecNotifyNonZeroExitWithBackgroundChildIsError(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	n, err := newExec(map[string]any{"command": "sleep 30 & echo $! > " + pidFile + "; echo ok; exit 3"})
	require.NoError(t, err)
	err = n.Notify(context.Background(), Notification{Title: "t"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit status 3")
	assert.Contains(t, err.Error(), "ok")
	killLeftoverChildOnCleanup(t, pidFile)
}
