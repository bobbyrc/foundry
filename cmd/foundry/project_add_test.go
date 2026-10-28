package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const addTestConfig = `# header comment that must survive
server:
  addr: 127.0.0.1:7777

projects:
  # the existing project
  app:
    repo: "acme/app"   # its repository
    path: "/src/app"
`

// demoBlock is the text project add writes for `demo --repo acme/demo --path /tmp/x --branch dev`
// under a projects section indented two spaces.
const demoBlock = "  demo:\n    repo: acme/demo\n    path: /tmp/x\n    default_branch: dev\n"

func writeAddTestConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "foundry.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestProjectAddKeepsCommentsAndLoads(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)

	code, out, errOut := runCLI("project", "add", "demo", "--repo", "acme/demo",
		"--path", "/tmp/x", "--branch", "dev", "--config", path)
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "added project demo (acme/demo) to "+path)
	assert.Contains(t, out, `run "foundry config --reload"`)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, addTestConfig+demoBlock, string(data), "original bytes must be kept, with only the block added")

	cfg, err := config.Parse(data, path)
	require.NoError(t, err)
	assert.Equal(t, "acme/app", cfg.Projects["app"].Repo)
	assert.Equal(t, "/src/app", cfg.Projects["app"].Path)
	require.Contains(t, cfg.Projects, "demo")
	assert.Equal(t, "acme/demo", cfg.Projects["demo"].Repo)
	assert.Equal(t, "/tmp/x", cfg.Projects["demo"].Path)
	assert.Equal(t, "dev", cfg.Projects["demo"].DefaultBranch)
	assert.Empty(t, cfg.SelfProject)
}

// TestProjectAddToExampleFileOnlyInsertsLines checks the real example config: the new text is the
// example with one block inserted and nothing else changed.
func TestProjectAddToExampleFileOnlyInsertsLines(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	original, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "foundry.example.yaml"))
	require.NoError(t, err)
	path := writeAddTestConfig(t, string(original))

	code, _, errOut := runCLI("project", "add", "demo", "--repo", "acme/demo",
		"--path", "/tmp/x", "--branch", "dev", "--config", path)
	require.Equal(t, 0, code, errOut)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(data), demoBlock), "the block appears exactly once")
	assert.Equal(t, string(original), strings.Replace(string(data), demoBlock, "", 1),
		"removing the inserted block must give back the original bytes")

	cfg, err := config.Parse(data, path)
	require.NoError(t, err)
	require.Contains(t, cfg.Projects, "demo")
	assert.Equal(t, "acme/demo", cfg.Projects["demo"].Repo)
	assert.Equal(t, "foundry", cfg.SelfProject, "an existing self_project is kept")
}

func TestProjectAddBeforeNextKeyKeepsBlankLineAndComment(t *testing.T) {
	original := "projects:\n  app:\n    repo: a/app\n\n# notes for the next key\nnotify:\n  - type: log\n"
	path := writeAddTestConfig(t, original)

	code, _, errOut := runCLI("project", "add", "demo", "--repo", "acme/demo", "--config", path)
	require.Equal(t, 0, code, errOut)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "projects:\n  app:\n    repo: a/app\n  demo:\n    repo: acme/demo\n\n# notes for the next key\nnotify:\n  - type: log\n",
		string(data))
}

func TestProjectAddCreatesProjectsKeyAtEnd(t *testing.T) {
	original := "# just a server\nserver:\n  addr: 127.0.0.1:7777\n"
	path := writeAddTestConfig(t, original)

	code, _, errOut := runCLI("project", "add", "demo", "--repo", "acme/demo", "--config", path)
	require.Equal(t, 0, code, errOut)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original+"\nprojects:\n  demo:\n    repo: acme/demo\n", string(data))
	cfg, err := config.Parse(data, path)
	require.NoError(t, err)
	assert.Equal(t, "acme/demo", cfg.Projects["demo"].Repo)
}

