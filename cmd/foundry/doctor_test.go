package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolRunner fakes git, gh, claude and codex. Commands named in failing return that error with
// no output. Any other command is a test bug.
func toolRunner(t *testing.T, failing map[string]error) *[]runCall {
	t.Helper()
	return fakeRunner(t, func(name string, args ...string) (string, error) {
		if err, ok := failing[name]; ok {
			return "", err
		}
		switch name {
		case "git":
			return "git version 2.45.0\n", nil
		case "gh":
			return "github.com\n  ✓ Logged in to github.com account bobbyrc\n", nil
		case "claude":
			return "2.1.0 (Claude Code)\n", nil
		case "codex":
			return "codex-cli 0.9.0\n", nil
		}
		return "", fmt.Errorf("unexpected command %s %v", name, args)
	})
}

// healthyDaemon serves the health route on a test server and points the CLI at it.
func healthyDaemon(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "9.9.9"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("FOUNDRY_API", srv.URL)
}

// doctorFixture is a temp data dir, a clone that exists, and a config that uses them. The
// config has three projects (a GitHub one with a clone, one that will be cloned, and a paused
// local-path one) and an exec harness that is not used by any agent.
type doctorFixture struct {
	dataDir string
	clone   string
	cfg     string
}

func newDoctorFixture(t *testing.T, extra string) doctorFixture {
	t.Helper()
	root := t.TempDir()
	f := doctorFixture{
		dataDir: filepath.Join(root, "data"),
		clone:   filepath.Join(root, "clone"),
	}
	require.NoError(t, os.MkdirAll(f.clone, 0o755))
	body := "server:\n" +
		"  data_dir: " + strconv.Quote(f.dataDir) + "\n" +
		"projects:\n" +
		"  foundry:\n" +
		"    repo: \"bobbyrc/foundry\"\n" +
		"    path: " + strconv.Quote(f.clone) + "\n" +
		"  fresh:\n" +
		"    repo: \"bobbyrc/fresh\"\n" +
		"    path: " + strconv.Quote(filepath.Join(root, "not-yet")) + "\n" +
		"  notes:\n" +
		"    repo: \"/Users/me/notes\"\n" +
		"    path: " + strconv.Quote(f.clone) + "\n" +
		"    paused: true\n" +
		"harnesses:\n" +
		"  tool:\n" +
		"    type: exec\n" +
		"    command: [\"/bin/sh\"]\n" + extra
	f.cfg = filepath.Join(root, "foundry.yaml")
	require.NoError(t, os.WriteFile(f.cfg, []byte(body), 0o600))
	return f
}

// assertLine checks that some output line has the given status and contains sub.
func assertLine(t *testing.T, out, status, sub string) {
	t.Helper()
	prefix := fmt.Sprintf("%-4s  ", status)
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) && strings.Contains(l, sub) {
			return
		}
	}
	t.Errorf("no %q line containing %q in output:\n%s", status, sub, out)
}

func TestDoctorAllHealthy(t *testing.T) {
	healthyDaemon(t)
	f := newDoctorFixture(t, "")
	calls := toolRunner(t, nil)

	code, out, errOut := runCLI("doctor", "--config", f.cfg)
	require.Equal(t, 0, code, out+errOut)

	assertLine(t, out, "ok", "config loads: "+f.cfg)
	assertLine(t, out, "ok", "data dir writable: "+f.dataDir)
	assertLine(t, out, "ok", "git: git version 2.45.0")
	assertLine(t, out, "ok", "gh: authenticated")
	assertLine(t, out, "ok", "harness claude: claude (2.1.0 (Claude Code))")
	assertLine(t, out, "ok", "harness codex: codex (codex-cli 0.9.0)")
	assertLine(t, out, "ok", "harness tool: /bin/sh (found)")
	assertLine(t, out, "ok", "project foundry: repo bobbyrc/foundry")
	assertLine(t, out, "ok", "project foundry: clone at "+f.clone)
	assertLine(t, out, "ok", "project fresh: no clone at")
	assertLine(t, out, "ok", "it will be cloned there")
	assertLine(t, out, "warn", "project notes: repo \"/Users/me/notes\" is not owner/name")
	assertLine(t, out, "warn", "project notes: paused — queued work will not start")
	assertLine(t, out, "ok", "daemon: running at")
	assert.NotContains(t, out, "FAIL")

	var names []string
	for _, c := range *calls {
		names = append(names, c.name+" "+strings.Join(c.args, " "))
	}
	assert.Equal(t, []string{"git --version", "gh auth status", "claude --version", "codex --version"}, names)
}

