package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bobbyrc/foundry/internal/auth"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/store"
)

// Sign-in limits: a username that fails signInLimit times within signInWindow is locked until the
// window passes. See auth.Limiter.
const (
	signInLimit  = 5
	signInWindow = 15 * time.Minute
)

// Password hashing is slow on purpose (see auth.passwordIterations), so the server runs only
// hashSlotCount of them at once. A request that cannot get a slot within hashWait is answered 503
// rather than queued without limit.
const (
	hashSlotCount = 4
	hashWait      = 5 * time.Second
)

// hashSlots is the semaphore for hashing. It is process-wide, which is one server for the daemon.
var hashSlots = make(chan struct{}, hashSlotCount)

// takeHashSlot waits up to hashWait for a hashing slot. It returns the function that gives the
// slot back. When no slot frees in time it answers 503 and returns false; when the client has
// already gone it returns false without writing anything.
func takeHashSlot(w http.ResponseWriter, r *http.Request) (func(), bool) {
	timer := time.NewTimer(hashWait)
	defer timer.Stop()
	select {
	case hashSlots <- struct{}{}:
		return func() { <-hashSlots }, true
	case <-timer.C:
		writeErrorMsg(w, http.StatusServiceUnavailable, "busy; try again")
		return nil, false
	case <-r.Context().Done():
		return nil, false
	}
}

// passwordLimitKey is the limiter key for guesses at a signed-in user's current password. It is
// kept apart from the sign-in key so that a changed-password guess and a sign-in guess do not
// share a count.
func passwordLimitKey(username string) string {
	return "password:" + username
}

// crossOrigin refuses browser requests that come from another site (Sec-Fetch-Site and Origin
// checks). Requests with neither header, such as the CLI's, pass.
var crossOrigin = http.NewCrossOriginProtection()

// principal is who a request is made by. A token principal is the CLI or the orchestrator agent
// and has no user. A user principal is a signed-in person, with the session that authenticated
// them so that a password change can keep that session alive.
type principal struct {
	token     bool
	user      domain.User
	sessionID string
}

type principalKey struct{}

func withPrincipal(ctx context.Context, p principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func principalFrom(ctx context.Context) (principal, bool) {
	p, ok := ctx.Value(principalKey{}).(principal)
	return p, ok
}

// errNoSession means the request carries no cookie for a live session of an existing user. It is
// the normal "not signed in" answer, not a failure of the store.
var errNoSession = errors.New("no valid session")

// principal resolves who is calling and stores it in the request context. The order is: the
// bearer token (or ?token= on GET /events), then the session cookie. A request with neither gets
// 401. A session cookie that no longer works is cleared.
func (s *server) principal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if s.tokenMatches(r) {
			next.ServeHTTP(w, r.WithContext(withPrincipal(ctx, principal{token: true})))
			return
		}
		user, sid, err := s.sessionUser(r)
		if err != nil {
			if !errors.Is(err, errNoSession) {
				s.fail(w, err)
				return
			}
			if _, cerr := r.Cookie(auth.SessionCookie); cerr == nil {
				http.SetCookie(w, s.cookie(r, "", -1))
			}
			writeErrorMsg(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		// A browser can be sent here by another site with the cookie attached. Refuse those
		// state changes. Safe methods are never refused.
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(w, r) {
			return
		}
		next.ServeHTTP(w, r.WithContext(withPrincipal(ctx, principal{user: user, sessionID: sid})))
	})
}

// tokenMatches reports whether the request carries the API token. An empty configured token
// never matches, because ConstantTimeCompare alone would treat two empty values as equal.
func (s *server) tokenMatches(r *http.Request) bool {
	tok := ""
	if h := r.Header.Get("Authorization"); len(h) > len("Bearer ") && strings.EqualFold(h[:len("Bearer ")], "Bearer ") {
		tok = h[len("Bearer "):]
	}
	// GET /events also accepts ?token=, because browsers' EventSource cannot set headers.
	if tok == "" && r.URL.Path == "/api/v1/events" {
		tok = r.URL.Query().Get("token")
	}
	if tok == "" || s.opts.Token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(s.opts.Token)) == 1
}

// sessionUser resolves the session cookie on r to its user and session ID. A missing, unknown or
// expired session, or one whose user was deleted, gives errNoSession.
func (s *server) sessionUser(r *http.Request) (domain.User, string, error) {
	c, err := r.Cookie(auth.SessionCookie)
	if err != nil {
		return domain.User{}, "", errNoSession
	}
	ctx := r.Context()
	id := auth.SessionID(c.Value)
	sess, err := s.opts.Store.GetSession(ctx, id, s.now())
	if errors.Is(err, store.ErrNotFound) {
		return domain.User{}, "", errNoSession
	}
	if err != nil {
		return domain.User{}, "", err
	}
	u, err := s.opts.Store.GetUser(ctx, sess.Username)
	if errors.Is(err, store.ErrNotFound) {
		return domain.User{}, "", errNoSession
	}
	if err != nil {
		return domain.User{}, "", err
	}
	return u, id, nil
}

