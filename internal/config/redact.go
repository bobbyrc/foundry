package config

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// redacted replaces the value of every secret-looking setting.
const redacted = "***"

// secretWords mark an Options key whose value is replaced when the config is shown.
var secretWords = []string{"token", "secret", "password", "key"}

// Redacted returns a deep copy of c that is safe to show: every environment variable value,
// every option whose name suggests a secret, and every value substituted for a ${NAME}
// reference anywhere in the config is replaced with "***". c is not modified.
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
	if len(c.expanded) > 0 {
		out = maskExpanded(out, c.expanded)
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

// maskExpanded replaces every occurrence of each value in every string of cfg with "***". The
// config is walked as generic JSON, so values inside commands, args, URLs and instructions are
// found too. Path and project names are kept as they are: they come from the file, not from
// substituted text. The result is a fresh Config.
func maskExpanded(cfg Config, values []string) Config {
	// Longest first, so a value that contains another one is masked whole.
	sorted := append([]string(nil), values...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	pairs := make([]string, 0, 2*len(sorted))
	for _, v := range sorted {
		pairs = append(pairs, v, redacted)
	}
	replacer := strings.NewReplacer(pairs...)

	data, err := json.Marshal(cfg)
	if err != nil {
		panic("config: redact: " + err.Error())
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // keep numbers exact rather than as float64
	var doc any
	if err := dec.Decode(&doc); err != nil {
		panic("config: redact: " + err.Error())
	}
	doc = replaceStrings(doc, replacer)
	masked, err := json.Marshal(doc)
	if err != nil {
		panic("config: redact: " + err.Error())
	}
	var out Config
	if err := json.Unmarshal(masked, &out); err != nil {
		// A substituted value that sat inside a typed non-string field (such as a duration)
		// can make the masked text unreadable as that type.
		panic("config: redact: " + err.Error())
	}
	out.Path = cfg.Path
	for name, p := range out.Projects {
		if p != nil {
			p.Name = name
		}
	}
	return out
}

// replaceStrings applies r to every string in the generic JSON value v, in place, and returns v.
func replaceStrings(v any, r *strings.Replacer) any {
	switch t := v.(type) {
	case string:
		return r.Replace(t)
	case []any:
		for i := range t {
			t[i] = replaceStrings(t[i], r)
		}
		return t
	case map[string]any:
		for k := range t {
			t[k] = replaceStrings(t[k], r)
		}
		return t
	default:
		return v
	}
}