func TestProjectAddFillsEmptyProjectsLine(t *testing.T) {
	original := "server:\n  addr: 127.0.0.1:7777\nprojects:   # none yet\nnotify:\n  - type: log\n"
	path := writeAddTestConfig(t, original)

	code, _, errOut := runCLI("project", "add", "demo", "--repo", "acme/demo", "--config", path)
	require.Equal(t, 0, code, errOut)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "server:\n  addr: 127.0.0.1:7777\nprojects:   # none yet\n  demo:\n    repo: acme/demo\nnotify:\n  - type: log\n",
		string(data))
}

func TestProjectAddRefusesFlowStyleProjects(t *testing.T) {
	original := "projects: {}\n"
	path := writeAddTestConfig(t, original)

	code, _, errOut := runCLI("project", "add", "demo", "--repo", "acme/demo", "--config", path)
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "cannot add to the projects block in "+path+" automatically (flow style); add it by hand")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, string(data))
}

func TestProjectAddPreservesFilePermissions(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	require.NoError(t, os.Chmod(path, 0o640))

	code, _, errOut := runCLI("project", "add", "demo", "--repo", "acme/demo", "--config", path)
	require.Equal(t, 0, code, errOut)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func TestProjectAddRefusesExistingName(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	code, _, errOut := runCLI("project", "add", "app", "--repo", "acme/other", "--config", path)
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, `project "app" already exists in `+path)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestProjectAddMissingConfigSuggestsInit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.yaml")

	code, _, errOut := runCLI("project", "add", "demo", "--repo", "acme/demo", "--config", path)
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "no config at "+path+`; run "foundry init" first`)
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "add must not create the config")
}

func TestProjectAddEmptyFileBecomesValidConfig(t *testing.T) {
	path := writeAddTestConfig(t, "")

	code, _, errOut := runCLI("project", "add", "demo", "--repo", "acme/demo", "--config", path)
	require.Equal(t, 0, code, errOut)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "projects:\n  demo:\n    repo: acme/demo\n", string(data))
}

func TestProjectAddQuotesValuesThatNeedIt(t *testing.T) {
	path := writeAddTestConfig(t, "projects:\n  app:\n    repo: a/app\n")
	odd := "/tmp/x: y #z"

	code, _, errOut := runCLI("project", "add", "demo", "--repo", "acme/demo", "--path", odd, "--config", path)
	require.Equal(t, 0, code, errOut)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	cfg, err := config.Parse(data, path)
	require.NoError(t, err)
	assert.Equal(t, odd, cfg.Projects["demo"].Path)
}

func TestProjectAddSetsSelfProjectOnlyWhenUnset(t *testing.T) {
	original := "server:\n  addr: 127.0.0.1:7777\n"
	path := writeAddTestConfig(t, original)

	code, _, errOut := runCLI("project", "add", "foundry", "--repo", selfRepo, "--config", path)
	require.Equal(t, 0, code, errOut)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original+"\nprojects:\n  foundry:\n    repo: bobbyrc/foundry\nself_project: foundry\n", string(data))
	cfg, err := config.Parse(data, path)
	require.NoError(t, err)
	assert.Equal(t, "foundry", cfg.SelfProject)

	path2 := writeAddTestConfig(t, "self_project: keep\nprojects:\n  keep:\n    repo: a/keep\n")
	code, _, errOut = runCLI("project", "add", "foundry", "--repo", selfRepo, "--config", path2)
	require.Equal(t, 0, code, errOut)
	data, err = os.ReadFile(path2)
	require.NoError(t, err)
	cfg, err = config.Parse(data, path2)
	require.NoError(t, err)
	assert.Equal(t, "keep", cfg.SelfProject)
}

func TestProjectAddRejectsInvalidInput(t *testing.T) {
	path := writeAddTestConfig(t, addTestConfig)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	for _, args := range [][]string{
		{"project", "add", "bad name", "--repo", "acme/demo", "--config", path},
		{"project", "add", "-lead", "--repo", "acme/demo", "--config", path},
		{"project", "add", "demo", "--config", path},
		{"project", "add", "--repo", "acme/demo", "--config", path},
		{"project", "add", "a", "b", "--repo", "acme/demo", "--config", path},
	} {
		code, _, errOut := runCLI(args...)
		assert.Equal(t, 2, code, "args %v: %s", args, errOut)
	}
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}
