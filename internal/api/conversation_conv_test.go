package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

func (f *fakeCtl) TaskConversation(_ context.Context, id string) (domain.TaskConversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id == "missing" {
		return domain.TaskConversation{}, store.ErrNotFound
	}
	return domain.TaskConversation{Task: domain.Task{ID: id}, CanSend: true}, nil
}

func (f *fakeCtl) AgentConversations(context.Context) ([]domain.AgentConversation, error) {
	return nil, nil
}

func TestCreateTaskRecordsCreatorRun(t *testing.T) {
	x := newHarness(t)
	body := `{"project":"demo","title":"t","body":"b"}`

	rec := x.authReq(t, http.MethodPost, "/api/v1/tasks", body,
		withToken(testToken), withHeader(RunIDHeader, "run-1"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "run-1", x.ctl.created.CreatedByRun, "the API token may name its run")

	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks", body, withToken(testToken))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Empty(t, x.ctl.created.CreatedByRun)

	c := x.newUserSession(t, "owner", domain.RoleOwner)
	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks", body,
		withCookie(c), withHeader(RunIDHeader, "run-2"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Empty(t, x.ctl.created.CreatedByRun, "a signed-in user's header is ignored")
}

func TestTaskConversationRoutes(t *testing.T) {
	x := newHarness(t)

	rec := x.call(t, http.MethodGet, "/api/v1/tasks/t1/conversation", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var tc domain.TaskConversation
	decode(t, rec, &tc)
	assert.Equal(t, "t1", tc.Task.ID)
	assert.True(t, tc.CanSend)

	rec = x.call(t, http.MethodGet, "/api/v1/tasks/missing/conversation", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = x.call(t, http.MethodGet, "/api/v1/agent-conversations", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `[]`, rec.Body.String())

	viewer := x.newUserSession(t, "watcher", domain.RoleViewer)
	rec = x.authReq(t, http.MethodGet, "/api/v1/agent-conversations", "", withCookie(viewer))
	assert.Equal(t, http.StatusOK, rec.Code, "reading needs only the read action")
}
