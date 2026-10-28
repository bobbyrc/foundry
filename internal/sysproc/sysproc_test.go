package sysproc

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alive reports whether a process with pid still exists. Signal 0 checks existence only.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// waitGone polls until pid no longer exists or the timeout passes.
func waitGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !alive(pid)
}

func TestKillGroupKillsGrandchild(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "sleep 30 & echo $!; wait")
	Configure(cmd)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = KillGroup(cmd.Process.Pid, syscall.SIGKILL) })

	var buf [64]byte
	n, err := out.Read(buf[:])
	require.NoError(t, err)
	grandchild, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	require.NoError(t, err)
	require.True(t, alive(grandchild))

	require.NoError(t, KillGroup(cmd.Process.Pid, syscall.SIGKILL))
	_ = cmd.Wait()
	assert.True(t, waitGone(grandchild, 2*time.Second), "the grandchild must die with the group")
}

func TestKillGroupMissingGroupIsNotAnError(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "true")
	Configure(cmd)
	require.NoError(t, cmd.Run())
	assert.NoError(t, KillGroup(cmd.Process.Pid, syscall.SIGTERM))
}

func TestKillGroupRejectsInvalidPid(t *testing.T) {
	assert.Error(t, KillGroup(0, syscall.SIGKILL), "pid 0 would signal foundry's own group")
	assert.Error(t, KillGroup(-5, syscall.SIGKILL))
}

func TestConfigureCancelKillsWholeTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & echo $!; sleep 30")
	Configure(cmd)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	var buf [64]byte
	n, err := out.Read(buf[:])
	require.NoError(t, err)
	grandchild, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	require.NoError(t, err)

	cancel()
	start := time.Now()
	_ = cmd.Wait()
	assert.Less(t, time.Since(start), 3*time.Second)
	assert.True(t, waitGone(grandchild, 2*time.Second))
}

func TestConfigureSetsGroupAndWaitDelay(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "true")
	Configure(cmd)
	require.NotNil(t, cmd.SysProcAttr)
	assert.True(t, cmd.SysProcAttr.Setpgid)
	assert.NotNil(t, cmd.Cancel)
	assert.Equal(t, waitDelay, cmd.WaitDelay)
}

func TestAgentEnvDropsForeignCredentials(t *testing.T) {
	t.Setenv("GH_TOKEN", "gh-secret")
	t.Setenv("GITHUB_TOKEN", "gh-secret-2")
	t.Setenv("SENTRY_AUTH_TOKEN", "sentry-secret")

	env := AgentEnv(nil, nil)
	for _, kv := range env {
		assert.False(t, strings.HasPrefix(kv, "GH_TOKEN="), kv)
		assert.False(t, strings.HasPrefix(kv, "GITHUB_TOKEN="), kv)
		assert.False(t, strings.HasPrefix(kv, "SENTRY_AUTH_TOKEN="), kv)
	}
}

func TestAgentEnvKeepsBaseVariables(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", "/home/tester")
	t.Setenv("MISE_DATA_DIR", "/mise")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")

	env := AgentEnv(nil, nil)
	assert.Contains(t, env, "PATH=/usr/bin:/bin")
	assert.Contains(t, env, "HOME=/home/tester")
	assert.Contains(t, env, "MISE_DATA_DIR=/mise")
	assert.Contains(t, env, "CLAUDE_CODE_USE_BEDROCK=1")
}

func TestAgentEnvPassesInheritedName(t *testing.T) {
	t.Setenv("FOUNDRY_TEST_INHERIT", "yes")
	assert.Contains(t, AgentEnv([]string{"FOUNDRY_TEST_INHERIT"}, nil), "FOUNDRY_TEST_INHERIT=yes")
	assert.NotContains(t, AgentEnv(nil, nil), "FOUNDRY_TEST_INHERIT=yes")
}

func TestAgentEnvInheritCanReleaseWithheldName(t *testing.T) {
	t.Setenv("GH_TOKEN", "gh-secret")
	assert.Contains(t, AgentEnv([]string{"GH_TOKEN"}, nil), "GH_TOKEN=gh-secret")
}

func TestAgentEnvExtraOverrides(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("FOUNDRY_TEST_INHERIT", "from-foundry")

	env := AgentEnv([]string{"FOUNDRY_TEST_INHERIT"}, map[string]string{
		"PATH":                  "/custom",
		"FOUNDRY_TEST_INHERIT":  "from-extra",
		"FOUNDRY_TEST_NEW_ONLY": "x",
	})
	assert.Contains(t, env, "PATH=/custom")
	assert.Contains(t, env, "FOUNDRY_TEST_INHERIT=from-extra")
	assert.Contains(t, env, "FOUNDRY_TEST_NEW_ONLY=x")
	assert.NotContains(t, env, "PATH=/usr/bin")
	assert.NotContains(t, env, "FOUNDRY_TEST_INHERIT=from-foundry")
}

func TestAgentEnvIsSorted(t *testing.T) {
	env := AgentEnv(nil, map[string]string{"ZZZ_TEST": "1", "AAA_TEST": "2"})
	for i := 1; i < len(env); i++ {
		assert.LessOrEqual(t, env[i-1], env[i])
	}
}

func TestWaitSucceedsWhenBackgroundChildHoldsOutput(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "sleep 30 & echo $! > "+pidFile+"; echo ok")
	Configure(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out

	start := time.Now()
	require.NoError(t, cmd.Start())
	require.NoError(t, Wait(cmd), "a 0 exit must succeed even though a child holds stdout")
	assert.Less(t, time.Since(start), 3*time.Second)
	assert.Equal(t, "ok", strings.TrimSpace(out.String()))

	raw, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	grandchild, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	assert.True(t, waitGone(grandchild, 2*time.Second), "the leftover child must be killed")
}

func TestWaitNonZeroExitWithBackgroundChildIsError(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "sleep 30 & echo $! > "+pidFile+"; exit 3")
	Configure(cmd)
	require.NoError(t, cmd.Start())
	err := Wait(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit status 3")

	raw, readErr := os.ReadFile(pidFile)
	require.NoError(t, readErr)
	grandchild, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, convErr)
	t.Cleanup(func() { _ = KillGroup(cmd.Process.Pid, syscall.SIGKILL) })
	assert.True(t, waitGone(grandchild, 2*time.Second), "the leftover child must be killed after a non-zero exit")
}

func TestWaitTimeoutWithBackgroundChildIsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & sleep 30")
	Configure(cmd)
	require.NoError(t, cmd.Start())
	start := time.Now()
	require.Error(t, Wait(cmd), "a timed-out command must stay an error")
	assert.Less(t, time.Since(start), 4*time.Second)
}
