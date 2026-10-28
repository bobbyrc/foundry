package deploy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckHealth(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ok.Close()
	require.NoError(t, CheckHealth(context.Background(), ok.URL, time.Second))

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	err := CheckHealth(context.Background(), bad.URL, time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
	assert.Contains(t, err.Error(), bad.URL)

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()
	err = CheckHealth(context.Background(), slow.URL, 100*time.Millisecond)
	require.Error(t, err)
}
