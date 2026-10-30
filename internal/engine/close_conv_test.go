package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

// finishedScript reports no_changes with finished set.
func finishedScript(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
	res, err := hfake.DefaultScript(ctx, spec, emit)
	if err != nil {
		return res, err
	}
	rr := domain.RunResult{Status: domain.ResultNoChanges, Summary: "Answered the question.", Finished: true}
	data, _ := json.Marshal(rr)
	err = os.WriteFile(filepath.Join(spec.Workdir, domain.OutboxDir, domain.ResultFile), data, 0o644)
	return res, err
}

func TestFinishedClosesConversation(t *testing.T) {
	env := rtNewEngine(t, rtOpts{script: finishedScript})
	task := env.create(domain.Task{Body: "answer me"})
	got := env.waitStatus(task.ID, domain.TaskDone)
	assert.Equal(t, domain.TaskDone, got.Status, "finished does not change the status the run decided")

	rtWait(t, 10*time.Second, "conversation closed", func() bool {
		c, err := env.st.GetConversationByTask(env.ctx, task.ID)
		return err == nil && c.ClosedAt != nil
	})
	c, err := env.st.GetConversationByTask(env.ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "agent", c.ClosedBy)
	assert.Equal(t, "Answered the question.", c.ClosedReason)
}

func TestCancelTaskClosesConversationAndKeepsQueue(t *testing.T) {
	env, rec, entered, release := fuEnv(t)
	task := env.create(domain.Task{Body: "HOLD"})
	<-entered
	_, err := env.e.SendTaskMessage(env.ctx, task.ID, "later")
	require.NoError(t, err)

	require.NoError(t, env.e.CancelTask(env.ctx, task.ID, "owner:alice"))
	close(release)
	env.waitStatus(task.ID, domain.TaskCancelled)

	c, err := env.st.GetConversationByTask(env.ctx, task.ID)
	require.NoError(t, err)
	require.NotNil(t, c.ClosedAt)
	assert.Equal(t, "owner:alice", c.ClosedBy)

	time.Sleep(300 * time.Millisecond)
	msgs := env.messages(task.ID)
	require.Len(t, msgs, 1)
	assert.True(t, msgs[0].Queued, "a queued message stays queued")
	assert.Empty(t, msgs[0].RunID)
	assert.Len(t, rec.all(), 1, "nothing was delivered")
	assert.Len(t, env.runs(task.ID), 1)
}

func TestCloseTwiceConflicts(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Body: "x"})
	env.waitStatus(task.ID, domain.TaskDone)

	require.NoError(t, env.e.CloseTaskConversation(env.ctx, task.ID, "orchestrator", "merged"))
	err := env.e.CloseTaskConversation(env.ctx, task.ID, "orchestrator", "again")
	assert.ErrorIs(t, err, api.ErrConflict)
}

func TestReopenClearsClosedOnly(t *testing.T) {
	env := rtNewEngine(t, rtOpts{})
	task := env.create(domain.Task{Body: "x"})
	done := env.waitStatus(task.ID, domain.TaskDone)

	assert.ErrorIs(t, env.e.ReopenTaskConversation(env.ctx, task.ID), api.ErrConflict, "open or absent")
	require.NoError(t, env.e.CloseTaskConversation(env.ctx, task.ID, "orchestrator", "done"))
	before, err := env.st.GetConversationByTask(env.ctx, task.ID)
	require.NoError(t, err)

	require.NoError(t, env.e.ReopenTaskConversation(env.ctx, task.ID))
	after, err := env.st.GetConversationByTask(env.ctx, task.ID)
	require.NoError(t, err)
	assert.Nil(t, after.ClosedAt)
	assert.Empty(t, after.ClosedBy)
	assert.Empty(t, after.ClosedReason)
	assert.Equal(t, before.ID, after.ID)
	assert.Equal(t, before.Title, after.Title)
	assert.Equal(t, done.Status, env.task(task.ID).Status, "the task is untouched")
}
