package deploy

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

func newTestCommand(t *testing.T, opts map[string]any, env map[string]string) Deployer {
	t.Helper()
	d, err := New(Config{Type: "command", Options: opts, Dir: t.TempDir(), Env: env})
	require.NoError(t, err)
	return d
}

func TestCommandDeployJSONLine(t *testing.T) {
	d := newTestCommand(t, map[string]any{
		"deploy": `echo building; echo '{"id":"dpl_1","url":"https://x.example","state":"ready"}'`,
	}, nil)
	h, err := d.Deploy(context.Background(), "abc123")
	require.NoError(t, err)
	assert.Equal(t, "dpl_1", h.ID)
	assert.Equal(t, "https://x.example", h.URL)
}

func TestCommandDeployURLLine(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo "https://preview.example"`}, nil)
	h, err := d.Deploy(context.Background(), "abcdef1234567890")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(h.ID, "cmd-abcdef12-"), h.ID)
	assert.Equal(t, "https://preview.example", h.URL)
}

func TestCommandDeployPlainOutput(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo done`}, nil)
	h, err := d.Deploy(context.Background(), "abc")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(h.ID, "cmd-abc-"), h.ID)
	assert.Empty(t, h.URL)
}

func TestCommandDeployEnvReachesScript(t *testing.T) {
	t.Setenv("FOUNDRY_TEST_PARENT", "parent")
	d := newTestCommand(t, map[string]any{
		"deploy": `echo "https://$FOUNDRY_REF.example/$FOUNDRY_TEST_CFG/$FOUNDRY_TEST_PARENT"`,
	}, map[string]string{"FOUNDRY_TEST_CFG": "cfg"})
	h, err := d.Deploy(context.Background(), "sha1")
	require.NoError(t, err)
	assert.Equal(t, "https://sha1.example/cfg/parent", h.URL)
}

func TestCommandDeployRunsInDir(t *testing.T) {
	dir := t.TempDir()
	d, err := New(Config{Type: "command", Dir: dir, Options: map[string]any{
		"deploy": "pwd -P > where.txt",
	}})
	require.NoError(t, err)
	_, err = d.Deploy(context.Background(), "sha")
	require.NoError(t, err)

	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(dir, "where.txt"))
	require.NoError(t, err)
	assert.Equal(t, resolved, strings.TrimSpace(string(got)))
}

func TestCommandDeployNonZeroExit(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo partial; echo boom >&2; exit 3`}, nil)
	_, err := d.Deploy(context.Background(), "sha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit status 3")
	assert.Contains(t, err.Error(), "partial")
	assert.Contains(t, err.Error(), "boom")
}

func TestCommandDeployCapturesOutput(t *testing.T) {
	// stdout and stderr both land in Output (their relative order is not guaranteed).
	d := newTestCommand(t, map[string]any{
		"deploy": `echo "uploading assets"; echo "warning: slow" >&2; echo "built ok"; echo '{"id":"dpl_9","url":"https://x.example","state":"ready"}'`,
	}, nil)
	h, err := d.Deploy(context.Background(), "sha")
	require.NoError(t, err)
	assert.Equal(t, "dpl_9", h.ID)
	assert.Equal(t, "https://x.example", h.URL)
	for _, line := range []string{"uploading assets", "warning: slow", "built ok"} {
		assert.Contains(t, h.Output, line+"\n")
	}
	assert.Contains(t, h.Output, `{"id":"dpl_9","url":"https://x.example","state":"ready"}`+"\n")
}

func TestCommandDeployOutputKeepsLastFourKilobytes(t *testing.T) {
	// 10,000 numbered lines (about 60KB): Output is the newest 4KB and ends with the last line.
	d := newTestCommand(t, map[string]any{
		"deploy": `i=0; while [ $i -lt 10000 ]; do echo "line $i"; i=$((i+1)); done; echo "https://last.example"`,
	}, nil)
	h, err := d.Deploy(context.Background(), "sha")
	require.NoError(t, err)
	assert.Equal(t, "https://last.example", h.URL)
	assert.LessOrEqual(t, len(h.Output), deployOutputTail)
	assert.True(t, strings.HasSuffix(h.Output, "line 9999\nhttps://last.example\n"), h.Output[len(h.Output)-40:])
	assert.NotContains(t, h.Output, "line 100\n")
}

func TestCommandDeployNoOutputIsEmpty(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `true`}, nil)
	h, err := d.Deploy(context.Background(), "sha")
	require.NoError(t, err)
	assert.Empty(t, h.Output)
}

func TestCommandStatusJSON(t *testing.T) {
	d := newTestCommand(t, map[string]any{
		"deploy": `echo https://ignored.example`,
		"status": `echo "{\"state\":\"building\",\"url\":\"https://$FOUNDRY_DEPLOY_ID.example\",\"message\":\"compiling\"}"`,
	}, nil)
	st, err := d.Status(context.Background(), "dpl_9")
	require.NoError(t, err)
	assert.Equal(t, StateBuilding, st.State)
	assert.Equal(t, "https://dpl_9.example", st.URL)
	assert.Equal(t, "compiling", st.Message)
}

