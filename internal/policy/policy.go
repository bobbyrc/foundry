// Package policy decides, deterministically, how risky a change is and what must happen before
// it merges. Agents can raise risk; nothing an agent says can lower it.
package policy

import (
	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
)

type FileChange struct {
	Path    string
	Added   int
	Deleted int
}

type Input struct {
	Kind      domain.TaskKind
	Files     []FileChange
	AgentRisk domain.Risk
	// AgentRiskReasons are copied into the decision's reasons when AgentRisk wins.
	AgentRiskReasons []string
	Labels           []string // PR/issue labels
	Source           string   // the task's source ("followup", "retro", ...)
}

type Decision struct {
	Risk            domain.Risk `json:"risk"`
	Reasons         []string    `json:"reasons"`
	RequireReview   bool        `json:"require_review"`   // a reviewer run must approve
	RequireCI       bool        `json:"require_ci"`       // CI must be green (or none configured)
	AutoMerge       bool        `json:"auto_merge"`       // merge without a human once gates pass
	RequireApproval bool        `json:"require_approval"` // a human must approve the merge
}

// Evaluate (evaluate.go) has the frozen signature:
//
//	func Evaluate(p config.Policy, in Input) Decision
var _ config.Policy
