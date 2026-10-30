package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func (f *fakeCtl) SendTaskMessage(_ context.Context, taskID, text string) (domain.ChatMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatConvID, f.chatText = taskID, text
	if err := f.note("SendTaskMessage"); err != nil {
		return domain.ChatMessage{}, err
	}
	return domain.ChatMessage{ID: "m1", Role: "user", Text: text, Queued: true}, nil
}

func (f *fakeCtl) FlushTaskMessages(_ context.Context, taskID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatConvID = taskID
	return f.note("FlushTaskMessages")
}

func TestTaskMessageRoutes(t *testing.T) {
	x := newHarness(t)
	cookie := x.newUserSession(t, "alice", domain.RoleOperator)

	rec := x.authReq(t, http.MethodPost, "/api/v1/tasks/t1/messages", `{"text":"keep going"}`, withCookie(cookie))
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var m domain.ChatMessage
	decode(t, rec, &m)
	assert.Equal(t, "keep going", m.Text)
	assert.Equal(t, "t1", x.ctl.chatConvID)

	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks/t1/messages", `{"text":"x","bogus":1}`, withCookie(cookie))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks/t1/messages", `{"text":""}`, withCookie(cookie))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	x.ctl.errs["SendTaskMessage"] = fmt.Errorf("%w: This agent has no session to continue. Retry the task instead.", ErrConflict)
	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks/t1/messages", `{"text":"hi"}`, withCookie(cookie))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "no session to continue")

	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks/t2/conversation/flush", "", withCookie(cookie))
	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Equal(t, "t2", x.ctl.chatConvID)
}

func TestTaskMessageRoutesNeedOperate(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "watcher", domain.RoleViewer)
	for _, tc := range []struct{ path, body string }{
		{"/api/v1/tasks/t1/messages", `{"text":"x"}`},
		{"/api/v1/tasks/t1/conversation/flush", ""},
	} {
		rec := x.authReq(t, http.MethodPost, tc.path, tc.body, withCookie(c))
		assert.Equal(t, http.StatusForbidden, rec.Code, tc.path)
	}
}
