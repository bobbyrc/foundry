package config

import (
	"testing"

	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinNotInFileHasBuiltinOrigin(t *testing.T) {
	cfg, err := Parse([]byte("agents:\n"), "")
	require.NoError(t, err)
	assert.Equal(t, OriginBuiltin, cfg.Agents["reviewer"].Origin)
}

func TestBuiltinOverrideIsEditedAndReplacesWhole(t *testing.T) {
	cfg, err := Parse([]byte(`
agents:
  reviewer:
    harness: claude
    prompt: builtin:reviewer
`), "")
	require.NoError(t, err)
	reviewer := cfg.Agents["reviewer"]
	assert.Equal(t, OriginEdited, reviewer.Origin)
	assert.Empty(t, reviewer.Model, "an override replaces the built-in, it does not merge with it")
}

func TestDisabledOnlyEntryKeepsBuiltinSettings(t *testing.T) {
	cfg, err := Parse([]byte(`
agents:
  improver:
    disabled: true
routing:
  improve: implementer
`), "")
	require.NoError(t, err)
	improver := cfg.Agents["improver"]
	assert.True(t, improver.Disabled)
	assert.Equal(t, "codex", improver.Harness)
	assert.Equal(t, "builtin:improver", improver.Prompt)
	assert.Equal(t, OriginBuiltin, improver.Origin)
	assert.NoError(t, cfg.Validate())
}

func TestCustomAgentOrigin(t *testing.T) {
	cfg, err := Parse([]byte(`
agents:
  docs:
    harness: claude
    prompt_text: "Write docs."
`), "")
	require.NoError(t, err)
	docs := cfg.Agents["docs"]
	assert.Equal(t, OriginCustom, docs.Origin)
	assert.Equal(t, "Write docs.", docs.PromptText)
	assert.Empty(t, docs.Prompt)
}

func TestPromptAndPromptTextBothSetIsError(t *testing.T) {
	_, err := Parse([]byte(`
agents:
  docs:
    harness: claude
    prompt: docs.md
    prompt_text: "Write docs."
`), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agents.docs: set prompt or prompt_text, not both")

	_, err = Parse([]byte(`
harnesses:
  alt: {type: fake}
projects:
  app:
    repo: bob/app
    agents:
      designer:
        harness: alt
        prompt: docs.md
        prompt_text: "Write docs."
`), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "projects.app.agents.designer: set prompt or prompt_text, not both")
}

func TestDisablingOrchestratorIsError(t *testing.T) {
	_, err := Parse([]byte("agents:\n  orchestrator:\n    disabled: true\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agents.orchestrator.disabled: chat needs the orchestrator; edit it instead of removing it")
}

func TestDisabledAgentOnDefaultRouteIsError(t *testing.T) {
	_, err := Parse([]byte("agents:\n  reviewer:\n    disabled: true\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agents.reviewer.disabled: routing.review (built-in default) still uses it (reviews every pull request before merge); route review to another agent first")
}

func TestDisabledAgentOnExplicitRouteIsError(t *testing.T) {
	_, err := Parse([]byte(`
agents:
  docs:
    harness: claude
    prompt_text: "Write docs."
    disabled: true
routing:
  feature: docs
`), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agents.docs.disabled: routing.feature still uses it (builds features); route feature to another agent first")
}

func TestProjectDisableBlockedByInheritedRoute(t *testing.T) {
	_, err := Parse([]byte(`
projects:
  app:
    repo: bob/app
    agents:
      reviewer:
        disabled: true
`), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "projects.app.agents.reviewer.disabled: routing.review (built-in default) still uses it (reviews every pull request before merge); set projects.app.routing.review to another agent first")
}

func TestProjectDisableAllowedWhenRerouted(t *testing.T) {
	_, err := Parse([]byte(`
projects:
  app:
    repo: bob/app
    agents:
      reviewer:
        disabled: true
    routing:
      review: implementer
`), "")
	require.NoError(t, err)
}

func TestProjectRouteToDisabledAgentIsError(t *testing.T) {
	_, err := Parse([]byte(`
agents:
  docs:
    harness: claude
    prompt_text: "Write docs."
    disabled: true
projects:
  app:
    repo: bob/app
    routing:
      feature: docs
`), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `projects.app.routing.feature: agent "docs" is disabled; route feature to another agent first`)
}

func TestProjectPromptTextClearsInheritedPrompt(t *testing.T) {
	cfg, err := Parse([]byte(`
projects:
  app:
    repo: bob/app
    agents:
      reviewer:
        prompt_text: "x"
`), "")
	require.NoError(t, err)
	reviewer := cfg.AgentsFor("app")["reviewer"]
	assert.Equal(t, "x", reviewer.PromptText)
	assert.Empty(t, reviewer.Prompt)
}

func TestAgentForProjectRefusesDisabled(t *testing.T) {
	cfg, err := Parse([]byte(`
agents:
  docs:
    harness: claude
    prompt_text: "Write docs."
    disabled: true
`), "")
	require.NoError(t, err)
	_, _, err = cfg.AgentForProject("", domain.KindFeature, "docs")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `agent "docs" is disabled`)
}

func TestAgentUsesGlobal(t *testing.T) {
	cfg, err := Parse([]byte(`
agents:
  docs:
    harness: claude
    prompt_text: "Write docs."
  scratch:
    harness: claude
    prompt_text: "Scratch."
    disabled: true
`), "")
	require.NoError(t, err)
	uses := cfg.AgentUses("")

	assert.Equal(t, AgentUse{Description: "the agent you chat with"}, uses["orchestrator"][0])
	assert.Contains(t, uses["reviewer"], AgentUse{Kind: "review", Description: "reviews every pull request before merge"})

	for name, a := range cfg.Agents {
		if a.Disabled || name == "orchestrator" {
			continue
		}
		list := uses[name]
		require.NotEmpty(t, list, name)
		assert.Equal(t, AgentUse{Description: "the orchestrator may also hand it work by name"}, list[len(list)-1], name)
	}

	assert.Equal(t, []AgentUse{{Description: "the orchestrator may also hand it work by name"}}, uses["docs"],
		"an agent with no routes has only the delegation use")
	assert.Empty(t, uses["scratch"], "a disabled agent has no uses")
}

func TestAgentUsesProjectRoute(t *testing.T) {
	cfg, err := Parse([]byte(`
agents:
  docs:
    harness: claude
    prompt_text: "Write docs."
projects:
  app:
    repo: bob/app
    routing:
      feature: docs
`), "")
	require.NoError(t, err)

	feature := AgentUse{Kind: "feature", Project: "app", Description: "builds features"}
	assert.Contains(t, cfg.AgentUses("")["docs"], feature, "the global view shows a project's own route")

	app := cfg.AgentUses("app")
	assert.Contains(t, app["docs"], feature)
	for _, u := range app["implementer"] {
		assert.NotEqual(t, "feature", u.Kind, "the project routes feature elsewhere, so implementer has no feature use")
	}
	assert.Contains(t, app["reviewer"], AgentUse{Kind: "review", Description: "reviews every pull request before merge"},
		"an inherited route has no project mark")
}

func TestSchemaOmitsOrigin(t *testing.T) {
	agents := schemaField(t, Schema().Fields, "agents")
	keys := map[string]bool{}
	for _, f := range agents.Item.Fields {
		keys[f.Key] = true
	}
	assert.False(t, keys["origin"])
	assert.True(t, keys["disabled"])
	assert.True(t, keys["prompt_text"])
}
