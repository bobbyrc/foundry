package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
)

// testGates mirrors a sensible default: only low risk auto-merges.
func testGates() map[string]config.Gate {
	return map[string]config.Gate{
		"low":    {AutoMerge: true, RequireReview: true, RequireCI: true},
		"medium": {AutoMerge: false, RequireReview: true, RequireCI: true},
		"high":   {AutoMerge: false, RequireReview: true, RequireCI: true},
	}
}

func lowGate() Decision {
	return Decision{Risk: domain.RiskLow, Reasons: []string{}, RequireReview: true, RequireCI: true, AutoMerge: true}
}

func TestEvaluate(t *testing.T) {
	migrationRule := config.Rule{Paths: []string{"**/migrations/**"}, Risk: domain.RiskHigh, Reason: "database migration"}

	tests := []struct {
		name   string
		policy config.Policy
		in     Input
		want   Decision
	}{
		{
			name:   "no rules and low agent risk stays low and auto-merges",
			policy: config.Policy{Gates: testGates()},
			in:     Input{Kind: domain.KindFeature, AgentRisk: domain.RiskLow},
			want:   lowGate(),
		},
		{
			name:   "path rule raises risk and names the file",
			policy: config.Policy{Rules: []config.Rule{migrationRule}, Gates: testGates()},
			in: Input{
				Kind:      domain.KindFeature,
				Files:     []FileChange{{Path: "db/migrations/001.sql", Added: 3}},
				AgentRisk: domain.RiskLow,
			},
			want: Decision{
				Risk:            domain.RiskHigh,
				Reasons:         []string{"database migration (db/migrations/001.sql)"},
				RequireReview:   true,
				RequireCI:       true,
				RequireApproval: true,
			},
		},
		{
			name:   "leading ./ is stripped before matching",
			policy: config.Policy{Rules: []config.Rule{migrationRule}, Gates: testGates()},
			in: Input{
				Kind:      domain.KindFeature,
				Files:     []FileChange{{Path: "./db/migrations/001.sql", Added: 1}},
				AgentRisk: domain.RiskLow,
			},
			want: Decision{
				Risk:            domain.RiskHigh,
				Reasons:         []string{"database migration (db/migrations/001.sql)"},
				RequireReview:   true,
				RequireCI:       true,
				RequireApproval: true,
			},
		},
		{
			name: "criteria are ANDed: paths and kinds must both match",
			policy: config.Policy{
				Rules: []config.Rule{{
					Paths:  []string{"auth/**"},
					Kinds:  []string{string(domain.KindBugfix)},
					Risk:   domain.RiskHigh,
					Reason: "auth bugfix",
				}},
				Gates: testGates(),
			},
			in: Input{
				Kind:      domain.KindFeature,
				Files:     []FileChange{{Path: "auth/login.go", Added: 1}},
				AgentRisk: domain.RiskLow,
			},
			want: lowGate(),
		},
		{
			name: "criteria are ANDed: both matching fires the rule",
			policy: config.Policy{
				Rules: []config.Rule{{
					Paths:  []string{"auth/**"},
					Kinds:  []string{string(domain.KindBugfix)},
					Risk:   domain.RiskHigh,
					Reason: "auth bugfix",
				}},
				Gates: testGates(),
			},
			in: Input{
				Kind:      domain.KindBugfix,
				Files:     []FileChange{{Path: "auth/login.go", Added: 1}},
				AgentRisk: domain.RiskLow,
			},
			want: Decision{
				Risk:            domain.RiskHigh,
				Reasons:         []string{"auth bugfix (auth/login.go)"},
				RequireReview:   true,
				RequireCI:       true,
				RequireApproval: true,
			},
		},
		{
			name: "labels match case-insensitively",
			policy: config.Policy{
				Rules: []config.Rule{{Labels: []string{"Security"}, Risk: domain.RiskHigh, Reason: "security label"}},
				Gates: testGates(),
			},
			in: Input{Kind: domain.KindFeature, Labels: []string{"SECURITY"}, AgentRisk: domain.RiskLow},
			want: Decision{
				Risk:            domain.RiskHigh,
				Reasons:         []string{"security label"},
				RequireReview:   true,
				RequireCI:       true,
				RequireApproval: true,
			},
		},
		{
			name: "label rule does not match an unrelated label",
			policy: config.Policy{
				Rules: []config.Rule{{Labels: []string{"security"}, Risk: domain.RiskHigh, Reason: "security label"}},
				Gates: testGates(),
			},
			in:   Input{Kind: domain.KindFeature, Labels: []string{"docs"}, AgentRisk: domain.RiskLow},
			want: lowGate(),
		},
		{
			name: "rule with no criteria never matches",
			policy: config.Policy{
				Rules: []config.Rule{{Risk: domain.RiskHigh, Reason: "empty"}},
				Gates: testGates(),
			},
			in:   Input{Kind: domain.KindFeature, AgentRisk: domain.RiskLow},
			want: lowGate(),
		},
		{
			name:   "max_lines equal to the limit does not bump risk",
			policy: config.Policy{MaxLines: 10, Gates: testGates()},
			in: Input{
				Kind:      domain.KindFeature,
				Files:     []FileChange{{Path: "a.go", Added: 6, Deleted: 4}},
				AgentRisk: domain.RiskLow,
			},
			want: lowGate(),
		},
		{
			name:   "max_lines exceeded bumps to medium with a reason",
			policy: config.Policy{MaxLines: 10, Gates: testGates()},
			in: Input{
				Kind:      domain.KindFeature,
				Files:     []FileChange{{Path: "a.go", Added: 6, Deleted: 5}},
				AgentRisk: domain.RiskLow,
			},
			want: Decision{
				Risk:            domain.RiskMedium,
				Reasons:         []string{"11 changed lines exceeds max_lines 10"},
				RequireReview:   true,
				RequireCI:       true,
				RequireApproval: true,
			},
		},
		{
			name:   "max_files equal to the limit does not bump risk",
			policy: config.Policy{MaxFiles: 2, Gates: testGates()},
			in: Input{
				Kind:      domain.KindFeature,
				Files:     []FileChange{{Path: "a.go"}, {Path: "b.go"}},
				AgentRisk: domain.RiskLow,
			},
			want: lowGate(),
		},
		{
			name:   "max_files exceeded bumps to medium with a reason",
			policy: config.Policy{MaxFiles: 2, Gates: testGates()},
			in: Input{
				Kind:      domain.KindFeature,
				Files:     []FileChange{{Path: "a.go"}, {Path: "b.go"}, {Path: "c.go"}},
				AgentRisk: domain.RiskLow,
			},
			want: Decision{
				Risk:            domain.RiskMedium,
				Reasons:         []string{"3 files changed exceeds max_files 2"},
				RequireReview:   true,
				RequireCI:       true,
				RequireApproval: true,
			},
		},
		{
			name:   "agent can raise risk and its reasons are copied",
			policy: config.Policy{Gates: testGates()},
			in: Input{
				Kind:             domain.KindFeature,
				AgentRisk:        domain.RiskHigh,
				AgentRiskReasons: []string{"touches session tokens", "no tests"},
			},
			want: Decision{
				Risk:            domain.RiskHigh,
				Reasons:         []string{"agent rated risk high: touches session tokens; no tests"},
				RequireReview:   true,
				RequireCI:       true,
				RequireApproval: true,
			},
		},
		{
			name: "agent cannot lower risk set by a rule",
			policy: config.Policy{
				Rules: []config.Rule{migrationRule},
				Gates: testGates(),
			},
			in: Input{
				Kind:      domain.KindFeature,
				Files:     []FileChange{{Path: "db/migrations/001.sql", Added: 1}},
				AgentRisk: domain.RiskLow,
			},
			want: Decision{
				Risk:            domain.RiskHigh,
				Reasons:         []string{"database migration (db/migrations/001.sql)"},
				RequireReview:   true,
				RequireCI:       true,
				RequireApproval: true,
			},
		},
		{
			name:   "empty agent risk counts as medium",
			policy: config.Policy{Gates: testGates()},
			in:     Input{Kind: domain.KindFeature},
			want: Decision{
				Risk:            domain.RiskMedium,
				Reasons:         []string{"agent did not rate its risk"},
				RequireReview:   true,
				RequireCI:       true,
				RequireApproval: true,
			},
		},
		{
			name:   "unrecognised agent risk counts as high",
			policy: config.Policy{Gates: testGates()},
			in:     Input{Kind: domain.KindFeature, AgentRisk: "bogus"},
			want: Decision{
				Risk:            domain.RiskHigh,
				Reasons:         []string{`agent gave an unrecognised risk "bogus"`},
				RequireReview:   true,
				RequireCI:       true,
				RequireApproval: true,
			},
		},
		{
			name: "extra rules match alongside default rules",
			policy: config.Policy{
				Rules:      config.DefaultRules(),
				ExtraRules: []config.Rule{{Paths: []string{"api/**"}, Risk: domain.RiskHigh, Reason: "public API"}},
				Gates:      testGates(),
			},
			in: Input{
				Kind: domain.KindFeature,
				Files: []FileChange{
					{Path: "api/openapi.go", Added: 2},
					{Path: "db/migrations/001.sql", Added: 1},
				},
				AgentRisk: domain.RiskLow,
			},
			want: Decision{
				Risk:            domain.RiskHigh,
				Reasons:         []string{"database schema or migration (db/migrations/001.sql)", "public API (api/openapi.go)"},
				RequireReview:   true,
				RequireCI:       true,
				RequireApproval: true,
			},
		},
		{
			name: "source rule matches the task source and names no file",
			policy: config.Policy{
				Rules: []config.Rule{{Sources: []string{"followup", "retro"}, Risk: domain.RiskMedium, Reason: "self-assigned"}},
				Gates: testGates(),
			},
			in:   Input{Kind: domain.KindBugfix, Source: "retro", AgentRisk: domain.RiskLow},
			want: Decision{Risk: domain.RiskMedium, Reasons: []string{"self-assigned"}, RequireReview: true, RequireCI: true, RequireApproval: true},
		},
		{
			name: "source rule does not match another source",
			policy: config.Policy{
				Rules: []config.Rule{{Sources: []string{"followup", "retro"}, Risk: domain.RiskMedium, Reason: "self-assigned"}},
				Gates: testGates(),
			},
			in:   Input{Kind: domain.KindBugfix, Source: "github_issue", AgentRisk: domain.RiskLow},
			want: lowGate(),
		},
		{
			name: "source criterion is ANDed with kinds",
			policy: config.Policy{
				Rules: []config.Rule{{Sources: []string{"followup"}, Kinds: []string{"chore"}, Risk: domain.RiskHigh, Reason: "x"}},
				Gates: testGates(),
			},
			in:   Input{Kind: domain.KindBugfix, Source: "followup", AgentRisk: domain.RiskLow},
			want: lowGate(),
		},
		{
			name: "a source rule with no task source never matches an empty source",
			policy: config.Policy{
				Rules: []config.Rule{{Sources: []string{"followup"}, Risk: domain.RiskHigh, Reason: "x"}},
				Gates: testGates(),
			},
			in:   Input{Kind: domain.KindFeature, AgentRisk: domain.RiskLow},
			want: lowGate(),
		},
		{
			name:   "missing gate requires approval and review and CI",
			policy: config.Policy{Gates: map[string]config.Gate{}},
			in:     Input{Kind: domain.KindFeature, AgentRisk: domain.RiskLow},
			want: Decision{
				Risk:            domain.RiskLow,
				Reasons:         []string{},
				RequireReview:   true,
				RequireCI:       true,
				AutoMerge:       false,
				RequireApproval: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.policy, tt.in)
			require.Equal(t, tt.want, got)
			assert.NotNil(t, got.Reasons)
		})
	}
}
