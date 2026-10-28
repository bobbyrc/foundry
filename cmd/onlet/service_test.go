package main

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCall is one command the service code asked the runner to run.
type runCall struct {
	name string
	args []string
}

// stubHost makes the service code believe it runs on macOS as uid 501, with a temp
// LaunchAgents directory and a temp HOME. Everything is restored when the test ends.
func stubHost(t *testing.T) (plistDir string) {
	t.Helper()
	plistDir = t.TempDir()
	t.Setenv("HOME", t.TempDir())

	prevOS, prevUID, prevDir := hostOS, currentUID, launchAgentsDir
	hostOS = "darwin"
	currentUID = func() int { return 501 }
	launchAgentsDir = plistDir
	t.Cleanup(func() {
		hostOS, currentUID, launchAgentsDir = prevOS, prevUID, prevDir
	})
	return plistDir
}

// fakeRunner replaces commandRunner. It records every call and answers with respond, or with
// empty success when respond is nil. Each call must carry the 10 second deadline.
func fakeRunner(t *testing.T, respond func(name string, args ...string) (string, error)) *[]runCall {
	t.Helper()
	var calls []runCall
	prev := commandRunner
	commandRunner = func(ctx context.Context, name string, args ...string) (string, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Errorf("command %s has no deadline", name)
		}
		calls = append(calls, runCall{name: name, args: args})
		if respond == nil {
			return "", nil
		}
		return respond(name, args...)
	}
	t.Cleanup(func() { commandRunner = prev })
	return &calls
}

// writeServiceConfig writes a config whose data_dir is dataDir and returns its path.
func writeServiceConfig(t *testing.T, dataDir string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "onlet.yaml")
	body := "server:\n  data_dir: " + strconv.Quote(dataDir) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestRenderPlistContent(t *testing.T) {
	out, err := renderPlist(plistSpec{
		Args:    []string{"/opt/onlet & co/onlet", "serve", "--config", "/etc/<cfg>.yaml"},
		Path:    "/opt/homebrew/bin:/usr/bin",
		LogPath: "/data/a&b/onlet.log",
	})
	require.NoError(t, err)

	// The document must be well-formed XML, which is what launchd parses.
	dec := xml.NewDecoder(strings.NewReader(out))
	for {
		_, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}

	assert.Contains(t, out, "<key>Label</key>\n\t<string>io.onlet.serve</string>")
	assert.Contains(t, out, "<key>KeepAlive</key>\n\t<true/>")
	assert.Contains(t, out, "<key>RunAtLoad</key>\n\t<true/>")
	assert.Contains(t, out, "<key>ThrottleInterval</key>\n\t<integer>10</integer>")
	assert.Contains(t, out, "<key>PATH</key>\n\t\t<string>/opt/homebrew/bin:/usr/bin</string>")
	assert.Contains(t, out, "<string>/opt/onlet &amp; co/onlet</string>")
	assert.Contains(t, out, "<string>/etc/&lt;cfg&gt;.yaml</string>")
	assert.Contains(t, out, "<key>StandardOutPath</key>\n\t<string>/data/a&amp;b/onlet.log</string>")
	assert.Contains(t, out, "<key>StandardErrorPath</key>\n\t<string>/data/a&amp;b/onlet.log</string>")
	assert.Contains(t, out, "\t\t<string>serve</string>\n\t\t<string>--config</string>")
}

func TestInstallWritesPlistAndBootstraps(t *testing.T) {
	plistDir := stubHost(t)
	dataDir := filepath.Join(t.TempDir(), "a&b")
	cfg := writeServiceConfig(t, dataDir)
	calls := fakeRunner(t, nil)

	code, out, errOut := runCLI("service", "install", "--config", cfg)
	require.Equal(t, 0, code, errOut)

	plist := filepath.Join(plistDir, "io.onlet.serve.plist")
	body, err := os.ReadFile(plist)
	require.NoError(t, err)
	exe, err := onletExecutable()
	require.NoError(t, err)
	absCfg, err := filepath.Abs(cfg)
	require.NoError(t, err)

	assert.Contains(t, string(body), "<string>"+xmlEscape(exe)+"</string>")
	assert.Contains(t, string(body), "<string>"+xmlEscape(absCfg)+"</string>")
	assert.Contains(t, string(body), "<key>KeepAlive</key>")
	assert.Contains(t, string(body), "<string>"+xmlEscape(os.Getenv("PATH"))+"</string>")
	assert.Contains(t, string(body), "<string>"+xmlEscape(filepath.Join(dataDir, "onlet.log"))+"</string>")
	assert.Contains(t, string(body), "a&amp;b", "data dir with & must be escaped in the plist")
	assert.DirExists(t, dataDir, "launchd opens the log file, so the data dir must exist")

	assert.Equal(t, []runCall{{name: "launchctl", args: []string{"bootstrap", "gui/501", plist}}}, *calls)
	assert.Contains(t, out, "installed io.onlet.serve")
}

