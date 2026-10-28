package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/events"
	"github.com/bobbyrc/foundry/internal/store/sqlite"
)

func TestSecurityHeadersOnAPIAndSPA(t *testing.T) {
	x := newHarness(t)
	web := fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}}
	spa := NewServer(Options{Store: x.st, Bus: x.bus, Controller: x.ctl, Token: testToken, Web: web})

	for name, h := range map[string]http.Handler{"api": x.h, "spa": spa} {
		req := testRequest(http.MethodGet, "/", nil)
		if name == "api" {
			req = testRequest(http.MethodGet, "/api/v1/health", nil)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, contentSecurityPolicy, rec.Header().Get("Content-Security-Policy"), name)
		assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"), name)
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), name)
		assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"), name)
	}
}

func TestSecurityHeadersOnRejectedHost(t *testing.T) {
	x := newHarness(t)
	req := testRequest(http.MethodGet, "/api/v1/health", nil)
	req.Host = "evil.example"
	rec := httptest.NewRecorder()
	x.h.ServeHTTP(rec, req)
	assert.Equal(t, contentSecurityPolicy, rec.Header().Get("Content-Security-Policy"))
}

func TestCSPAllowsBuiltUIWithoutInlineScripts(t *testing.T) {
	// The built index.html must reference its script and stylesheet by URL only.
	body, err := fs.ReadFile(DefaultWeb(), "index.html")
	if err != nil {
		t.Skipf("web UI not built (run npm run build in web/): %v", err)
	}
	assert.Contains(t, string(body), `<script type="module" crossorigin src="/assets/`)
	assert.NotContains(t, string(body), "<script>")
	assert.NotContains(t, string(body), "<style")
}

func TestHostGuard(t *testing.T) {
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	h := NewServer(Options{
		Store:        st,
		Bus:          events.NewBus(st, nil),
		Controller:   &fakeCtl{cfg: testConfig()},
		Token:        testToken,
		AllowedHosts: []string{"foundry.lan:7777", " Extra.Example "},
		Web:          fstest.MapFS{},
	})

	cases := []struct {
		host   string
		status int
	}{
		{"localhost:7777", http.StatusOK},
		{"localhost", http.StatusOK},
		{"127.0.0.1", http.StatusOK},
		{"127.0.0.1:7777", http.StatusOK},
		{"[::1]:7777", http.StatusOK},
		{"[::1]", http.StatusOK},
		{"foundry.lan", http.StatusOK},
		{"extra.example:8080", http.StatusOK},
		{"evil.example", http.StatusMisdirectedRequest},
		{"evil.example:7777", http.StatusMisdirectedRequest},
		{"127.0.0.1.evil.example", http.StatusMisdirectedRequest},
		{"", http.StatusMisdirectedRequest},
	}
	for _, c := range cases {
		req := testRequest(http.MethodGet, "/api/v1/health", nil)
		req.Host = c.host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, c.status, rec.Code, "host %q", c.host)
		if c.status == http.StatusMisdirectedRequest {
			assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"), "host %q", c.host)
		}
	}
}

func TestClampLimits(t *testing.T) {
	var seen, seenProject string
	h := clampLimits(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query().Get("limit")
		seenProject = r.URL.Query().Get("project")
	}))
	cases := []struct {
		target string
		want   string
	}{
		{"/api/v1/tasks?limit=5000&project=p", "1000"},
		{"/api/v1/tasks?limit=1000", "1000"},
		{"/api/v1/tasks?limit=999&project=p", "999"},
		{"/api/v1/tasks?limit=abc&project=p", "abc"},
		{"/api/v1/tasks?limit=-5&project=p", "-5"},
		{"/api/v1/runs/r1/logs?limit=6000&project=p", "5000"},
		{"/api/v1/runs/r1/logs?limit=2000&project=p", "2000"},
		{"/api/v1/runs/r1/logs/x?limit=2000&project=p", "1000"},
	}
	for _, c := range cases {
		h.ServeHTTP(httptest.NewRecorder(), testRequest(http.MethodGet, c.target, nil))
		assert.Equal(t, c.want, seen, c.target)
		if strings.Contains(c.target, "project=p") {
			assert.Equal(t, "p", seenProject, "other parameters are kept: %s", c.target)
		}
	}
}
