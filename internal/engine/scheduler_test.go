package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/foundry/internal/domain"
)

// Two stalling tasks must never run at the same time under either limit: the per-project limit
// (same project) or the total limit (different projects).
func TestSchedulerLimitsConcurrentRuns(t *testing.T) {
	cases := []struct {
		name          string
		projects      []string
		maxConcurrent int
	}{
		{name: "per project", projects: []string{"demo"}, maxConcurrent: 3},
		{name: "total", projects: []string{"demo", "other"}, maxConcurrent: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			env := rtNewEngine(t, rtOpts{projects: c.projects, maxConcurrent: c.maxConcurrent, maxPerProject: 1})
			var ids []string
			for _, p := range c.projects {
				ids = append(ids, env.create(domain.Task{Project: p, Body: "[fake:stall]"}).ID)
			}

			peak := 0
			deadline := time.Now().Add(20 * time.Second)
			for {
				if _, total := env.e.activeTaskRuns(); total > peak {
					peak = total
				}
				blocked := 0
				for _, id := range ids {
					if env.task(id).Status == domain.TaskBlocked {
						blocked++
					}
				}
				if blocked == len(ids) {
					break
				}
				require.True(t, time.Now().Before(deadline), "tasks did not all block in time")
				time.Sleep(10 * time.Millisecond)
			}
			assert.Equal(t, 1, peak, "at most one run was live at a time")
		})
	}
}
