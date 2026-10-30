package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestParseToolIDs(t *testing.T) {
	events, _ := replay(t, "testdata/tool_ids.jsonl")
	var use, result *string
	for _, ev := range events {
		switch ev.Kind {
		case domain.LogToolUse:
			id := ev.ToolID
			use = &id
		case domain.LogToolResult:
			id := ev.ToolID
			result = &id
		}
	}
	require.NotNil(t, use, "a tool_use event")
	require.NotNil(t, result, "a tool_result event")
	assert.Equal(t, "toolu_A", *use)
	assert.Equal(t, "toolu_A", *result)
}
