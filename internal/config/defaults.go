package config

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bobbyrc/onlet/internal/domain"
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
		CIGrace:  Duration{3 * time.Minute},
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
		// Onlet manages its own repository, so an agent's change to onlet's gates, agent
		// prompts, environment allowlist or who may do what in the API must never auto-merge.
		// These paths exist only in onlet's repo, so this rule is inert in every other project.
		{
			Risk:   domain.RiskHigh,
			Reason: "onlet's own gates or agent instructions",
			Paths: []string{
				"internal/engine/prompts/**", "internal/policy/**", "internal/engine/loop_shepherd.go",
				"internal/engine/helpers.go", "internal/config/defaults.go", "internal/sysproc/**",
				"internal/auth/**", "internal/api/auth.go", "internal/api/routes.go",
			},
		},
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
			Reason: "agent instructions or onlet config",
			Paths:  []string{".onlet/**", "**/onlet.yaml", "**/AGENTS.md", "**/CLAUDE.md"},
		},
		{
			Risk:   domain.RiskMedium,
			Reason: "dependency manifest",
			Paths: []string{
				"**/go.mod", "**/package.json", "**/Cargo.toml", "**/pubspec.yaml",
				"**/requirements*.txt", "**/pyproject.toml", "**/Gemfile",
			},
		},
		// Follow-ups and retrospectives are work onlet assigned itself. Both had design
		// problems tonight, so their changes get a human look until the owner relaxes this.
		{
			Risk:    domain.RiskMedium,
			Reason:  "work onlet assigned itself (follow-up or retrospective)",
			Sources: []string{"followup", "retro"},
		},
	}
}

func applyServerDefaults(c *Config) {
	if c.Server.Addr == "" {
		c.Server.Addr = "127.0.0.1:7777"
	}
	if c.Server.DataDir == "" {
		c.Server.DataDir = "~/.local/share/onlet"
	}
	c.Server.DataDir = expandHome(c.Server.DataDir)
	if c.Server.PublicURL == "" {
		c.Server.PublicURL = LocalURL(c.Server.Addr)
	}
	if c.Server.RetainEvents.Duration == 0 {
		c.Server.RetainEvents.Duration = 720 * time.Hour
	}
	if c.Server.RetainRunLogs.Duration == 0 {
		c.Server.RetainRunLogs.Duration = 2160 * time.Hour
	}
}

// LocalURL is the http URL a client on this machine uses to reach a server listening on addr.
// A wildcard or empty host (0.0.0.0, ::, ":7777") is replaced by loopback, because the server's
// Host header check accepts localhost, 127.0.0.1 and ::1 but rejects a wildcard address with 421.
func LocalURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
		if ip != nil && ip.To4() == nil {
			host = "::1"
		}
	}
	return "http://" + net.JoinHostPort(host, port)
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

// BuiltinAgents returns the six built-in agents by name, with prompts written as
// "builtin:<name>". Each call returns a fresh map.
func BuiltinAgents() map[string]Agent {
	return map[string]Agent{
		"orchestrator": {
			Harness:     "claude",
			Model:       "opus",
			Prompt:      "builtin:orchestrator",
			Description: "The agent you chat with. Turns what you ask for into tasks for the other agents and reports back; never edits code.",
		},
		"implementer": {
			Harness:     "codex",
			Prompt:      "builtin:implementer",
			Description: "Writes and tests the code change for a feature, bug fix, issue or chore; on a re-run, fixes the CI failure or review findings first.",
		},
		"reviewer": {
			Harness:     "claude",
			Model:       "sonnet",
			Prompt:      "builtin:reviewer",
			Description: "Reviews each pull request's diff for defects and risk before merge, without the author's reasoning; approves or requests changes.",
		},
		"triager": {
			Harness:     "claude",
			Model:       "sonnet",
			Prompt:      "builtin:triager",
			Description: "Answers questions about the code, and triages bug reports: reproduces them and splits them into well-scoped follow-up tasks.",
		},
		"sre": {
			Harness:     "claude",
			Model:       "sonnet",
			Prompt:      "builtin:sre",
			Description: "Investigates a failed deploy or production error from its evidence, finds the root cause and ships the smallest safe fix.",
		},
		"improver": {
			Harness:     "codex",
			Prompt:      "builtin:improver",
			Description: "Fixes the cause of friction agents report and of problems retrospectives find: tooling, docs, flaky tests, agent instructions.",
		},
	}
}

