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
	assert.Len(t, pol.Rules, 3)
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
