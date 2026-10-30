package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientSendsRunIDHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(RunIDHeader)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	t.Setenv("ONETT_RUN_ID", "run-9")
	_, err := New(srv.URL, "tok").AgentConversations(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "run-9", got)

	t.Setenv("ONETT_RUN_ID", "")
	_, err = New(srv.URL, "tok").AgentConversations(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}
