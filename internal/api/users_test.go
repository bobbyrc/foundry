package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onlet/internal/domain"
)

func TestOwnerCreatesUserAndSeesNoHash(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)

	body := `{"username":"bob","password":"` + testPassword + `","role":"operator"}`
	rec := x.authReq(t, http.MethodPost, "/api/v1/users", body, withCookie(c))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"role":"operator"`)
	assert.NotContains(t, rec.Body.String(), "password_hash")

	rec = x.authReq(t, http.MethodPost, "/api/v1/users", body, withCookie(c))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.JSONEq(t, `{"error":"that username is taken"}`, rec.Body.String())

	rec = x.authReq(t, http.MethodPost, "/api/v1/users",
		`{"username":"carl","password":"`+testPassword+`","role":"admin"}`, withCookie(c))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	rec = x.authReq(t, http.MethodPost, "/api/v1/users",
		`{"username":"Carl","password":"`+testPassword+`","role":"viewer"}`, withCookie(c))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "usernames must be lowercase")
}

func TestLastOwnerCannotBeDemotedOrDeleted(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)

	rec := x.authReq(t, http.MethodPost, "/api/v1/users/boss/role", `{"role":"viewer"}`, withCookie(c))
	assert.Equal(t, http.StatusConflict, rec.Code)

	rec = x.authReq(t, http.MethodDelete, "/api/v1/users/boss", "", withCookie(c))
	assert.Equal(t, http.StatusConflict, rec.Code)

	// With a second owner, the first may be demoted.
	x.newUserSession(t, "second", domain.RoleOwner)
	rec = x.authReq(t, http.MethodPost, "/api/v1/users/boss/role", `{"role":"viewer"}`, withCookie(c))
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestUserRoutesReturnNotFound(t *testing.T) {
	x := newHarness(t)
	c := x.newUserSession(t, "boss", domain.RoleOwner)
	rec := x.authReq(t, http.MethodPost, "/api/v1/users/nobody/role", `{"role":"viewer"}`, withCookie(c))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = x.authReq(t, http.MethodPost, "/api/v1/users/nobody/password", `{"password":"`+testPassword+`"}`, withCookie(c))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = x.authReq(t, http.MethodDelete, "/api/v1/users/nobody", "", withCookie(c))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOwnerResetsAnotherUsersPasswordAndEndsTheirSessions(t *testing.T) {
	x := newHarness(t)
	boss := x.newUserSession(t, "boss", domain.RoleOwner)
	victimSession := x.newUserSession(t, "vic", domain.RoleOperator)

	rec := x.authReq(t, http.MethodPost, "/api/v1/users/vic/password",
		`{"password":"a fresh passphrase"}`, withCookie(boss))
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	rec = x.authReq(t, http.MethodGet, "/api/v1/stats", "", withCookie(victimSession))
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "a reset signs the user out everywhere")

	rec = x.authReq(t, http.MethodDelete, "/api/v1/users/vic", "", withCookie(boss))
	assert.Equal(t, http.StatusNoContent, rec.Code)
}
