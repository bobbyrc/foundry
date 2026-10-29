package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const promptEnvRefMsg = "must not contain ${NAME} references; agent prompts are given to agents, which never get credentials"

// An inline prompt is given to the agent verbatim, so a ${NAME} reference in it would hand the
// daemon's environment to the agent. The refusal does not depend on the variable being set.
func TestPromptTextRefusesEnvReferenceWhetherOrNotSet(t *testing.T) {
	doc := "agents:\n  reviewer:\n    harness: claude\n    prompt_text: \"Never print ${GH_TOKEN}\"\n"

	t.Run("unset", func(t *testing.T) {
		_, err := Parse([]byte(doc), "")
		require.Error(t, err)
		assert.ErrorContains(t, err, "agents.reviewer.prompt_text: "+promptEnvRefMsg)
		assert.NotContains(t, err.Error(), "undefined environment variable")
	})

	t.Run("set", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "ghp_example_value_long_enough")
		_, err := Parse([]byte(doc), "")
		require.Error(t, err)
		assert.ErrorContains(t, err, "agents.reviewer.prompt_text: "+promptEnvRefMsg)
		assert.NotContains(t, err.Error(), "ghp_example_value_long_enough")
	})
}

// A bare $NAME is not a reference, so a prompt may mention a shell variable as written.
func TestPromptTextAcceptsBareDollarName(t *testing.T) {
	doc := "agents:\n  reviewer:\n    harness: claude\n    prompt_text: \"Scratch files go in $HOME/tmp.\"\n"
	cfg, err := Parse([]byte(doc), "")
	require.NoError(t, err)
	assert.Equal(t, "Scratch files go in $HOME/tmp.", cfg.Agents["reviewer"].PromptText)
}

func TestProjectPromptTextRefusesEnvReferenceWithProjectPrefix(t *testing.T) {
	doc := "projects:\n  app:\n    repo: bobbyrc/app\n    agents:\n      reviewer:\n" +
		"        prompt_text: \"Use ${GH_TOKEN}\"\n"
	_, err := Parse([]byte(doc), "")
	require.Error(t, err)
	assert.ErrorContains(t, err, "projects.app.agents.reviewer.prompt_text: "+promptEnvRefMsg)
}
