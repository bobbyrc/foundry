package main

import (
	"os"
	"strings"
	"testing"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scalarTailConfig ends with a block scalar whose last line looks like a comment. The line is
// indented deeper than the project names, so it is part of the run script, not a comment.
const scalarTailConfig = `projects:
  app:
    repo: acme/app
    setup:
      - name: build
        run: |
          make build
          # done
`

// Before the indentation bound, the new project was inserted above "# done", which cut the run
// script down to "make build". config.Parse accepts that, so only the scalar value shows it.
func TestProjectAddKeepsBlockScalarEndingInCommentLikeLine(t *testing.T) {
	path := writeAddTestConfig(t, scalarTailConfig)

	code, _, errOut := runCLI("project", "add", "demo", "--repo", "acme/demo", "--config", path)
	require.Equal(t, 0, code, errOut)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, scalarTailConfig+"  demo:\n    repo: acme/demo\n", string(data), "the entry goes after the scalar")

	cfg, err := config.Parse(data, path)
	require.NoError(t, err)
	require.Len(t, cfg.Projects["app"].Setup, 1)
	assert.Equal(t, "make build\n# done\n", cfg.Projects["app"].Setup[0].Run, "the run script is unchanged")
	require.Contains(t, cfg.Projects, "demo")
	assert.Equal(t, "acme/demo", cfg.Projects["demo"].Repo)
}

// The guard refuses a text that loads but changes existing settings, and writes nothing.
func TestCheckInsertionRefusesChangedSettings(t *testing.T) {
	const path = "onett.yaml"
	before := []byte(scalarTailConfig)

	// Dropping the comment-looking line changes the run script, though the text still parses.
	changed := []byte(strings.Replace(scalarTailConfig, "          # done\n", "", 1) + demoBlock)
	_, err := config.Parse(changed, path)
	require.NoError(t, err, "the changed text must still load, or the guard is not what catches it")

	err = checkInsertion(before, changed, path, "demo", false)
	require.Error(t, err)
	assert.EqualError(t, err, "refusing to write "+path+": inserting project demo would change existing settings; add it by hand")

	// The correct insertion passes the guard.
	good := []byte(scalarTailConfig + demoBlock)
	assert.NoError(t, checkInsertion(before, good, path, "demo", false))
}

// The self_project line that a run may add is allowed only when that run set it, and is compared
// as the only other change.
func TestCheckInsertionSelfProject(t *testing.T) {
	const path = "onett.yaml"
	before := []byte(scalarTailConfig)
	after := []byte(scalarTailConfig + demoBlock + "self_project: demo\n")

	assert.NoError(t, checkInsertion(before, after, path, "demo", true),
		"a self_project set by this run is cleared back for the comparison")
	assert.Error(t, checkInsertion(before, after, path, "demo", false),
		"a self_project the run did not set is a change to existing settings")
}
