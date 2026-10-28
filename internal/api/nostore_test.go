package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A browser that restores tabs after a restart may replay a stored API answer. A replayed
// GET /auth/state from before the owner account existed showed the setup page again, so no API
// response may be stored.
func TestAPIResponsesAreNeverStored(t *testing.T) {
	x := newHarness(t)
	for _, path := range []string{"/api/v1/auth/state", "/api/v1/health", "/api/v1/stats", "/api/v1/nope"} {
		rec := httptest.NewRecorder()
		x.h.ServeHTTP(rec, testRequest(http.MethodGet, path, nil))
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"), path)
	}
}
