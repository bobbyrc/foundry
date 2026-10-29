package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /agents shows descriptions, args and prompts to every viewer, so a value substituted
// for a ${NAME} reference must be masked there as it is on GET /config.
func TestAgentsViewMasksEnvironmentValues(t *testing.T) {
	t.Setenv("ONETT_TEST_SECRET", "supersecretvalue")
	x := newHarness(t)
	withConfig(t, x, "agents:\n  reviewer:\n    harness: claude\n    prompt: builtin:reviewer\n    description: \"uses ${ONETT_TEST_SECRET}\"\n")

	rec := x.call(t, http.MethodGet, "/api/v1/agents", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.NotContains(t, body, "supersecretvalue")
	assert.Contains(t, body, "***")
}
