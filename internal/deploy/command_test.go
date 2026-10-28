package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCommand(t *testing.T, opts map[string]any, env map[string]string) Deployer {
	t.Helper()
	d, err := New(Config{Type: "command", Options: opts, Dir: t.TempDir(), Env: env})
	require.NoError(t, err)
	return d
}

func TestCommandDeployJSONLine(t *testing.T) {
	d := newTestCommand(t, map[string]any{
		"deploy": `echo building; echo '{"id":"dpl_1","url":"https://x.example","state":"ready"}'`,
	}, nil)
	h, err := d.Deploy(context.Background(), "abc123")
	require.NoError(t, err)
	assert.Equal(t, "dpl_1", h.ID)
	assert.Equal(t, "https://x.example", h.URL)
}

func TestCommandDeployURLLine(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo "https://preview.example"`}, nil)
	h, err := d.Deploy(context.Background(), "abcdef1234567890")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(h.ID, "cmd-abcdef12-"), h.ID)
	assert.Equal(t, "https://preview.example", h.URL)
}

func TestCommandDeployPlainOutput(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo done`}, nil)
	h, err := d.Deploy(context.Background(), "abc")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(h.ID, "cmd-abc-"), h.ID)
	assert.Empty(t, h.URL)
}

func TestCommandDeployEnvReachesScript(t *testing.T) {
	t.Setenv("FOUNDRY_TEST_PARENT", "parent")
	d := newTestCommand(t, map[string]any{
		"deploy": `echo "https://$FOUNDRY_REF.example/$FOUNDRY_TEST_CFG/$FOUNDRY_TEST_PARENT"`,
	}, map[string]string{"FOUNDRY_TEST_CFG": "cfg"})
	h, err := d.Deploy(context.Background(), "sha1")
	require.NoError(t, err)
	assert.Equal(t, "https://sha1.example/cfg/parent", h.URL)
}

func TestCommandDeployRunsInDir(t *testing.T) {
	dir := t.TempDir()
	d, err := New(Config{Type: "command", Dir: dir, Options: map[string]any{
		"deploy": "pwd -P > where.txt",
	}})
	require.NoError(t, err)
	_, err = d.Deploy(context.Background(), "sha")
	require.NoError(t, err)

	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(dir, "where.txt"))
	require.NoError(t, err)
	assert.Equal(t, resolved, strings.TrimSpace(string(got)))
}

func TestCommandDeployNonZeroExit(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo partial; echo boom >&2; exit 3`}, nil)
	_, err := d.Deploy(context.Background(), "sha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit status 3")
	assert.Contains(t, err.Error(), "partial")
	assert.Contains(t, err.Error(), "boom")
}

func TestCommandStatusJSON(t *testing.T) {
	d := newTestCommand(t, map[string]any{
		"deploy": `echo https://ignored.example`,
		"status": `echo "{\"state\":\"building\",\"url\":\"https://$FOUNDRY_DEPLOY_ID.example\",\"message\":\"compiling\"}"`,
	}, nil)
	st, err := d.Status(context.Background(), "dpl_9")
	require.NoError(t, err)
	assert.Equal(t, StateBuilding, st.State)
	assert.Equal(t, "https://dpl_9.example", st.URL)
	assert.Equal(t, "compiling", st.Message)
}

func TestCommandStatusBareWord(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo x`, "status": `echo warming; echo error`}, nil)
	st, err := d.Status(context.Background(), "id")
	require.NoError(t, err)
	assert.Equal(t, StateError, st.State)
}

func TestCommandStatusUnrecognised(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo x`, "status": `echo bogus`}, nil)
	_, err := d.Status(context.Background(), "id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")

	empty := newTestCommand(t, map[string]any{"deploy": `echo x`, "status": `true`}, nil)
	_, err = empty.Status(context.Background(), "id")
	require.Error(t, err)
}

func TestCommandStatusNonZeroExit(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo x`, "status": `exit 1`}, nil)
	_, err := d.Status(context.Background(), "id")
	require.Error(t, err)
}

func TestCommandRememberedStatus(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo "https://site.example"`}, nil)
	h, err := d.Deploy(context.Background(), "sha")
	require.NoError(t, err)

	st, err := d.Status(context.Background(), h.ID)
	require.NoError(t, err)
	assert.Equal(t, StateReady, st.State)
	assert.Equal(t, "https://site.example", st.URL)

	unknown, err := d.Status(context.Background(), "never-seen")
	require.NoError(t, err)
	assert.Equal(t, StateReady, unknown.State)
	assert.Empty(t, unknown.URL)
}

func TestCommandRollbackWithoutCommand(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo x`}, nil)
	err := d.Rollback(context.Background(), "id", "ref")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no rollback command configured")
}

func TestCommandRollbackRunsWithEnv(t *testing.T) {
	dir := t.TempDir()
	d, err := New(Config{Type: "command", Dir: dir, Options: map[string]any{
		"deploy":   "echo x",
		"rollback": `printf '%s %s' "$FOUNDRY_ROLLBACK_ID" "$FOUNDRY_ROLLBACK_REF" > rb.txt`,
	}})
	require.NoError(t, err)
	require.NoError(t, d.Rollback(context.Background(), "dpl_old", "prev-sha"))
	got, err := os.ReadFile(filepath.Join(dir, "rb.txt"))
	require.NoError(t, err)
	assert.Equal(t, "dpl_old prev-sha", string(got))
}

func TestCommandRollbackNonZeroExit(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `echo x`, "rollback": `echo nope >&2; exit 4`}, nil)
	err := d.Rollback(context.Background(), "id", "ref")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope")
}

func TestCommandTimeoutKillsCommand(t *testing.T) {
	d := newTestCommand(t, map[string]any{"deploy": `exec sleep 10`, "timeout": "200ms"}, nil)
	start := time.Now()
	_, err := d.Deploy(context.Background(), "sha")
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestCommandFactoryErrors(t *testing.T) {
	_, err := New(Config{Type: "command", Options: map[string]any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"deploy" is required`)

	_, err = New(Config{Type: "command", Options: map[string]any{"deploy": "x", "timeout": "soon"}})
	require.Error(t, err)

	_, err = New(Config{Type: "command", Options: map[string]any{"deploy": "x", "timeout": "-1s"}})
	require.Error(t, err)
}

func TestOptHelpers(t *testing.T) {
	assert.Equal(t, "", optString(map[string]any{}, "k"))
	assert.Equal(t, "42", optString(map[string]any{"k": 42}, "k"))

	d, err := optDuration(map[string]any{}, "k", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, time.Minute, d)
}
