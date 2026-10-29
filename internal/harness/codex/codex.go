// Package codex runs OpenAI Codex non-interactively (codex exec --json) as an onett harness of
// type "codex".
package codex

import (
	"context"

	"github.com/bobbyrc/onett/internal/harness"
)

const harnessType = "codex"

// defaultSandboxArgs are used when no sandbox or approval flag was given. --approve-for-me alone
// implies the workspace-write sandbox and routes approval requests to automatic review. Codex
// 0.165 rejects it combined with --sandbox, so --sandbox must not be added here.
var defaultSandboxArgs = []string{"--approve-for-me"}

// instructionSeparator joins Spec.Instructions and Spec.Prompt on stdin, because codex has no
// flag for a system prompt.
const instructionSeparator = "\n\n---\n\n"

func init() {
	harness.Register(harnessType, func(cfg harness.Config) (harness.Harness, error) {
		return &runner{cfg: cfg}, nil
	})
}

// runner starts Codex sessions for one configured harness.
type runner struct {
	cfg harness.Config
}

// Start runs one codex exec session, or resumes one. The prompt is written to stdin.
func (r *runner) Start(ctx context.Context, spec harness.Spec) (harness.Session, error) {
	ps := newParser()
	var proc *harness.Proc
	p, err := harness.StartProcess(ctx, harness.ProcSpec{
		Command:    buildArgs(r.cfg, spec),
		Dir:        spec.Workdir,
		Env:        mergeEnv(r.cfg.Env, spec.Env),
		InheritEnv: r.cfg.InheritEnv,
		Stdin:      stdinText(spec),
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

// stdinText is the prompt as codex receives it: instructions first, when there are any.
func stdinText(spec harness.Spec) string {
	if spec.Instructions == "" {
		return spec.Prompt
	}
	return spec.Instructions + instructionSeparator + spec.Prompt
}

// buildArgs returns the command line. A new session gets -C; a resumed one does not, because
// `codex exec resume` has no -C, --sandbox or --approve-for-me flags. The sandbox default is
// therefore applied only to new sessions.
func buildArgs(cfg harness.Config, spec harness.Spec) []string {
	args := []string{"codex"}
	if len(cfg.Command) > 0 {
		args = append([]string(nil), cfg.Command...)
	}
	args = append(args, "exec")
	resume := spec.ResumeSessionID != ""
	if resume {
		args = append(args, "resume", spec.ResumeSessionID)
	}
	args = append(args, "--json", "--skip-git-repo-check")
	if !resume && spec.Workdir != "" {
		args = append(args, "-C", spec.Workdir)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	args = append(args, cfg.Args...)
	args = append(args, spec.Args...)
	if !resume && !hasSandboxFlag(cfg.Args, spec.Args) {
		args = append(args, defaultSandboxArgs...)
	}
	// "-" reads the prompt from stdin.
	return append(args, "-")
}

// hasSandboxFlag reports whether an extra argument already chooses a sandbox or approval mode.
func hasSandboxFlag(lists ...[]string) bool {
	for _, list := range lists {
		for _, a := range list {
			switch a {
			case "--sandbox", "-s", "--approve-for-me", "--full-auto",
				"--dangerously-bypass-approvals-and-sandbox":
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
