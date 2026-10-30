package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestOrchestratorRoutesRejectTaskConversation(t *testing.T) {
	x := newHarness(t)
	conv, err := x.st.CreateConversation(t.Context(), domain.Conversation{TaskID: "t1", Title: "agent chat"})
	require.NoError(t, err)

	rec := x.call(t, http.MethodGet, "/api/v1/conversations/"+conv.ID, "")
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	rec = x.call(t, http.MethodPost, "/api/v1/conversations/"+conv.ID+"/messages", `{"text":"hi"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	rec = x.call(t, http.MethodPost, "/api/v1/conversations/"+conv.ID+"/flush", "")
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Empty(t, x.ctl.chatText, "the controller was never reached")
}

func TestTaskMessagingIsPersonOnly(t *testing.T) {
	x := newHarness(t)
	cookie := x.newUserSession(t, "alice", domain.RoleOperator)
	for _, tc := range []struct {
		method, path, body string
		ok                 int
	}{
		{http.MethodPost, "/api/v1/tasks/t1/messages", `{"text":"x"}`, http.StatusAccepted},
		{http.MethodPost, "/api/v1/tasks/t1/conversation/flush", "", http.StatusAccepted},
		{http.MethodPatch, "/api/v1/messages/m1", `{"text":"x"}`, http.StatusOK},
		{http.MethodDelete, "/api/v1/messages/m1", "", http.StatusNoContent},
	} {
		rec := x.call(t, tc.method, tc.path, tc.body)
		assert.Equal(t, http.StatusForbidden, rec.Code, "token: "+tc.method+" "+tc.path)
		rec = x.authReq(t, tc.method, tc.path, tc.body, withCookie(cookie))
		assert.Equal(t, tc.ok, rec.Code, "operator: "+tc.method+" "+tc.path+" "+rec.Body.String())
	}
}
