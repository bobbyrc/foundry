package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestApplyChangesWritesMultilineStringAsLiteralBlock(t *testing.T) {
	out, err := ApplyChanges([]byte("server:\n  addr: x\n"), []Change{{
		Op:    "set",
		Path:  []string{"agents", "docs"},
		Value: map[string]any{"harness": "claude", "prompt_text": "line one\nline two\n"},
	}})
	require.NoError(t, err)
	text := string(out)
	assert.Contains(t, text, "prompt_text: |")
	assert.Contains(t, text, "\n      line one\n")
	assert.Contains(t, text, "\n      line two\n")
	assert.False(t, strings.Contains(text, `\n`), "newlines must not be written as escapes:\n%s", text)

	var back struct {
		Agents map[string]struct {
			PromptText string `yaml:"prompt_text"`
		} `yaml:"agents"`
	}
	require.NoError(t, yaml.Unmarshal(out, &back))
	assert.Equal(t, "line one\nline two\n", back.Agents["docs"].PromptText)
}

func TestApplyChangesWritesNewMappingsInBlockStyle(t *testing.T) {
	out, err := ApplyChanges([]byte("server:\n  addr: x\n"), []Change{{
		Op:    "set",
		Path:  []string{"agents", "docs"},
		Value: map[string]any{"harness": "claude", "prompt_text": "line one\nline two\n"},
	}})
	require.NoError(t, err)
	text := string(out)
	assert.Contains(t, text, "\nagents:\n  docs:\n", "agents must be a block mapping:\n%s", text)
	assert.NotContains(t, text, "{", "no flow mappings expected:\n%s", text)
}

func TestApplyChangesTurnsEmptyFlowMappingIntoBlock(t *testing.T) {
	out, err := ApplyChanges([]byte("agents: {}\n"), []Change{{
		Op:    "set",
		Path:  []string{"agents", "docs"},
		Value: map[string]any{"harness": "claude"},
	}})
	require.NoError(t, err)
	text := string(out)
	assert.NotContains(t, text, "{", "agents: {} must become block style:\n%s", text)

	tree, err := DocumentTree(out)
	require.NoError(t, err)
	agents := tree.(map[string]any)["agents"].(map[string]any)
	assert.Equal(t, "claude", agents["docs"].(map[string]any)["harness"])
}

func TestApplyChangesKeepsSingleLineStringsPlain(t *testing.T) {
	out, err := ApplyChanges([]byte("server:\n  addr: x\n"), []Change{{
		Op:    "set",
		Path:  []string{"server", "addr"},
		Value: "127.0.0.1:1",
	}})
	require.NoError(t, err)
	text := string(out)
	assert.Contains(t, text, "addr: 127.0.0.1:1\n")
	assert.NotContains(t, text, "|")
	assert.NotContains(t, text, `"`)
}

func TestApplyChangesKeepsCommentWhenOpeningFlowMapping(t *testing.T) {
	out, err := ApplyChanges([]byte("# top\nagents: {} # empty agents\nserver:\n  addr: x # listen\n"), []Change{{
		Op:    "set",
		Path:  []string{"agents", "docs", "harness"},
		Value: "claude",
	}})
	require.NoError(t, err)
	var agentsLine, serverLine string
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "agents:"):
			agentsLine = line
		case strings.HasPrefix(line, "server:"):
			serverLine = line
		}
	}
	assert.Contains(t, agentsLine, "# empty agents", "output:\n%s", out)
	assert.NotContains(t, serverLine, "# empty agents", "output:\n%s", out)
	assert.Contains(t, string(out), "addr: x # listen")
}

func TestApplyChangesNestedFlowStaysValid(t *testing.T) {
	out, err := ApplyChanges([]byte("x: {a: {b: 1} # inner\n, c: 2}\n"), []Change{{
		Op:    "set",
		Path:  []string{"x", "a", "d"},
		Value: "claude",
	}})
	require.NoError(t, err)

	var back map[string]any
	require.NoError(t, yaml.Unmarshal(out, &back), "output must re-parse:\n%s", out)
	x := back["x"].(map[string]any)
	assert.Equal(t, "claude", x["a"].(map[string]any)["d"])
	assert.Equal(t, 1, x["a"].(map[string]any)["b"])
	assert.Equal(t, 2, x["c"])
}