// need returns middleware that lets through only the callers allowed to perform a. The token may
// do Read and Operate; a user is limited by their role (see auth.Allows).
func (s *server) need(a auth.Action) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := principalFrom(r.Context())
			if !ok {
				writeErrorMsg(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			allowed := auth.TokenAllows(a)
			if !p.token {
				allowed = auth.Allows(p.user.Role, a)
			}
			if !allowed {
				writeErrorMsg(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// sameOrigin answers 403 and returns false when the request came from another site.
func sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if err := crossOrigin.Check(r); err != nil {
		writeErrorMsg(w, http.StatusForbidden, "cross-origin request refused")
		return false
	}
	return true
}

// userPrincipal returns the signed-in user making the request. A token has no user, so it gets
// 403 and false.
func userPrincipal(w http.ResponseWriter, r *http.Request) (principal, bool) {
	p, ok := principalFrom(r.Context())
	if !ok || p.token {
		writeErrorMsg(w, http.StatusForbidden, "forbidden")
		return principal{}, false
	}
	return p, true
}

// cookie returns the session cookie with the attributes foundry always uses. Secure is set when
// the public URL is https or the request itself came over TLS. maxAge -1 clears the cookie.
func (s *server) cookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
		Secure:   s.opts.SecureCookies || r.TLS != nil,
	}
}

// startSession stores a new session for u and sets its cookie on w.
func (s *server) startSession(w http.ResponseWriter, r *http.Request, u domain.User) error {
	value, id, err := auth.NewSessionToken()
	if err != nil {
		return err
	}
	now := s.now()
	if err := s.opts.Store.CreateSession(r.Context(), domain.Session{
		ID:        id,
		Username:  u.Username,
		CreatedAt: now,
		ExpiresAt: now.Add(auth.SessionTTL),
	}); err != nil {
		return err
	}
	http.SetCookie(w, s.cookie(r, value, int(auth.SessionTTL.Seconds())))
	return nil
}

// authState is the body of GET /auth/state.
type authState struct {
	SetupRequired bool         `json:"setup_required"`
	User          *domain.User `json:"user"`
}

// credentials is the body of POST /auth/setup and POST /auth/login.
type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *server) authState(w http.ResponseWriter, r *http.Request) {
	n, err := s.opts.Store.CountUsers(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := authState{SetupRequired: n == 0}
	u, _, err := s.sessionUser(r)
	switch {
	case err == nil:
		out.User = &u
	case errors.Is(err, errNoSession):
	default:
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// authSetup creates the first account, as an owner. The store refuses unless no user exists.
func (s *server) authSetup(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(w, r) {
		return
	}
	var in credentials
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if err := auth.ValidateUsername(in.Username); err != nil {
		writeErrorMsg(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		writeErrorMsg(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	release, ok := takeHashSlot(w, r)
	if !ok {
		return
	}
	hash, err := auth.HashPassword(in.Password)
	release()
	if err != nil {
		s.fail(w, err)
		return
	}
	now := s.now()
	u, err := s.opts.Store.CreateFirstUser(r.Context(), domain.User{
		Username:     in.Username,
		Role:         domain.RoleOwner,
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if errors.Is(err, store.ErrConflict) {
		writeErrorMsg(w, http.StatusConflict, "an owner account already exists")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// authLogin checks a username and password and starts a session. Failures are counted per
// username; a locked username gets 429 before its password is checked.
func (s *server) authLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(w, r) {
		return
	}
	var in credentials
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	ctx := r.Context()
	key := strings.ToLower(in.Username)
	if auth.ValidateUsername(key) != nil {
		// No account can have this name, so there is nothing to guess at. Answer as a wrong
		// password without counting the attempt or spending a hash.
		signInRefused(w)
		return
	}
	release, ok := takeHashSlot(w, r)
	if !ok {
		return
	}
	defer release()
	// Acquire counts this attempt before the password is checked, so parallel requests cannot
	// all pass the limit together. A successful sign-in clears the count below.
	if !s.limiter.Acquire(key) {
		writeErrorMsg(w, http.StatusTooManyRequests, "too many attempts; try again in a few minutes")
		return
	}
	u, err := s.opts.Store.GetUser(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		// Spend the same time as a real check, so the response does not reveal which names exist.
		auth.BurnPasswordCheck(in.Password)
		signInRefused(w)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.Password) {
		signInRefused(w)
		return
	}
	s.limiter.Reset(key)
	if err := s.opts.Store.DeleteExpiredSessions(ctx, s.now()); err != nil {
		s.log.Warn("api: delete expired sessions", "err", err)
	}
	if err := s.startSession(w, r, u); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// signInRefused answers 401. The message is the same whether the username or the password was
// wrong. The attempt was already counted by the limiter when it was acquired.
func signInRefused(w http.ResponseWriter) {
	writeErrorMsg(w, http.StatusUnauthorized, "wrong username or password")
}

// authLogout ends the current session and clears its cookie.
func (s *server) authLogout(w http.ResponseWriter, r *http.Request) {
	p, ok := userPrincipal(w, r)
	if !ok {
		return
	}
	if err := s.opts.Store.DeleteSession(r.Context(), p.sessionID); err != nil {
		s.fail(w, err)
		return
	}
	http.SetCookie(w, s.cookie(r, "", -1))
	w.WriteHeader(http.StatusNoContent)
}

// changePassword sets a new password for the signed-in user. Their other sessions end; this one
// stays signed in.
func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	p, ok := userPrincipal(w, r)
	if !ok {
		return
	}
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	// Guesses at the current password are limited like sign-in guesses: a session that is
	// stolen cannot be used to try passwords without end. The attempt is counted before the check.
	release, ok := takeHashSlot(w, r)
	if !ok {
		return
	}
	defer release()
	key := passwordLimitKey(p.user.Username)
	if !s.limiter.Acquire(key) {
		writeErrorMsg(w, http.StatusTooManyRequests, "too many attempts; try again in a few minutes")
		return
	}
	if !auth.CheckPassword(p.user.PasswordHash, in.Current) {
		// 403, not 401: the web client treats 401 as a lost session and signs the person out.
		writeErrorMsg(w, http.StatusForbidden, "the current password is wrong")
		return
	}
	s.limiter.Reset(key)
	if err := auth.ValidatePassword(in.New); err != nil {
		writeErrorMsg(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.opts.Store.SetUserPassword(r.Context(), p.user.Username, hash, p.sessionID); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
