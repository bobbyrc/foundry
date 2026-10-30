package codex

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onett/internal/harness"
)

func TestSandboxModeConfigOverrideCountsAsChoosingASandbox(t *testing.T) {
	for _, extra := range [][]string{
		{"-c", `sandbox_mode="read-only"`},
		{"--config", "sandbox_mode=danger-full-access"},
		{`-c=sandbox_mode="read-only"`},
		{`--config=sandbox_mode="read-only"`},
	} {
		assert.True(t, hasSandboxFlag(extra), "%v", extra)

		resumed := buildArgs(harness.Config{Args: extra}, harness.Spec{ResumeSessionID: "s1"})
		assert.Equal(t, 0, countOf(resumed, `sandbox_mode="workspace-write"`),
			"the resume override must not replace the user's choice: %v", resumed)

		fresh := buildArgs(harness.Config{}, harness.Spec{Args: extra})
		assert.Equal(t, 0, countOf(fresh, "--approve-for-me"), "%v", fresh)
	}
}

func TestOtherConfigOverridesDoNotChooseASandbox(t *testing.T) {
	assert.False(t, hasSandboxFlag([]string{"-c", `model="o3"`}))
	assert.False(t, hasSandboxFlag([]string{"-c"}))
	assert.False(t, hasSandboxFlag([]string{`--config=model="o3"`}))
}
