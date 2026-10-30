package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/api"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

// fuSpecs records the spec of every run the fake harness starts.
type fuSpecs struct {
	mu    sync.Mutex
	specs []harness.Spec
}

func (s *fuSpecs) add(spec harness.Spec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.specs = append(s.specs, spec)
}

func (s *fuSpecs) all() []harness.Spec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]harness.Spec(nil), s.specs...)
}

// fuScript records each spec, then behaves like the default fake. A prompt with HOLD waits for
// release first; with DENYLATER it then reports a denied tool call instead.
func fuScript(rec *fuSpecs, entered chan<- struct{}, release <-chan struct{}) hfake.Script {
	return func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
		rec.add(spec)
		if strings.Contains(spec.Prompt, "HOLD") {
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			if strings.Contains(spec.Prompt, "DENYLATER") {
				spec.Prompt = "[fake:deny]"
			}
		}
		return hfake.DefaultScript(ctx, spec, emit)
	}
}

func fuEnv(t *testing.T) (*rtEnv, *fuSpecs, chan struct{}, chan struct{}) {
	t.Helper()
	rec := &fuSpecs{}
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	env := rtNewEngine(t, rtOpts{script: fuScript(rec, entered, release)})
	return env, rec, entered, release
}

func (env *rtEnv) messages(taskID string) []domain.ChatMessage {
	env.t.Helper()
	conv, err := env.st.GetConversationByTask(env.ctx, taskID)
	require.NoError(env.t, err)
	msgs, err := env.st.ListChatMessages(env.ctx, conv.ID)
	require.NoError(env.t, err)
	return msgs
}

func fuWaitRuns(t *testing.T, env *rtEnv, taskID string, n int) []domain.Run {
	t.Helper()
	var runs []domain.Run
	rtWait(t, 20*time.Second, "runs to finish", func() bool {
		runs = env.runs(taskID)
		env.e.runsMu.Lock()
		live := len(env.e.active)
		env.e.runsMu.Unlock()
		if len(runs) != n || live != 0 {
			return false
		}
		for _, r := range runs {
			if r.EndedAt == nil {
				return false
			}
		}
		return true
	})
	return runs
}

func TestMessageToBlockedTaskResumesSession(t *testing.T) {
	env, rec, _, _ := fuEnv(t)
	task := env.create(domain.Task{Body: "[fake:block]"})
	env.waitStatus(task.ID, domain.TaskBlocked)
	first := fuWaitRuns(t, env, task.ID, 1)[0]
	require.NotEmpty(t, first.SessionID)

	msg, err := env.e.SendTaskMessage(env.ctx, task.ID, "carry on")
	require.NoError(t, err)
	assert.True(t, msg.Queued, "stored queued until the follow-up run takes it")

	runs := fuWaitRuns(t, env, task.ID, 2)
	specs := rec.all()
	require.Len(t, specs, 2)
	assert.Equal(t, first.SessionID, specs[1].ResumeSessionID)
	assert.Equal(t, ownerPrompt([]string{"carry on"}), specs[1].Prompt)

	msgs := env.messages(task.ID)
	require.Len(t, msgs, 1)
	assert.False(t, msgs[0].Queued)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, runs[0].ID, msgs[0].RunID, "the message names the run that took it (newest first)")
}

func TestMessageDuringNonSteerableRunQueuesThenFollowsUp(t *testing.T) {
	env, rec, entered, release := fuEnv(t)
	task := env.create(domain.Task{Body: "HOLD [fake:commit]"})
	<-entered

	msg, err := env.e.SendTaskMessage(env.ctx, task.ID, "also add tests")
	require.NoError(t, err)
	assert.True(t, msg.Queued)
	assert.Empty(t, msg.RunID)
	assert.Len(t, rec.all(), 1, "nothing is delivered into a run that takes no input")

	close(release)
	runs := fuWaitRuns(t, env, task.ID, 2)
	specs := rec.all()
	require.Len(t, specs, 2)
	assert.Equal(t, ownerPrompt([]string{"also add tests"}), specs[1].Prompt)
	assert.Equal(t, runs[1].SessionID, specs[1].ResumeSessionID, "resumes the run that just ended")
	env.waitStatus(task.ID, domain.TaskReview)
	msgs := env.messages(task.ID)
	require.Len(t, msgs, 1)
	assert.False(t, msgs[0].Queued)
	assert.Equal(t, runs[0].ID, msgs[0].RunID)
}

func TestQueuedMessageNotDeliveredAfterDenial(t *testing.T) {
	env, rec, entered, release := fuEnv(t)
	task := env.create(domain.Task{Body: "HOLD DENYLATER"})
	<-entered
	_, err := env.e.SendTaskMessage(env.ctx, task.ID, "while you work")
	require.NoError(t, err)

	close(release)
	env.waitStatus(task.ID, domain.TaskBlocked)
	fuWaitRuns(t, env, task.ID, 1)
	time.Sleep(300 * time.Millisecond) // a wrongly triggered follow-up would start by now
	assert.Len(t, env.runs(task.ID), 1, "a denial never delivers queued messages")
	assert.Len(t, rec.all(), 1)
	assert.Equal(t, domain.TaskBlocked, env.task(task.ID).Status)
	require.True(t, env.messages(task.ID)[0].Queued)

	require.NoError(t, env.e.FlushTaskMessages(env.ctx, task.ID))
	fuWaitRuns(t, env, task.ID, 2)
	specs := rec.all()
	require.Len(t, specs, 2)
	assert.Equal(t, ownerPrompt([]string{"while you work"}), specs[1].Prompt)
	assert.False(t, env.messages(task.ID)[0].Queued)
}

