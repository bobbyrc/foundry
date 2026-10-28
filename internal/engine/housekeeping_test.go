package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/store"
)

func TestHousekeepDeletesRowsOutsideRetention(t *testing.T) {
	env := lpNewEngine(t, `server:
  retain_events: 24h
  retain_run_logs: 72h
`)
	now := env.now
	_, err := env.st.AppendEvent(env.ctx, domain.Event{Type: domain.EvTaskCreated, Project: "demo", Message: "old", Time: now.Add(-48 * time.Hour)})
	require.NoError(t, err)
	_, err = env.st.AppendEvent(env.ctx, domain.Event{Type: domain.EvTaskCreated, Project: "demo", Message: "new", Time: now.Add(-time.Hour)})
	require.NoError(t, err)
	_, err = env.st.AppendRunLogs(env.ctx, "run-old", []domain.LogLine{{Kind: domain.LogText, Text: "old", Time: now.Add(-100 * time.Hour)}})
	require.NoError(t, err)
	_, err = env.st.AppendRunLogs(env.ctx, "run-new", []domain.LogLine{{Kind: domain.LogText, Text: "new", Time: now.Add(-10 * time.Hour)}})
	require.NoError(t, err)

	env.e.housekeep(env.ctx)

	events, err := env.st.ListEvents(env.ctx, store.EventFilter{Limit: 100})
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "new", events[0].Message)

	old, err := env.st.ListRunLogs(env.ctx, "run-old", 0, 100)
	require.NoError(t, err)
	assert.Empty(t, old)
	fresh, err := env.st.ListRunLogs(env.ctx, "run-new", 0, 100)
	require.NoError(t, err)
	assert.Len(t, fresh, 1)
}

func TestHousekeepUsesDefaultWindowsWhenUnset(t *testing.T) {
	env := lpNewEngine(t, "")
	now := env.now
	// 40 days is past the 30-day event default but inside the 90-day run log default.
	_, err := env.st.AppendEvent(env.ctx, domain.Event{Type: domain.EvTaskCreated, Project: "demo", Message: "old", Time: now.Add(-40 * 24 * time.Hour)})
	require.NoError(t, err)
	_, err = env.st.AppendRunLogs(env.ctx, "run", []domain.LogLine{{Kind: domain.LogText, Text: "kept", Time: now.Add(-40 * 24 * time.Hour)}})
	require.NoError(t, err)

	env.e.housekeep(env.ctx)

	events, err := env.st.ListEvents(env.ctx, store.EventFilter{Limit: 100})
	require.NoError(t, err)
	assert.Empty(t, events)
	logs, err := env.st.ListRunLogs(env.ctx, "run", 0, 100)
	require.NoError(t, err)
	assert.Len(t, logs, 1)
}
