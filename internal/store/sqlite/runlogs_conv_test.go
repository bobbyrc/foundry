package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestRunLogToolIDRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	_, err := s.AppendRunLogs(ctx, "r1", []domain.LogLine{
		{Kind: domain.LogToolUse, Text: "Bash: ls", ToolID: "toolu_1"},
		{Kind: domain.LogText, Text: "hi"},
	})
	require.NoError(t, err)

	got, err := s.ListRunLogs(ctx, "r1", 0, 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "toolu_1", got[0].ToolID)
	assert.Equal(t, "", got[1].ToolID)
}
