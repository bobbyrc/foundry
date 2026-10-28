// Package config defines onlet.yaml. load.go reads, defaults and validates it.
//
// The struct tags are the file format; the JSON tags are how GET /api/v1/config shows it.
package config

import (
	"time"

	"github.com/bobbyrc/onlet/internal/domain"
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
	// SelfProject names the project that is onlet itself; factory friction is filed there.
	SelfProject string `yaml:"self_project" json:"self_project"`

	// Path is the file this config was loaded from (not part of the file).
	Path string `yaml:"-" json:"path"`

	expanded []string `yaml:"-" json:"-"` // values substituted for ${NAME} references at load time; redacted wherever they appear
}

type Server struct {
	Addr      string `yaml:"addr" json:"addr"`             // default 127.0.0.1:7777
	DataDir   string `yaml:"data_dir" json:"data_dir"`     // default ~/.local/share/onlet
	PublicURL string `yaml:"public_url" json:"public_url"` // for links in notifications; default http://<addr>
	// AllowedHosts are extra Host header values the server accepts (DNS-rebinding guard).
	// localhost, 127.0.0.1, ::1 and the host parts of addr and public_url are always allowed.
	// A wildcard bind (0.0.0.0, ::) adds no name: to reach onlet remotely (e.g. over
	// Tailscale), list the name you use here, or requests get 421 Misdirected Request.
	AllowedHosts []string `yaml:"allowed_hosts" json:"allowed_hosts,omitempty"`
	// RetainEvents and RetainRunLogs bound how long the event history and agent run logs are
	// kept; older rows are deleted once a day, so a daemon that runs for months does not grow
	// without limit. Tasks, runs, deployments and approvals are kept. Defaults 720h (30 days)
	// and 2160h (90 days); a very large value (e.g. 876000h) keeps them in practice forever.
	RetainEvents  Duration `yaml:"retain_events" json:"retain_events"`
	RetainRunLogs Duration `yaml:"retain_run_logs" json:"retain_run_logs"`
}

type Limits struct {
	MaxConcurrentRuns int      `yaml:"max_concurrent_runs" json:"max_concurrent_runs"`   // default 3
	MaxRunsPerProject int      `yaml:"max_runs_per_project" json:"max_runs_per_project"` // default 1
	StallTimeout      Duration `yaml:"stall_timeout" json:"stall_timeout"`               // default 15m
	RunTimeout        Duration `yaml:"run_timeout" json:"run_timeout"`                   // default 2h
	MaxAttempts       int      `yaml:"max_attempts" json:"max_attempts"`                 // default 3
	// DailyBudgetUSD stops new task runs once today's reported agent cost reaches it (0 = no
	// limit). Chat with the orchestrator is exempt. Codex reports no cost, so it is counted only
	// when its harness has a price.
	DailyBudgetUSD float64 `yaml:"daily_budget_usd" json:"daily_budget_usd"`
}

// Harness configures one agent CLI. Type is a registered harness type: claude-code, codex,
// exec, fake.
type Harness struct {
	Type    string            `yaml:"type" json:"type"`
	Command []string          `yaml:"command" json:"command,omitempty"`
	Args    []string          `yaml:"args" json:"args,omitempty"`
	Env     map[string]string `yaml:"env" json:"env,omitempty"`
	// InheritEnv names extra variables passed through from onlet's own environment. Agents
	// otherwise get only a small allowlist (PATH, HOME, USER, LANG, TERM, TMPDIR, ...), so
	// secrets loaded into onlet (GH_TOKEN, provider tokens) do not reach them.
	InheritEnv []string `yaml:"inherit_env" json:"inherit_env,omitempty"`
	// Price lets onlet estimate the cost of runs whose harness reports tokens but no dollar
	// cost (codex), so limits.daily_budget_usd counts them.
	Price *Price `yaml:"price" json:"price,omitempty"`
}

// Price is USD per million tokens. Every field is optional; 0 means unknown. Cached input
// tokens are billed at CachedInputPerMTok, falling back to InputPerMTok when that is 0.
type Price struct {
	InputPerMTok       float64 `yaml:"input_per_mtok" json:"input_per_mtok"`
	OutputPerMTok      float64 `yaml:"output_per_mtok" json:"output_per_mtok"`
	CachedInputPerMTok float64 `yaml:"cached_input_per_mtok" json:"cached_input_per_mtok"`
}

