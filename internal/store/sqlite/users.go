package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/store"
)

// userCols lists the users columns in the order scanUser reads them.
const userCols = "username, role, password_hash, created_at, updated_at"

// scanUser reads one users row. The password hash is a plain column, not part of a JSON data
// column, so it is read back here even though it never serialises to JSON.
func scanUser(r rowScanner) (domain.User, error) {
	var u domain.User
	var role string
	var created, updated int64
	if err := r.Scan(&u.Username, &role, &u.PasswordHash, &created, &updated); err != nil {
		return domain.User{}, err
	}
	u.Role = domain.Role(role)
	u.CreatedAt = fromNanos(created)
	u.UpdatedAt = fromNanos(updated)
	return u, nil
}

// getUser loads the user named username, wrapping store.ErrNotFound when there is none.
func getUser(ctx context.Context, ex execer, username string) (domain.User, error) {
	return scanOne(ctx, ex, fmt.Sprintf("user %s", username), scanUser,
		"SELECT "+userCols+" FROM users WHERE username = ?", username)
}

// scanOne runs a query returning one row and maps it with scan. A missing row wraps
// store.ErrNotFound.
func scanOne[T any](ctx context.Context, ex execer, notFound string, scan func(rowScanner) (T, error),
	query string, args ...any) (T, error) {
	var zero T
	v, err := scan(ex.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return zero, fmt.Errorf("%s: %w", notFound, store.ErrNotFound)
	}
	if err != nil {
		return zero, fmt.Errorf("load %s: %w", notFound, err)
	}
	return v, nil
}

// countUsersIn counts users, optionally only those with role. An empty role counts every user.
func countUsersIn(ctx context.Context, ex execer, role domain.Role) (int, error) {
	q := `SELECT COUNT(*) FROM users`
	var args []any
	if role != "" {
		q += ` WHERE role = ?`
		args = append(args, string(role))
	}
	var n int
	if err := ex.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// insertUser writes a user row; a taken username is an error.
func insertUser(ctx context.Context, ex execer, u domain.User) error {
	_, err := ex.ExecContext(ctx,
		`INSERT INTO users (username, role, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		u.Username, string(u.Role), u.PasswordHash, nanos(u.CreatedAt), nanos(u.UpdatedAt))
	if err != nil {
		return fmt.Errorf("write user %s: %w", u.Username, err)
	}
	return nil
}

// prepareUser validates u and fills its timestamps when they are zero.
func prepareUser(u domain.User) (domain.User, error) {
	if !u.Role.Valid() {
		return u, fmt.Errorf("user %s: invalid role %q", u.Username, u.Role)
	}
	now := nowUTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	if u.UpdatedAt.IsZero() {
		u.UpdatedAt = now
	}
	return u, nil
}

// CreateFirstUser stores u only when no user exists yet, checking and inserting in one
// transaction. It returns store.ErrConflict otherwise.
func (s *Store) CreateFirstUser(ctx context.Context, u domain.User) (domain.User, error) {
	u, err := prepareUser(u)
	if err != nil {
		return domain.User{}, err
	}
	err = withTx(ctx, s.db, func(tx *sql.Tx) error {
		n, err := countUsersIn(ctx, tx, "")
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("first user: %w", store.ErrConflict)
		}
		return insertUser(ctx, tx, u)
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("create first user: %w", err)
	}
	return u, nil
}

// CreateUser stores u. It returns store.ErrConflict when the username is taken.
func (s *Store) CreateUser(ctx context.Context, u domain.User) (domain.User, error) {
	u, err := prepareUser(u)
	if err != nil {
		return domain.User{}, err
	}
	err = withTx(ctx, s.db, func(tx *sql.Tx) error {
		_, err := getUser(ctx, tx, u.Username)
		if err == nil {
			return fmt.Errorf("user %s: %w", u.Username, store.ErrConflict)
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		return insertUser(ctx, tx, u)
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// GetUser returns the user named username, or store.ErrNotFound.
func (s *Store) GetUser(ctx context.Context, username string) (domain.User, error) {
	return getUser(ctx, s.db, username)
}

// ListUsers returns every user ordered by username.
func (s *Store) ListUsers(ctx context.Context) ([]domain.User, error) {
	out, err := queryRows(ctx, s.db, scanUser,
		"SELECT "+userCols+" FROM users ORDER BY username")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return out, nil
}

// CountUsers returns how many users exist.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	return countUsersIn(ctx, s.db, "")
}

// SetUserRole changes a user's role and returns the stored user. It returns store.ErrLastOwner
// when the change would demote the only owner.
func (s *Store) SetUserRole(ctx context.Context, username string, role domain.Role) (domain.User, error) {
	if !role.Valid() {
		return domain.User{}, fmt.Errorf("set role %s: invalid role %q", username, role)
	}
	var out domain.User
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, err := getUser(ctx, tx, username)
		if err != nil {
			return err
		}
		if cur.Role == domain.RoleOwner && role != domain.RoleOwner {
			owners, err := countUsersIn(ctx, tx, domain.RoleOwner)
			if err != nil {
				return err
			}
			if owners <= 1 {
				return fmt.Errorf("set role %s: %w", username, store.ErrLastOwner)
			}
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE users SET role = ?, updated_at = ? WHERE username = ?`,
			string(role), nanos(nowUTC()), username); err != nil {
			return fmt.Errorf("update role %s: %w", username, err)
		}
		out, err = getUser(ctx, tx, username)
		return err
	})
	if err != nil {
		return domain.User{}, err
	}
	return out, nil
}

