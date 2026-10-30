package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

var runs = entity[domain.Run]{
	name:  "run",
	table: "runs",
	id:    func(r *domain.Run) *string { return &r.ID },
	cols: func(r domain.Run) []col {
		return []col{
			{"id", r.ID},
			{"task_id", r.TaskID},
			{"project", r.Project},
			{"status", string(r.Status)},
			{"started_at", nanos(r.StartedAt)},
		}
	},
}

// CreateRun stores r, assigning ID and StartedAt when empty.
func (s *Store) CreateRun(ctx context.Context, r domain.Run) (domain.Run, error) {
	if r.ID == "" {
		r.ID = newID()
	}
	if r.StartedAt.IsZero() {
		r.StartedAt = nowUTC()
	}
	if err := runs.insert(ctx, s.db, r); err != nil {
		return domain.Run{}, fmt.Errorf("create run: %w", err)
	}
	return r, nil
}

// GetRun returns the run with id, or ErrNotFound.
func (s *Store) GetRun(ctx context.Context, id string) (domain.Run, error) {
	return runs.get(ctx, s.db, id)
}

// ListRuns returns runs matching f, newest first.
func (s *Store) ListRuns(ctx context.Context, f store.RunFilter) ([]domain.Run, error) {
	var c cond
	if f.TaskID != "" {
		c.add("task_id = ?", f.TaskID)
	}
	if f.Project != "" {
		c.add("project = ?", f.Project)
	}
	if len(f.Statuses) > 0 {
		c.in("status", false, toAny(f.Statuses))
	}
	args := append(c.args, limitOr(f.Limit, 200))
	out, err := queryDocs[domain.Run](ctx, s.db,
		"SELECT data FROM runs"+c.where()+" ORDER BY started_at DESC, id DESC LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	return out, nil
}

// UpdateRun applies fn to the run with id in one transaction and returns the saved run.
func (s *Store) UpdateRun(ctx context.Context, id string, fn func(*domain.Run) error) (domain.Run, error) {
	return runs.update(ctx, s.db, id, fn)
}

// AppendRunLogs stores lines after the run's current last sequence number and returns them with
// Seq, RunID and (when zero) Time set.
func (s *Store) AppendRunLogs(ctx context.Context, runID string, lines []domain.LogLine) ([]domain.LogLine, error) {
	out := make([]domain.LogLine, 0, len(lines))
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		var seq int64
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(seq), 0) FROM run_logs WHERE run_id = ?`, runID).Scan(&seq); err != nil {
			return fmt.Errorf("read last seq: %w", err)
		}
		for _, l := range lines {
			seq++
			l.RunID = runID
			l.Seq = seq
			if l.Time.IsZero() {
				l.Time = nowUTC()
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO run_logs (run_id, seq, time, kind, text, tool_id) VALUES (?, ?, ?, ?, ?, ?)`,
				runID, l.Seq, nanos(l.Time), string(l.Kind), l.Text, l.ToolID); err != nil {
				return fmt.Errorf("insert log: %w", err)
			}
			out = append(out, l)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("append run logs %s: %w", runID, err)
	}
	return out, nil
}

// PruneRunLogs deletes run log lines whose time is before before and returns how many it deleted.
func (s *Store) PruneRunLogs(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM run_logs WHERE time < ?`, nanos(before))
	if err != nil {
		return 0, fmt.Errorf("prune run logs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune run logs: rows affected: %w", err)
	}
	return n, nil
}

// ListRunLogs returns the run's lines with Seq > afterSeq in ascending order.
func (s *Store) ListRunLogs(ctx context.Context, runID string, afterSeq int64, limit int) ([]domain.LogLine, error) {
	out, err := queryRows(ctx, s.db, func(r rowScanner) (domain.LogLine, error) {
		var l domain.LogLine
		var kind string
		var t int64
		if err := r.Scan(&l.RunID, &l.Seq, &t, &kind, &l.Text, &l.ToolID); err != nil {
			return l, err
		}
		l.Time = fromNanos(t)
		l.Kind = domain.LogKind(kind)
		return l, nil
	}, `SELECT run_id, seq, time, kind, text, tool_id FROM run_logs
	    WHERE run_id = ? AND seq > ? ORDER BY seq ASC LIMIT ?`, runID, afterSeq, limitOr(limit, 1000))
	if err != nil {
		return nil, fmt.Errorf("list run logs %s: %w", runID, err)
	}
	return out, nil
}

// CostSince sums CostUSD of runs started at or after since. CostUSD lives in the JSON data column;
// runs with no reported cost count as zero.
func (s *Store) CostSince(ctx context.Context, since time.Time) (float64, error) {
	var total float64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(json_extract(data, '$.cost_usd')), 0) FROM runs WHERE started_at >= ?`,
		nanos(since)).Scan(&total); err != nil {
		return 0, fmt.Errorf("cost since: %w", err)
	}
	return total, nil
}
