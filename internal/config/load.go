package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"unicode/utf8"

	"github.com/bobbyrc/onlet/internal/domain"
	"go.yaml.in/yaml/v3"
)

// envRef matches ${NAME} references. A bare $NAME is left alone, so shell snippets in
// config values such as "vercel rollback $ONLET_ROLLBACK_ID" pass through unchanged.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// EnvRefs returns the names of the environment variables that data references as ${NAME},
// sorted and each once. `onlet service install` records them, because a daemon started by
// launchd does not inherit the installing shell's environment.
func EnvRefs(data []byte) []string {
	seen := map[string]bool{}
	var names []string
	for _, m := range envRef.FindAllSubmatch(data, -1) {
		if name := string(m[1]); !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Load reads, expands, decodes, defaults and validates the config file at path. Path is
// stored as an absolute path in Config.Path.
func Load(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("config: resolve %s: %w", path, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", abs, err)
	}
	cfg, err := Parse(data, abs)
	if err != nil {
		return nil, fmt.Errorf("config: %s: %w", abs, err)
	}
	return cfg, nil
}

// Parse decodes config bytes as Load does. path is recorded in Config.Path and is used to
// resolve relative agent prompt paths; an empty path resolves them against the working
// directory. An empty document is valid and yields the defaults.
func Parse(data []byte, path string) (*Config, error) {
	expanded, values, err := expandEnv(data)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(expanded))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode: %w", err)
	}
	cfg.Path = path
	cfg.expanded = values
	ApplyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// minSecretLen is the shortest substituted value that Redacted masks. Shorter values are too
// likely to occur by chance inside other config text, and masking them would garble the output.
const minSecretLen = 6

// expandEnv replaces ${NAME} with the value of the environment variable NAME. A variable that is
// set to the empty string expands to nothing. A variable that is not set is an error, so a typo
// or a missing secret fails at startup instead of becoming an empty value. The error names each
// unset variable once, in sorted order. On success it also returns each distinct substituted
// value of at least minSecretLen characters, so that Redacted can mask them wherever they appear.
func expandEnv(data []byte) ([]byte, []string, error) {
	var missing []string
	seen := map[string]bool{}
	var values []string
	seenValue := map[string]bool{}
	out := envRef.ReplaceAllFunc(data, func(match []byte) []byte {
		name := string(envRef.FindSubmatch(match)[1])
		value, ok := os.LookupEnv(name)
		if !ok {
			if !seen[name] {
				seen[name] = true
				missing = append(missing, name)
			}
			return nil
		}
		if utf8.RuneCountInString(value) >= minSecretLen && !seenValue[value] {
			seenValue[value] = true
			values = append(values, value)
		}
		return []byte(value)
	})
	if len(missing) == 0 {
		return out, values, nil
	}
	sort.Strings(missing)
	errs := make([]error, len(missing))
	for i, name := range missing {
		errs[i] = fmt.Errorf("undefined environment variable ${%s}", name)
	}
	return nil, nil, errors.Join(errs...)
}

// RollbackEnabled reports whether a failed verify window rolls the release back. Default true.
func (d *Deploy) RollbackEnabled() bool {
	return d.AutoRollback == nil || *d.AutoRollback
}

// FrictionAutoFile reports whether friction reports are filed as GitHub issues. Default true.
func (l Loops) FrictionAutoFile() bool {
	return l.Friction.AutoFile == nil || *l.Friction.AutoFile
}

// RetroOn reports whether the retro loop runs for project. An explicit enabled setting wins.
// Unset, it runs only for selfProject, because a retrospective spends agent time.
func (l Loops) RetroOn(project, selfProject string) bool {
	if l.Retro.Enabled != nil {
		return *l.Retro.Enabled
	}
	return selfProject != "" && project == selfProject
}

// LoopsFor returns the loop settings that apply to project: the global Loops with each
// field the project overrides applied on top. Unset override fields keep the global value.
func (c *Config) LoopsFor(project string) Loops {
	out := c.Loops
	p := c.Projects[project]
	if p == nil || p.Loops == nil {
		return out
	}
	ov := p.Loops

	if ov.IssueIntake.Enabled != nil {
		out.IssueIntake.Enabled = ov.IssueIntake.Enabled
	}
	if ov.IssueIntake.Every.Duration != 0 {
		out.IssueIntake.Every = ov.IssueIntake.Every
	}
	if ov.IssueIntake.Label != "" {
		out.IssueIntake.Label = ov.IssueIntake.Label
	}

	if ov.PRShepherd.Enabled != nil {
		out.PRShepherd.Enabled = ov.PRShepherd.Enabled
	}
	if ov.PRShepherd.Every.Duration != 0 {
		out.PRShepherd.Every = ov.PRShepherd.Every
	}

	if ov.DeployWatch.Enabled != nil {
		out.DeployWatch.Enabled = ov.DeployWatch.Enabled
	}
	if ov.DeployWatch.Every.Duration != 0 {
		out.DeployWatch.Every = ov.DeployWatch.Every
	}

	if ov.ProdWatch.Enabled != nil {
		out.ProdWatch.Enabled = ov.ProdWatch.Enabled
	}
	if ov.ProdWatch.Every.Duration != 0 {
		out.ProdWatch.Every = ov.ProdWatch.Every
	}
	if ov.ProdWatch.MinLevel != "" {
		out.ProdWatch.MinLevel = ov.ProdWatch.MinLevel
	}
	if ov.ProdWatch.MinCount != 0 {
		out.ProdWatch.MinCount = ov.ProdWatch.MinCount
	}

	if ov.Friction.Enabled != nil {
		out.Friction.Enabled = ov.Friction.Enabled
	}
	if ov.Friction.Every.Duration != 0 {
		out.Friction.Every = ov.Friction.Every
	}
	if ov.Friction.AutoFile != nil {
		out.Friction.AutoFile = ov.Friction.AutoFile
	}
	if ov.Friction.AutoQueue {
		out.Friction.AutoQueue = true
	}
	if ov.Friction.Label != "" {
		out.Friction.Label = ov.Friction.Label
	}
	if ov.Friction.MinCount != 0 {
		out.Friction.MinCount = ov.Friction.MinCount
	}

	if ov.Retro.Enabled != nil {
		out.Retro.Enabled = ov.Retro.Enabled
	}
	if ov.Retro.Every.Duration != 0 {
		out.Retro.Every = ov.Retro.Every
	}
	if ov.Retro.Lookback.Duration != 0 {
		out.Retro.Lookback = ov.Retro.Lookback
	}

	if ov.ExecIntake.Enabled != nil {
		out.ExecIntake.Enabled = ov.ExecIntake.Enabled
	}
	if ov.ExecIntake.Every.Duration != 0 {
		out.ExecIntake.Every = ov.ExecIntake.Every
	}
	return out
}

// AgentsFor returns the agent roles that apply to project: the global roles with the project's
// overrides applied. A set field of an override (Harness, Model, Prompt, Args, Description)
// replaces the global role's field; a name the global map lacks adds a project-only role. The
// map is fresh, so the caller may change it. An empty or unconfigured project gets the global
// roles.
func (c *Config) AgentsFor(project string) map[string]Agent {
	out := make(map[string]Agent, len(c.Agents))
	for name, a := range c.Agents {
		out[name] = a
	}
	p := c.Projects[project]
	if p == nil {
		return out
	}
	for name, ov := range p.Agents {
		base, ok := out[name]
		if !ok {
			ov.Origin = OriginCustom
			out[name] = ov
			continue
		}
		if ov.Harness != "" {
			base.Harness = ov.Harness
		}
		if ov.Model != "" {
			base.Model = ov.Model
		}
		// Prompt and PromptText replace each other, so the agent never has both.
		if ov.Prompt != "" {
			base.Prompt = ov.Prompt
			base.PromptText = ""
		}
		if ov.PromptText != "" {
			base.PromptText = ov.PromptText
			base.Prompt = ""
		}
		if len(ov.Args) > 0 {
			base.Args = ov.Args
		}
		if ov.Description != "" {
			base.Description = ov.Description
		}
		if ov.Disabled {
			base.Disabled = true
		}
		out[name] = base
	}
	return out
}

// AgentFor picks the agent for a task from the global roles. See AgentForProject.
func (c *Config) AgentFor(kind domain.TaskKind, role string) (string, Agent, error) {
	return c.AgentForProject("", kind, role)
}

// AgentForProject picks the agent for a task of project. A non-empty role names the agent
// directly and must exist in AgentsFor(project). Otherwise the agent is the one routed to kind:
// the project's Routing entry if it has one, else the global Routing entry. It returns the
// agent's name too.
func (c *Config) AgentForProject(project string, kind domain.TaskKind, role string) (string, Agent, error) {
	agents := c.AgentsFor(project)
	if role != "" {
		a, ok := agents[role]
		if !ok {
			return "", Agent{}, fmt.Errorf("config: unknown agent role %q", role)
		}
		if a.Disabled {
			return "", Agent{}, fmt.Errorf("config: agent %q is disabled", role)
		}
		return role, a, nil
	}
	name, ok := c.routeFor(project, kind)
	if !ok {
		return "", Agent{}, fmt.Errorf("config: no agent routed for task kind %q", kind)
	}
	a, ok := agents[name]
	if !ok {
		return "", Agent{}, fmt.Errorf("config: routing %q names unknown agent %q", kind, name)
	}
	if a.Disabled {
		return "", Agent{}, fmt.Errorf("config: agent %q is disabled", name)
	}
	return name, a, nil
}

// routeFor returns the agent name routed to kind for project, preferring the project's routing.
func (c *Config) routeFor(project string, kind domain.TaskKind) (string, bool) {
	if p := c.Projects[project]; p != nil {
		if name, ok := p.Routing[string(kind)]; ok {
			return name, true
		}
	}
	name, ok := c.Routing[string(kind)]
	return name, ok
}

// PolicyFor returns the policy for project: its named policy, falling back to "default".
func (c *Config) PolicyFor(project string) Policy {
	name := "default"
	if p := c.Projects[project]; p != nil && p.Policy != "" {
		name = p.Policy
	}
	if pol, ok := c.Policies[name]; ok {
		return pol
	}
	return c.Policies["default"]
}

// ProjectNames returns the configured project names in sorted order.
func (c *Config) ProjectNames() []string {
	names := make([]string, 0, len(c.Projects))
	for name := range c.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
