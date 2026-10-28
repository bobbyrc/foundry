package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCollectsAllProblemsWithPaths(t *testing.T) {
	_, err := Parse([]byte(`
harnesses:
  broken:
    type: ""
agents:
  reviewer:
    harness: x
  triager:
    harness: claude
    prompt: ""
routing:
  bogus: reviewer
  feature: nobody
policies:
  bad:
    rules:
      - risk: extreme
        paths: ["[unclosed"]
      - risk: low
    gates:
      urgent: {auto_merge: true}
projects:
  app:
    repo: not a repo
    policy: missing
    deploy:
      type: ""
    observe:
      - name: dup
        type: sentry
      - name: dup
        type: sentry
limits:
  max_concurrent_runs: -1
notify:
  - type: ""
    min_priority: urgent
self_project: ghost
`), "")
	require.Error(t, err)
	msg := err.Error()
	for _, want := range []string{
		"harnesses.broken.type",
		"agents.reviewer.harness: unknown harness \"x\"",
		"agents.triager.prompt",
		"routing.bogus: unknown task kind",
		"routing.feature: unknown agent \"nobody\"",
		"policies.bad.rules[0].risk",
		"policies.bad.rules[0].paths[0]",
		"policies.bad.rules[1]: must set paths, labels or kinds",
		"policies.bad.gates.urgent",
		"projects.app.repo",
		"projects.app.policy: unknown policy \"missing\"",
		"projects.app.deploy.type",
		"projects.app.observe[1].name: duplicate observer \"dup\"",
		"limits.max_concurrent_runs",
		"notify[0].type",
		"notify[0].min_priority",
		"self_project: unknown project \"ghost\"",
	} {
		assert.Contains(t, msg, want)
	}
}

func TestValidateAcceptsRepoForms(t *testing.T) {
	cfg, err := Parse([]byte(`
projects:
  gh:
    repo: bobbyrc/foundry.v2_x
  local:
    repo: /src/local
`), "")
	require.NoError(t, err)
	assert.NoError(t, cfg.Validate())
}

func TestValidateDailyBudgetMustNotBeNegative(t *testing.T) {
	_, err := Parse([]byte("limits:\n  daily_budget_usd: -1\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limits.daily_budget_usd")

	_, err = Parse([]byte("limits:\n  daily_budget_usd: 0\n"), "")
	assert.NoError(t, err, "0 means no limit")
}

func TestValidateProjectAgentsAndRouting(t *testing.T) {
	_, err := Parse([]byte(`
harnesses:
  alt: {type: fake}
projects:
  app:
    repo: bob/app
    agents:
      implementer:
        harness: nowhere
      designer:
        harness: alt
        description: no prompt for a project-only role
      reviewer:
        prompt: ""
    routing:
      bogus: implementer
      bugfix: ghost
`), "")
	require.Error(t, err)
	msg := err.Error()
	for _, want := range []string{
		`projects.app.agents.implementer.harness: unknown harness "nowhere"`,
		"projects.app.agents.designer.prompt: must not be empty",
		"projects.app.routing.bogus: unknown task kind",
		`projects.app.routing.bugfix: unknown agent "ghost"`,
	} {
		assert.Contains(t, msg, want)
	}
	assert.NotContains(t, msg, "projects.app.agents.designer.harness", "designer names a harness that exists")
}

func TestValidateAcceptsProjectOverrides(t *testing.T) {
	_, err := Parse([]byte(`
harnesses:
  alt: {type: fake}
agents:
  designer:
    harness: alt
    prompt: builtin:implementer
projects:
  app:
    repo: bob/app
    agents:
      implementer:
        harness: alt
      designer:
        model: sonnet
    routing:
      bugfix: designer
      feature: implementer
`), "")
	require.NoError(t, err)
}

func TestValidateCIGraceMustNotBeNegative(t *testing.T) {
	_, err := Parse([]byte("policies:\n  bad:\n    ci_grace: -1m\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "policies.bad.ci_grace")

	_, err = Parse([]byte("policies:\n  ok:\n    ci_grace: 0s\n"), "")
	assert.NoError(t, err, "0 means the default")
}

func TestValidateRetroLookbackMustNotBeNegative(t *testing.T) {
	_, err := Parse([]byte("loops:\n  retro:\n    lookback: -1h\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loops.retro.lookback")

	_, err = Parse([]byte("projects:\n  app:\n    repo: bob/app\n    loops:\n      retro:\n        lookback: -1h\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "projects.app.loops.retro.lookback")
}
