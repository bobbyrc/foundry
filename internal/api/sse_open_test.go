package api

import (
	"bufio"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Firefox fires EventSource "open" only once body bytes arrive, so a new stream must write
// something straight away rather than leave the UI showing "reconnecting…" until the first ping.
func TestSSEWritesAtOnceOnOpen(t *testing.T) {
	x := newHarness(t)
	resp := openStream(t, x, "/api/v1/events", testToken)
	require.Equal(t, 200, resp.StatusCode)

	line := make(chan string, 1)
	go func() {
		s, _ := bufio.NewReader(resp.Body).ReadString('\n')
		line <- s
	}()
	select {
	case s := <-line:
		require.Equal(t, ": connected\n", s)
	case <-time.After(2 * time.Second):
		t.Fatalf("no body bytes within 2s (the first ping is %s away)", pingInterval)
	}
}
