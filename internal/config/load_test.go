package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestLoopsForMergesOverrides(t *testing.T) {
	cfg, err := Parse([]byte(`
projects:
  app:
    repo: bob/app
    loops:
      prod_watch:
        every: 2m
        min_level: fatal
      friction:
        auto_file: false
        auto_queue: true
        min_count: 4
  plain:
    repo: bob/plain
`), "")
	require.NoError(t, err)

	app := cfg.LoopsFor("app")
	assert.Equal(t, 2*time.Minute, app.ProdWatch.Every.Duration)
	assert.Equal(t, "fatal", app.ProdWatch.MinLevel)
	assert.Equal(t, int64(1), app.ProdWatch.MinCount, "unset override keeps the global value")
	assert.Equal(t, 1*time.Minute, app.PRShepherd.Every.Duration)
	assert.False(t, app.FrictionAutoFile())
	assert.True(t, app.Friction.AutoQueue)
	assert.Equal(t, 4, app.Friction.MinCount)
	assert.Equal(t, "friction", app.Friction.Label)

	assert.Equal(t, cfg.Loops, cfg.LoopsFor("plain"), "no override means the global loops")
	assert.Equal(t, cfg.Loops, cfg.LoopsFor("unknown"))
	assert.Equal(t, "error", cfg.Loops.ProdWatch.MinLevel, "global loops are not changed")
}

func TestLoopsForCanDisableALoop(t *testing.T) {
	cfg, err := Parse([]byte(`
projects:
  app:
    repo: bob/app
    loops:
      deploy_watch:
        enabled: false
`), "")
	require.NoError(t, err)
	assert.False(t, cfg.LoopsFor("app").DeployWatch.On())
	assert.True(t, cfg.Loops.DeployWatch.On())
}

func TestAgentForByRoleAndRouting(t *testing.T) {
	cfg, err := Parse([]byte(`
agents:
  implementer:
    harness: claude
    prompt: builtin:implementer
`), "")
	require.NoError(t, err)

	name, agent, err := cfg.AgentFor(domain.KindFeature, "")
	require.NoError(t, err)
	assert.Equal(t, "implementer", name)
	assert.Equal(t, "claude", agent.Harness, "routing picks the overridden implementer")

	name, agent, err = cfg.AgentFor(domain.KindReview, "")
	require.NoError(t, err)
	assert.Equal(t, "reviewer", name)
	assert.Equal(t, "sonnet", agent.Model)

	name, _, err = cfg.AgentFor(domain.KindQuestion, "orchestrator")
	require.NoError(t, err)
	assert.Equal(t, "orchestrator", name, "an explicit role wins over routing")

	_, _, err = cfg.AgentFor(domain.KindFeature, "nobody")
	assert.ErrorContains(t, err, `unknown agent role "nobody"`)

	cfg.Routing["chore"] = "ghost"
	_, _, err = cfg.AgentFor(domain.KindChore, "")
	assert.ErrorContains(t, err, `unknown agent "ghost"`)

	delete(cfg.Routing, "chore")
	_, _, err = cfg.AgentFor(domain.KindChore, "")
	assert.ErrorContains(t, err, "no agent routed")
}

func TestPolicyForFallsBackToDefault(t *testing.T) {
	cfg, err := Parse([]byte(`
policies:
  strict:
    max_lines: 10
projects:
  app:
    repo: bob/app
    policy: strict
  plain:
    repo: bob/plain
`), "")
	require.NoError(t, err)

	assert.Equal(t, 10, cfg.PolicyFor("app").MaxLines)
	assert.Equal(t, 400, cfg.PolicyFor("plain").MaxLines)
	assert.Equal(t, 400, cfg.PolicyFor("missing").MaxLines)
}

func TestProjectNamesSorted(t *testing.T) {
	cfg, err := Parse([]byte("projects:\n  zeta:\n    repo: a/z\n  alpha:\n    repo: a/a\n"), "")
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "zeta"}, cfg.ProjectNames())
}

func TestRollbackEnabled(t *testing.T) {
	off := false
	assert.True(t, (&Deploy{}).RollbackEnabled())
	assert.False(t, (&Deploy{AutoRollback: &off}).RollbackEnabled())
}

func TestDurationYAMLAndJSONRoundTrip(t *testing.T) {
	cfg, err := Parse([]byte("limits:\n  run_timeout: 90m\n  stall_timeout: 1h30m\n"), "")
	require.NoError(t, err)
	assert.Equal(t, 90*time.Minute, cfg.Limits.RunTimeout.Duration)
	assert.Equal(t, 90*time.Minute, cfg.Limits.StallTimeout.Duration)

	again, err := Parse([]byte(mustYAML(t, cfg)), "")
	require.NoError(t, err)
	assert.Equal(t, cfg.Limits.RunTimeout, again.Limits.RunTimeout)

	data := mustJSON(t, cfg)
	var fromJSON Config
	require.NoError(t, unmarshalJSON(data, &fromJSON))
	assert.Equal(t, cfg.Limits.StallTimeout, fromJSON.Limits.StallTimeout)
}

