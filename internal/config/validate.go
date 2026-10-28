package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/bobbyrc/foundry/internal/domain"
)

// repoPattern matches a GitHub owner/name pair.
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// intakeNamePattern matches an intake source name. It appears in task refs and in logs, so it is
// kept to lowercase words.
var intakeNamePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Validate checks c and returns every problem it finds, joined into one error. Each message
// starts with the path of the setting at fault, such as "agents.reviewer.harness".
// It expects defaults to have been applied already, as Parse does.
func (c *Config) Validate() error {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	for name, h := range c.Harness {
		if h.Type == "" {
			add("harnesses.%s.type: must not be empty", name)
		}
		if p := h.Price; p != nil && (!validPrice(p.InputPerMTok) || !validPrice(p.OutputPerMTok) || !validPrice(p.CachedInputPerMTok)) {
			add("harnesses.%s.price: prices must be finite numbers, 0 (unknown) or greater", name)
		}
	}

	for name, a := range c.Agents {
		if _, ok := c.Harness[a.Harness]; !ok {
			add("agents.%s.harness: unknown harness %q", name, a.Harness)
		}
		if a.Prompt == "" {
			add("agents.%s.prompt: must not be empty", name)
		}
	}

	for kind, agent := range c.Routing {
		if !isTaskKind(kind) {
			add("routing.%s: unknown task kind %q", kind, kind)
		}
		if _, ok := c.Agents[agent]; !ok {
			add("routing.%s: unknown agent %q", kind, agent)
		}
	}

	for name, p := range c.Policies {
		for i, r := range p.Rules {
			validateRule(add, fmt.Sprintf("policies.%s.rules[%d]", name, i), r)
		}
		for i, r := range p.ExtraRules {
			validateRule(add, fmt.Sprintf("policies.%s.extra_rules[%d]", name, i), r)
		}
		if p.CIGrace.Duration < 0 {
			add("policies.%s.ci_grace: must be 0 (default 3m) or greater", name)
		}
		for level := range p.Gates {
			switch level {
			case string(domain.RiskLow), string(domain.RiskMedium), string(domain.RiskHigh):
			default:
				add("policies.%s.gates.%s: must be low, medium or high", name, level)
			}
		}
	}

	for name, p := range c.Projects {
		prefix := "projects." + name
		if !repoPattern.MatchString(p.Repo) && !filepath.IsAbs(p.Repo) {
			add("%s.repo: want owner/name or an absolute local path, not %q", prefix, p.Repo)
		}
		if p.Loops != nil && p.Loops.Retro.Lookback.Duration < 0 {
			add("%s.loops.retro.lookback: must be 0 (default = every) or greater", prefix)
		}
		if _, ok := c.Policies[p.Policy]; !ok {
			add("%s.policy: unknown policy %q", prefix, p.Policy)
		}
		// Roles are checked as merged, so a project sees the same roles the engine will run.
		agents := c.AgentsFor(name)
		for role, a := range agents {
			if _, ok := c.Harness[a.Harness]; !ok {
				add("%s.agents.%s.harness: unknown harness %q", prefix, role, a.Harness)
			}
			if a.Prompt == "" {
				add("%s.agents.%s.prompt: must not be empty", prefix, role)
			}
		}
		for kind, role := range p.Routing {
			if !isTaskKind(kind) {
				add("%s.routing.%s: unknown task kind %q", prefix, kind, kind)
			}
			if _, ok := agents[role]; !ok {
				add("%s.routing.%s: unknown agent %q", prefix, kind, role)
			}
		}
		seenSource := map[string]bool{}
		for i, src := range p.Intake {
			sprefix := fmt.Sprintf("%s.intake[%d]", prefix, i)
			switch {
			case src.Name == "":
				add("%s.name: must not be empty", sprefix)
			case !intakeNamePattern.MatchString(src.Name):
				add("%s.name: must match ^[a-z0-9_-]+$, not %q", sprefix, src.Name)
			case seenSource[src.Name]:
				add("%s.name: duplicate intake source %q", sprefix, src.Name)
			}
			seenSource[src.Name] = true
			if strings.TrimSpace(src.Command) == "" {
				add("%s.command: must not be empty", sprefix)
			}
			if !isTaskKind(src.Kind) || src.Kind == string(domain.KindReview) || src.Kind == string(domain.KindIncident) {
				add("%s.kind: must be a task kind other than review or incident, not %q", sprefix, src.Kind)
			}
			if src.Timeout.Duration < 0 {
				add("%s.timeout: must be 0 (default 60s) or greater", sprefix)
			}
		}
		if p.Deploy != nil && p.Deploy.Type == "" {
			add("%s.deploy.type: must not be empty", prefix)
		}
		seen := map[string]bool{}
		for i, o := range p.Observe {
			oprefix := fmt.Sprintf("%s.observe[%d]", prefix, i)
			if o.Type == "" {
				add("%s.type: must not be empty", oprefix)
			}
			if seen[o.Name] {
				add("%s.name: duplicate observer %q", oprefix, o.Name)
			}
			seen[o.Name] = true
		}
	}

	if c.SelfProject != "" {
		if _, ok := c.Projects[c.SelfProject]; !ok {
			add("self_project: unknown project %q", c.SelfProject)
		}
	}

	l := c.Limits
	if l.MaxConcurrentRuns <= 0 {
		add("limits.max_concurrent_runs: must be greater than 0")
	}
	if l.MaxRunsPerProject <= 0 {
		add("limits.max_runs_per_project: must be greater than 0")
	}
	if l.StallTimeout.Duration <= 0 {
		add("limits.stall_timeout: must be greater than 0")
	}
	if l.RunTimeout.Duration <= 0 {
		add("limits.run_timeout: must be greater than 0")
	}
	if l.MaxAttempts <= 0 {
		add("limits.max_attempts: must be greater than 0")
	}
	if c.Loops.Retro.Lookback.Duration < 0 {
		add("loops.retro.lookback: must be 0 (default = every) or greater")
	}
	if l.DailyBudgetUSD < 0 {
		add("limits.daily_budget_usd: must be 0 (no limit) or greater, not %v", l.DailyBudgetUSD)
	}

	for i, h := range c.Server.AllowedHosts {
		prefix := fmt.Sprintf("server.allowed_hosts[%d]", i)
		switch {
		case strings.TrimSpace(h) == "":
			add("%s: must not be empty", prefix)
		case strings.Contains(h, "://"):
			add("%s: want a host name, not a URL with a scheme, not %q", prefix, h)
		case strings.Contains(h, "/"):
			add("%s: must not include a path, not %q", prefix, h)
		}
	}

	// Notifications link to the server with this URL, so a bare "host:port" would make broken links.
	if u, err := url.Parse(c.Server.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		add("server.public_url %q must be an absolute http or https URL", c.Server.PublicURL)
	}

	for i, n := range c.Notify {
		prefix := fmt.Sprintf("notify[%d]", i)
		if n.Type == "" {
			add("%s.type: must not be empty", prefix)
		}
		switch n.MinPriority {
		case "", "low", "normal", "high":
		default:
			add("%s.min_priority: must be low, normal or high, not %q", prefix, n.MinPriority)
		}
	}

	return errors.Join(errs...)
}