func TestDoctorFailsWhenUsedHarnessIsMissing(t *testing.T) {
	healthyDaemon(t)
	f := newDoctorFixture(t, "")
	toolRunner(t, map[string]error{"codex": errors.New(`exec: "codex": executable file not found in $PATH`)})

	code, out, _ := runCLI("doctor", "--config", f.cfg)
	assert.Equal(t, 1, code)
	assertLine(t, out, "FAIL", "harness codex: codex not runnable")
	assertLine(t, out, "FAIL", "agents implementer, improver use it")
	assertLine(t, out, "FAIL", "fix: install codex and make sure it is on PATH")
}

func TestDoctorWarnsWhenUnusedHarnessIsMissing(t *testing.T) {
	healthyDaemon(t)
	f := newDoctorFixture(t, "  missing:\n    type: exec\n    command: [\"/nonexistent/tool-xyz\"]\n")
	toolRunner(t, nil)

	code, out, _ := runCLI("doctor", "--config", f.cfg)
	assert.Equal(t, 0, code, out)
	assertLine(t, out, "warn", "harness missing: /nonexistent/tool-xyz not runnable")
	assertLine(t, out, "warn", "no agent uses it")
}

func TestDoctorFailsOnUnknownHarnessType(t *testing.T) {
	healthyDaemon(t)
	f := newDoctorFixture(t, "  weird:\n    type: nope\n")
	toolRunner(t, nil)

	code, out, _ := runCLI("doctor", "--config", f.cfg)
	assert.Equal(t, 1, code)
	assertLine(t, out, "FAIL", `harness weird: unknown type "nope"`)
}

func TestDoctorFailsWhenGitHubIsNotAuthenticated(t *testing.T) {
	healthyDaemon(t)
	f := newDoctorFixture(t, "")
	toolRunner(t, map[string]error{"gh": errors.New("You are not logged into any GitHub hosts")})

	code, out, _ := runCLI("doctor", "--config", f.cfg)
	assert.Equal(t, 1, code)
	assertLine(t, out, "FAIL", "gh: not authenticated: You are not logged into any GitHub hosts")
	assertLine(t, out, "FAIL", "fix: gh auth login")
}

func TestDoctorFailsWhenGitIsMissing(t *testing.T) {
	healthyDaemon(t)
	f := newDoctorFixture(t, "")
	toolRunner(t, map[string]error{"git": errors.New("executable file not found")})

	code, out, _ := runCLI("doctor", "--config", f.cfg)
	assert.Equal(t, 1, code)
	assertLine(t, out, "FAIL", "git: executable file not found")
}

func TestDoctorReportsBrokenConfigAndStillChecksDaemon(t *testing.T) {
	healthyDaemon(t)
	toolRunner(t, nil)
	missing := filepath.Join(t.TempDir(), "absent.yaml")

	code, out, _ := runCLI("doctor", "--config", missing)
	assert.Equal(t, 1, code)
	assertLine(t, out, "FAIL", "config: ")
	assertLine(t, out, "FAIL", "foundry init")
	assertLine(t, out, "ok", "git: git version 2.45.0")
	assertLine(t, out, "ok", "daemon: running at")
}

func TestDoctorWarnsWhenDaemonIsDown(t *testing.T) {
	// A server that was started and closed gives a port that refuses connections.
	srv := httptest.NewServer(http.NotFoundHandler())
	down := srv.URL
	srv.Close()
	t.Setenv("FOUNDRY_API", down)

	f := newDoctorFixture(t, "")
	toolRunner(t, nil)

	code, out, _ := runCLI("doctor", "--config", f.cfg)
	assert.Equal(t, 0, code, "a daemon that is not running is a warning, not a failure")
	assertLine(t, out, "warn", "daemon: not reachable at "+down)
	assertLine(t, out, "warn", "foundry service install")
}

func TestDoctorReadsHealthJSON(t *testing.T) {
	// The health route answers with the version; doctor prints it.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": "1.2.3"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("FOUNDRY_API", srv.URL)

	f := newDoctorFixture(t, "")
	toolRunner(t, nil)

	_, out, _ := runCLI("doctor", "--config", f.cfg)
	assertLine(t, out, "ok", "daemon: running at "+srv.URL+" (version 1.2.3)")
}
