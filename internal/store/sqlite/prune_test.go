package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/store"
)

func TestPruneEventsDeletesOnlyOlderThanCutoff(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-30 * 24 * time.Hour)

	for _, at := range []time.Time{now.Add(-60 * 24 * time.Hour), cutoff.Add(-time.Second), cutoff.Add(time.Second), now} {
		_, err := s.AppendEvent(ctx, domain.Event{Type: domain.EvTaskCreated, Project: "p", Message: "m", Time: at})
		require.NoError(t, err)
	}

	n, err := s.PruneEvents(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n, "the two events before the cutoff are deleted")

	left, err := s.ListEvents(ctx, store.EventFilter{Limit: 100})
	require.NoError(t, err)
	require.Len(t, left, 2)
	assert.True(t, left[0].Time.Equal(cutoff.Add(time.Second)))
	assert.True(t, left[1].Time.Equal(now))

	n, err = s.PruneEvents(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "a second prune with the same cutoff deletes nothing")
}

func TestPruneRunLogsDeletesOnlyOlderThanCutoff(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-90 * 24 * time.Hour)

	_, err := s.AppendRunLogs(ctx, "run-old", []domain.LogLine{
		{Kind: domain.LogText, Text: "a", Time: cutoff.Add(-time.Hour)},
		{Kind: domain.LogText, Text: "b", Time: cutoff.Add(-time.Minute)},
	})
	require.NoError(t, err)
	_, err = s.AppendRunLogs(ctx, "run-mixed", []domain.LogLine{
		{Kind: domain.LogText, Text: "c", Time: cutoff.Add(-time.Hour)},
		{Kind: domain.LogText, Text: "d", Time: now},
	})
	require.NoError(t, err)
	_, err = s.AppendRunLogs(ctx, "run-new", []domain.LogLine{
		{Kind: domain.LogText, Text: "e", Time: now.Add(-time.Hour)},
	})
	require.NoError(t, err)

	n, err := s.PruneRunLogs(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)

	old, err := s.ListRunLogs(ctx, "run-old", 0, 100)
	require.NoError(t, err)
	assert.Empty(t, old)

	mixed, err := s.ListRunLogs(ctx, "run-mixed", 0, 100)
	require.NoError(t, err)
	require.Len(t, mixed, 1)
	assert.Equal(t, "d", mixed[0].Text)

	fresh, err := s.ListRunLogs(ctx, "run-new", 0, 100)
	require.NoError(t, err)
	assert.Len(t, fresh, 1, "a recent run keeps its lines")
}

func TestPruneOnEmptyStoreDeletesNothing(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	n, err := s.PruneEvents(ctx, time.Now())
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
	n, err = s.PruneRunLogs(ctx, time.Now())
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}
