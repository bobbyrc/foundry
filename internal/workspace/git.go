package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// hooksOff turns off git hooks for every command this package runs. Hooks live in the clone's
// shared .git/hooks, which an agent may be able to write; if a hook ran, it would run with
// onett's credentials and environment.
var hooksOff = []string{"-c", "core.hooksPath=/dev/null"}

// gitCommand builds `git` with hooksOff in front of args, run in dir (the process working
// directory when dir is empty).
func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, hooksOff...), args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd
}

// git runs one git command in dir (or the process working directory when dir is empty) and
// returns its trimmed stdout. A failure is reported with the arguments and git's stderr.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := gitCommand(ctx, dir, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err,
			strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// gitExitCode runs git in dir and returns its exit status without treating a non-zero status as
// an error. It is used for commands such as `git diff --quiet`, where exit 1 is an answer.
func gitExitCode(ctx context.Context, dir string, args ...string) (int, error) {
	cmd := gitCommand(ctx, dir, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err,
		strings.TrimSpace(stderr.String()))
}

// gitOutputAllowExit runs git in dir and returns its trimmed stdout even when git exits non-zero.
// It is used for commands that print their findings and signal them with the exit status, such
// as `git diff --check`. Only a failure to run git at all is an error.
func gitOutputAllowExit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := gitCommand(ctx, dir, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err,
				strings.TrimSpace(stderr.String()))
		}
	}
	return strings.TrimSpace(stdout.String()), nil
}
