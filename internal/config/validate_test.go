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
