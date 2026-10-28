package policy

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
)

// Evaluate decides how risky a change is and which gates it must pass before it merges. It is
// deterministic and performs no I/O. Risk starts at low and can only go up: rules, size limits
// and the agent's own rating each raise it. The returned Reasons is never nil.
func Evaluate(p config.Policy, in Input) Decision {
	risk := domain.RiskLow
	reasons := []string{}

	paths := make([]string, len(in.Files))
	for i, f := range in.Files {
		paths[i] = strings.TrimPrefix(f.Path, "./")
	}

	// ExtraRules apply on top of Rules (or the defaults), with the same matching.
	for _, rule := range slices.Concat(p.Rules, p.ExtraRules) {
		file, pathMatched, ok := matchRule(rule, paths, in)
		if !ok {
			continue
		}
		risk = domain.MaxRisk(risk, rule.Risk)
		reason := rule.Reason
		if pathMatched {
			reason += " (" + file + ")"
		}
		reasons = append(reasons, reason)
	}

	total := 0
	for _, f := range in.Files {
		total += f.Added + f.Deleted
	}
	if p.MaxLines > 0 && total > p.MaxLines {
		risk = domain.MaxRisk(risk, domain.RiskMedium)
		reasons = append(reasons, fmt.Sprintf("%d changed lines exceeds max_lines %d", total, p.MaxLines))
	}
	if p.MaxFiles > 0 && len(in.Files) > p.MaxFiles {
		risk = domain.MaxRisk(risk, domain.RiskMedium)
		reasons = append(reasons, fmt.Sprintf("%d files changed exceeds max_files %d", len(in.Files), p.MaxFiles))
	}

	switch {
	case in.AgentRisk == domain.RiskUnknown:
		risk = domain.MaxRisk(risk, domain.RiskMedium)
		reasons = append(reasons, "agent did not rate its risk")
	case !slices.Contains([]domain.Risk{domain.RiskLow, domain.RiskMedium, domain.RiskHigh}, in.AgentRisk):
		risk = domain.RiskHigh
		reasons = append(reasons, fmt.Sprintf("agent gave an unrecognised risk %q", string(in.AgentRisk)))
	case in.AgentRisk.Rank() > risk.Rank():
		risk = in.AgentRisk
		reason := "agent rated risk " + string(in.AgentRisk)
		if len(in.AgentRiskReasons) > 0 {
			reason += ": " + strings.Join(in.AgentRiskReasons, "; ")
		}
		reasons = append(reasons, reason)
	}

	g, ok := p.Gates[string(risk)]
	if !ok {
		g = config.Gate{RequireReview: true, RequireCI: true}
	}
	return Decision{
		Risk:            risk,
		Reasons:         reasons,
		RequireReview:   g.RequireReview,
		RequireCI:       g.RequireCI,
		AutoMerge:       g.AutoMerge,
		RequireApproval: !g.AutoMerge,
	}
}

// matchRule reports whether every non-empty criterion of rule matches. It returns the first
// changed file that matched the rule's Paths, and whether Paths was a criterion that matched.
// A rule with no criteria never matches.
func matchRule(rule config.Rule, paths []string, in Input) (file string, pathMatched, ok bool) {
	if len(rule.Paths) == 0 && len(rule.Labels) == 0 && len(rule.Kinds) == 0 {
		return "", false, false
	}
	if len(rule.Paths) > 0 {
		file, pathMatched = firstPathMatch(rule.Paths, paths)
		if !pathMatched {
			return "", false, false
		}
	}
	if len(rule.Labels) > 0 && !anyLabelMatches(rule.Labels, in.Labels) {
		return "", false, false
	}
	if len(rule.Kinds) > 0 && !slices.Contains(rule.Kinds, string(in.Kind)) {
		return "", false, false
	}
	return file, pathMatched, true
}

// firstPathMatch returns the first path, in file order, that matches any of the patterns.
// Malformed patterns are skipped; config validation reports them.
func firstPathMatch(patterns, paths []string) (string, bool) {
	for _, path := range paths {
		for _, pattern := range patterns {
			if ok, err := doublestar.Match(pattern, path); err == nil && ok {
				return path, true
			}
		}
	}
	return "", false
}

// anyLabelMatches reports whether any label in have equals any label in want, ignoring case.
func anyLabelMatches(want, have []string) bool {
	for _, h := range have {
		for _, w := range want {
			if strings.EqualFold(h, w) {
				return true
			}
		}
	}
	return false
}
