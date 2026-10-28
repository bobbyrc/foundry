// Package claude runs Claude Code non-interactively (claude -p --output-format stream-json) as a
// onlet harness of type "claude-code".
package claude

import (
	"context"
	"strings"

	"github.com/bobbyrc/onlet/internal/harness"
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

// Start runs one Claude Code session. The prompt is written to stdin.
func (r *runner) Start(ctx context.Context, spec harness.Spec) (harness.Session, error) {
	ps := newParser(spec.Model)
	var proc *harness.Proc
	p, err := harness.StartProcess(ctx, harness.ProcSpec{
		Command:    buildArgs(r.cfg, spec),
		Dir:        spec.Workdir,
		Env:        mergeEnv(r.cfg.Env, spec.Env),
		InheritEnv: r.cfg.InheritEnv,
		Stdin:      spec.Prompt,
		ParseLine:  ps.parseLine,
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
	return p, nil
}

// buildArgs returns the full command line: the binary, the fixed flags, the config and profile
// extras, and the default permission mode when no permission flag was given.
func buildArgs(cfg harness.Config, spec harness.Spec) []string {
	args := []string{"claude"}
	if len(cfg.Command) > 0 {
		args = append([]string(nil), cfg.Command...)
	}
	args = append(args, "-p", "--output-format", "stream-json", "--verbose")
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
