package config

import (
	"encoding/json"
	"strings"
)

// redacted replaces the value of every secret-looking setting.
const redacted = "***"

// secretWords mark an Options key whose value is replaced when the config is shown.
var secretWords = []string{"token", "secret", "password", "key"}

// Redacted returns a deep copy of c that is safe to show: every environment variable value
// and every option whose name suggests a secret is replaced with "***". c is not modified.
func (c *Config) Redacted() *Config {
	data, err := json.Marshal(c)
	if err != nil {
		// Config only holds JSON-safe values, so this cannot happen for a loaded config.
		panic("config: redact: " + err.Error())
	}
	var out Config
	if err := json.Unmarshal(data, &out); err != nil {
		panic("config: redact: " + err.Error())
	}
	out.Path = c.Path
	for name, p := range out.Projects {
		if p == nil {
			continue
		}
		p.Name = name
		redactEnv(p.Env)
		if p.Deploy != nil {
			redactOptions(p.Deploy.Options)
		}
		for i := range p.Observe {
			redactOptions(p.Observe[i].Options)
		}
	}
	for name, h := range out.Harness {
		redactEnv(h.Env)
		out.Harness[name] = h
	}
	for i := range out.Notify {
		redactOptions(out.Notify[i].Options)
	}
	return &out
}

// redactEnv replaces every value in env in place.
func redactEnv(env map[string]string) {
	for k := range env {
		env[k] = redacted
	}
}

// redactOptions replaces the value of each option whose key contains a secret word, in place.
func redactOptions(opts map[string]any) {
	for k := range opts {
		lower := strings.ToLower(k)
		for _, w := range secretWords {
			if strings.Contains(lower, w) {
				opts[k] = redacted
				break
			}
		}
	}
}
