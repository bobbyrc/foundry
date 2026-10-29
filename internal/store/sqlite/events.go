package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// AppendEvent stores e, assigning Time when zero, and returns it with ID set.
func (s *Store) AppendEvent(ctx context.Context, e domain.Event) (domain.Event, error) {
	if e.Time.IsZero() {
		e.Time = nowUTC()
	}
	var data any // NULL when there is no payload
	if len(e.Data) > 0 {
		data = string(e.Data)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO events (time, type, project, task_id, run_id, message, data)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		nanos(e.Time), e.Type, e.Project, e.TaskID, e.RunID, e.Message, data)
	if err != nil {
		return domain.Event{}, fmt.Errorf("append event %s: %w", e.Type, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Event{}, fmt.Errorf("append event %s: last id: %w", e.Type, err)
	}
	e.ID = id
	return e, nil
}

// PruneEvents deletes events whose time is before before and returns how many it deleted.
func (s *Store) PruneEvents(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE time < ?`, nanos(before))
	if err != nil {
		return 0, fmt.Errorf("prune events: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune events: rows affected: %w", err)
	}
	return n, nil
}

const eventColumns = `id, time, type, project, task_id, run_id, message, data`

func scanEvent(r rowScanner) (domain.Event, error) {
	var e domain.Event
	var t int64
	var data sql.NullString
	if err := r.Scan(&e.ID, &t, &e.Type, &e.Project, &e.TaskID, &e.RunID, &e.Message, &data); err != nil {
		return e, err
	}
	e.Time = fromNanos(t)
	if data.Valid {
		e.Data = []byte(data.String)
	}
	return e, nil
}

// ListEvents returns events matching f. With AfterID > 0 (or FromStart) it returns the next events in
// ascending order; otherwise the newest Limit events, still ascending.
func (s *Store) ListEvents(ctx context.Context, f store.EventFilter) ([]domain.Event, error) {
	var c cond
	if f.Project != "" {
		c.add("project = ?", f.Project)
	}
	if f.TaskID != "" {
		c.add("task_id = ?", f.TaskID)
	}
	limit := limitOr(f.Limit, 500)
	var query string
	if f.AfterID > 0 || f.FromStart {
		c.add("id > ?", f.AfterID)
		query = "SELECT " + eventColumns + " FROM events" + c.where() +
			" ORDER BY id ASC LIMIT ?"
	} else {
		query = "SELECT * FROM (SELECT " + eventColumns + " FROM events" + c.where() +
			" ORDER BY id DESC LIMIT ?) ORDER BY id ASC"
	}
	out, err := queryRows(ctx, s.db, scanEvent, query, append(c.args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return out, nil
}