// Estimate returns the cost of a run from its token counts. input includes cached.
func (p Price) Estimate(input, cached, output int64) float64 {
	cached = min(max(cached, 0), input)
	cachedPrice := p.CachedInputPerMTok
	if cachedPrice == 0 {
		cachedPrice = p.InputPerMTok
	}
	return (float64(input-cached)*p.InputPerMTok + float64(cached)*cachedPrice + float64(output)*p.OutputPerMTok) / 1e6
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
	Paths  []string `yaml:"paths" json:"paths,omitempty"`
	Labels []string `yaml:"labels" json:"labels,omitempty"` // any label matches
	Kinds  []string `yaml:"kinds" json:"kinds,omitempty"`   // any task kind matches
	// Sources matches the task source ("followup", "retro", "github_issue", "signal", ...).
	Sources []string    `yaml:"sources" json:"sources,omitempty"`
	Risk    domain.Risk `yaml:"risk" json:"risk"`
	Reason  string      `yaml:"reason" json:"reason"`
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
	MaxFiles int `yaml:"max_files" json:"max_files"`
	// Rules replaces the built-in default rules when set (an explicit empty list means none).
	Rules []Rule `yaml:"rules" json:"rules"`
	// ExtraRules are added to Rules (or to the defaults when Rules is unset). Prefer this:
	// it keeps the built-in protections for migrations, CI, auth and onlet's own gates.
	ExtraRules []Rule `yaml:"extra_rules" json:"extra_rules,omitempty"`
	// CIGrace: after onlet pushes, a PR with no checks yet counts as CI pending (not "no CI")
	// for this long, so a fast review cannot merge before CI registers. Default 3m.
	CIGrace Duration `yaml:"ci_grace" json:"ci_grace"`
	// RequireChecks: for repositories that always run CI, a PR with no checks never passes the
	// CI gate (it waits, then blocks after 30m) instead of counting as "no CI configured".
	RequireChecks bool `yaml:"require_checks" json:"require_checks"`
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
	// Checks run in the worktree after the agent finishes and before onlet commits and
	// pushes (formatters, linters, fast tests). A failing check fails the attempt: the task is
	// re-queued with the check's output, like a CI failure, without spending a CI run.
	Checks []Check `yaml:"checks" json:"checks,omitempty"`
	// Setup runs in a fresh worktree before the agent starts (install dependencies, generate
	// code), so the agent can build and test. A failing setup command fails the attempt before
	// the agent runs: the task is re-queued with the command's output. Setup and checks run
	// with the agent's environment, not onlet's, because they execute code the agent may
	// have written.
	Setup []Check `yaml:"setup" json:"setup,omitempty"`
	// Intake sources turn external work queues into tasks: each command prints tasks as JSON
	// lines (see docs/plugins.md, "Intake sources"). Run by the exec_intake loop.
	Intake  []IntakeSource `yaml:"intake" json:"intake,omitempty"`
	Deploy  *Deploy        `yaml:"deploy" json:"deploy,omitempty"`
	Observe []Observer     `yaml:"observe" json:"observe,omitempty"`
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
	// Logs is a shell command that prints the service's recent logs (e.g. "tail -n 200
	// /var/log/app.log", "vercel logs <url>"). Its output tail is attached to incidents.
	Logs string `yaml:"logs" json:"logs,omitempty"`
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
		Label string `yaml:"label" json:"label"` // default "onlet"
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
	// Retro periodically gives an improver agent the evidence of what went wrong recently
	// (blocked/failed tasks and their reasons, open friction, incidents, review findings) to fix
	// the top problem, consolidate duplicate friction issues and file follow-ups. Default every
	// 24h; enabled by default only for self_project (it spends agent time).
	Retro struct {
		Loop     `yaml:",inline"`
		Lookback Duration `yaml:"lookback" json:"lookback"` // evidence window; default = Every
	} `yaml:"retro" json:"retro"`
	// ExecIntake runs each project's intake sources. Default every 1m.
	ExecIntake Loop `yaml:"exec_intake" json:"exec_intake"`
	// Friction files GitHub issues for reported friction.
	Friction struct {
		Loop      `yaml:",inline"`
		AutoFile  *bool  `yaml:"auto_file" json:"auto_file,omitempty"` // default true
		AutoQueue bool   `yaml:"auto_queue" json:"auto_queue"`         // also label the issue for intake
		Label     string `yaml:"label" json:"label"`                   // default "friction"
		MinCount  int    `yaml:"min_count" json:"min_count"`           // reports before filing; default 1
	} `yaml:"friction" json:"friction"`
}

// Check is one command onlet runs in an agent's worktree: a check before pushing its work, or a
// setup command before the agent starts.
type Check struct {
	Name string `yaml:"name" json:"name"`
	// Run is a shell command (sh -c) run in the worktree with the agent's environment (the
	// allowlist, the harness's inherit_env, the project's env).
	Run string `yaml:"run" json:"run"`
	// Timeout defaults to 10m.
	Timeout Duration `yaml:"timeout" json:"timeout"`
}

// IntakeSource is an external queue of work, read by running a command.
type IntakeSource struct {
	Name string `yaml:"name" json:"name"`
	// Command is a shell command (sh -c) that prints zero or more tasks, one JSON object per
	// line: {"id","title","body","kind","priority","role"}. id is required and dedupes.
	Command string `yaml:"command" json:"command"`
	// Kind is the default task kind when a line has none (default "feature").
	Kind string `yaml:"kind" json:"kind,omitempty"`
	// Timeout for one run of Command (default 60s).
	Timeout Duration `yaml:"timeout" json:"timeout"`
}
