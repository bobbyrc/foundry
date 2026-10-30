package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func (f *fakeCtl) FlushChat(_ context.Context, convID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatConvID = convID
	return f.note("FlushChat")
}

func (f *fakeCtl) EditQueuedMessage(_ context.Context, id, text string) (domain.ChatMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatText = text
	if err := f.note("EditQueuedMessage"); err != nil {
		return domain.ChatMessage{}, err
	}
	return domain.ChatMessage{ID: id, Role: "user", Text: text, Queued: true}, nil
}

func (f *fakeCtl) CancelQueuedMessage(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatText = id
	return f.note("CancelQueuedMessage")
}

func TestMessageRoutes(t *testing.T) {
	x := newHarness(t)

	rec := x.call(t, http.MethodPatch, "/api/v1/messages/m1", `{"text":"fixed"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var m domain.ChatMessage
	decode(t, rec, &m)
	assert.Equal(t, "m1", m.ID)
	assert.Equal(t, "fixed", m.Text)

	rec = x.call(t, http.MethodPatch, "/api/v1/messages/m1", `{"text":"x","bogus":1}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "unknown fields are errors")

	rec = x.call(t, http.MethodDelete, "/api/v1/messages/m1", "")
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	rec = x.call(t, http.MethodPost, "/api/v1/conversations/c1/flush", "")
	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Equal(t, "c1", x.ctl.chatConvID)

	x.ctl.errs["EditQueuedMessage"] = ErrConflict
	x.ctl.errs["CancelQueuedMessage"] = ErrConflict
	rec = x.call(t, http.MethodPatch, "/api/v1/messages/m1", `{"text":"late"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	rec = x.call(t, http.MethodDelete, "/api/v1/messages/m1", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestMessageRoutesNeedOperate(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "watcher", domain.RoleViewer)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPatch, "/api/v1/messages/m1", `{"text":"x"}`},
		{http.MethodDelete, "/api/v1/messages/m1", ""},
		{http.MethodPost, "/api/v1/conversations/c1/flush", ""},
	} {
		rec := x.authReq(t, tc.method, tc.path, tc.body, withCookie(c))
		assert.Equal(t, http.StatusForbidden, rec.Code, tc.method+" "+tc.path)
	}
}
