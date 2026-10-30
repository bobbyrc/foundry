package domain

// OutboxDir is the directory, relative to a run's worktree, where the agent writes its report.
// It is excluded from git through .git/info/exclude.
const OutboxDir = ".onett-run"

// ResultFile is the agent's report, written to OutboxDir/ResultFile before it finishes.
const ResultFile = "result.json"

type ResultStatus string

const (
	// ResultCompleted: the work is done; any code changes are committed on the branch.
	ResultCompleted ResultStatus = "completed"
	// ResultNoChanges: the task needed no code change (question answered, triage done, the bug
	// is not reproducible). Summary says why.
	ResultNoChanges ResultStatus = "no_changes"
	// ResultBlocked: the agent cannot continue without a human. BlockedReason says what it needs.
	ResultBlocked ResultStatus = "blocked"
	// ResultFailed: the agent tried and could not do it.
	ResultFailed ResultStatus = "failed"
)

// RunResult is the schema of result.json. Missing or unparsable result.json is treated as
// ResultFailed with the harness's final message as the summary.
type RunResult struct {
	Status        ResultStatus `json:"status"`
	Summary       string       `json:"summary"`
	BlockedReason string       `json:"blocked_reason,omitempty"`
	// Risk is the agent's own rating of its change. Policy may raise it, never lower it.
	Risk        Risk     `json:"risk,omitempty"`
	RiskReasons []string `json:"risk_reasons,omitempty"`
	PRTitle     string   `json:"pr_title,omitempty"`
	PRBody      string   `json:"pr_body,omitempty"`
	// Review is set only by reviewer runs.
	Review    *ReviewVerdict   `json:"review,omitempty"`
	Followups []FollowupTask   `json:"followups,omitempty"`
	Friction  []FrictionReport `json:"friction,omitempty"`
	Finished  bool             `json:"finished,omitempty"`
}

type ReviewVerdict struct {
	Approve  bool            `json:"approve"`
	Findings []ReviewFinding `json:"findings,omitempty"`
}

type ReviewFinding struct {
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Severity string `json:"severity"` // "blocker", "major", "minor"
	Message  string `json:"message"`
}

// FollowupTask is new work the agent found but did not do. Onett queues it as a new task.
type FollowupTask struct {
	Title string   `json:"title"`
	Body  string   `json:"body"`
	Kind  TaskKind `json:"kind,omitempty"`
}

// FrictionReport is something that slowed the agent down.
type FrictionReport struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Target is "project" (the repo being worked on) or "onett" (the factory itself).
	Target string `json:"target"`
}
