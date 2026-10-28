package notify

import (
	"context"
	"os"
	"path/filepath"
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
	command := `printf '%s|%s|%s|%s' "$FOUNDRY_NOTIFY_TITLE" "$FOUNDRY_NOTIFY_BODY" "$FOUNDRY_NOTIFY_PRIORITY" "$FOUNDRY_NOTIFY_URL" > '` + out + `'`

	n, err := New(Config{Type: "exec", Options: map[string]any{"command": command}})
	require.NoError(t, err)

	err = n.Notify(context.Background(), Notification{
		Title:    "Deploy",
		Body:     "v1 is live",
		Priority: PriorityHigh,
		URL:      "https://foundry.local/x",
	})
	require.NoError(t, err)

	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "Deploy|v1 is live|high|https://foundry.local/x", string(got))
}

func TestExecNotifyDefaultsPriorityToNormal(t *testing.T) {
	out := filepath.Join(t.TempDir(), "prio.txt")
	command := `printf '%s' "$FOUNDRY_NOTIFY_PRIORITY" > '` + out + `'`

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