func TestBadDurationNamesTheValue(t *testing.T) {
	_, err := Parse([]byte("limits:\n  run_timeout: soon\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"soon"`)
}

func TestUnsetEnvVarIsAnErrorNamingEachOnce(t *testing.T) {
	_, err := Parse([]byte(`
server:
  public_url: "${FOUNDRY_TEST_ZULU}/${FOUNDRY_TEST_ALPHA}"
projects:
  app:
    repo: bob/app
    deploy:
      type: command
      options:
        rollback: "${FOUNDRY_TEST_ALPHA} ${FOUNDRY_TEST_MIKE}"
`), "")
	require.Error(t, err)
	assert.EqualError(t, err, "undefined environment variable ${FOUNDRY_TEST_ALPHA}\n"+
		"undefined environment variable ${FOUNDRY_TEST_MIKE}\n"+
		"undefined environment variable ${FOUNDRY_TEST_ZULU}")
}

func TestEmptyEnvVarExpandsToNothing(t *testing.T) {
	t.Setenv("FOUNDRY_TEST_EMPTY", "")
	cfg, err := Parse([]byte(`
server:
  public_url: "https://host${FOUNDRY_TEST_EMPTY}/foundry"
`), "")
	require.NoError(t, err)
	assert.Equal(t, "https://host/foundry", cfg.Server.PublicURL)
}

func TestExampleFileLoads(t *testing.T) {
	cfg, err := Load("../../foundry.example.yaml")
	require.NoError(t, err)

	assert.Equal(t, []string{"foundry", "myapp"}, cfg.ProjectNames())
	assert.Nil(t, cfg.Projects["foundry"].Deploy)

	myapp := cfg.Projects["myapp"]
	require.NotNil(t, myapp.Deploy)
	assert.Equal(t, 10*time.Minute, myapp.Deploy.VerifyFor.Duration)
	assert.Equal(t, "vercel rollback $FOUNDRY_ROLLBACK_ID --yes", myapp.Deploy.Options["rollback"])
	assert.Equal(t, "sentry-prod", myapp.Observe[0].Name)

	assert.Equal(t, "claude", cfg.Agents["implementer"].Harness)
	assert.Equal(t, "exec", cfg.Harness["my-agent"].Type)
	assert.Equal(t, "foundry", cfg.SelfProject)

	loops := cfg.LoopsFor("myapp")
	assert.Equal(t, 2*time.Minute, loops.ProdWatch.Every.Duration)
	assert.Equal(t, "fatal", loops.ProdWatch.MinLevel)

	pol := cfg.PolicyFor("myapp")
	assert.Len(t, pol.Rules, 7, "the example adds rules with extra_rules, so the built-ins stay")
	assert.Len(t, pol.ExtraRules, 2)
	assert.Equal(t, 200, pol.MaxLines)
}

func mustYAML(t *testing.T, v any) string {
	t.Helper()
	data, err := yaml.Marshal(v)
	require.NoError(t, err)
	return string(data)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}

func unmarshalJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func TestAgentsForMergesProjectOverrides(t *testing.T) {
	cfg, err := Parse([]byte(`
agents:
  implementer:
    harness: codex
    model: gpt-5
    prompt: builtin:implementer
    description: global implementer
projects:
  app:
    repo: bob/app
    agents:
      implementer:
        model: sonnet
      designer:
        harness: claude
        prompt: builtin:implementer
        description: project only
  plain:
    repo: bob/plain
`), "")
	require.NoError(t, err)

	app := cfg.AgentsFor("app")
	impl := app["implementer"]
	assert.Equal(t, "codex", impl.Harness, "override model only keeps the global harness")
	assert.Equal(t, "sonnet", impl.Model)
	assert.Equal(t, "builtin:implementer", impl.Prompt, "override model only keeps the global prompt")
	assert.Equal(t, "global implementer", impl.Description)

	assert.Equal(t, Agent{Harness: "claude", Prompt: "builtin:implementer", Description: "project only"},
		app["designer"], "a name the global map lacks adds a project-only role")
	assert.NotContains(t, cfg.AgentsFor("plain"), "designer")
	assert.Equal(t, "codex", cfg.AgentsFor("plain")["implementer"].Harness)
	assert.Equal(t, "gpt-5", cfg.Agents["implementer"].Model, "global roles are not changed")
	assert.NotContains(t, cfg.Agents, "designer")

	app["implementer"] = Agent{}
	assert.Equal(t, "sonnet", cfg.AgentsFor("app")["implementer"].Model, "changing the returned map does not change the config")
}

func TestAgentForProjectRoutingFallsBackToGlobal(t *testing.T) {
	cfg, err := Parse([]byte(`
agents:
  implementer:
    harness: codex
    prompt: builtin:implementer
projects:
  app:
    repo: bob/app
    agents:
      implementer:
        harness: claude
    routing:
      bugfix: implementer
      chore: reviewer
  plain:
    repo: bob/plain
    routing:
      feature: triager
`), "")
	require.NoError(t, err)

	// The project routes bugfix to its overridden implementer, which runs on claude.
	name, agent, err := cfg.AgentForProject("app", domain.KindBugfix, "")
	require.NoError(t, err)
	assert.Equal(t, "implementer", name)
	assert.Equal(t, "claude", agent.Harness)

	// No project routing for review: the global routing applies.
	name, _, err = cfg.AgentForProject("app", domain.KindReview, "")
	require.NoError(t, err)
	assert.Equal(t, "reviewer", name)

	// The project's routing wins over the global one for the same kind.
	name, _, err = cfg.AgentForProject("plain", domain.KindFeature, "")
	require.NoError(t, err)
	assert.Equal(t, "triager", name)
	name, _, err = cfg.AgentForProject("plain", domain.KindBugfix, "")
	require.NoError(t, err)
	assert.Equal(t, "implementer", name, "kinds the project does not route use the global routing")

	// An explicit role is checked against the project's roles.
	_, _, err = cfg.AgentForProject("app", domain.KindFeature, "triager")
	assert.NoError(t, err)
	_, _, err = cfg.AgentForProject("plain", domain.KindFeature, "nobody")
	assert.ErrorContains(t, err, `unknown agent role "nobody"`)

	// AgentFor is the global view: the project-only designer role does not exist there.
	_, agent, err = cfg.AgentFor(domain.KindBugfix, "")
	require.NoError(t, err)
	assert.Equal(t, "codex", agent.Harness)
}

func TestLoopsForMergesRetroOverrides(t *testing.T) {
	cfg, err := Parse([]byte(`
self_project: app
projects:
  app:
    repo: bob/app
  other:
    repo: bob/other
    loops:
      retro:
        enabled: true
        every: 6h
        lookback: 2h
`), "")
	require.NoError(t, err)

	other := cfg.LoopsFor("other")
	require.NotNil(t, other.Retro.Enabled)
	assert.True(t, *other.Retro.Enabled)
	assert.Equal(t, 6*time.Hour, other.Retro.Every.Duration)
	assert.Equal(t, 2*time.Hour, other.Retro.Lookback.Duration)
	assert.Nil(t, cfg.Loops.Retro.Enabled, "the global loop is not changed")
	assert.Equal(t, 24*time.Hour, cfg.Loops.Retro.Every.Duration)
}

func TestRetroOnDefaultsToSelfProject(t *testing.T) {
	cfg, err := Parse([]byte(`
self_project: app
projects:
  app:
    repo: bob/app
    loops:
      retro:
        enabled: false
  other:
    repo: bob/other
  forced:
    repo: bob/forced
    loops:
      retro:
        enabled: true
`), "")
	require.NoError(t, err)

	assert.False(t, cfg.LoopsFor("app").RetroOn("app", cfg.SelfProject), "explicit false wins over self")
	assert.False(t, cfg.LoopsFor("other").RetroOn("other", cfg.SelfProject), "other projects default off")
	assert.True(t, cfg.LoopsFor("forced").RetroOn("forced", cfg.SelfProject), "explicit true wins over default")

	plain, err := Parse([]byte("projects:\n  app:\n    repo: bob/app\n"), "")
	require.NoError(t, err)
	assert.False(t, plain.Loops.RetroOn("app", plain.SelfProject), "no self project means no retro")

	self, err := Parse([]byte("self_project: app\nprojects:\n  app:\n    repo: bob/app\n"), "")
	require.NoError(t, err)
	assert.True(t, self.Loops.RetroOn("app", self.SelfProject))
}

func TestLoopsForMergesExecIntakeOverrides(t *testing.T) {
	cfg, err := Parse([]byte(`
projects:
  app:
    repo: bob/app
  other:
    repo: bob/other
    loops:
      exec_intake:
        enabled: false
        every: 30s
`), "")
	require.NoError(t, err)

	other := cfg.LoopsFor("other")
	require.NotNil(t, other.ExecIntake.Enabled)
	assert.False(t, *other.ExecIntake.Enabled)
	assert.Equal(t, 30*time.Second, other.ExecIntake.Every.Duration)
	assert.Equal(t, time.Minute, cfg.LoopsFor("app").ExecIntake.Every.Duration, "unset override keeps the global value")
	assert.Nil(t, cfg.LoopsFor("app").ExecIntake.Enabled)
}
