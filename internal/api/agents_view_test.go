package api

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
)

// withConfig parses a config document (defaults applied, as the daemon does) and serves it.
func withConfig(t *testing.T, x *harness, doc string) {
	t.Helper()
	cfg, err := config.Parse([]byte(doc), "")
	require.NoError(t, err)
	x.ctl.cfg = cfg
}

// agentViews calls GET target and returns the views by role name.
func agentViews(t *testing.T, x *harness, target string) map[string]AgentView {
	t.Helper()
	rec := x.call(t, http.MethodGet, target, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var agents []AgentView
	decode(t, rec, &agents)
	out := make(map[string]AgentView, len(agents))
	for _, a := range agents {
		out[a.Name] = a
	}
	return out
}

func TestAgentsViewShowsOriginPromptAndUses(t *testing.T) {
	x := newHarness(t)
	withConfig(t, x, "")

	reviewer := agentViews(t, x, "/api/v1/agents")["reviewer"]
	assert.Equal(t, config.OriginBuiltin, reviewer.Origin)
	assert.True(t, reviewer.PromptUpdates)
	assert.Empty(t, reviewer.BuiltinPromptText)
	assert.Contains(t, reviewer.PromptText, "# Role: reviewer")
	assert.Contains(t, reviewer.Uses, config.AgentUse{Kind: "review", Description: config.KindUse("review")})
}

func TestAgentsViewEditedPromptShowsBuiltinForCompare(t *testing.T) {
	x := newHarness(t)
	withConfig(t, x, "agents:\n  reviewer:\n    harness: claude\n    prompt_text: Mine.\n")

	reviewer := agentViews(t, x, "/api/v1/agents")["reviewer"]
	assert.Equal(t, config.OriginEdited, reviewer.Origin)
	assert.False(t, reviewer.PromptUpdates)
	assert.Equal(t, "Mine.", reviewer.PromptText)
	assert.Contains(t, reviewer.BuiltinPromptText, "# Role: reviewer")
}

func TestAgentsViewMissingPromptFileStill200(t *testing.T) {
	x := newHarness(t)
	missing := filepath.Join(t.TempDir(), "gone.md")
	withConfig(t, x, "agents:\n  reviewer:\n    harness: claude\n    prompt: \""+missing+"\"\n")

	agents := agentViews(t, x, "/api/v1/agents")
	reviewer := agents["reviewer"]
	assert.NotEmpty(t, reviewer.PromptError)
	assert.Empty(t, reviewer.PromptText)
	assert.Contains(t, agents, "implementer")
}

func TestAgentsViewListsDisabled(t *testing.T) {
	x := newHarness(t)
	withConfig(t, x, "routing:\n  improve: triager\nagents:\n  improver:\n    disabled: true\n")

	improver, ok := agentViews(t, x, "/api/v1/agents")["improver"]
	require.True(t, ok, "a disabled agent stays in the list")
	assert.True(t, improver.Disabled)
	assert.NotNil(t, improver.Uses, "uses is [] for a disabled agent, never null")
	assert.Empty(t, improver.Uses)
}

func TestAgentsViewProjectQuery(t *testing.T) {
	x := newHarness(t)
	withConfig(t, x, "projects:\n  app:\n    repo: bobbyrc/app\n    agents:\n      reviewer:\n        description: App review rules.\n")

	global := agentViews(t, x, "/api/v1/agents")
	assert.NotEqual(t, "App review rules.", global["reviewer"].Description)

	app := agentViews(t, x, "/api/v1/agents?project=app")
	assert.Equal(t, "App review rules.", app["reviewer"].Description)

	rec := x.call(t, http.MethodGet, "/api/v1/agents?project=nope", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
