package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultsOnEmptyConfig(t *testing.T) {
	cfg, err := Parse(nil, "")
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:7777", cfg.Server.Addr)
	assert.Equal(t, filepath.Join(home, ".local", "share", "foundry"), cfg.Server.DataDir)
	assert.Equal(t, "http://127.0.0.1:7777", cfg.Server.PublicURL)

	assert.Equal(t, 3, cfg.Limits.MaxConcurrentRuns)
	assert.Equal(t, 1, cfg.Limits.MaxRunsPerProject)
	assert.Equal(t, 15*time.Minute, cfg.Limits.StallTimeout.Duration)
	assert.Equal(t, 2*time.Hour, cfg.Limits.RunTimeout.Duration)
	assert.Equal(t, 3, cfg.Limits.MaxAttempts)

	assert.Equal(t, "claude-code", cfg.Harness["claude"].Type)
	assert.Equal(t, "codex", cfg.Harness["codex"].Type)

	assert.Len(t, cfg.Agents, 6)
	assert.Equal(t, "opus", cfg.Agents["orchestrator"].Model)
	assert.Equal(t, "codex", cfg.Agents["implementer"].Harness)
	assert.Equal(t, "builtin:sre", cfg.Agents["sre"].Prompt)

	assert.Equal(t, map[string]string{
		"feature":  "implementer",
		"bugfix":   "implementer",
		"issue":    "implementer",
		"chore":    "implementer",
		"incident": "sre",
		"review":   "reviewer",
		"improve":  "improver",
		"question": "triager",
	}, cfg.Routing)

	pol := cfg.Policies["default"]
	assert.Equal(t, 400, pol.MaxLines)
	assert.Equal(t, 20, pol.MaxFiles)
	assert.Len(t, pol.Rules, 5)
	assert.Equal(t, DefaultGates(), pol.Gates)

	assert.Equal(t, "foundry", cfg.Loops.IssueIntake.Label)
	assert.Equal(t, 5*time.Minute, cfg.Loops.IssueIntake.Every.Duration)
	assert.Equal(t, time.Minute, cfg.Loops.PRShepherd.Every.Duration)
	assert.Equal(t, 30*time.Second, cfg.Loops.DeployWatch.Every.Duration)
	assert.Equal(t, 10*time.Minute, cfg.Loops.ProdWatch.Every.Duration)
	assert.Equal(t, "error", cfg.Loops.ProdWatch.MinLevel)
	assert.Equal(t, int64(1), cfg.Loops.ProdWatch.MinCount)
	assert.Equal(t, "friction", cfg.Loops.Friction.Label)
	assert.Equal(t, 1, cfg.Loops.Friction.MinCount)
	assert.True(t, cfg.Loops.FrictionAutoFile())

	assert.Equal(t, []Notifier{{Type: "log"}}, cfg.Notify)
}

func TestUserValuesAreNotOverwritten(t *testing.T) {
	cfg, err := Parse([]byte(`
server:
  addr: 0.0.0.0:9000
  data_dir: /var/lib/foundry
limits:
  max_attempts: 5
harnesses:
  claude:
    type: claude-code
    args: ["--verbose"]
agents:
  reviewer:
    harness: codex
    model: gpt-5
    prompt: builtin:reviewer
routing:
  feature: triager
policies:
  default:
    max_lines: 50
    gates:
      low:
        auto_merge: false
loops:
  prod_watch:
    min_level: fatal
`), "")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:9000", cfg.Server.Addr)
	assert.Equal(t, "/var/lib/foundry", cfg.Server.DataDir)
	assert.Equal(t, "http://0.0.0.0:9000", cfg.Server.PublicURL)
	assert.Equal(t, 5, cfg.Limits.MaxAttempts)
	assert.Equal(t, 3, cfg.Limits.MaxConcurrentRuns)
	assert.Equal(t, "codex", cfg.Agents["reviewer"].Harness)
	assert.Equal(t, "gpt-5", cfg.Agents["reviewer"].Model)
	assert.Equal(t, "triager", cfg.Routing["feature"])
	assert.Equal(t, "fatal", cfg.Loops.ProdWatch.MinLevel)

	pol := cfg.Policies["default"]
	assert.Equal(t, 50, pol.MaxLines)
	assert.Equal(t, 20, pol.MaxFiles)
	assert.False(t, pol.Gates["low"].AutoMerge)
	assert.Equal(t, DefaultGates()["medium"], pol.Gates["medium"], "missing levels are filled")
	assert.Equal(t, DefaultGates()["high"], pol.Gates["high"], "missing levels are filled")
}