func TestCommandStatusBareWord(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo x`, "status": `echo warming; echo error`}, nil)
	st, err := d.Status(context.Background(), "id")
	require.NoError(t, err)
	assert.Equal(t, StateError, st.State)
}

func TestCommandStatusUnrecognised(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo x`, "status": `echo bogus`}, nil)
	_, err := d.Status(context.Background(), "id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")

	empty := newTestCommand(t, map[string]any{"deploy": `echo x`, "status": `true`}, nil)
	_, err = empty.Status(context.Background(), "id")
	require.Error(t, err)
}

func TestCommandStatusNonZeroExit(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo x`, "status": `exit 1`}, nil)
	_, err := d.Status(context.Background(), "id")
	require.Error(t, err)
}

func TestCommandRememberedStatus(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo "https://site.example"`}, nil)
	h, err := d.Deploy(context.Background(), "sha")
	require.NoError(t, err)

	st, err := d.Status(context.Background(), h.ID)
	require.NoError(t, err)
	assert.Equal(t, StateReady, st.State)
	assert.Equal(t, "https://site.example", st.URL)

	unknown, err := d.Status(context.Background(), "never-seen")
	require.NoError(t, err)
	assert.Equal(t, StateReady, unknown.State)
	assert.Empty(t, unknown.URL)
}

func TestCommandRollbackWithoutCommand(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo x`}, nil)
	err := d.Rollback(context.Background(), "id", "ref")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no rollback command configured")
}

func TestCommandRollbackRunsWithEnv(t *testing.T) {
	dir := t.TempDir()
	d, err := New(Config{Type: "command", Dir: dir, Options: map[string]any{
		"deploy":   "echo x",
		"rollback": `printf '%s %s' "$FOUNDRY_ROLLBACK_ID" "$FOUNDRY_ROLLBACK_REF" > rb.txt`,
	}})
	require.NoError(t, err)
	require.NoError(t, d.Rollback(context.Background(), "dpl_old", "prev-sha"))
	got, err := os.ReadFile(filepath.Join(dir, "rb.txt"))
	require.NoError(t, err)
	assert.Equal(t, "dpl_old prev-sha", string(got))
}

func TestCommandRollbackNonZeroExit(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo x`, "rollback": `echo nope >&2; exit 4`}, nil)
	err := d.Rollback(context.Background(), "id", "ref")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope")
}

