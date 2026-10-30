// Package claude runs Claude Code non-interactively (claude -p with stream-json input and output) as a
// onett harness of type "claude-code".
package claude

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/bobbyrc/onett/internal/harness"
)

const harnessType = "claude-code"

// defaultPermissionMode is used when neither the config nor the agent profile chooses one.
// Claude Code's auto mode reviews risky actions; its refusals surface as denials.
const defaultPermissionMode = "auto"

func init() {
	harness.Register(harnessType, func(cfg harness.Config) (harness.Harness, error) {
		return &runner{cfg: cfg}, nil
	})
}

// runner starts Claude Code sessions for one configured harness.
type runner struct {
	cfg harness.Config
}

// session is a running Claude Code process that takes more user messages on stdin.
type session struct {
	*harness.Proc
}

var _ harness.Steerable = (*session)(nil)

// Send writes one user message in stream-json input form. Claude reads it at its next step
// (inside the current turn). The harness closes stdin on every result line, so a Send after
// that returns harness.ErrInputClosed.
func (s *session) Send(text string) error {
	line, err := userLine(text)
	if err != nil {
		return err
	}
	return s.WriteStdin(line)
}

// userLine encodes text as one stream-json input line.
func userLine(text string) ([]byte, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	b, err := json.Marshal(struct {
		Type    string `json:"type"`
		Message msg    `json:"message"`
	}{"user", msg{"user", text}})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Start runs one Claude Code session. The prompt goes to stdin as the first stream-json input
// line, and stdin stays open until the first result line so Send can add messages.
func (r *runner) Start(ctx context.Context, spec harness.Spec) (harness.Session, error) {
	ps := newParser(spec.Model)
	first, err := userLine(spec.Prompt)
	if err != nil {
		return nil, err
	}
	var proc *harness.Proc
	// ready is closed once proc is assigned; the parser goroutine can start before that.
	ready := make(chan struct{})
	defer close(ready)
	ps.onResult = func() {
		// Claude waits for more input after each result and exits only when stdin closes.
		<-ready
		proc.CloseStdin()
	}
	p, err := harness.StartProcess(ctx, harness.ProcSpec{
		Command:       buildArgs(r.cfg, spec),
		Dir:           spec.Workdir,
		Env:           mergeEnv(r.cfg.Env, spec.Env),
		InheritEnv:    r.cfg.InheritEnv,
		Stdin:         string(first),
		KeepStdinOpen: true,
		ParseLine:     ps.parseLine,
		// proc is assigned before StartProcess returns, and OnExit only runs from Wait.
		OnExit: func(exitCode int, _ error) (harness.Result, error) {
			var tail []string
			if proc != nil {
				tail = proc.StderrTail()
			}
			return ps.result(exitCode, tail), nil
		},
	})
	if err != nil {
		return nil, err
	}
	proc = p
	return &session{Proc: p}, nil
}

// buildArgs returns the full command line: the binary, the fixed flags, the config and profile
// extras, and the default permission mode when no permission flag was given.
func buildArgs(cfg harness.Config, spec harness.Spec) []string {
	args := []string{"claude"}
	if len(cfg.Command) > 0 {
		args = append([]string(nil), cfg.Command...)
	}
	args = append(args, "-p", "--output-format", "stream-json", "--input-format", "stream-json", "--verbose")
	if spec.Instructions != "" {
		args = append(args, "--append-system-prompt", spec.Instructions)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	if spec.ResumeSessionID != "" {
		args = append(args, "--resume", spec.ResumeSessionID)
	}
	args = append(args, cfg.Args...)
	args = append(args, spec.Args...)
	if !hasPermissionFlag(cfg.Args, spec.Args) {
		args = append(args, "--permission-mode", defaultPermissionMode)
	}
	return args
}

// hasPermissionFlag reports whether any extra argument already chooses a permission mode.
func hasPermissionFlag(lists ...[]string) bool {
	for _, list := range lists {
		for _, a := range list {
			if a == "--permission-mode" || strings.HasPrefix(a, "--permission-mode=") ||
				a == "--dangerously-skip-permissions" {
				return true
			}
		}
	}
	return false
}

// mergeEnv returns the union of the maps, with later maps winning.
func mergeEnv(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
