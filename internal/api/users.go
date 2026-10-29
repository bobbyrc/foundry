package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bobbyrc/onett/internal/auth"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// Account routes are owner-only (see apiRoutes). Responses are domain.User, whose password hash
// is never sent.

// validateAccount checks the fields of a new account: a username, a password and a role.
func validateAccount(username, password string, role domain.Role) error {
	if err := auth.ValidateUsername(username); err != nil {
		return err
	}
	if err := auth.ValidatePassword(password); err != nil {
		return err
	}
	if !role.Valid() {
		return errors.New("role must be viewer, operator or owner")
	}
	return nil
}

func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.opts.Store.ListUsers(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(users))
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string      `json:"username"`
		Password string      `json:"password"`
		Role     domain.Role `json:"role"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if err := validateAccount(in.Username, in.Password, in.Role); err != nil {
		writeErrorMsg(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		s.fail(w, err)
		return
	}
	now := s.now()
	u, err := s.opts.Store.CreateUser(r.Context(), domain.User{
		Username:     in.Username,
		Role:         in.Role,
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if errors.Is(err, store.ErrConflict) {
		writeErrorMsg(w, http.StatusConflict, "that username is taken")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *server) setUserRole(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role domain.Role `json:"role"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if !in.Role.Valid() {
		writeErrorMsg(w, http.StatusUnprocessableEntity, "role must be viewer, operator or owner")
		return
	}
	u, err := s.opts.Store.SetUserRole(r.Context(), chi.URLParam(r, "name"), in.Role)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// setUserPassword resets a user's password. When the caller changes their own password, their
// current session is kept; every other session of that user ends.
func (s *server) setUserPassword(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var in struct {
		Password string `json:"password"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		writeErrorMsg(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		s.fail(w, err)
		return
	}
	keep := ""
	if p, ok := principalFrom(r.Context()); ok && !p.token && p.user.Username == name {
		keep = p.sessionID
	}
	if err := s.opts.Store.SetUserPassword(r.Context(), name, hash, keep); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteUser(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Store.DeleteUser(r.Context(), chi.URLParam(r, "name")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
