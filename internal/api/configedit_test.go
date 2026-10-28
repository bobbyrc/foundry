package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
)

// configHarness points the fake controller at a real config file in a temp dir. The file has a
// comment, so tests can see whether saves keep it.
func configHarness(t *testing.T) (*harness, string, string) {
	t.Helper()
	x := newHarness(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "foundry.yaml")
	text := fmt.Sprintf("# keep me\nserver:\n  addr: 127.0.0.1:7777\n  data_dir: %s\n", dir)
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	cfg, err := config.Parse([]byte(text), path)
	require.NoError(t, err)
	x.ctl.cfg = cfg
	return x, path, text
}

func TestConfigFileStaleVersionIsRefused(t *testing.T) {
	x, path, _ := configHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)

	rec := x.authReq(t, http.MethodPut, "/api/v1/config/file",
		`{"version":"stale","text":"server:\n  addr: 127.0.0.1:8888\n"}`, withCookie(c))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "changed since you opened it")
	assert.Zero(t, x.ctl.reloadCalls)
	got, _ := os.ReadFile(path)
	assert.Contains(t, string(got), "127.0.0.1:7777", "file untouched")
}

func TestConfigFileInvalidIsRefusedWithProblems(t *testing.T) {
	x, path, text := configHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	version := config.Version([]byte(text))

	body := fmt.Sprintf(`{"version":%q,"text":"server:\n  bogus_key: 1\n"}`, version)
	rec := x.authReq(t, http.MethodPut, "/api/v1/config/file", body, withCookie(c))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	var out struct {
		Error    string           `json:"error"`
		Problems []config.Problem `json:"problems"`
	}
	decode(t, rec, &out)
	assert.Equal(t, "the config is not valid", out.Error)
	assert.NotEmpty(t, out.Problems)
	assert.Zero(t, x.ctl.reloadCalls)

	body = fmt.Sprintf(`{"version":%q,"text":"limits: [\n"}`, version)
	rec = x.authReq(t, http.MethodPut, "/api/v1/config/file", body, withCookie(c))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	got, _ := os.ReadFile(path)
	assert.Equal(t, text, string(got), "a refused save writes nothing")
}

func TestConfigFileValidSaveWritesReloadsAndReportsRestart(t *testing.T) {
	x, path, text := configHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	version := config.Version([]byte(text))

	next := strings.Replace(text, "127.0.0.1:7777", "127.0.0.1:9999", 1)
	body := fmt.Sprintf(`{"version":%q,"text":%q}`, version, next)
	rec := x.authReq(t, http.MethodPut, "/api/v1/config/file", body, withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"restart_required":["server.addr"]`)
	assert.Equal(t, 1, x.ctl.reloadCalls)

	got, _ := os.ReadFile(path)
	assert.Equal(t, next, string(got))
}

func TestConfigGetFileRequiresOwner(t *testing.T) {
	x, path, _ := configHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	rec := x.authReq(t, http.MethodGet, "/api/v1/config/file", "", withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"path":"`+path+`"`)
	assert.Contains(t, rec.Body.String(), "# keep me")
}

func TestConfigDocumentPatchKeepsComments(t *testing.T) {
	x, path, text := configHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	version := config.Version([]byte(text))

	rec := x.authReq(t, http.MethodGet, "/api/v1/config/document", "", withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"schema"`)

	body := fmt.Sprintf(`{"version":%q,"changes":[{"op":"set","path":["server","addr"],"value":"127.0.0.1:9999"}]}`, version)
	rec = x.authReq(t, http.MethodPatch, "/api/v1/config/document", body, withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"restart_required":["server.addr"]`)

	got, _ := os.ReadFile(path)
	assert.Contains(t, string(got), "# keep me", "the comment survives the edit")
	assert.Contains(t, string(got), "127.0.0.1:9999")
	assert.Equal(t, 1, x.ctl.reloadCalls)
}