// applyAgentDefaults adds the built-in agents the file does not name, and sets each agent's
// Origin. An entry that names a built-in and sets nothing but Disabled turns that agent off
// without changing it. Any other entry that names a built-in replaces it whole.
func applyAgentDefaults(c *Config) {
	if c.Agents == nil {
		c.Agents = map[string]Agent{}
	}
	builtins := BuiltinAgents()
	for name, def := range builtins {
		a, ok := c.Agents[name]
		switch {
		case !ok:
			def.Origin = OriginBuiltin
			c.Agents[name] = def
		case onlyDisabled(a):
			def.Disabled = a.Disabled
			def.Origin = OriginBuiltin
			c.Agents[name] = def
		default:
			a.Origin = OriginEdited
			c.Agents[name] = a
		}
	}
	for name, a := range c.Agents {
		if _, ok := builtins[name]; !ok {
			a.Origin = OriginCustom
			c.Agents[name] = a
		}
	}
	dir := configDir(c)
	for name, a := range c.Agents {
		c.Agents[name] = resolvePrompt(dir, a)
	}
}

// onlyDisabled reports whether a sets no field but possibly Disabled.
func onlyDisabled(a Agent) bool {
	return a.Harness == "" && a.Model == "" && a.Prompt == "" && a.PromptText == "" &&
		len(a.Args) == 0 && a.Description == ""
}

// configDir is the directory of the config file, or "" when the config has no path.
func configDir(c *Config) string {
	if c.Path == "" {
		return ""
	}
	return filepath.Dir(c.Path)
}

// resolvePrompt makes a relative prompt path absolute against dir. Builtin and absolute prompts
// and empty prompts are left alone.
func resolvePrompt(dir string, a Agent) Agent {
	if a.Prompt == "" || strings.HasPrefix(a.Prompt, "builtin:") || filepath.IsAbs(a.Prompt) {
		return a
	}
	a.Prompt = filepath.Join(dir, a.Prompt)
	return a
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
	c.defaultRoutes = map[string]bool{}
	for kind, agent := range defaults {
		if _, ok := c.Routing[kind]; !ok {
			c.Routing[kind] = agent
			c.defaultRoutes[kind] = true
		}
	}
}

// IsDefaultRoute reports whether the route for kind came from the built-in defaults rather than
// from the file.
func (c *Config) IsDefaultRoute(kind string) bool {
	return c.defaultRoutes[kind]
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
		if p.CIGrace.Duration == 0 {
			p.CIGrace.Duration = 3 * time.Minute
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
	dir := configDir(c)
	for name, p := range c.Projects {
		if p == nil {
			p = &Project{}
			c.Projects[name] = p
		}
		p.Name = name
		for role, a := range p.Agents {
			p.Agents[role] = resolvePrompt(dir, a)
		}
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
		if p.Deploy != nil && p.Deploy.Timeout.Duration == 0 {
			p.Deploy.Timeout.Duration = 30 * time.Minute
		}
		for i := range p.Observe {
			if p.Observe[i].Name == "" {
				p.Observe[i].Name = p.Observe[i].Type
			}
		}
		for i := range p.Intake {
			if p.Intake[i].Kind == "" {
				p.Intake[i].Kind = string(domain.KindFeature)
			}
			if p.Intake[i].Timeout.Duration == 0 {
				p.Intake[i].Timeout.Duration = time.Minute
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
		l.IssueIntake.Label = "onlet"
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
	if l.Retro.Every.Duration == 0 {
		l.Retro.Every.Duration = 24 * time.Hour
	}
	if l.Retro.Lookback.Duration == 0 {
		l.Retro.Lookback.Duration = l.Retro.Every.Duration
	}
	if l.ExecIntake.Every.Duration == 0 {
		l.ExecIntake.Every.Duration = time.Minute
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
