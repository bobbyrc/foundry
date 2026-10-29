package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A custom agent that runs a built-in prompt of another name still follows that prompt, so
// it reports prompt_updates; only an agent with its own inline text or file stops following.
func TestAgentsViewCustomAgentOnBuiltinPromptUpdates(t *testing.T) {
	x := newHarness(t)
	withConfig(t, x, "agents:\n  mine:\n    harness: claude\n    prompt: builtin:implementer\n")

	mine := agentViews(t, x, "/api/v1/agents")["mine"]
	assert.True(t, mine.PromptUpdates)
	assert.Empty(t, mine.BuiltinPromptText, "a custom name is not a built-in, so there is nothing to compare")
}
