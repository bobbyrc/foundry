package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onlet/internal/auth"
	"github.com/bobbyrc/onlet/internal/domain"
)

// testPassword is the password every user made by newUserSession has.
const testPassword = "correct horse battery"

// reqOpt adjusts a request made by authReq.
type reqOpt func(*http.Request)

func withToken(tok string) reqOpt {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
}

func withCookie(value string) reqOpt {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: value}) }
}

func withHeader(k, v string) reqOpt {
	return func(r *http.Request) { r.Header.Set(k, v) }
}

// authReq sends a request through the handler with the given options. A non-empty body is JSON.
func (x *harness) authReq(t *testing.T, method, target, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = testRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = testRequest(method, target, nil)
	}
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	x.h.ServeHTTP(rec, req)
	return rec
}

// newUserSession creates a user with role and testPassword, signs them in directly in the store,
// and returns the session cookie value.
func (x *harness) newUserSession(t *testing.T, name string, role domain.Role) string {
	t.Helper()
	hash, err := auth.HashPassword(testPassword)
	require.NoError(t, err)
	now := time.Now()
	ctx := context.Background()
	_, err = x.st.CreateUser(ctx, domain.User{Username: name, Role: role, PasswordHash: hash, CreatedAt: now, UpdatedAt: now})
	require.NoError(t, err)
	value, id, err := auth.NewSessionToken()
	require.NoError(t, err)
	require.NoError(t, x.st.CreateSession(ctx, domain.Session{
		ID: id, Username: name, CreatedAt: now, ExpiresAt: now.Add(auth.SessionTTL),
	}))
	return value
}

func TestTokenCanReadAndOperateButNotAdminister(t *testing.T) {
	x := newHarness(t)

	rec := x.authReq(t, http.MethodGet, "/api/v1/stats", "", withToken(testToken))
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks", `{"project":"onlet","title":"x"}`, withToken(testToken))
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = x.authReq(t, http.MethodGet, "/api/v1/config/file", "", withToken(testToken))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.JSONEq(t, `{"error":"forbidden"}`, rec.Body.String())

	rec = x.authReq(t, http.MethodPost, "/api/v1/users", `{"username":"eve","password":"`+testPassword+`","role":"owner"}`, withToken(testToken))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = x.authReq(t, http.MethodPost, "/api/v1/auth/logout", "", withToken(testToken))
	assert.Equal(t, http.StatusForbidden, rec.Code, "a token has no session to end")
}

