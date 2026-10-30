package codex

import (
	"testing"

	"github.com/bobbyrc/onett/internal/harness"
	"github.com/stretchr/testify/assert"
)

func TestBuildArgsResumeKeepsWorkspaceWrite(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{Workdir: "/w", ResumeSessionID: "s1"})
	i := indexOf(args, "-c")
	if assert.NotEqual(t, -1, i, "resume must carry a config override: %v", args) {
		assert.Equal(t, `sandbox_mode="workspace-write"`, args[i+1])
	}
	assert.Equal(t, 0, countOf(args, "--sandbox"))
	assert.Equal(t, 0, countOf(args, "--approve-for-me"))
	assert.Equal(t, "-", args[len(args)-1])
}

func TestBuildArgsResumeRespectsConfiguredSandbox(t *testing.T) {
	for _, flag := range []string{"--sandbox", "-s", "--approve-for-me", "--full-auto",
		"--dangerously-bypass-approvals-and-sandbox"} {
		cfgArgs := buildArgs(harness.Config{Args: []string{flag}},
			harness.Spec{ResumeSessionID: "s1"})
		assert.Equal(t, -1, indexOf(cfgArgs, "-c"), "config flag %s", flag)

		specArgs := buildArgs(harness.Config{},
			harness.Spec{ResumeSessionID: "s1", Args: []string{flag}})
		assert.Equal(t, -1, indexOf(specArgs, "-c"), "spec flag %s", flag)
	}
}

func TestBuildArgsNewSessionKeepsApproveForMe(t *testing.T) {
	args := buildArgs(harness.Config{}, harness.Spec{Workdir: "/w"})
	assert.Equal(t, -1, indexOf(args, "-c"), "a new session needs no config override")
	assert.Equal(t, 1, countOf(args, "--approve-for-me"))
}

// indexOf returns the position of want in args, or -1.
func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}
