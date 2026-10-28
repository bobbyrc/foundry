package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/store"
)

// createUser inserts a user with the given role and password hash, failing the test on error.
func createUser(t *testing.T, s *Store, name string, role domain.Role, hash string) domain.User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), domain.User{Username: name, Role: role, PasswordHash: hash})
	require.NoError(t, err)
	return u
}

func TestCreateFirstUserOnlyOnce(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	u, err := s.CreateFirstUser(ctx, domain.User{Username: "ada", Role: domain.RoleOwner, PasswordHash: "h1"})
	require.NoError(t, err)
	assert.False(t, u.CreatedAt.IsZero())
	assert.False(t, u.UpdatedAt.IsZero())

	_, err = s.CreateFirstUser(ctx, domain.User{Username: "bob", Role: domain.RoleOwner, PasswordHash: "h2"})
	assert.True(t, errors.Is(err, store.ErrConflict))

	n, err := s.CountUsers(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestCreateUserRejectsDuplicateUsername(t *testing.T) {
	s := newStore(t)
	createUser(t, s, "ada", domain.RoleOwner, "h1")

	_, err := s.CreateUser(context.Background(), domain.User{Username: "ada", Role: domain.RoleViewer, PasswordHash: "h2"})
	assert.True(t, errors.Is(err, store.ErrConflict))

	got, err := s.GetUser(context.Background(), "ada")
	require.NoError(t, err)
	assert.Equal(t, domain.RoleOwner, got.Role, "the duplicate must not overwrite the first user")
	assert.Equal(t, "h1", got.PasswordHash)
}

func TestCreateUserRejectsInvalidRole(t *testing.T) {
	s := newStore(t)
	_, err := s.CreateUser(context.Background(), domain.User{Username: "ada", Role: "admin"})
	assert.Error(t, err)
	n, err := s.CountUsers(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestGetUserMissingIsNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.GetUser(context.Background(), "nobody")
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestPasswordHashRoundTrips(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	created := createUser(t, s, "ada", domain.RoleOperator, "argon2id$hash")

	got, err := s.GetUser(ctx, "ada")
	require.NoError(t, err)
	assert.Equal(t, "argon2id$hash", got.PasswordHash)
	assert.Equal(t, domain.RoleOperator, got.Role)
	assert.True(t, created.CreatedAt.Equal(got.CreatedAt))

	list, err := s.ListUsers(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "argon2id$hash", list[0].PasswordHash)
}

func TestListUsersOrderedByUsername(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	createUser(t, s, "carol", domain.RoleViewer, "h")
	createUser(t, s, "ada", domain.RoleOwner, "h")
	createUser(t, s, "bob", domain.RoleOperator, "h")

	list, err := s.ListUsers(ctx)
	require.NoError(t, err)
	var names []string
	for _, u := range list {
		names = append(names, u.Username)
	}
	assert.Equal(t, []string{"ada", "bob", "carol"}, names)

	n, err := s.CountUsers(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
}

func TestDemotingOneOfTwoOwnersSucceeds(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	createUser(t, s, "ada", domain.RoleOwner, "h")
	createUser(t, s, "bob", domain.RoleOwner, "h")

	u, err := s.SetUserRole(ctx, "ada", domain.RoleViewer)
	require.NoError(t, err)
	assert.Equal(t, domain.RoleViewer, u.Role)

	got, err := s.GetUser(ctx, "ada")
	require.NoError(t, err)
	assert.Equal(t, domain.RoleViewer, got.Role)
}

func TestDemotingLastOwnerIsRefused(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	createUser(t, s, "ada", domain.RoleOwner, "h")
	createUser(t, s, "bob", domain.RoleOperator, "h")

	_, err := s.SetUserRole(ctx, "ada", domain.RoleOperator)
	assert.True(t, errors.Is(err, store.ErrLastOwner))

	got, err := s.GetUser(ctx, "ada")
	require.NoError(t, err)
	assert.Equal(t, domain.RoleOwner, got.Role)
}

func TestSetUserRoleMissingIsNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.SetUserRole(context.Background(), "nobody", domain.RoleViewer)
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestDeletingLastOwnerIsRefused(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	createUser(t, s, "ada", domain.RoleOwner, "h")

	err := s.DeleteUser(ctx, "ada")
	assert.True(t, errors.Is(err, store.ErrLastOwner))
	_, err = s.GetUser(ctx, "ada")
	assert.NoError(t, err, "the refused delete must leave the user in place")
}

func TestDeletingOneOfTwoOwnersSucceeds(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	createUser(t, s, "ada", domain.RoleOwner, "h")
	createUser(t, s, "bob", domain.RoleOwner, "h")

	require.NoError(t, s.DeleteUser(ctx, "ada"))
	_, err := s.GetUser(ctx, "ada")
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestDeleteUserRemovesTheirSessions(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	createUser(t, s, "ada", domain.RoleOwner, "h")
	createUser(t, s, "bob", domain.RoleOperator, "h")
	require.NoError(t, s.CreateSession(ctx, domain.Session{ID: "s-ada", Username: "ada", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}))
	require.NoError(t, s.CreateSession(ctx, domain.Session{ID: "s-bob", Username: "bob", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}))

	require.NoError(t, s.DeleteUser(ctx, "bob"))

	_, err := s.GetSession(ctx, "s-bob", now)
	assert.True(t, errors.Is(err, store.ErrNotFound))
	_, err = s.GetSession(ctx, "s-ada", now)
	assert.NoError(t, err)
}

func TestDeleteUserMissingIsNotFound(t *testing.T) {
	s := newStore(t)
	err := s.DeleteUser(context.Background(), "nobody")
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestSetUserPasswordKeepsOnlyTheNamedSession(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	createUser(t, s, "ada", domain.RoleOwner, "old")
	for _, id := range []string{"keep", "drop1", "drop2"} {
		require.NoError(t, s.CreateSession(ctx, domain.Session{ID: id, Username: "ada", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}))
	}

	require.NoError(t, s.SetUserPassword(ctx, "ada", "new", "keep"))

	got, err := s.GetUser(ctx, "ada")
	require.NoError(t, err)
	assert.Equal(t, "new", got.PasswordHash)
	_, err = s.GetSession(ctx, "keep", now)
	assert.NoError(t, err)
	for _, id := range []string{"drop1", "drop2"} {
		_, err := s.GetSession(ctx, id, now)
		assert.True(t, errors.Is(err, store.ErrNotFound), id)
	}
}

func TestSetUserPasswordWithoutKeepDeletesAllSessions(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	createUser(t, s, "ada", domain.RoleOwner, "old")
	require.NoError(t, s.CreateSession(ctx, domain.Session{ID: "a", Username: "ada", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}))

	require.NoError(t, s.SetUserPassword(ctx, "ada", "new", ""))
	_, err := s.GetSession(ctx, "a", now)
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestSetUserPasswordMissingIsNotFound(t *testing.T) {
	s := newStore(t)
	err := s.SetUserPassword(context.Background(), "nobody", "h", "")
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestGetSessionExpiredIsNotFound(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, s.CreateSession(ctx, domain.Session{ID: "live", Username: "ada", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}))
	require.NoError(t, s.CreateSession(ctx, domain.Session{ID: "dead", Username: "ada", CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)}))

	got, err := s.GetSession(ctx, "live", now)
	require.NoError(t, err)
	assert.Equal(t, "ada", got.Username)
	assert.Equal(t, "live", got.ID)
	assert.True(t, now.Add(time.Hour).Equal(got.ExpiresAt))

	_, err = s.GetSession(ctx, "dead", now)
	assert.True(t, errors.Is(err, store.ErrNotFound))

	// Expiry is ExpiresAt <= now, so a session is dead at the exact instant it expires.
	_, err = s.GetSession(ctx, "live", now.Add(time.Hour))
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestDeleteSessionMissingIsNotAnError(t *testing.T) {
	s := newStore(t)
	assert.NoError(t, s.DeleteSession(context.Background(), "never-existed"))
}

func TestDeleteExpiredSessionsKeepsLiveOnes(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, s.CreateSession(ctx, domain.Session{ID: "live", Username: "ada", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}))
	require.NoError(t, s.CreateSession(ctx, domain.Session{ID: "dead", Username: "ada", CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Minute)}))

	require.NoError(t, s.DeleteExpiredSessions(ctx, now))

	_, err := s.GetSession(ctx, "live", now)
	assert.NoError(t, err)
	var n int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&n))
	assert.Equal(t, 1, n, "the expired row is removed, not just hidden")
}

func TestDeleteSessionRemovesOnlyThatSession(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	require.NoError(t, s.CreateSession(ctx, domain.Session{ID: "a", Username: "ada", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}))
	require.NoError(t, s.CreateSession(ctx, domain.Session{ID: "b", Username: "ada", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}))

	require.NoError(t, s.DeleteSession(ctx, "a"))
	_, err := s.GetSession(ctx, "a", now)
	assert.True(t, errors.Is(err, store.ErrNotFound))
	_, err = s.GetSession(ctx, "b", now)
	assert.NoError(t, err)
}
