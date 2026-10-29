package main

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerbCommandsPrintUsageForHelp(t *testing.T) {
	verbs := map[string][]string{
		"task":    {"create", "list", "show", "cancel", "retry"},
		"run":     {"list", "show", "cancel", "logs"},
		"project": {"add", "pause", "resume"},
	}
	for cmd, names := range verbs {
		for _, flag := range []string{"-h", "--help", "help"} {
			t.Run(cmd+" "+flag, func(t *testing.T) {
				code, out, errOut := runCLI(cmd, flag)
				require.Equal(t, 0, code, errOut)
				assert.Contains(t, out, "usage: onett "+cmd)
				for _, v := range names {
					assert.Contains(t, out, v)
				}
				assert.NotContains(t, errOut, "unknown")
			})
		}
	}
}

func TestProjectPauseHelpDoesNotCallAPI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
	})
	fakeAPI(t, mux)
	// No token: if the command reached apiClient it would fail with "no token".
	t.Setenv("ONETT_TOKEN", "")
	t.Setenv("ONETT_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))

	for _, flag := range []string{"-h", "--help"} {
		code, out, errOut := runCLI("project", "pause", flag)
		assert.Equal(t, 0, code, errOut)
		assert.Contains(t, out, "usage: onett project")
		assert.NotContains(t, errOut, "no token")
	}
}

func TestProjectVerbRejectsDashName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
	})
	fakeAPI(t, mux)

	for _, verb := range []string{"pause", "resume"} {
		code, out, errOut := runCLI("project", verb, "-x")
		assert.Equal(t, 2, code)
		assert.Empty(t, out)
		assert.Contains(t, errOut, "usage: onett project "+verb+" NAME")
	}
}

func TestLoopRunRejectsDashName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
	})
	fakeAPI(t, mux)

	code, _, errOut := runCLI("loop", "run", "--", "-x")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "usage: onett loop run NAME")
}
