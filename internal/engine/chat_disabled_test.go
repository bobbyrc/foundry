package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bobbyrc/onlet/internal/domain"
)

// A disabled agent is not in the factory the orchestrator is told about, so it cannot be
// handed work by name.
func TestChatInstructionsOmitDisabledAgents(t *testing.T) {
	r := ctNewRig(t)
	improver := r.cfg.Agents["improver"]
	improver.Disabled = true
	r.cfg.Agents["improver"] = improver
	r.cfg.Routing["improve"] = "implementer"

	text := chatInstructions(r.cfg, domain.Conversation{}, "BASE")
	assert.NotContains(t, text, "**improver**")
	assert.NotContains(t, text, "improver", "no line mentions the disabled agent")
	assert.Contains(t, text, "- **implementer** (codex) — ")
	assert.Contains(t, text, "handles: bugfix, chore, feature, improve, issue", "the kind it was routed to now shows on the agent that handles it")
}
