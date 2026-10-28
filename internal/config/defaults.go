package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
)

// ApplyDefaults fills every zero value in c with its default. It never overwrites a value the
// user set. It also expands a leading "~" in paths and makes relative agent prompt paths
// absolute against the directory of c.Path.
func ApplyDefaults(c *Config) {
	applyServerDefaults(c)
	applyLimitDefaults(c)
	applyHarnessDefaults(c)
	applyAgentDefaults(c)
	applyRoutingDefaults(c)
	applyPolicyDefaults(c)
	applyProjectDefaults(c)
	applyLoopDefaults(c)
	if len(c.Notify) == 0 {
		c.Notify = []Notifier{{Type: "log"}}
	}
}

// DefaultPolicy returns the policy used when a project names none. Each call returns fresh
// values.
func DefaultPolicy() Policy {
	return Policy{
		MaxLines: 400,
		MaxFiles: 20,
		Rules:    DefaultRules(),
		Gates:    DefaultGates(),
	}
}

// DefaultGates returns the per-risk gates used when a policy sets none. Each call returns a
// fresh map.
func DefaultGates() map[string]Gate {
	return map[string]Gate{
		"low":    {AutoMerge: true, RequireReview: true, RequireCI: true},
		"medium": {AutoMerge: false, RequireReview: true, RequireCI: true},
		"high":   {AutoMerge: false, RequireReview: true, RequireCI: true},
	}
}

// DefaultRules returns the path rules that raise a change's risk. Each call returns fresh
// slices.
func DefaultRules() []Rule {
	return []Rule{
		{
			Risk:   domain.RiskHigh,
			Reason: "database schema or migration",
			Paths:  []string{"**/migrations/**", "**/migrate/**", "**/*.sql", "**/schema.prisma", "**/drizzle/**"},
		},
		{
			Risk:   domain.RiskHigh,
			Reason: "CI, deploy or infrastructure",
			Paths: []string{
				".github/workflows/**", "**/.gitlab-ci.yml", "**/Dockerfile", "**/vercel.json",
				"**/fly.toml", "**/render.yaml", "deploy/**", "infra/**", "**/*.tf", "**/k8s/**",
				"**/Makefile",
			},
		},
		{
			Risk:   domain.RiskHigh,
			Reason: "auth, secrets or security",
			Paths: []string{
				"**/auth/**", "**/*auth*", "**/security/**", "**/*secret*", "**/.env*", "**/*.pem",
				"**/*.key",
			},
		},
		{
			Risk:   domain.RiskMedium,
			Reason: "agent instructions or foundry config",
			Paths:  []string{".foundry/**", "**/foundry.yaml", "**/AGENTS.md", "**/CLAUDE.md"},
		},
		{
			Risk:   domain.RiskMedium,
			Reason: "dependency manifest",
			Paths: []string{
				"**/go.mod", "**/package.json", "**/Cargo.toml", "**/pubspec.yaml",
				"**/requirements*.txt", "**/pyproject.toml", "**/Gemfile",
			},
		},
	}
}

func applyServerDefaults(c *Config) {
	if c.Server.Addr == "" {
		c.Server.Addr = "127.0.0.1:7777"
	}
	if c.Server.DataDir == "" {
		c.Server.DataDir = "~/.local/share/foundry"
	}
	c.Server.DataDir = expandHome(c.Server.DataDir)
	if c.Server.PublicURL == "" {
		c.Server.PublicURL = "http://" + c.Server.Addr
	}
}

func applyLimitDefaults(c *Config) {
	l := &c.Limits
	if l.MaxConcurrentRuns == 0 {
		l.MaxConcurrentRuns = 3
	}
	if l.MaxRunsPerProject == 0 {
		l.MaxRunsPerProject = 1
	}
	if l.StallTimeout.Duration == 0 {
		l.StallTimeout.Duration = 15 * time.Minute
	}
	if l.RunTimeout.Duration == 0 {
		l.RunTimeout.Duration = 2 * time.Hour
	}
	if l.MaxAttempts == 0 {
		l.MaxAttempts = 3
	}
}

func applyHarnessDefaults(c *Config) {
	if c.Harness == nil {
		c.Harness = map[string]Harness{}
	}
	if _, ok := c.Harness["claude"]; !ok {
		c.Harness["claude"] = Harness{Type: "claude-code"}
	}
	if _, ok := c.Harness["codex"]; !ok {
		c.Harness["codex"] = Harness{Type: "codex"}
	}
}