// isTaskKind reports whether kind names a domain.TaskKind.
func isTaskKind(kind string) bool {
	switch domain.TaskKind(kind) {
	case domain.KindFeature, domain.KindBugfix, domain.KindIssue, domain.KindIncident,
		domain.KindReview, domain.KindChore, domain.KindImprove, domain.KindQuestion:
		return true
	}
	return false
}

// validateRule reports each problem with one policy rule. prefix is the rule's settings path,
// such as "policies.default.rules[0]".
func validateRule(add func(string, ...any), prefix string, r Rule) {
	switch r.Risk {
	case domain.RiskLow, domain.RiskMedium, domain.RiskHigh:
	default:
		add("%s.risk: must be low, medium or high, not %q", prefix, r.Risk)
	}
	if len(r.Paths) == 0 && len(r.Labels) == 0 && len(r.Kinds) == 0 && len(r.Sources) == 0 {
		add("%s: must set paths, labels, kinds or sources", prefix)
	}
	for j, pat := range r.Paths {
		if !doublestar.ValidatePattern(pat) {
			add("%s.paths[%d]: invalid glob %q", prefix, j, pat)
		}
	}
	for j, src := range r.Sources {
		if strings.TrimSpace(src) == "" {
			add("%s.sources[%d]: must not be empty", prefix, j)
		}
	}
}

// validPrice reports whether a per-million-token price is usable: finite and not negative. YAML
// accepts .inf and .nan, and either would make cost estimates meaningless or unencodable as JSON.
func validPrice(v float64) bool {
	return v >= 0 && !math.IsInf(v, 0)
}
