package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultsOnEmptyConfig(t *testing.T) {
	cfg, err := Parse(nil, "")
	require.NoError(t, err)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:7777", cfg.Server.Addr)
	assert.Equal(t, filepath.Join(home, ".local", "share", "onett"), cfg.Server.DataDir)
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
	assert.Len(t, pol.Rules, 7)
	assert.Nil(t, pol.ExtraRules, "extra rules default to none")
	assert.Equal(t, DefaultGates(), pol.Gates)

	assert.Equal(t, "onett", cfg.Loops.IssueIntake.Label)
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
  data_dir: /var/lib/onett
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
	assert.Equal(t, "/var/lib/onett", cfg.Server.DataDir)
	assert.Equal(t, "http://127.0.0.1:9000", cfg.Server.PublicURL)
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
	t.Setenv("ONETT_TEST_HOST", "example.test")
	cfg, err := Parse([]byte(`
server:
  public_url: "https://${ONETT_TEST_HOST}/onett"
projects:
  app:
    repo: bob/app
    deploy:
      type: command
      options:
        rollback: "vercel rollback $ONETT_TEST_HOST --yes"
`), "")
	require.NoError(t, err)
	assert.Equal(t, "https://example.test/onett", cfg.Server.PublicURL)
	assert.Equal(t, "vercel rollback $ONETT_TEST_HOST --yes", cfg.Projects["app"].Deploy.Options["rollback"])
}

func TestRulesNilVersusEmpty(t *testing.T) {
	cfg, err := Parse([]byte(`
policies:
  absent: {}
  empty:
    rules: []
`), "")
	require.NoError(t, err)

	assert.Len(t, cfg.Policies["absent"].Rules, 7, "absent rules take the defaults")
	require.NotNil(t, cfg.Policies["empty"].Rules, "an explicit empty list stays non-nil")
	assert.Empty(t, cfg.Policies["empty"].Rules)
}

func TestPromptPathsResolveAgainstConfigDir(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "onett.yaml"))
	require.NoError(t, err)

	dir, err := filepath.Abs("testdata")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "onett.yaml"), cfg.Path)
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
	a.Rules[1].Paths[0] = "changed"
	a.Gates["low"] = Gate{}
	b := DefaultPolicy()
	assert.Equal(t, "**/migrations/**", b.Rules[1].Paths[0])
	assert.True(t, b.Gates["low"].AutoMerge)
	assert.Equal(t, domain.RiskHigh, b.Rules[1].Risk)
}

func TestProjectAgentPromptPathsResolveAgainstConfigDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "onett.yaml")
	cfg, err := Parse([]byte(`
projects:
  app:
    repo: bob/app
    agents:
      implementer:
        prompt: prompts/app.md
      designer:
        harness: claude
        prompt: /abs/designer.md
`), path)
	require.NoError(t, err)

	app := cfg.Projects["app"]
	assert.Equal(t, filepath.Join(dir, "prompts", "app.md"), app.Agents["implementer"].Prompt)
	assert.Equal(t, "/abs/designer.md", app.Agents["designer"].Prompt, "absolute paths are left alone")
	assert.Equal(t, app.Agents["implementer"].Prompt, cfg.AgentsFor("app")["implementer"].Prompt, "merged roles see the resolved path")
}

func TestCIGraceDefaultsToThreeMinutesForEveryPolicy(t *testing.T) {
	assert.Equal(t, 3*time.Minute, DefaultPolicy().CIGrace.Duration)

	cfg, err := Parse([]byte("policies:\n  strict:\n    max_lines: 10\n"), "")
	require.NoError(t, err)
	assert.Equal(t, 3*time.Minute, cfg.Policies["default"].CIGrace.Duration)
	assert.Equal(t, 3*time.Minute, cfg.Policies["strict"].CIGrace.Duration)
}

func TestCIGraceUserValueIsKept(t *testing.T) {
	cfg, err := Parse([]byte("policies:\n  quick:\n    ci_grace: 45s\n"), "")
	require.NoError(t, err)
	assert.Equal(t, 45*time.Second, cfg.Policies["quick"].CIGrace.Duration)
}

func TestRetroDefaultsToDailyWithLookbackOfEvery(t *testing.T) {
	cfg, err := Parse([]byte(""), "")
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, cfg.Loops.Retro.Every.Duration)
	assert.Equal(t, 24*time.Hour, cfg.Loops.Retro.Lookback.Duration)
	assert.Nil(t, cfg.Loops.Retro.Enabled, "unset enabled is left to RetroOn")

	cfg, err = Parse([]byte("loops:\n  retro:\n    every: 6h\n"), "")
	require.NoError(t, err)
	assert.Equal(t, 6*time.Hour, cfg.Loops.Retro.Lookback.Duration, "lookback 0 takes every")
}

// Self-assigned work (follow-ups and retrospectives) is rated medium by default, and the rule
// comes last so it does not change the order of the path rules.
func TestDefaultRulesRateSelfAssignedWorkMedium(t *testing.T) {
	rules := DefaultRules()
	last := rules[len(rules)-1]
	assert.Equal(t, domain.RiskMedium, last.Risk)
	assert.Equal(t, "work onett assigned itself (follow-up or retrospective)", last.Reason)
	assert.Equal(t, []string{"followup", "retro"}, last.Sources)
	assert.Empty(t, last.Paths)
}

// Exec intake defaults: the loop runs every minute, and a source takes kind feature and a 60s
// timeout unless it sets them.
func TestExecIntakeDefaults(t *testing.T) {
	cfg, err := Parse([]byte("projects:\n  app:\n    repo: bob/app\n    intake:\n      - name: inbox\n        command: cat inbox.jsonl\n"), "")
	require.NoError(t, err)
	assert.Equal(t, time.Minute, cfg.Loops.ExecIntake.Every.Duration)
	src := cfg.Projects["app"].Intake[0]
	assert.Equal(t, "feature", src.Kind)
	assert.Equal(t, time.Minute, src.Timeout.Duration)

	cfg, err = Parse([]byte("projects:\n  app:\n    repo: bob/app\n    intake:\n      - name: inbox\n        command: x\n        kind: chore\n        timeout: 5s\n"), "")
	require.NoError(t, err)
	src = cfg.Projects["app"].Intake[0]
	assert.Equal(t, "chore", src.Kind, "a set kind is kept")
	assert.Equal(t, 5*time.Second, src.Timeout.Duration, "a set timeout is kept")
}

func TestLocalURL(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:7777": "http://127.0.0.1:7777",
		"0.0.0.0:7777":   "http://127.0.0.1:7777",
		":7777":          "http://127.0.0.1:7777",
		"[::]:7777":      "http://[::1]:7777",
		"localhost:7777": "http://localhost:7777",
		"onett.lan:80":   "http://onett.lan:80",
		"[::1]:7777":     "http://[::1]:7777",
		"garbage":        "http://garbage",
	}
	for addr, want := range cases {
		assert.Equal(t, want, LocalURL(addr), addr)
	}
}

func TestDefaultPublicURLUsesLoopbackForWildcardAddr(t *testing.T) {
	cfg, err := Parse([]byte("server:\n  addr: 0.0.0.0:7777\n"), "")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:7777", cfg.Server.Addr)
	assert.Equal(t, "http://127.0.0.1:7777", cfg.Server.PublicURL)
}
