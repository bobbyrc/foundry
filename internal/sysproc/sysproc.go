// Package sysproc holds the process-level rules every child process in foundry shares: run each
// command in its own process group so one kill reaches its whole tree, and build the environment
// an agent process may see.
package sysproc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
)

// waitDelay bounds how long Wait keeps reading output from a command after the command itself
// has exited or been killed. A background child can hold stdout open long after its parent is
// gone; without this bound Wait would block on it forever.
const waitDelay = 2 * time.Second

// Configure makes cmd run in its own process group, so that cancelling its context kills the
// command and every child it started, not just the shell. It also sets WaitDelay so that a
// process that leaves its output pipes open cannot hold Wait forever. cmd must be created with
// exec.CommandContext for the cancel behaviour to apply. Call Configure before cmd.Start.
func Configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return KillGroup(cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = waitDelay
}

// KillGroup sends sig to the process group whose ID is pid. The group must have been created
// with Setpgid, so its ID is the leader's PID. A group that no longer exists is not an error.
func KillGroup(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("kill process group: invalid pid %d", pid)
	}
	err := syscall.Kill(-pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// Wait waits for a cmd started with Start after Configure, then kills any process its group still
// holds: once the command has finished, a background child it left behind is no longer wanted,
// whatever the exit status. It differs from cmd.Wait in one more case: a command that exits 0 but
// leaves a child holding its stdout or stderr. Go reports that as exec.ErrWaitDelay once WaitDelay
// passes, which would turn a successful command into a failure, so Wait treats it as success. A
// non-zero exit, a signal or a timeout is returned unchanged as the error; a failed group kill
// never replaces it.
func Wait(cmd *exec.Cmd) error {
	err := cmd.Wait()
	var kerr error
	if cmd.Process != nil {
		kerr = KillGroup(cmd.Process.Pid, syscall.SIGKILL)
	}
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		return kerr
	}
	return err
}

// WaitKeepOnSuccess is Wait for a command whose successful run may start processes that should
// keep running, such as a deploy script that launches a service in the background. When the
// command exits 0 it returns nil and leaves the group alone, so those processes survive. That
// includes a background child that holds the output pipes: exec.ErrWaitDelay is success here too.
// When the command exits non-zero, is killed by a signal or times out, the group is killed as Wait
// does and the error is returned unchanged. Use it for commands whose leftovers are wanted after a
// success (deploy, rollback). Use Wait for everything else: a status check or notifier has no
// reason to leave a process running.
func WaitKeepOnSuccess(cmd *exec.Cmd) error {
	err := cmd.Wait()
	if cmd.ProcessState != nil && cmd.ProcessState.Success() && (err == nil || errors.Is(err, exec.ErrWaitDelay)) {
		return nil
	}
	if cmd.Process != nil {
		_ = KillGroup(cmd.Process.Pid, syscall.SIGKILL)
	}
	return err
}

// baseEnv is the allowlist of foundry's environment that agent processes receive. Everything
// else is withheld, so credentials foundry holds for itself (GH_TOKEN, GITHUB_TOKEN,
// SENTRY_AUTH_TOKEN, ...) never reach an agent that runs arbitrary commands.
var baseEnv = map[string]bool{
	// Locating binaries, user identity and locale.
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true,
	"LANG": true, "LC_ALL": true, "LC_CTYPE": true, "TERM": true, "TMPDIR": true, "TZ": true,
	"XDG_CONFIG_HOME": true, "XDG_CACHE_HOME": true, "XDG_DATA_HOME": true, "XDG_STATE_HOME": true,
	// Network access and TLS trust.
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true,
	"http_proxy": true, "https_proxy": true, "no_proxy": true,
	"SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
	// The agents' own authentication and configuration.
	"ANTHROPIC_API_KEY": true, "ANTHROPIC_BASE_URL": true, "CLAUDE_CONFIG_DIR": true,
	"OPENAI_API_KEY": true, "OPENAI_BASE_URL": true, "CODEX_HOME": true,
}

// basePrefixes admits every variable whose name starts with one of these: the agents' settings
// (CLAUDE_CODE_*) and the mise toolchain manager (MISE_*), which agents rely on for their tools.
var basePrefixes = []string{"CLAUDE_CODE_", "MISE_"}

// AgentEnv returns the environment for an agent process as KEY=value pairs, sorted by key. It
// starts from an allowlist of foundry's own environment (see baseEnv), adds each name in inherit
// when foundry has it set, then applies extra, which wins on conflicts.
//
// Why an allowlist and not a copy of os.Environ: an agent runs commands and reads files on the
// operator's machine, so anything in foundry's environment is in reach of the agent. Forwarding
// everything would hand it every token foundry holds. The inherit list lets a config opt a
// specific variable back in.
func AgentEnv(inherit []string, extra map[string]string) []string {
	want := make(map[string]string)
	for _, kv := range os.Environ() {
		key, val, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			continue
		}
		if baseEnv[key] || hasBasePrefix(key) || contains(inherit, key) {
			want[key] = val
		}
	}
	for k, v := range extra {
		want[k] = v
	}

	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+want[k])
	}
	return out
}

func hasBasePrefix(key string) bool {
	for _, p := range basePrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
