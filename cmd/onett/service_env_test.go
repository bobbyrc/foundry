package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The daemon under launchd does not inherit the installing shell's environment, so a config
// that uses ${NAME} would fail to load there. Install records each such variable in the plist.
func TestInstallRecordsTheConfigsVariables(t *testing.T) {
	plistDir := stubHost(t)
	fakeRunner(t, nil)
	t.Setenv("ONETT_SVC_ROOT", t.TempDir())
	t.Setenv("ONETT_SVC_SUB", "a&b")
	cfg := filepath.Join(t.TempDir(), "onett.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte("server:\n  data_dir: ${ONETT_SVC_ROOT}/${ONETT_SVC_SUB}\n"), 0o600))

	code, out, errOut := runCLI("service", "install", "--config", cfg)
	require.Equal(t, 0, code, errOut)

	plist := filepath.Join(plistDir, "io.onett.serve.plist")
	body, err := os.ReadFile(plist)
	require.NoError(t, err)
	assert.Contains(t, string(body), "<key>ONETT_SVC_ROOT</key>\n\t\t<string>"+xmlEscape(os.Getenv("ONETT_SVC_ROOT"))+"</string>")
	assert.Contains(t, string(body), "<key>ONETT_SVC_SUB</key>\n\t\t<string>a&amp;b</string>")
	assert.Contains(t, out, "ONETT_SVC_ROOT, ONETT_SVC_SUB")

	info, err := os.Stat(plist)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the plist can hold values, so only the user reads it")
}

func TestInstallDoesNotRecordPathTwice(t *testing.T) {
	plistDir := stubHost(t)
	fakeRunner(t, nil)
	cfg := filepath.Join(t.TempDir(), "onett.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte("server:\n  data_dir: "+t.TempDir()+"\nprojects:\n  p:\n    repo: o/p\n    env:\n      TOOLS: ${PATH}\n"), 0o600))

	code, _, errOut := runCLI("service", "install", "--config", cfg)
	require.Equal(t, 0, code, errOut)

	body, err := os.ReadFile(filepath.Join(plistDir, "io.onett.serve.plist"))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(body), "<key>PATH</key>"))
}
