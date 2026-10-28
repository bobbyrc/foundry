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

var signals = entity[domain.Signal]{
	name:  "signal",
	table: "signals",
	id:    func(s *domain.Signal) *string { return &s.ID },
	stamp: func(s *domain.Signal, now time.Time) { s.UpdatedAt = now },
	cols: func(s domain.Signal) []col {
		return []col{
			{"id", s.ID},
			{"project", s.Project},
			{"source", s.Source},
			{"fingerprint", s.Fingerprint},
			{"updated_at", nanos(s.UpdatedAt)},
		}
	},
}

var friction = entity[domain.Friction]{
	name:  "friction",
	table: "friction",
	id:    func(f *domain.Friction) *string { return &f.ID },
	stamp: func(f *domain.Friction, now time.Time) { f.UpdatedAt = now },
	cols: func(f domain.Friction) []col {
		return []col{
			{"id", f.ID},
			{"project", f.Project},
			{"fingerprint", f.Fingerprint},
			{"updated_at", nanos(f.UpdatedAt)},
		}
	},
}

// UpsertSignal inserts sig, or refreshes the stored signal with the same project, source and
// fingerprint: Count, LastSeen, Title, URL, Level, Culprit and Release come from sig, while ID,
// TaskID, FirstSeen and CreatedAt are kept. isNew reports an insert.
func (s *Store) UpsertSignal(ctx context.Context, sig domain.Signal) (domain.Signal, bool, error) {
	var out domain.Signal
	var isNew bool
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, err := queryOne[domain.Signal](ctx, tx,
			fmt.Sprintf("signal %s/%s/%s", sig.Project, sig.Source, sig.Fingerprint),
			`SELECT data FROM signals WHERE project = ? AND source = ? AND fingerprint = ?`,
			sig.Project, sig.Source, sig.Fingerprint)
		switch {
		case errors.Is(err, store.ErrNotFound):
			isNew = true
			now := nowUTC()
			cur = sig
			if cur.ID == "" {
				cur.ID = newID()
			}
			if cur.CreatedAt.IsZero() {
				cur.CreatedAt = now
			}
			cur.UpdatedAt = now
			if err := signals.insert(ctx, tx, cur); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			cur.Count = sig.Count
			cur.LastSeen = sig.LastSeen
			cur.Title = sig.Title
			cur.URL = sig.URL
			cur.Level = sig.Level
			cur.Culprit = sig.Culprit
			cur.Release = sig.Release
			cur.UpdatedAt = nowUTC()
			if err := signals.save(ctx, tx, cur); err != nil {
				return err
			}
		}
		out = cur
		return nil
	})
	if err != nil {
		return domain.Signal{}, false, fmt.Errorf("upsert signal: %w", err)
	}
	return out, isNew, nil
}

// ListSignals returns project's signals most recently updated first (all projects when "").
func (s *Store) ListSignals(ctx context.Context, project string, limit int) ([]domain.Signal, error) {
	var c cond
	if project != "" {
		c.add("project = ?", project)
	}
	args := append(c.args, limitOr(limit, 200))
	out, err := queryDocs[domain.Signal](ctx, s.db,
		"SELECT data FROM signals"+c.where()+" ORDER BY updated_at DESC, id DESC LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("list signals: %w", err)
	}
	return out, nil
}

// UpdateSignal applies fn to the signal with id in one transaction.
func (s *Store) UpdateSignal(ctx context.Context, id string, fn func(*domain.Signal) error) (domain.Signal, error) {
	return signals.update(ctx, s.db, id, fn)
}

// UpsertFriction inserts f with Count at least 1, or increments the Count of the friction with the
// same project and fingerprint. isNew reports an insert. IssueURL and the rest of the stored
// friction are kept.
func (s *Store) UpsertFriction(ctx context.Context, f domain.Friction) (domain.Friction, bool, error) {
	var out domain.Friction
	var isNew bool
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		cur, err := queryOne[domain.Friction](ctx, tx,
			fmt.Sprintf("friction %s/%s", f.Project, f.Fingerprint),
			`SELECT data FROM friction WHERE project = ? AND fingerprint = ?`,
			f.Project, f.Fingerprint)
		switch {
		case errors.Is(err, store.ErrNotFound):
			isNew = true
			now := nowUTC()
			cur = f
			if cur.ID == "" {
				cur.ID = newID()
			}
			if cur.CreatedAt.IsZero() {
				cur.CreatedAt = now
			}
			cur.UpdatedAt = now
			cur.Count = max(1, f.Count)
			if err := friction.insert(ctx, tx, cur); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			cur.Count++
			cur.UpdatedAt = nowUTC()
			if err := friction.save(ctx, tx, cur); err != nil {
				return err
			}
		}
		out = cur
		return nil
	})
	if err != nil {
		return domain.Friction{}, false, fmt.Errorf("upsert friction: %w", err)
	}
	return out, isNew, nil
}

// ListFriction returns project's friction most recently updated first (all projects when "").
func (s *Store) ListFriction(ctx context.Context, project string, limit int) ([]domain.Friction, error) {
	var c cond
	if project != "" {
		c.add("project = ?", project)
	}
	args := append(c.args, limitOr(limit, 200))
	out, err := queryDocs[domain.Friction](ctx, s.db,
		"SELECT data FROM friction"+c.where()+" ORDER BY updated_at DESC, id DESC LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("list friction: %w", err)
	}
	return out, nil
}

// UpdateFriction applies fn to the friction with id in one transaction.
func (s *Store) UpdateFriction(ctx context.Context, id string, fn func(*domain.Friction) error) (domain.Friction, error) {
	return friction.update(ctx, s.db, id, fn)
}