func TestUnknownKeyIsAnError(t *testing.T) {
	_, err := Parse([]byte("limits:\n  max_atempts: 5\n"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_atempts")
}

func TestEnvExpansionOnlyTouchesBracedNames(t *testing.T) {
	t.Setenv("FOUNDRY_TEST_HOST", "example.test")
	cfg, err := Parse([]byte(`
server:
  public_url: "https://${FOUNDRY_TEST_HOST}/foundry"
projects:
  app:
    repo: bob/app
    deploy:
      type: command
      options:
        rollback: "vercel rollback $FOUNDRY_TEST_HOST --yes"
`), "")
	require.NoError(t, err)
	assert.Equal(t, "https://example.test/foundry", cfg.Server.PublicURL)
	assert.Equal(t, "vercel rollback $FOUNDRY_TEST_HOST --yes", cfg.Projects["app"].Deploy.Options["rollback"])
}

func TestRulesNilVersusEmpty(t *testing.T) {
	cfg, err := Parse([]byte(`
policies:
  absent: {}
  empty:
    rules: []
`), "")
	require.NoError(t, err)

	assert.Len(t, cfg.Policies["absent"].Rules, 5, "absent rules take the defaults")
	require.NotNil(t, cfg.Policies["empty"].Rules, "an explicit empty list stays non-nil")
	assert.Empty(t, cfg.Policies["empty"].Rules)
}

func TestPromptPathsResolveAgainstConfigDir(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "foundry.yaml"))
	require.NoError(t, err)

	dir, err := filepath.Abs("testdata")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "foundry.yaml"), cfg.Path)
	assert.Equal(t, filepath.Join(dir, "prompts", "reviewer.md"), cfg.Agents["reviewer"].Prompt)
	assert.Equal(t, "/abs/triager.md", cfg.Agents["triager"].Prompt, "absolute paths are left alone")
	assert.Equal(t, "builtin:sre", cfg.Agents["sre"].Prompt, "builtin prompts are left alone")
}

func TestProjectDefaults(t *testing.T) {
	cfg, err := Parse([]byte(`
server:
  data_dir: /data
projects:
  app:
    repo: bob/app
    deploy:
      type: command
  local:
    repo: /src/local
    path: /work/local
    observe:
      - type: sentry
`), "")
	require.NoError(t, err)

	app := cfg.Projects["app"]
	assert.Equal(t, "app", app.Name)
	assert.Equal(t, "main", app.DefaultBranch)
	assert.Equal(t, "default", app.Policy)
	assert.Equal(t, filepath.Join("/data", "repos", "app"), app.Path)
	assert.Equal(t, 10*time.Minute, app.Deploy.VerifyFor.Duration)
	assert.Equal(t, 30*time.Minute, app.Deploy.Timeout.Duration)
	assert.True(t, app.Deploy.RollbackEnabled())

	local := cfg.Projects["local"]
	assert.Equal(t, "/work/local", local.Path)
	assert.Equal(t, "sentry", local.Observe[0].Name)
}

func TestTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	cfg, err := Parse([]byte("projects:\n  app:\n    repo: bob/app\n    path: ~/code/app\n"), "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "code", "app"), cfg.Projects["app"].Path)
}

func TestNotifyDefaultIsLogOnly(t *testing.T) {
	cfg, err := Parse([]byte("notify:\n  - type: ntfy\n    options: {topic: t}\n"), "")
	require.NoError(t, err)
	assert.Equal(t, "ntfy", cfg.Notify[0].Type)
	assert.Len(t, cfg.Notify, 1)
}

func TestDefaultPolicyReturnsFreshValues(t *testing.T) {
	a := DefaultPolicy()
	a.Rules[0].Paths[0] = "changed"
	a.Gates["low"] = Gate{}
	b := DefaultPolicy()
	assert.Equal(t, "**/migrations/**", b.Rules[0].Paths[0])
	assert.True(t, b.Gates["low"].AutoMerge)
	assert.Equal(t, domain.RiskHigh, b.Rules[0].Risk)
}
