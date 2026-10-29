package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/store/sqlite"
)

func TestWebManifestContentType(t *testing.T) {
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	web := fstest.MapFS{
		"index.html":           {Data: []byte("<html>app</html>")},
		"manifest.webmanifest": {Data: []byte(`{"name":"onett"}`)},
	}
	h := NewServer(Options{Store: st, Bus: events.NewBus(st, nil), Controller: &fakeCtl{cfg: testConfig()}, Web: web})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, testRequest(http.MethodGet, "/manifest.webmanifest", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/manifest+json", rec.Header().Get("Content-Type"))
	assert.Equal(t, `{"name":"onett"}`, rec.Body.String())
}
