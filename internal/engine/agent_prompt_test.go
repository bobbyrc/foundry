package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onlet/internal/config"
	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/store"
)

func TestAgentPromptPrefersInlineText(t *testing.T) {
	e := &Engine{}
	got, err := e.AgentPrompt(config.Agent{PromptText: "Do X."})
	require.NoError(t, err)
	assert.Equal(t, "Do X.", got)
}

func TestAgentPromptReadsBuiltin(t *testing.T) {
	e := &Engine{}
	got, err := e.AgentPrompt(config.Agent{Prompt: "builtin:reviewer"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got, "# Role: reviewer"), got)
}

func TestBuiltinPromptUnknown(t *testing.T) {
	e := &Engine{}
	_, err := e.BuiltinPrompt("nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown builtin prompt "nope"`)
}

func TestBuildInstructionsUsesPromptText(t *testing.T) {
	e := &Engine{}
	out, err := e.buildInstructions(config.Agent{PromptText: "Inline role."}, nil)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "Inline role."), out)
	assert.Contains(t, out, e.commonPrompt())
}

// ctDocsYAML is ctYAML plus a custom "docs" agent with inline instructions. It is disabled
// when disabled is true.
func ctDocsYAML(dataDir string, disabled bool) string {
	off := ""
	if disabled {
		off = ", disabled: true"
	}
	return fmt.Sprintf(ctYAML, dataDir) +
		fmt.Sprintf("agents:\n  docs: {harness: fake, prompt_text: Write docs.%s}\n", off)
}

func TestDisabledRoleBlocksRun(t *testing.T) {
	ctx := context.Background()
	r := ctNewRig(t)
	dataDir := t.TempDir()
	enabled, err := config.Parse([]byte(ctDocsYAML(dataDir, false)), filepath.Join(t.TempDir(), "onlet.yaml"))
	require.NoError(t, err)
	r.cfg = enabled
	e := r.engine(func(path string) (*config.Config, error) {
		return config.Parse([]byte(ctDocsYAML(dataDir, true)), path)
	})

	task, err := e.createTask(ctx, domain.Task{
		Project: "demo", Kind: domain.KindFeature, Title: "Write docs", Body: "original body", Role: "docs",
	})
	require.NoError(t, err)
	require.NoError(t, e.ReloadConfig(ctx))

	claimed, err := r.st.ClaimNextTask(ctx, nil)
	require.NoError(t, err)
	e.startTask(ctx, claimed)

	got, err := r.st.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskBlocked, got.Status)
	assert.Contains(t, got.BlockedReason, `agent "docs" is disabled`)

	runs, err := r.st.ListRuns(ctx, store.RunFilter{TaskID: task.ID})
	require.NoError(t, err)
	assert.Empty(t, runs, "a disabled agent must not start a run")
}
