package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
	"github.com/bobbyrc/onett/internal/store"
)

func TestRetryAfterCancelDoesNotResumeOrDeliver(t *testing.T) {
	env, rec, _, _ := fuEnv(t)
	task := env.create(domain.Task{Body: "[fake:block]"})
	env.waitStatus(task.ID, domain.TaskBlocked)
	fuWaitRuns(t, env, task.ID, 1)

	// Pause so the follow-up the message sets up does not start before the owner cancels.
	require.NoError(t, env.e.SetPaused(env.ctx, true))
	_, err := env.e.SendTaskMessage(env.ctx, task.ID, "carry on")
	require.NoError(t, err)
	got := env.task(task.ID)
	require.Equal(t, domain.TaskQueued, got.Status)
	require.True(t, got.FollowUpPending)

	require.NoError(t, env.e.CancelTask(env.ctx, task.ID, "owner:alice"))
	assert.False(t, env.task(task.ID).FollowUpPending, "cancel clears the pending follow-up")
	require.NoError(t, env.e.RetryTask(env.ctx, task.ID, ""))
	assert.False(t, env.task(task.ID).FollowUpPending)
	require.NoError(t, env.e.SetPaused(env.ctx, false))

	fuWaitRuns(t, env, task.ID, 2)
	specs := rec.all()
	require.Len(t, specs, 2)
	assert.Empty(t, specs[1].ResumeSessionID, "a retry never resumes a session")
	assert.NotContains(t, specs[1].Prompt, "carry on")
	msgs := env.messages(task.ID)
	require.Len(t, msgs, 1)
	assert.True(t, msgs[0].Queued, "the message is still waiting")
}

func TestFirstRunSkipsClosedConversationQueue(t *testing.T) {
	env, rec, _, _ := fuEnv(t)
	require.NoError(t, env.e.SetPaused(env.ctx, true))
	task := env.create(domain.Task{Body: "[fake:block]"})
	_, err := env.e.SendTaskMessage(env.ctx, task.ID, "early note")
	require.NoError(t, err)
	require.NoError(t, env.e.CloseTaskConversation(env.ctx, task.ID, "owner:alice", "done talking"))
	require.NoError(t, env.e.SetPaused(env.ctx, false))

	fuWaitRuns(t, env, task.ID, 1)
	specs := rec.all()
	require.Len(t, specs, 1)
	assert.NotContains(t, specs[0].Prompt, "early note")
	assert.True(t, env.messages(task.ID)[0].Queued)
}

func TestChatRejectsTaskConversation(t *testing.T) {
	env, _, _, _ := fuEnv(t)
	conv, err := env.st.CreateConversation(env.ctx, domain.Conversation{TaskID: "some-task", Title: "t"})
	require.NoError(t, err)
	_, err = env.e.SendChat(env.ctx, conv.ID, "hello")
	assert.ErrorIs(t, err, store.ErrNotFound)
	assert.ErrorIs(t, env.e.FlushChat(env.ctx, conv.ID), store.ErrNotFound)
}

func TestFailedFollowUpBlocksAndRestoresMessages(t *testing.T) {
	env, rec, _, _ := fuEnv(t)
	task := env.create(domain.Task{Body: "[fake:block]"})
	env.waitStatus(task.ID, domain.TaskBlocked)
	fuWaitRuns(t, env, task.ID, 1)

	_, err := env.e.SendTaskMessage(env.ctx, task.ID, "now do it [fake:fail]")
	require.NoError(t, err)
	fuWaitRuns(t, env, task.ID, 2)
	got := env.waitStatus(task.ID, domain.TaskBlocked)

	assert.True(t, strings.HasPrefix(got.BlockedReason, "follow-up failed: "), got.BlockedReason)
	time.Sleep(300 * time.Millisecond) // a wrongly queued retry would start by now
	assert.Len(t, env.runs(task.ID), 2, "no automatic retry run starts")
	assert.Len(t, rec.all(), 2)
	msgs := env.messages(task.ID)
	require.Len(t, msgs, 1)
	assert.True(t, msgs[0].Queued, "the message shows as not delivered")
	assert.Empty(t, msgs[0].RunID)
}

// blockedFinishedScript reports blocked with finished set.
func blockedFinishedScript(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
	res, err := hfake.DefaultScript(ctx, spec, emit)
	if err != nil {
		return res, err
	}
	rr := domain.RunResult{Status: domain.ResultBlocked, BlockedReason: "need input", Summary: "stuck", Finished: true}
	data, _ := json.Marshal(rr)
	err = os.WriteFile(filepath.Join(spec.Workdir, domain.OutboxDir, domain.ResultFile), data, 0o644)
	return res, err
}

func TestFinishedIgnoredOnBlockedReport(t *testing.T) {
	env := rtNewEngine(t, rtOpts{script: blockedFinishedScript})
	task := env.create(domain.Task{Body: "ask me"})
	env.waitStatus(task.ID, domain.TaskBlocked)
	time.Sleep(200 * time.Millisecond)
	c, err := env.st.GetConversationByTask(env.ctx, task.ID)
	if err == nil {
		assert.Nil(t, c.ClosedAt, "a blocked report never closes the conversation")
	} else {
		assert.ErrorIs(t, err, store.ErrNotFound)
	}
}
