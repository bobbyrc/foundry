package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/bobbyrc/foundry/internal/domain"
)

// repoPattern matches a GitHub owner/name pair.
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

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
			prefix := fmt.Sprintf("policies.%s.rules[%d]", name, i)
			switch r.Risk {
			case domain.RiskLow, domain.RiskMedium, domain.RiskHigh:
			default:
				add("%s.risk: must be low, medium or high, not %q", prefix, r.Risk)
			}
			if len(r.Paths) == 0 && len(r.Labels) == 0 && len(r.Kinds) == 0 {
				add("%s: must set paths, labels or kinds", prefix)
			}
			for j, pat := range r.Paths {
				if !doublestar.ValidatePattern(pat) {
					add("%s.paths[%d]: invalid glob %q", prefix, j, pat)
				}
			}
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
