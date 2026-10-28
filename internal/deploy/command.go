package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// outputTail is how many trailing bytes of a failed command's output go into its error.
const outputTail = 2 << 10

func init() {
	Register("command", newCommandDeployer)
}

// commandDeployer runs user-supplied shell commands for deploy, status and rollback. It is the
// escape hatch for any platform: the command does the platform work and reports back on stdout.
type commandDeployer struct {
	dir         string
	env         []string // cfg.Env as sorted KEY=value pairs
	deployCmd   string
	statusCmd   string
	rollbackCmd string
	timeout     time.Duration

	mu       sync.Mutex
	statuses map[string]Status // deployments made without a status command, by ID
}

// newCommandDeployer builds a "command" deployer from its options: "deploy" (required),
// "status", "rollback" and "timeout" (default 30m, applied to each command).
func newCommandDeployer(cfg Config) (Deployer, error) {
	deployCmd := optString(cfg.Options, "deploy")
	if deployCmd == "" {
		return nil, errors.New(`deploy command: option "deploy" is required`)
	}
	timeout, err := optDuration(cfg.Options, "timeout", 30*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("deploy command: %w", err)
	}
	return &commandDeployer{
		dir:         cfg.Dir,
		env:         envList(cfg.Env),
		deployCmd:   deployCmd,
		statusCmd:   optString(cfg.Options, "status"),
		rollbackCmd: optString(cfg.Options, "rollback"),
		timeout:     timeout,
		statuses:    map[string]Status{},
	}, nil
}

// Deploy runs the deploy command with FOUNDRY_REF set. The last stdout line may be a JSON object
// {"id","url","state"} or a bare URL; without either the ID is generated. Without a status command
// the deploy is treated as synchronous and recorded as ready.
func (d *commandDeployer) Deploy(ctx context.Context, ref string) (Handle, error) {
	out, err := d.run(ctx, "deploy", d.deployCmd, "FOUNDRY_REF="+ref)
	if err != nil {
		return Handle{}, err
	}
	res := parseDeployLine(lastLine(out))
	id := res.ID
	if id == "" {
		id = fmt.Sprintf("cmd-%s-%d", prefix(ref, 8), time.Now().Unix())
	}
	if d.statusCmd == "" {
		d.mu.Lock()
		d.statuses[id] = Status{State: StateReady, URL: res.URL}
		d.mu.Unlock()
	}
	return Handle{ID: id, URL: res.URL}, nil
}

// Status runs the status command with FOUNDRY_DEPLOY_ID set, or without one returns the remembered
// status of a deployment made by this process (unknown IDs count as ready).
func (d *commandDeployer) Status(ctx context.Context, id string) (Status, error) {
	if d.statusCmd == "" {
		d.mu.Lock()
		defer d.mu.Unlock()
		if st, ok := d.statuses[id]; ok {
			return st, nil
		}
		return Status{State: StateReady}, nil
	}
	out, err := d.run(ctx, "status", d.statusCmd, "FOUNDRY_DEPLOY_ID="+id)
	if err != nil {
		return Status{}, err
	}
	return parseStatusLine(lastLine(out))
}

// Rollback runs the rollback command with FOUNDRY_ROLLBACK_ID and FOUNDRY_ROLLBACK_REF set.
func (d *commandDeployer) Rollback(ctx context.Context, id, ref string) error {
	if d.rollbackCmd == "" {
		return errors.New("deploy: no rollback command configured")
	}
	_, err := d.run(ctx, "rollback", d.rollbackCmd, "FOUNDRY_ROLLBACK_ID="+id, "FOUNDRY_ROLLBACK_REF="+ref)
	return err
}

// run executes command with sh -c in the project directory and returns its stdout. The environment
// is the process environment, then the project's env, then extra, so extra wins on conflicts. A
// non-zero exit or timeout is an error that carries the tail of the command's output.
func (d *commandDeployer) run(ctx context.Context, label, command string, extra ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = d.dir
	cmd.Env = append(append(os.Environ(), d.env...), extra...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// On timeout kill the whole process group, not just sh: a grandchild (e.g. sleep) would
	// otherwise hold the output pipes open. WaitDelay is the backstop for a grandchild that left
	// the group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(stdout.String() + stderr.String())
		return "", fmt.Errorf("deploy: %s command failed: %w: output: %s", label, err, tail(output))
	}
	return stdout.String(), nil
}

type deployLine struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	State string `json:"state"`
}

// parseDeployLine reads the last line of deploy output: a JSON object, or a bare http(s) URL.
// Anything else yields an empty result.
func parseDeployLine(line string) deployLine {
	if strings.HasPrefix(line, "{") {
		var res deployLine
		if err := json.Unmarshal([]byte(line), &res); err == nil {
			return res
		}
	}
	if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
		return deployLine{URL: line}
	}
	return deployLine{}
}

type statusLine struct {
	State   State  `json:"state"`
	URL     string `json:"url"`
	Message string `json:"message"`
}

// parseStatusLine reads the last line of status output: a JSON object or a bare state word.
func parseStatusLine(line string) (Status, error) {
	if line == "" {
		return Status{}, errors.New("deploy: status command printed no output")
	}
	if strings.HasPrefix(line, "{") {
		var res statusLine
		if err := json.Unmarshal([]byte(line), &res); err != nil {
			return Status{}, fmt.Errorf("deploy: status command printed invalid JSON: %w", err)
		}
		if !validState(res.State) {
			return Status{}, fmt.Errorf("deploy: status command printed unknown state %q", res.State)
		}
		return Status{State: res.State, URL: res.URL, Message: res.Message}, nil
	}
	state := State(line)
	if !validState(state) {
		return Status{}, fmt.Errorf("deploy: status command printed unrecognised output %q", line)
	}
	return Status{State: state}, nil
}

func validState(s State) bool {
	switch s {
	case StatePending, StateBuilding, StateReady, StateError, StateCanceled:
		return true
	}
	return false
}

// lastLine returns the last non-empty line of out, trimmed, or "" if there is none.
func lastLine(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// envList turns an env map into sorted KEY=value pairs, so the child environment is deterministic.
func envList(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

// tail returns the last outputTail bytes of s, starting on a rune boundary.
func tail(s string) string {
	if len(s) <= outputTail {
		return s
	}
	i := len(s) - outputTail
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return "..." + s[i:]
}

func prefix(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

// optString returns opts[key] formatted with fmt.Sprint, or "" when the key is absent or nil.
func optString(opts map[string]any, key string) string {
	v, ok := opts[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// optDuration parses opts[key] as a positive time.Duration, returning def when the key is absent.
func optDuration(opts map[string]any, key string, def time.Duration) (time.Duration, error) {
	s := optString(opts, key)
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("option %q: %w", key, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("option %q: must be positive, got %s", key, s)
	}
	return d, nil
}