func TestCommandTimeoutKillsCommand(t *testing.T) {
	// The forked sleep must die with its shell: a timeout that kills only sh leaves the pipe open.
	d := newTestCommand(t, map[string]any{"deploy": `sleep 30 & sleep 30`, "timeout": "200ms"}, nil)
	start := time.Now()
	_, err := d.Deploy(context.Background(), "sha")
	require.Error(t, err)
	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestCommandFactoryErrors(t *testing.T) {
	_, err := New(Config{Type: "command", Options: map[string]any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"deploy" is required`)

	_, err = New(Config{Type: "command", Options: map[string]any{"deploy": "x", "timeout": "soon"}})
	require.Error(t, err)

	_, err = New(Config{Type: "command", Options: map[string]any{"deploy": "x", "timeout": "-1s"}})
	require.Error(t, err)
}

func TestOptHelpers(t *testing.T) {
	assert.Equal(t, "", optString(map[string]any{}, "k"))
	assert.Equal(t, "42", optString(map[string]any{"k": 42}, "k"))

	d, err := optDuration(map[string]any{}, "k", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, time.Minute, d)
}

// killLeftoverChildOnCleanup kills the background child whose PID a command wrote to pidFile, so a
// test that leaves a child running (a successful deploy keeps its services) does not leak it.
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

// deployedChildPID reads the PID a deploy wrote to pidFile and registers its cleanup.
func deployedChildPID(t *testing.T, pidFile string) int {
	t.Helper()
	killLeftoverChildOnCleanup(t, pidFile)
	raw, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	return pid
}

// processAlive reports whether a process with pid exists (signal 0 checks existence only).
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func TestCommandDeploySucceedsAndKeepsBackgroundService(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	d := newTestCommand(t, map[string]any{
		"deploy": "sleep 30 >/dev/null 2>&1 & echo $! > " + pidFile + "; echo https://ok.example",
	}, nil)
	start := time.Now()
	h, err := d.Deploy(context.Background(), "sha")
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 3*time.Second)
	assert.Equal(t, "https://ok.example", h.URL)
	assert.True(t, processAlive(deployedChildPID(t, pidFile)), "a service started by a successful deploy must keep running")
}

func TestCommandDeploySucceedsAndKeepsBackgroundChildHoldingOutput(t *testing.T) {
	// The child inherits the output pipe, so the deploy returns only after sysproc's wait delay.
	pidFile := filepath.Join(t.TempDir(), "pid")
	d := newTestCommand(t, map[string]any{
		"deploy": "sleep 30 & echo $! > " + pidFile + "; echo https://ok.example",
	}, nil)
	start := time.Now()
	h, err := d.Deploy(context.Background(), "sha")
	require.NoError(t, err, "a 0 exit with a child holding the output must not be a failed deploy")
	assert.Less(t, time.Since(start), 3*time.Second)
	assert.Equal(t, "https://ok.example", h.URL)
	assert.True(t, processAlive(deployedChildPID(t, pidFile)), "a service started by a successful deploy must keep running")
}

func TestCommandDeployFailureKillsBackgroundChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	d := newTestCommand(t, map[string]any{
		"deploy": "sleep 30 >/dev/null 2>&1 & echo $! > " + pidFile + "; exit 3",
	}, nil)
	_, err := d.Deploy(context.Background(), "sha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit status 3")
	assertLeftoverChildGone(t, pidFile)
}

func TestCommandDeployTimeoutKillsBackgroundChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	d, err := New(Config{Type: "command", Dir: t.TempDir(), Options: map[string]any{
		"deploy":  "sleep 30 >/dev/null 2>&1 & echo $! > " + pidFile + "; sleep 30",
		"timeout": "1s",
	}})
	require.NoError(t, err)
	start := time.Now()
	_, err = d.Deploy(context.Background(), "sha")
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second)
	assertLeftoverChildGone(t, pidFile)
}

func TestCommandDeployNonZeroExitWithBackgroundChildIsError(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	d := newTestCommand(t, map[string]any{
		"deploy": "sleep 30 & echo $! > " + pidFile + "; echo ok; exit 3",
	}, nil)
	_, err := d.Deploy(context.Background(), "sha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit status 3")
	assert.Contains(t, err.Error(), "ok")
	killLeftoverChildOnCleanup(t, pidFile)
}

func TestCommandDeployBackgroundChildWritingOutputSurvives(t *testing.T) {
	// The child writes to the command's stdout (no redirect) for about 2s. A pipe would be closed
	// when the deploy returns, so the child would die of SIGPIPE on its next write. A file keeps it
	// alive, and the deploy returns without waiting for the child.
	pidFile := filepath.Join(t.TempDir(), "pid")
	d := newTestCommand(t, map[string]any{
		"deploy": "( for i in $(seq 20); do echo tick; sleep 0.1; done ) & echo $! > " + pidFile + "; echo https://ok.example",
	}, nil)
	start := time.Now()
	_, err := d.Deploy(context.Background(), "sha")
	require.NoError(t, err)
	assert.Less(t, time.Since(start), time.Second)

	pid := deployedChildPID(t, pidFile)
	time.Sleep(time.Second)
	assert.True(t, processAlive(pid), "a background child writing to the deploy's stdout must not be killed by SIGPIPE")
}
