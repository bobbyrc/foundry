package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/domain"
)

func TestSuperviseStallBlocksTask(t *testing.T) {
	env := rtNewEngine(t, rtOpts{}) // stall_timeout is 2s
	start := time.Now()
	task := env.create(domain.Task{Body: "[fake:stall]"})

	got := env.waitStatus(task.ID, domain.TaskBlocked)
	elapsed := time.Since(start)
	assert.Contains(t, got.BlockedReason, "stalled: no output for 2s")
	assert.Less(t, elapsed, 6*time.Second, "stall detected near stall_timeout, took %s", elapsed)

	runs := env.runs(task.ID)
	require.Len(t, runs, 1)
	assert.Equal(t, domain.RunStalled, runs[0].Status)
	assert.Contains(t, runs[0].Error, "stalled")
}