func applyAgentDefaults(c *Config) {
	if c.Agents == nil {
		c.Agents = map[string]Agent{}
	}
	defaults := map[string]Agent{
		"orchestrator": {
			Harness:     "claude",
			Model:       "opus",
			Prompt:      "builtin:orchestrator",
			Description: "The agent you chat with. Plans work and delegates it to other roles; never edits code itself.",
		},
		"implementer": {
			Harness:     "codex",
			Prompt:      "builtin:implementer",
			Description: "Writes code for features, bug fixes, issues and chores, with tests, and commits it on the task branch.",
		},
		"reviewer": {
			Harness:     "claude",
			Model:       "sonnet",
			Prompt:      "builtin:reviewer",
			Description: "Reviews a pull request's diff for defects and risk; approves or requests changes.",
		},
		"triager": {
			Harness:     "claude",
			Model:       "sonnet",
			Prompt:      "builtin:triager",
			Description: "Answers questions and triages issues: reproduces, scopes, and splits them into tasks.",
		},
		"sre": {
			Harness:     "claude",
			Model:       "sonnet",
			Prompt:      "builtin:sre",
			Description: "Investigates production incidents from observability data and ships the smallest safe fix.",
		},
		"improver": {
			Harness:     "codex",
			Prompt:      "builtin:improver",
			Description: "Fixes friction in the factory and the codebase: tooling, docs, flaky tests, agent instructions.",
		},
	}
	for name, a := range defaults {
		if _, ok := c.Agents[name]; !ok {
			c.Agents[name] = a
		}
	}
	dir := ""
	if c.Path != "" {
		dir = filepath.Dir(c.Path)
	}
	for name, a := range c.Agents {
		if a.Prompt == "" || strings.HasPrefix(a.Prompt, "builtin:") || filepath.IsAbs(a.Prompt) {
			continue
		}
		a.Prompt = filepath.Join(dir, a.Prompt)
		c.Agents[name] = a
	}
}

func applyRoutingDefaults(c *Config) {
	if c.Routing == nil {
		c.Routing = map[string]string{}
	}
	defaults := map[string]string{
		string(domain.KindFeature):  "implementer",
		string(domain.KindBugfix):   "implementer",
		string(domain.KindIssue):    "implementer",
		string(domain.KindChore):    "implementer",
		string(domain.KindIncident): "sre",
		string(domain.KindReview):   "reviewer",
		string(domain.KindImprove):  "improver",
		string(domain.KindQuestion): "triager",
	}
	for kind, agent := range defaults {
		if _, ok := c.Routing[kind]; !ok {
			c.Routing[kind] = agent
		}
	}
}

func applyPolicyDefaults(c *Config) {
	if c.Policies == nil {
		c.Policies = map[string]Policy{}
	}
	if _, ok := c.Policies["default"]; !ok {
		c.Policies["default"] = DefaultPolicy()
	}
	for name, p := range c.Policies {
		if p.MaxLines == 0 {
			p.MaxLines = 400
		}
		if p.MaxFiles == 0 {
			p.MaxFiles = 20
		}
		if p.Rules == nil {
			p.Rules = DefaultRules()
		}
		if p.Gates == nil {
			p.Gates = DefaultGates()
		}
		for level, g := range DefaultGates() {
			if _, ok := p.Gates[level]; !ok {
				p.Gates[level] = g
			}
		}
		c.Policies[name] = p
	}
}

func applyProjectDefaults(c *Config) {
	if c.Projects == nil {
		c.Projects = map[string]*Project{}
	}
	for name, p := range c.Projects {
		if p == nil {
			p = &Project{}
			c.Projects[name] = p
		}
		p.Name = name
		if p.DefaultBranch == "" {
			p.DefaultBranch = "main"
		}
		if p.Policy == "" {
			p.Policy = "default"
		}
		if p.Path == "" {
			p.Path = filepath.Join(c.Server.DataDir, "repos", name)
		}
		p.Path = expandHome(p.Path)
		if p.Deploy != nil && p.Deploy.VerifyFor.Duration == 0 {
			p.Deploy.VerifyFor.Duration = 10 * time.Minute
		}
		for i := range p.Observe {
			if p.Observe[i].Name == "" {
				p.Observe[i].Name = p.Observe[i].Type
			}
		}
	}
}

func applyLoopDefaults(c *Config) {
	l := &c.Loops
	if l.IssueIntake.Every.Duration == 0 {
		l.IssueIntake.Every.Duration = 5 * time.Minute
	}
	if l.IssueIntake.Label == "" {
		l.IssueIntake.Label = "foundry"
	}
	if l.PRShepherd.Every.Duration == 0 {
		l.PRShepherd.Every.Duration = time.Minute
	}
	if l.DeployWatch.Every.Duration == 0 {
		l.DeployWatch.Every.Duration = 30 * time.Second
	}
	if l.ProdWatch.Every.Duration == 0 {
		l.ProdWatch.Every.Duration = 10 * time.Minute
	}
	if l.ProdWatch.MinLevel == "" {
		l.ProdWatch.MinLevel = "error"
	}
	if l.ProdWatch.MinCount == 0 {
		l.ProdWatch.MinCount = 1
	}
	if l.Friction.Every.Duration == 0 {
		l.Friction.Every.Duration = 10 * time.Minute
	}
	if l.Friction.Label == "" {
		l.Friction.Label = "friction"
	}
	if l.Friction.MinCount == 0 {
		l.Friction.MinCount = 1
	}
}

// expandHome replaces a leading "~" with the user's home directory. If the home directory is
// unknown, the path is returned unchanged.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[1:])
}
