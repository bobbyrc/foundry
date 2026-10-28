package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bobbyrc/foundry/internal/sysproc"
)

// outputTail is how many trailing bytes of a failed command's output go into its error.
const outputTail = 2 << 10

// deployOutputTail is how many trailing bytes of a deploy command's output are kept on its Handle.
const deployOutputTail = 4 << 10

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
// the deploy is treated as synchronous and recorded as ready. The Handle carries the tail of the
// command's output (the tail of stdout, then stderr) as Output.
func (d *commandDeployer) Deploy(ctx context.Context, ref string) (Handle, error) {
	stdout, output, err := d.runFiles(ctx, "deploy", d.deployCmd, sysproc.WaitKeepOnSuccess, "FOUNDRY_REF="+ref)
	if err != nil {
		return Handle{}, err
	}
	res := parseDeployLine(lastLine(stdout))
	id := res.ID
	if id == "" {
		id = fmt.Sprintf("cmd-%s-%d", prefix(ref, 8), time.Now().Unix())
	}
	if d.statusCmd == "" {
		d.mu.Lock()
		d.statuses[id] = Status{State: StateReady, URL: res.URL}
		d.mu.Unlock()
	}
	return Handle{ID: id, URL: res.URL, Output: output}, nil
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
	out, _, err := d.run(ctx, "status", d.statusCmd, sysproc.Wait, "FOUNDRY_DEPLOY_ID="+id)
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
	_, _, err := d.runFiles(ctx, "rollback", d.rollbackCmd, sysproc.WaitKeepOnSuccess, "FOUNDRY_ROLLBACK_ID="+id, "FOUNDRY_ROLLBACK_REF="+ref)
	return err
}

// run executes command with sh -c in the project directory, reading its stdout and stderr through
// pipes. It is used by the status command only; deploy and rollback use runFiles. It returns the
// command's stdout (which the caller parses for its result) and the last deployOutputTail bytes of
// stdout and stderr combined. The environment is the process environment, then the project's env,
// then extra, so extra wins on conflicts. A non-zero exit or timeout is an error that carries the
// tail of the command's output. wait decides what happens to processes the command leaves behind
// (see sysproc.Wait).
func (d *commandDeployer) run(ctx context.Context, label, command string, wait func(*exec.Cmd) error, extra ...string) (stdout, output string, err error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	var out bytes.Buffer
	combined := &tailWriter{max: deployOutputTail}
	cmd := d.newCmd(ctx, command, extra)
	cmd.Stdout = io.MultiWriter(&out, combined)
	cmd.Stderr = combined
	err = cmd.Start()
	if err == nil {
		err = wait(cmd)
	}
	if err != nil {
		return "", "", fmt.Errorf("deploy: %s command failed: %w: output: %s", label, err, tail(strings.TrimSpace(combined.String())))
	}
	return out.String(), combined.String(), nil
}

// runFiles executes command like run, but sends its stdout and stderr to two temporary files
// instead of pipes, and waits with sysproc.WaitKeepOnSuccess. Why files: a pipe ties the command
// to foundry. Once a deploy returns, foundry stops reading the pipe, so a service the deploy left
// running gets SIGPIPE on its next write and dies. A file has no reader that can go away: a
// leftover process keeps a valid descriptor to an unlinked file and can write without blocking.
// foundry reads each file once, up to its size when the command exits, so later writes by
// leftover processes are never read. The files are removed before runFiles returns.
//
// It returns the command's stdout, which the caller parses (its last non-empty line is the
// result), and the last deployOutputTail bytes of stdout followed by stderr. Unlike the pipe
// version, the two streams are not interleaved in the order they were written.
func (d *commandDeployer) runFiles(ctx context.Context, label, command string, wait func(*exec.Cmd) error, extra ...string) (stdout, output string, err error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	outFile, err := os.CreateTemp("", "foundry-"+label+"-stdout-*")
	if err != nil {
		return "", "", fmt.Errorf("deploy: %s command: create stdout file: %w", label, err)
	}
	defer removeTemp(outFile)
	errFile, err := os.CreateTemp("", "foundry-"+label+"-stderr-*")
	if err != nil {
		return "", "", fmt.Errorf("deploy: %s command: create stderr file: %w", label, err)
	}
	defer removeTemp(errFile)

	cmd := d.newCmd(ctx, command, extra)
	cmd.Stdout = outFile
	cmd.Stderr = errFile
	err = cmd.Start()
	if err == nil {
		err = wait(cmd)
	}

	var outBuf bytes.Buffer
	combined := &tailWriter{max: deployOutputTail}
	readErr := readBack(outFile, io.MultiWriter(&outBuf, combined))
	if errR := readBack(errFile, combined); readErr == nil {
		readErr = errR
	}
	if err == nil {
		err = readErr
	}
	if err != nil {
		return "", "", fmt.Errorf("deploy: %s command failed: %w: output: %s", label, err, tail(strings.TrimSpace(combined.String())))
	}
	return outBuf.String(), combined.String(), nil
}

// newCmd builds the sh -c command for command in the project directory. Its environment is the
// process environment, then the project's env, then extra, so extra wins on conflicts. It is
// placed in its own process group (see sysproc.Configure), so a timeout kills the whole tree, not
// just sh, and the wait does not block on a grandchild that still holds output open. A deploy that
// exits 0 may leave a service running (see sysproc.WaitKeepOnSuccess).
func (d *commandDeployer) newCmd(ctx context.Context, command string, extra []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = d.dir
	cmd.Env = append(append(os.Environ(), d.env...), extra...)
	sysproc.Configure(cmd)
	return cmd
}

// readBack copies the bytes f held when the command exited into w. It reads with ReadAt, so the
// file offset that the command's processes share is not moved, and it stops at the size measured
// here, so output a leftover process writes afterwards cannot keep the read going.
func readBack(f *os.File, w io.Writer) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	_, err = io.Copy(w, io.NewSectionReader(f, 0, info.Size()))
	return err
}

// removeTemp closes and deletes a temporary output file. A process that still holds it keeps
// writing to the unlinked file, which the kernel frees when that process exits.
func removeTemp(f *os.File) {
	_ = f.Close()
	_ = os.Remove(f.Name())
}

// tailWriter keeps the last max bytes written to it, so a chatty command cannot grow foundry's
// memory. It is safe for the concurrent writes of a child's stdout and stderr.
type tailWriter struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > 2*w.max {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-w.max:]...)
	}
	return len(p), nil
}

// String returns the last max bytes written, starting on a rune boundary.
func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	b := w.buf
	if len(b) > w.max {
		b = b[len(b)-w.max:]
	}
	for len(b) > 0 && !utf8.RuneStart(b[0]) {
		b = b[1:]
	}
	return string(b)
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
