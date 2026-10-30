package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func (f *fakeCtl) CloseTaskConversation(_ context.Context, taskID, by, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatConvID, f.chatText = taskID, by+"|"+reason
	return f.note("CloseTaskConversation")
}

func (f *fakeCtl) ReopenTaskConversation(_ context.Context, taskID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatConvID = taskID
	return f.note("ReopenTaskConversation")
}

func TestCloseAndReopenConversationRoutes(t *testing.T) {
	x := newHarness(t)

	rec := x.call(t, http.MethodPost, "/api/v1/tasks/t1/conversation/close", `{"reason":"merged"}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, "t1", x.ctl.chatConvID)
	assert.Equal(t, "orchestrator|merged", x.ctl.chatText, "the API token is the orchestrator")

	rec = x.call(t, http.MethodPost, "/api/v1/tasks/t1/conversation/close", `{"reason":"x","bogus":1}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = x.call(t, http.MethodPost, "/api/v1/tasks/t1/conversation/reopen", "")
	assert.Equal(t, http.StatusForbidden, rec.Code, "the token cannot reopen")
	assert.NotContains(t, x.ctl.calls, "ReopenTaskConversation")

	op := x.newUserSession(t, "ops", domain.RoleOperator)
	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks/t1/conversation/close", `{"reason":"enough"}`, withCookie(op))
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, "owner:ops|enough", x.ctl.chatText)
	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks/t1/conversation/reopen", "", withCookie(op))
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestCloseConversationNeedsOperate(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "watcher", domain.RoleViewer)
	for _, p := range []string{"close", "reopen"} {
		rec := x.authReq(t, http.MethodPost, "/api/v1/tasks/t1/conversation/"+p, `{"reason":"x"}`, withCookie(c))
		assert.Equal(t, http.StatusForbidden, rec.Code, p)
	}
	assert.Empty(t, x.ctl.calls)
}
