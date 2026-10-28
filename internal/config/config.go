// Package config defines foundry.yaml. load.go reads, defaults and validates it.
//
// The struct tags are the file format; the JSON tags are how GET /api/v1/config shows it.
package config

import (
	"time"

	"github.com/bobbyrc/foundry/internal/domain"
)

// Duration is a time.Duration written as "15m" in YAML and JSON.
type Duration struct{ time.Duration }

type Config struct {
	Server   Server              `yaml:"server" json:"server"`
	Limits   Limits              `yaml:"limits" json:"limits"`
	Harness  map[string]Harness  `yaml:"harnesses" json:"harnesses"`
	Agents   map[string]Agent    `yaml:"agents" json:"agents"`
	Routing  map[string]string   `yaml:"routing" json:"routing"` // task kind → agent name
	Policies map[string]Policy   `yaml:"policies" json:"policies"`
	Projects map[string]*Project `yaml:"projects" json:"projects"`
	Loops    Loops               `yaml:"loops" json:"loops"`
	Notify   []Notifier          `yaml:"notify" json:"notify"`
	// SelfProject names the project that is foundry itself; factory friction is filed there.
	SelfProject string `yaml:"self_project" json:"self_project"`

	// Path is the file this config was loaded from (not part of the file).
	Path string `yaml:"-" json:"path"`
}

type Server struct {
	Addr      string `yaml:"addr" json:"addr"`             // default 127.0.0.1:7777
	DataDir   string `yaml:"data_dir" json:"data_dir"`     // default ~/.local/share/foundry
	PublicURL string `yaml:"public_url" json:"public_url"` // for links in notifications; default http://<addr>
}

type Limits struct {
	MaxConcurrentRuns int      `yaml:"max_concurrent_runs" json:"max_concurrent_runs"`   // default 3
	MaxRunsPerProject int      `yaml:"max_runs_per_project" json:"max_runs_per_project"` // default 1
	StallTimeout      Duration `yaml:"stall_timeout" json:"stall_timeout"`               // default 15m
	RunTimeout        Duration `yaml:"run_timeout" json:"run_timeout"`                   // default 2h
	MaxAttempts       int      `yaml:"max_attempts" json:"max_attempts"`                 // default 3
	// DailyBudgetUSD stops new task runs once today's reported agent cost reaches it (0 = no
	// limit). Chat with the orchestrator is exempt. Codex reports no cost, so it is not counted.
	DailyBudgetUSD float64 `yaml:"daily_budget_usd" json:"daily_budget_usd"`
}

// Harness configures one agent CLI. Type is a registered harness type: claude-code, codex,
// exec, fake.
type Harness struct {
	Type    string            `yaml:"type" json:"type"`
	Command []string          `yaml:"command" json:"command,omitempty"`
	Args    []string          `yaml:"args" json:"args,omitempty"`
	Env     map[string]string `yaml:"env" json:"env,omitempty"`
}

// Agent is a role: which harness and model run it and with what instructions.
type Agent struct {
	Harness string `yaml:"harness" json:"harness"`
	Model   string `yaml:"model" json:"model,omitempty"`
	// Prompt is "builtin:<name>" or a path to a Markdown file (relative to the config file).
	Prompt string   `yaml:"prompt" json:"prompt"`
	Args   []string `yaml:"args" json:"args,omitempty"`
	// Description is shown in the UI and given to the orchestrator so it can pick roles.
	Description string `yaml:"description" json:"description,omitempty"`
}

type Rule struct {
	// Paths are doublestar globs matched against changed file paths.
	Paths  []string    `yaml:"paths" json:"paths,omitempty"`
	Labels []string    `yaml:"labels" json:"labels,omitempty"` // any label matches
	Kinds  []string    `yaml:"kinds" json:"kinds,omitempty"`   // any task kind matches
	Risk   domain.Risk `yaml:"risk" json:"risk"`
	Reason string      `yaml:"reason" json:"reason"`
}

type Gate struct {
	AutoMerge     bool `yaml:"auto_merge" json:"auto_merge"`
	RequireReview bool `yaml:"require_review" json:"require_review"`
	RequireCI     bool `yaml:"require_ci" json:"require_ci"`
}

type Policy struct {
	// MaxLines: a diff with more added+deleted lines than this is at least medium risk.
	MaxLines int `yaml:"max_lines" json:"max_lines"`
	// MaxFiles: a diff touching more files than this is at least medium risk.
	MaxFiles int    `yaml:"max_files" json:"max_files"`
	Rules    []Rule `yaml:"rules" json:"rules"`
	// Gates by risk level: "low", "medium", "high".
	Gates map[string]Gate `yaml:"gates" json:"gates"`
}