func TestViewerReadsButCannotAct(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "vee", domain.RoleViewer)

	rec := x.authReq(t, http.MethodGet, "/api/v1/stats", "", withCookie(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks", `{"project":"onlet","title":"x"}`, withCookie(c))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, x.ctl.calls, "a viewer must not reach the controller")
}

func TestOperatorActsButCannotAdminister(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "ops", domain.RoleOperator)

	rec := x.authReq(t, http.MethodPost, "/api/v1/tasks", `{"project":"onlet","title":"x"}`, withCookie(c))
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = x.authReq(t, http.MethodGet, "/api/v1/users", "", withCookie(c))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = x.authReq(t, http.MethodGet, "/api/v1/config/file", "", withCookie(c))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOwnerReachesUsers(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)

	rec := x.authReq(t, http.MethodGet, "/api/v1/users", "", withCookie(c))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"username":"boss"`)
	assert.NotContains(t, rec.Body.String(), "password", "the hash must never be sent")
}

func TestCookieCrossSiteWriteIsRefused(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "ops", domain.RoleOperator)
	body := `{"project":"onlet","title":"x"}`

	rec := x.authReq(t, http.MethodPost, "/api/v1/tasks", body,
		withCookie(c), withHeader("Sec-Fetch-Site", "cross-site"))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.JSONEq(t, `{"error":"cross-origin request refused"}`, rec.Body.String())
	assert.Empty(t, x.ctl.calls)

	rec = x.authReq(t, http.MethodPost, "/api/v1/tasks", body,
		withCookie(c), withHeader("Sec-Fetch-Site", "same-origin"))
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// A cross-site read is not a state change and still passes.
	rec = x.authReq(t, http.MethodGet, "/api/v1/stats", "",
		withCookie(c), withHeader("Sec-Fetch-Site", "cross-site"))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSetupCreatesOwnerOnce(t *testing.T) {
	x := newHarness(t)

	rec := x.authReq(t, http.MethodGet, "/api/v1/auth/state", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"setup_required":true,"user":null}`, rec.Body.String())

	body := `{"username":"owner","password":"` + testPassword + `"}`
	rec = x.authReq(t, http.MethodPost, "/api/v1/auth/setup", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"role":"owner"`)
	cookie := sessionCookieFrom(t, rec)
	assert.True(t, cookie.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	assert.Equal(t, "/", cookie.Path)

	rec = x.authReq(t, http.MethodPost, "/api/v1/auth/setup", `{"username":"second","password":"`+testPassword+`"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.JSONEq(t, `{"error":"an owner account already exists"}`, rec.Body.String())

	rec = x.authReq(t, http.MethodGet, "/api/v1/auth/state", "", withCookie(cookie.Value))
	assert.JSONEq(t, `{"setup_required":false,"user":{"username":"owner","role":"owner","created_at":`+
		jsonTime(t, rec, "user", "created_at")+`,"updated_at":`+jsonTime(t, rec, "user", "updated_at")+`}}`, rec.Body.String())
}

func TestSetupRejectsShortPassword(t *testing.T) {
	x := newHarness(t)
	rec := x.authReq(t, http.MethodPost, "/api/v1/auth/setup", `{"username":"owner","password":"short"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.JSONEq(t, `{"error":"password must be at least 12 characters"}`, rec.Body.String())

	rec = x.authReq(t, http.MethodGet, "/api/v1/auth/state", "")
	assert.JSONEq(t, `{"setup_required":true,"user":null}`, rec.Body.String(), "nothing was created")
}

func TestLoginLocksAfterFailedAttempts(t *testing.T) {
	x := newHarness(t)
	x.newUserSession(t, "alice", domain.RoleOperator)
	body := `{"username":"alice","password":"wrong password here"}`

	for i := 1; i <= 5; i++ {
		rec := x.authReq(t, http.MethodPost, "/api/v1/auth/login", body)
		require.Equal(t, http.StatusUnauthorized, rec.Code, "attempt %d", i)
		assert.JSONEq(t, `{"error":"wrong username or password"}`, rec.Body.String())
	}
	good := `{"username":"alice","password":"` + testPassword + `"}`
	rec := x.authReq(t, http.MethodPost, "/api/v1/auth/login", good)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "the sixth attempt is refused even with the right password")
}

func TestLoginSucceedsAndLogoutEndsSession(t *testing.T) {
	x := newHarness(t)
	x.newUserSession(t, "alice", domain.RoleOperator)

	rec := x.authReq(t, http.MethodPost, "/api/v1/auth/login", `{"username":"alice","password":"`+testPassword+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	cookie := sessionCookieFrom(t, rec)

	rec = x.authReq(t, http.MethodGet, "/api/v1/stats", "", withCookie(cookie.Value))
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = x.authReq(t, http.MethodPost, "/api/v1/auth/logout", "", withCookie(cookie.Value))
	require.Equal(t, http.StatusNoContent, rec.Code)
	cleared := sessionCookieFrom(t, rec)
	assert.Less(t, cleared.MaxAge, 0, "logout clears the cookie")

	rec = x.authReq(t, http.MethodGet, "/api/v1/stats", "", withCookie(cookie.Value))
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "the signed-out session no longer works")
}

func TestExpiredSessionIsUnauthorized(t *testing.T) {
	x := newHarness(t)
	x.newUserSession(t, "alice", domain.RoleOwner)
	value, id, err := auth.NewSessionToken()
	require.NoError(t, err)
	past := time.Now().Add(-time.Hour)
	require.NoError(t, x.st.CreateSession(context.Background(), domain.Session{
		ID: id, Username: "alice", CreatedAt: past.Add(-time.Hour), ExpiresAt: past,
	}))

	rec := x.authReq(t, http.MethodGet, "/api/v1/stats", "", withCookie(value))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("Set-Cookie"), "Max-Age=0", "a dead cookie is cleared")
}

func TestDeletedUserSessionIsUnauthorized(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "gone", domain.RoleOwner)
	x.newUserSession(t, "keeper", domain.RoleOwner)
	require.NoError(t, x.st.DeleteUser(context.Background(), "gone"))

	rec := x.authReq(t, http.MethodGet, "/api/v1/stats", "", withCookie(c))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestPasswordChangeKeepsOnlyCurrentSession(t *testing.T) {
	x := newHarness(t)
	current := x.newUserSession(t, "alice", domain.RoleOperator)
	x.newUserSession(t, "bob", domain.RoleOperator)
	// A second session for alice, from another browser.
	other := sessionFor(t, x, "alice")

	body := `{"current":"` + testPassword + `","new":"a brand new passphrase"}`
	rec := x.authReq(t, http.MethodPost, "/api/v1/auth/password", body, withCookie(current))
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	rec = x.authReq(t, http.MethodGet, "/api/v1/stats", "", withCookie(current))
	assert.Equal(t, http.StatusOK, rec.Code, "the session that changed the password stays signed in")
	rec = x.authReq(t, http.MethodGet, "/api/v1/stats", "", withCookie(other))
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "other sessions end")

	rec = x.authReq(t, http.MethodPost, "/api/v1/auth/password", `{"current":"wrong","new":"a brand new passphrase"}`, withCookie(current))
	assert.Equal(t, http.StatusForbidden, rec.Code, "a wrong current password is 403; 401 means the session is gone")
	assert.JSONEq(t, `{"error":"the current password is wrong"}`, rec.Body.String())
}

