package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/harness"
	hfake "github.com/bobbyrc/onett/internal/harness/fake"
)

func TestRunLogsStartWithPrompt(t *testing.T) {
	t.Run("task run", func(t *testing.T) {
		prompts := make(chan string, 4)
		env := rtNewEngine(t, rtOpts{script: func(ctx context.Context, spec harness.Spec, emit func(harness.Event)) (harness.Result, error) {
			select {
			case prompts <- spec.Prompt:
			default:
			}
			return hfake.DefaultScript(ctx, spec, emit)
		}})
		task := env.create(domain.Task{Title: "Add greeting", Body: "[fake:commit]"})
		env.waitStatus(task.ID, domain.TaskReview)

		prompt := <-prompts
		runs := env.runs(task.ID)
		require.NotEmpty(t, runs)
		logs, err := env.st.ListRunLogs(env.ctx, runs[0].ID, 0, 100)
		require.NoError(t, err)
		require.NotEmpty(t, logs)
		assert.Equal(t, domain.LogPrompt, logs[0].Kind)
		assert.Equal(t, prompt, logs[0].Text)
	})

	t.Run("chat turn", func(t *testing.T) {
		ctx := context.Background()
		r := ctNewRig(t)
		e := r.engine(nil)
		prompts := make(chan string, 1)
		e.d.HarnessFor = func(string, config.Harness) (harness.Harness, error) {
			return hfake.New(func(_ context.Context, spec harness.Spec, _ func(harness.Event)) (harness.Result, error) {
				prompts <- spec.Prompt
				return harness.Result{SessionID: "s", FinalText: "ok"}, nil
			}), nil
		}
		conv, err := r.st.CreateConversation(ctx, domain.Conversation{Project: "demo"})
		require.NoError(t, err)
		_, err = e.SendChat(ctx, conv.ID, "list the tasks")
		require.NoError(t, err)
		ctWaitIdle(t, r, conv.ID)

		msgs, err := r.st.ListChatMessages(ctx, conv.ID)
		require.NoError(t, err)
		require.Len(t, msgs, 2)
		logs, err := r.st.ListRunLogs(ctx, msgs[1].RunID, 0, 100)
		require.NoError(t, err)
		require.NotEmpty(t, logs)
		assert.Equal(t, domain.LogPrompt, logs[0].Kind)
		assert.Equal(t, <-prompts, logs[0].Text)
	})
}