func TestInstallTwiceKickstarts(t *testing.T) {
	plistDir := stubHost(t)
	cfg := writeServiceConfig(t, filepath.Join(t.TempDir(), "data"))
	calls := fakeRunner(t, nil)

	code, _, errOut := runCLI("service", "install", "--config", cfg)
	require.Equal(t, 0, code, errOut)
	before, err := os.ReadFile(filepath.Join(plistDir, "io.onlet.serve.plist"))
	require.NoError(t, err)
	*calls = nil

	code, out, errOut := runCLI("service", "install", "--config", cfg)
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, []runCall{{name: "launchctl", args: []string{"kickstart", "-k", "gui/501/io.onlet.serve"}}}, *calls)
	assert.Contains(t, out, "already installed")

	after, err := os.ReadFile(filepath.Join(plistDir, "io.onlet.serve.plist"))
	require.NoError(t, err)
	assert.Equal(t, before, after, "a second install must not rewrite the plist")
}

func TestInstallFailedBootstrapRemovesPlist(t *testing.T) {
	plistDir := stubHost(t)
	cfg := writeServiceConfig(t, filepath.Join(t.TempDir(), "data"))
	fakeRunner(t, func(name string, args ...string) (string, error) {
		return "Bootstrap failed: 5: Input/output error", errors.New("exit status 5")
	})

	code, _, errOut := runCLI("service", "install", "--config", cfg)
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "Bootstrap failed")
	assert.NoFileExists(t, filepath.Join(plistDir, "io.onlet.serve.plist"))
}

func TestInstallNeedsLoadableConfig(t *testing.T) {
	plistDir := stubHost(t)
	calls := fakeRunner(t, nil)
	missing := filepath.Join(t.TempDir(), "absent.yaml")

	code, _, errOut := runCLI("service", "install", "--config", missing)
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "onlet init")
	assert.NoFileExists(t, filepath.Join(plistDir, "io.onlet.serve.plist"))
	assert.Empty(t, *calls)
}

func TestUninstallBootsOutAndRemoves(t *testing.T) {
	plistDir := stubHost(t)
	cfg := writeServiceConfig(t, filepath.Join(t.TempDir(), "data"))
	fakeRunner(t, nil)
	code, _, errOut := runCLI("service", "install", "--config", cfg)
	require.Equal(t, 0, code, errOut)

	calls := fakeRunner(t, func(name string, args ...string) (string, error) {
		return "Boot-out failed: 3: No such process", errors.New("exit status 3")
	})
	code, out, errOut := runCLI("service", "uninstall")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, []runCall{{name: "launchctl", args: []string{"bootout", "gui/501/io.onlet.serve"}}}, *calls)
	assert.NoFileExists(t, filepath.Join(plistDir, "io.onlet.serve.plist"))
	assert.Contains(t, out, "uninstalled")
}

func TestUninstallKeepsPlistOnRealBootoutError(t *testing.T) {
	plistDir := stubHost(t)
	cfg := writeServiceConfig(t, filepath.Join(t.TempDir(), "data"))
	fakeRunner(t, nil)
	code, _, errOut := runCLI("service", "install", "--config", cfg)
	require.Equal(t, 0, code, errOut)

	fakeRunner(t, func(name string, args ...string) (string, error) {
		return "Operation not permitted", errors.New("exit status 1")
	})
	code, _, errOut = runCLI("service", "uninstall")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "Operation not permitted")
	assert.FileExists(t, filepath.Join(plistDir, "io.onlet.serve.plist"))
}

func TestUninstallWhenNotInstalled(t *testing.T) {
	stubHost(t)
	fakeRunner(t, func(name string, args ...string) (string, error) {
		return "Could not find service \"io.onlet.serve\" in domain for port", errors.New("exit status 113")
	})

	code, out, errOut := runCLI("service", "uninstall")
	assert.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "not installed")
}

func TestStatusStates(t *testing.T) {
	stubHost(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	cfg := writeServiceConfig(t, dataDir)

	// Not installed: launchctl print fails and there is no plist.
	fakeRunner(t, func(name string, args ...string) (string, error) {
		return "Could not find service", errors.New("exit status 113")
	})
	code, out, _ := runCLI("service", "status", "--config", cfg)
	require.Equal(t, 0, code)
	assert.Contains(t, out, "not installed\n")
	assert.Contains(t, out, "log: ")

	// Running: launchctl print reports a pid.
	fakeRunner(t, func(name string, args ...string) (string, error) {
		return "gui/501/io.onlet.serve = {\n\tstate = running\n\tpid = 4242\n}\n", nil
	})
	code, out, _ = runCLI("service", "status", "--config", cfg)
	require.Equal(t, 0, code)
	assert.Contains(t, out, "running (pid 4242)\n")

	// Installed but not running: plist exists and launchctl print has no pid.
	code, _, errOut := runCLI("service", "install", "--config", cfg)
	require.Equal(t, 0, code, errOut)
	fakeRunner(t, func(name string, args ...string) (string, error) {
		return "", errors.New("exit status 113")
	})
	code, out, _ = runCLI("service", "status", "--config", cfg)
	require.Equal(t, 0, code)
	assert.Contains(t, out, "installed, not running\n")
	assert.Contains(t, out, "log: "+filepath.Join(dataDir, "onlet.log")+"\n")
}

func TestServiceRefusesNonDarwin(t *testing.T) {
	stubHost(t)
	hostOS = "linux"
	calls := fakeRunner(t, nil)

	code, _, errOut := runCLI("service", "install")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "macOS")
	assert.Empty(t, *calls)
}

func TestServiceUsageErrors(t *testing.T) {
	stubHost(t)
	code, _, errOut := runCLI("service")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "usage: onlet service")

	code, _, errOut = runCLI("service", "restart")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, `unknown service action "restart"`)
}