func TestStateWithoutSessionIsNotUnauthorized(t *testing.T) {
	x := newHarness(t)
	x.newUserSession(t, "alice", domain.RoleViewer)
	rec := x.authReq(t, http.MethodGet, "/api/v1/auth/state", "", withCookie("not-a-session"))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"setup_required":false,"user":null}`, rec.Body.String())
}

// sessionCookieFrom returns the onlet session cookie a response sets.
func sessionCookieFrom(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookie {
			return c
		}
	}
	require.Fail(t, "no session cookie in response", "Set-Cookie: %v", rec.Header().Values("Set-Cookie"))
	return nil
}

// sessionFor signs a user in directly in the store and returns a new cookie value for them.
func sessionFor(t *testing.T, x *harness, name string) string {
	t.Helper()
	value, id, err := auth.NewSessionToken()
	require.NoError(t, err)
	now := time.Now()
	require.NoError(t, x.st.CreateSession(context.Background(), domain.Session{
		ID: id, Username: name, CreatedAt: now, ExpiresAt: now.Add(auth.SessionTTL),
	}))
	return value
}

// jsonTime returns the JSON text of a timestamp field, so a test can compare the rest of a body
// exactly.
func jsonTime(t *testing.T, rec *httptest.ResponseRecorder, path ...string) string {
	t.Helper()
	var body map[string]any
	decode(t, rec, &body)
	var cur any = body
	for _, p := range path {
		m, ok := cur.(map[string]any)
		require.True(t, ok, "path %v", path)
		cur = m[p]
	}
	s, ok := cur.(string)
	require.True(t, ok, "path %v is not a string", path)
	return `"` + s + `"`
}

// newProbeServer returns a server built directly from the harness's store and bus. Tests use it to
// call one handler, or to see state (the limiter) that the harness's handler keeps private.
func newProbeServer(x *harness) *server {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &server{
		opts:    Options{Store: x.st, Bus: x.bus, Token: testToken, Version: "test-version", Logger: logger},
		log:     logger,
		streams: make(chan struct{}, maxEventStreams),
		limiter: auth.NewLimiter(signInLimit, signInWindow, time.Now),
		now:     time.Now,
	}
}

func TestLoginInvalidUsernameDoesNotTouchLimiter(t *testing.T) {
	x := newHarness(t)
	s := newProbeServer(x)
	for i := 0; i < signInLimit+1; i++ {
		rec := httptest.NewRecorder()
		req := testRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"Bad Name!","password":"whatever"}`))
		req.Header.Set("Content-Type", "application/json")
		s.authLogin(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, "attempt %d", i+1)
		assert.JSONEq(t, `{"error":"wrong username or password"}`, rec.Body.String())
	}
	assert.False(t, s.limiter.Locked("bad name!"), "a name no account can have must not fill the limiter")
}

func TestSignInAnswersBusyWhenNoHashingSlotFrees(t *testing.T) {
	x := newHarness(t)
	x.newUserSession(t, "alice", domain.RoleOperator)
	s := newProbeServer(x)
	// Hold every hashing slot for the whole test, and free them afterwards.
	for i := 0; i < hashSlotCount; i++ {
		hashSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < hashSlotCount; i++ {
			<-hashSlots
		}
	})

	rec := httptest.NewRecorder()
	req := testRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"alice","password":"`+testPassword+`"}`))
	req.Header.Set("Content-Type", "application/json")
	s.authLogin(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.JSONEq(t, `{"error":"busy; try again"}`, rec.Body.String())
	assert.False(t, s.limiter.Locked("alice"), "a busy answer does not count as a failed sign-in")
}

func TestPasswordChangeIsLockedAfterWrongGuesses(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "alice", domain.RoleOperator)
	body := func(current string) string {
		return `{"current":"` + current + `","new":"a brand new passphrase"}`
	}
	for i := 1; i <= signInLimit; i++ {
		rec := x.authReq(t, http.MethodPost, "/api/v1/auth/password", body("wrong"), withCookie(c))
		require.Equal(t, http.StatusForbidden, rec.Code, "guess %d", i)
	}
	rec := x.authReq(t, http.MethodPost, "/api/v1/auth/password", body(testPassword), withCookie(c))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "the right password is refused once guesses are locked out")
	assert.JSONEq(t, `{"error":"too many attempts; try again in a few minutes"}`, rec.Body.String())
}
