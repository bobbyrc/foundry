package observe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/sysproc"
)

// outputTail is how many trailing bytes of a failed command's output go into its error.
const outputTail = 2 << 10

func init() {
	Register("exec", newExecObserver)
}

// execObserver runs a shell command that prints the production signals it sees.
type execObserver struct {
	name    string
	command string
	env     []string // cfg.Env as sorted KEY=value pairs
	timeout time.Duration
}

// newExecObserver builds an "exec" observer from its options: "command" (required) and "timeout"
// (default 60s).
func newExecObserver(cfg Config) (Observer, error) {
	command := optString(cfg.Options, "command")
	if command == "" {
		return nil, fmt.Errorf("observer %q: option \"command\" is required", cfg.Name)
	}
	timeout, err := optDuration(cfg.Options, "timeout", 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("observer %q: %w", cfg.Name, err)
	}
	return &execObserver{
		name:    cfg.Name,
		command: command,
		env:     envList(cfg.Env),
		timeout: timeout,
	}, nil
}

// Poll runs the command with ONETT_SINCE set to since (RFC 3339, UTC). Stdout is a JSON array of
// SignalObservation or JSON Lines, one per line. Observations without a fingerprint are dropped.
func (o *execObserver) Poll(ctx context.Context, since time.Time) ([]domain.SignalObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "sh", "-c", o.command)
	cmd.Env = append(append(os.Environ(), o.env...), "ONETT_SINCE="+since.UTC().Format(time.RFC3339))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// On timeout the whole process group is killed, not just sh, and Wait does not block on pipes a
	// grandchild still holds open. A command that exits 0 but leaves a background child behind still
	// succeeds (see sysproc.Wait).
	sysproc.Configure(cmd)
	err := cmd.Start()
	if err == nil {
		err = sysproc.Wait(cmd)
	}
	if err != nil {
		output := strings.TrimSpace(stdout.String() + stderr.String())
		return nil, fmt.Errorf("observer %q: command failed: %w: output: %s", o.name, err, tail(output))
	}

	obs, err := parseObservations(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("observer %q: %w", o.name, err)
	}
	kept := obs[:0]
	for _, ob := range obs {
		if ob.Fingerprint != "" {
			kept = append(kept, ob)
		}
	}
	return kept, nil
}

// parseObservations decodes a JSON array, or JSON Lines when the first non-space byte is not '['.
// Empty output yields no observations.
func parseObservations(out []byte) ([]domain.SignalObservation, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var obs []domain.SignalObservation
		if err := json.Unmarshal(trimmed, &obs); err != nil {
			return nil, fmt.Errorf("decode JSON array: %w", err)
		}
		return obs, nil
	}

	var obs []domain.SignalObservation
	sc := bufio.NewScanner(bytes.NewReader(trimmed))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ob domain.SignalObservation
		if err := json.Unmarshal(line, &ob); err != nil {
			return nil, fmt.Errorf("decode JSON Lines line %d: %w", n, err)
		}
		obs = append(obs, ob)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read JSON Lines: %w", err)
	}
	return obs, nil
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
