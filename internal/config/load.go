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

	"github.com/bobbyrc/foundry/internal/domain"
	"go.yaml.in/yaml/v3"
)

// envRef matches ${NAME} references. A bare $NAME is left alone, so shell snippets in
// config values such as "vercel rollback $FOUNDRY_ROLLBACK_ID" pass through unchanged.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

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
	expanded := expandEnv(data)
	dec := yaml.NewDecoder(bytes.NewReader(expanded))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode: %w", err)
	}
	cfg.Path = path
	ApplyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// expandEnv replaces ${NAME} with the value of the environment variable NAME. An unset
// variable expands to the empty string, as os.Getenv does.
func expandEnv(data []byte) []byte {
	return envRef.ReplaceAllFunc(data, func(match []byte) []byte {
		name := envRef.FindSubmatch(match)[1]
		return []byte(os.Getenv(string(name)))
	})
}

// RollbackEnabled reports whether a failed verify window rolls the release back. Default true.
func (d *Deploy) RollbackEnabled() bool {
	return d.AutoRollback == nil || *d.AutoRollback
}

// FrictionAutoFile reports whether friction reports are filed as GitHub issues. Default true.
func (l Loops) FrictionAutoFile() bool {
	return l.Friction.AutoFile == nil || *l.Friction.AutoFile
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
	return out
}

// AgentFor picks the agent for a task. A non-empty role names the agent directly and must
// exist. Otherwise the agent is the one routed to kind. It returns the agent's name too.
func (c *Config) AgentFor(kind domain.TaskKind, role string) (string, Agent, error) {
	if role != "" {
		a, ok := c.Agents[role]
		if !ok {
			return "", Agent{}, fmt.Errorf("config: unknown agent role %q", role)
		}
		return role, a, nil
	}
	name, ok := c.Routing[string(kind)]
	if !ok {
		return "", Agent{}, fmt.Errorf("config: no agent routed for task kind %q", kind)
	}
	a, ok := c.Agents[name]
	if !ok {
		return "", Agent{}, fmt.Errorf("config: routing %q names unknown agent %q", kind, name)
	}
	return name, a, nil
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
