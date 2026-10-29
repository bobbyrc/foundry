// Package exec runs any external program as an onett harness of type "exec". The program
// receives the task through files and environment variables and reports through JSON lines on
// stdout; see docs/plugins.md for the protocol.
package exec

import (
	"context"
	"fmt"
	"os"

	"github.com/bobbyrc/onett/internal/harness"
)

const harnessType = "exec"

func init() {
	harness.Register(harnessType, func(cfg harness.Config) (harness.Harness, error) {
		if len(cfg.Command) == 0 {
			return nil, fmt.Errorf("harness %q: type %q needs a command", cfg.Name, harnessType)
		}
		return &runner{cfg: cfg}, nil
	})
}

// runner starts plugin processes for one configured harness.
type runner struct {
	cfg harness.Config
}

// Start runs the plugin. The prompt and instructions are written to temporary files whose paths
// are passed in ONETT_PROMPT_FILE and ONETT_INSTRUCTIONS_FILE; the prompt is also written to
// stdin. The files are removed when the process has exited and Wait has been called.
func (r *runner) Start(ctx context.Context, spec harness.Spec) (harness.Session, error) {
	promptPath, err := writeTemp("onett-prompt-*.txt", spec.Prompt)
	if err != nil {
		return nil, err
	}
	instructionsPath, err := writeTemp("onett-instructions-*.md", spec.Instructions)
	if err != nil {
		_ = os.Remove(promptPath)
		return nil, err
	}
	removeFiles := func() {
		_ = os.Remove(promptPath)
		_ = os.Remove(instructionsPath)
	}

	command := make([]string, 0, len(r.cfg.Command)+len(r.cfg.Args)+len(spec.Args))
	command = append(command, r.cfg.Command...)
	command = append(command, r.cfg.Args...)
	command = append(command, spec.Args...)

	env := mergeEnv(map[string]string{
		"ONETT_PROMPT_FILE":       promptPath,
		"ONETT_INSTRUCTIONS_FILE": instructionsPath,
		"ONETT_MODEL":             spec.Model,
		"ONETT_RESUME_SESSION":    spec.ResumeSessionID,
	}, r.cfg.Env, spec.Env)

	ps := newParser()
	p, err := harness.StartProcess(ctx, harness.ProcSpec{
		Command:    command,
		Dir:        spec.Workdir,
		Env:        env,
		InheritEnv: r.cfg.InheritEnv,
		Stdin:      spec.Prompt,
		ParseLine:  ps.parseLine,
		OnExit: func(exitCode int, _ error) (harness.Result, error) {
			removeFiles()
			return ps.result(exitCode), nil
		},
	})
	if err != nil {
		removeFiles()
		return nil, err
	}
	return p, nil
}

// writeTemp writes content to a new temporary file and returns its path.
func writeTemp(pattern, content string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("exec harness: create temp file: %w", err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("exec harness: write %s: %w", f.Name(), err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("exec harness: close %s: %w", f.Name(), err)
	}
	return f.Name(), nil
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