type Project struct {
	Name          string `yaml:"-" json:"name"`
	Repo          string `yaml:"repo" json:"repo"`                     // owner/name on GitHub
	Path          string `yaml:"path" json:"path"`                     // local clone; default <data_dir>/repos/<name>
	DefaultBranch string `yaml:"default_branch" json:"default_branch"` // default main
	Policy        string `yaml:"policy" json:"policy"`                 // default "default"
	// Instructions are appended to every agent prompt for this project.
	Instructions string            `yaml:"instructions" json:"instructions,omitempty"`
	Env          map[string]string `yaml:"env" json:"env,omitempty"` // added to agent sessions
	// Agents overrides agent roles for this project only: set fields replace the global
	// role's fields (Harness, Model, Prompt, Args, Description); a new name adds a role that
	// only this project has.
	Agents map[string]Agent `yaml:"agents" json:"agents,omitempty"`
	// Routing overrides the global task kind → agent routing for this project.
	Routing map[string]string `yaml:"routing" json:"routing,omitempty"`
	Deploy  *Deploy           `yaml:"deploy" json:"deploy,omitempty"`
	Observe []Observer        `yaml:"observe" json:"observe,omitempty"`
	// Loops overrides the global loop settings for this project.
	Loops *Loops `yaml:"loops" json:"loops,omitempty"`
	// Paused stops the scheduler from starting runs and the loops from creating tasks.
	Paused bool `yaml:"paused" json:"paused"`
}

type Deploy struct {
	Type    string         `yaml:"type" json:"type"`
	Options map[string]any `yaml:"options" json:"options,omitempty"`
	// HealthURL must answer 2xx during the verify window. Empty skips the HTTP check.
	HealthURL string `yaml:"health_url" json:"health_url,omitempty"`
	// VerifyFor is how long a release is watched before it is called healthy. Default 10m.
	VerifyFor Duration `yaml:"verify_for" json:"verify_for"`
	// Timeout fails a release still building after this long. Default 30m.
	Timeout Duration `yaml:"timeout" json:"timeout"`
	// AutoRollback rolls back on a failed verify window. Default true.
	AutoRollback *bool `yaml:"auto_rollback" json:"auto_rollback,omitempty"`
}

type Observer struct {
	Name    string         `yaml:"name" json:"name"`
	Type    string         `yaml:"type" json:"type"`
	Options map[string]any `yaml:"options" json:"options,omitempty"`
}

type Notifier struct {
	Type        string         `yaml:"type" json:"type"`
	Options     map[string]any `yaml:"options" json:"options,omitempty"`
	MinPriority string         `yaml:"min_priority" json:"min_priority,omitempty"`
}

type Loop struct {
	Enabled *bool    `yaml:"enabled" json:"enabled,omitempty"`
	Every   Duration `yaml:"every" json:"every"`
}

// On reports whether the loop is enabled (default true).
func (l Loop) On() bool { return l.Enabled == nil || *l.Enabled }

type Loops struct {
	// IssueIntake turns open issues carrying Label into tasks.
	IssueIntake struct {
		Loop  `yaml:",inline"`
		Label string `yaml:"label" json:"label"` // default "foundry"
	} `yaml:"issue_intake" json:"issue_intake"`
	// PRShepherd moves tasks in review through CI, review, policy and merge.
	PRShepherd Loop `yaml:"pr_shepherd" json:"pr_shepherd"`
	// DeployWatch releases merged work and verifies it.
	DeployWatch Loop `yaml:"deploy_watch" json:"deploy_watch"`
	// ProdWatch polls observers and opens incident tasks.
	ProdWatch struct {
		Loop     `yaml:",inline"`
		MinLevel string `yaml:"min_level" json:"min_level"` // default "error"
		MinCount int64  `yaml:"min_count" json:"min_count"` // default 1
	} `yaml:"prod_watch" json:"prod_watch"`
	// Friction files GitHub issues for reported friction.
	Friction struct {
		Loop      `yaml:",inline"`
		AutoFile  *bool  `yaml:"auto_file" json:"auto_file,omitempty"` // default true
		AutoQueue bool   `yaml:"auto_queue" json:"auto_queue"`         // also label the issue for intake
		Label     string `yaml:"label" json:"label"`                   // default "friction"
		MinCount  int    `yaml:"min_count" json:"min_count"`           // reports before filing; default 1
	} `yaml:"friction" json:"friction"`
}
