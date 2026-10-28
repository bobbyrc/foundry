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
    extra_rules:
      - risk: extreme
        paths: ["[unclosed"]
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
		"policies.bad.rules[1]: must set paths, labels, kinds or sources",
		"policies.bad.extra_rules[0].risk",
		"policies.bad.extra_rules[0].paths[0]",
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

func TestValidateRuleSourcesMustNotBeEmpty(t *testing.T) {
	_, err := Parse([]byte("policies:\n  bad:\n    extra_rules:\n      - risk: high\n        reason: x\n        sources: [\"followup\", \" \"]\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "policies.bad.extra_rules[0].sources[1]: must not be empty")
	assert.NotContains(t, err.Error(), "sources[0]")

	_, err = Parse([]byte("policies:\n  ok:\n    extra_rules:\n      - risk: high\n        reason: x\n        sources: [\"retro\"]\n"), "")
	assert.NoError(t, err, "a rule that matches only on source is valid")
}

func TestValidateIntakeSources(t *testing.T) {
	valid := "projects:\n  app:\n    repo: bob/app\n    intake:\n      - name: inbox\n        command: cat x\n"
	_, err := Parse([]byte(valid), "")
	require.NoError(t, err)

	cases := map[string]struct{ src, want string }{
		"empty name":   {"      - name: ''\n        command: x\n", "intake[0].name: must not be empty"},
		"bad name":     {"      - name: Phone App\n        command: x\n", `intake[0].name: must match ^[a-z0-9_-]+$, not "Phone App"`},
		"no command":   {"      - name: inbox\n        command: '  '\n", "intake[0].command: must not be empty"},
		"review kind":  {"      - name: inbox\n        command: x\n        kind: review\n", "intake[0].kind"},
		"incident":     {"      - name: inbox\n        command: x\n        kind: incident\n", "intake[0].kind"},
		"unknown kind": {"      - name: inbox\n        command: x\n        kind: nope\n", "intake[0].kind"},
		"neg timeout":  {"      - name: inbox\n        command: x\n        timeout: -1s\n", "intake[0].timeout: must be 0"},
	}
	for name, tc := range cases {
		doc := "projects:\n  app:\n    repo: bob/app\n    intake:\n" + tc.src
		_, err := Parse([]byte(doc), "")
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "projects.app."+tc.want, name)
	}

	dup := "projects:\n  app:\n    repo: bob/app\n    intake:\n      - name: inbox\n        command: x\n      - name: inbox\n        command: y\n"
	_, err = Parse([]byte(dup), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `projects.app.intake[1].name: duplicate intake source "inbox"`)
}

func TestValidateAllowedHosts(t *testing.T) {
	_, err := Parse([]byte("server:\n  allowed_hosts: [foundry.lan, 'foundry.lan:7777']\n"), "")
	require.NoError(t, err, "a bare host name, with or without a port, is valid")

	cases := map[string]struct{ src, want string }{
		"empty":  {"server:\n  allowed_hosts: ['  ']\n", "server.allowed_hosts[0]: must not be empty"},
		"scheme": {"server:\n  allowed_hosts: [https://foundry.lan]\n", "server.allowed_hosts[0]: want a host name, not a URL with a scheme"},
		"path":   {"server:\n  allowed_hosts: [foundry.lan/ui]\n", "server.allowed_hosts[0]: must not include a path"},
	}
	for name, tc := range cases {
		_, err := Parse([]byte(tc.src), "")
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), tc.want, name)
	}
}

func TestValidatePublicURL(t *testing.T) {
	_, err := Parse([]byte("server:\n  public_url: https://foundry.example.com\n"), "")
	require.NoError(t, err)

	_, err = Parse([]byte("server:\n  public_url: foundry.lan:7777\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `server.public_url "foundry.lan:7777" must be an absolute http or https URL`)
}