// SetUserPassword replaces the user's password hash and deletes their sessions except
// keepSessionID. An empty keepSessionID deletes all of them.
func (s *Store) SetUserPassword(ctx context.Context, username, passwordHash, keepSessionID string) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE users SET password_hash = ?, updated_at = ? WHERE username = ?`,
			passwordHash, nanos(nowUTC()), username)
		if err != nil {
			return fmt.Errorf("update password %s: %w", username, err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return fmt.Errorf("update password %s: %w", username, err)
		} else if n == 0 {
			return fmt.Errorf("user %s: %w", username, store.ErrNotFound)
		}
		if keepSessionID == "" {
			_, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE username = ?`, username)
		} else {
			_, err = tx.ExecContext(ctx,
				`DELETE FROM sessions WHERE username = ? AND id != ?`, username, keepSessionID)
		}
		if err != nil {
			return fmt.Errorf("delete sessions of %s: %w", username, err)
		}
		return nil
	})
}

// DeleteUser deletes the user and all their sessions. It returns store.ErrLastOwner when the
// user is the only owner.
func (s *Store) DeleteUser(ctx context.Context, username string) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, err := getUser(ctx, tx, username)
		if err != nil {
			return err
		}
		if cur.Role == domain.RoleOwner {
			owners, err := countUsersIn(ctx, tx, domain.RoleOwner)
			if err != nil {
				return err
			}
			if owners <= 1 {
				return fmt.Errorf("delete user %s: %w", username, store.ErrLastOwner)
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE username = ?`, username); err != nil {
			return fmt.Errorf("delete sessions of %s: %w", username, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE username = ?`, username); err != nil {
			return fmt.Errorf("delete user %s: %w", username, err)
		}
		return nil
	})
}

// scanSession reads one sessions row.
func scanSession(r rowScanner) (domain.Session, error) {
	var se domain.Session
	var created, expires int64
	if err := r.Scan(&se.ID, &se.Username, &created, &expires); err != nil {
		return domain.Session{}, err
	}
	se.CreatedAt = fromNanos(created)
	se.ExpiresAt = fromNanos(expires)
	return se, nil
}

// CreateSession stores a session.
func (s *Store) CreateSession(ctx context.Context, se domain.Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, username, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		se.ID, se.Username, nanos(se.CreatedAt), nanos(se.ExpiresAt))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// GetSession returns the session with id, or store.ErrNotFound when it is unknown or expired at
// now (ExpiresAt <= now).
func (s *Store) GetSession(ctx context.Context, id string, now time.Time) (domain.Session, error) {
	se, err := scanOne(ctx, s.db, "session", scanSession,
		`SELECT id, username, created_at, expires_at FROM sessions WHERE id = ? AND expires_at > ?`,
		id, nanos(now))
	if err != nil {
		return domain.Session{}, err
	}
	return se, nil
}

// DeleteSession deletes the session with id. A missing session is not an error.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions deletes every session that has expired at now (ExpiresAt <= now).
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, nanos(now)); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}