func TestMessageAfterMergeCreatesConversationChild(t *testing.T) {
	env, rec, _, _ := fuEnv(t)
	task := env.create(domain.Task{Body: "[fake:commit]"})
	env.waitStatus(task.ID, domain.TaskReview)
	rootRun := fuWaitRuns(t, env, task.ID, 1)[0]
	merged, err := env.st.UpdateTask(env.ctx, task.ID, func(x *domain.Task) error {
		x.Status, x.MergeSHA = domain.TaskMerged, "abc123"
		return nil
	})
	require.NoError(t, err)

	_, err = env.e.SendTaskMessage(env.ctx, task.ID, "one more thing [fake:commit]")
	require.NoError(t, err)

	var child domain.Task
	rtWait(t, 20*time.Second, "a conversation child", func() bool {
		kids := rtChildren(env, task.ID)
		if len(kids) != 1 {
			return false
		}
		child = kids[0]
		return true
	})
	assert.Equal(t, domain.SourceConversation, child.Source)
	assert.Equal(t, task.ID, child.ParentID)
	assert.Equal(t, "Follow-up: "+task.Title, child.Title)
	assert.Equal(t, merged.Body, child.Body)
	assert.Equal(t, merged.Role, child.Role)
	assert.Equal(t, merged.Kind, child.Kind)

	got := env.waitStatus(child.ID, domain.TaskReview)
	assert.NotEqual(t, merged.Branch, got.Branch, "a follow-up gets its own branch")
	assert.NotEmpty(t, got.Branch)
	specs := rec.all()
	require.Len(t, specs, 2)
	assert.Equal(t, rootRun.SessionID, specs[1].ResumeSessionID)
	assert.Equal(t, ownerPrompt([]string{"one more thing [fake:commit]"}), specs[1].Prompt)
}

func TestMessageToQuestionTaskDoneWithoutPRRequeues(t *testing.T) {
	env, rec, _, _ := fuEnv(t)
	task := env.create(domain.Task{Kind: domain.KindQuestion, Body: "what is the answer"})
	env.waitStatus(task.ID, domain.TaskDone)
	first := fuWaitRuns(t, env, task.ID, 1)[0]

	_, err := env.e.SendTaskMessage(env.ctx, task.ID, "and why")
	require.NoError(t, err)

	runs := fuWaitRuns(t, env, task.ID, 2)
	assert.Empty(t, rtChildren(env, task.ID), "no PR, so the same task runs again")
	specs := rec.all()
	require.Len(t, specs, 2)
	assert.Equal(t, first.SessionID, specs[1].ResumeSessionID)
	assert.Equal(t, ownerPrompt([]string{"and why"}), specs[1].Prompt)
	assert.NotEmpty(t, runs)
	env.waitStatus(task.ID, domain.TaskDone)
}

func TestMessageToNeverRunTaskJoinsFirstPrompt(t *testing.T) {
	env, rec, _, _ := fuEnv(t)
	require.NoError(t, env.e.SetPaused(env.ctx, true))
	task := env.create(domain.Task{Body: "make a thing"})

	msg, err := env.e.SendTaskMessage(env.ctx, task.ID, "use tabs")
	require.NoError(t, err)
	assert.True(t, msg.Queued)
	assert.Empty(t, rec.all())

	require.NoError(t, env.e.SetPaused(env.ctx, false))
	runs := fuWaitRuns(t, env, task.ID, 1)
	specs := rec.all()
	require.Len(t, specs, 1)
	assert.Empty(t, specs[0].ResumeSessionID)
	assert.Contains(t, specs[0].Prompt, "make a thing")
	assert.Contains(t, specs[0].Prompt, "\n\n"+ownerPrompt([]string{"use tabs"}))
	msgs := env.messages(task.ID)
	require.Len(t, msgs, 1)
	assert.False(t, msgs[0].Queued)
	assert.Equal(t, runs[0].ID, msgs[0].RunID)
}

func TestSendTaskMessageRefusals(t *testing.T) {
	env, _, _, _ := fuEnv(t)
	task := env.create(domain.Task{Body: "just answer"})
	env.waitStatus(task.ID, domain.TaskDone)
	fuWaitRuns(t, env, task.ID, 1)
	// Strip the session: the agent has nothing to continue.
	runs := env.runs(task.ID)
	_, err := env.st.UpdateRun(env.ctx, runs[0].ID, func(r *domain.Run) error {
		r.SessionID = ""
		return nil
	})
	require.NoError(t, err)

	_, err = env.e.SendTaskMessage(env.ctx, task.ID, "hello")
	require.ErrorIs(t, err, api.ErrConflict)
	assert.Contains(t, err.Error(), "This agent has no session to continue. Retry the task instead.")

	_, err = env.e.SendTaskMessage(env.ctx, task.ID, "  ")
	assert.ErrorIs(t, err, api.ErrInvalid)
}
