package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/store"
)

var tasks = entity[domain.Task]{
	name:  "task",
	table: "tasks",
	id:    func(t *domain.Task) *string { return &t.ID },
	stamp: func(t *domain.Task, now time.Time) { t.UpdatedAt = now },
	cols: func(t domain.Task) []col {
		return []col{
			{"id", t.ID},
			{"project", t.Project},
			{"status", string(t.Status)},
			{"kind", string(t.Kind)},
			{"parent_id", t.ParentID},
			{"source_ref", t.SourceRef},
			{"priority", t.Priority},
			{"created_at", nanos(t.CreatedAt)},
		}
	},
}

// CreateTask stores t, assigning ID and timestamps when empty.
func (s *Store) CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	if t.ID == "" {
		t.ID = newID()
	}
	now := nowUTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	if err := tasks.insert(ctx, s.db, t); err != nil {
		return domain.Task{}, fmt.Errorf("create task: %w", err)
	}
	return t, nil
}

// GetTask returns the task with id, or ErrNotFound.
func (s *Store) GetTask(ctx context.Context, id string) (domain.Task, error) {
	return tasks.get(ctx, s.db, id)
}

// FindTaskBySourceRef returns the newest task in project with that source_ref, or ErrNotFound.
func (s *Store) FindTaskBySourceRef(ctx context.Context, project, sourceRef string) (domain.Task, error) {
	return queryOne[domain.Task](ctx, s.db,
		fmt.Sprintf("task %s/%s", project, sourceRef),
		`SELECT data FROM tasks WHERE project = ? AND source_ref = ?
		 ORDER BY created_at DESC, id DESC LIMIT 1`, project, sourceRef)
}

// ListTasks returns tasks matching f, newest first.
func (s *Store) ListTasks(ctx context.Context, f store.TaskFilter) ([]domain.Task, error) {
	var c cond
	if f.Project != "" {
		c.add("project = ?", f.Project)
	}
	if len(f.Statuses) > 0 {
		c.in("status", false, toAny(f.Statuses))
	}
	if f.Kind != "" {
		c.add("kind = ?", string(f.Kind))
	}
	if f.ParentID != "" {
		c.add("parent_id = ?", f.ParentID)
	}
	args := append(c.args, limitOr(f.Limit, 200))
	out, err := queryDocs[domain.Task](ctx, s.db,
		"SELECT data FROM tasks"+c.where()+" ORDER BY created_at DESC, id DESC LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return out, nil
}

// UpdateTask applies fn to the task with id in one transaction and returns the saved task.
func (s *Store) UpdateTask(ctx context.Context, id string, fn func(*domain.Task) error) (domain.Task, error) {
	return tasks.update(ctx, s.db, id, fn)
}

// ClaimNextTask moves the highest-priority, oldest queued task outside busyProjects to running,
// increments its Attempts, and returns it. ErrNotFound when no task is eligible.
func (s *Store) ClaimNextTask(ctx context.Context, busyProjects []string) (domain.Task, error) {
	var out domain.Task
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		var c cond
		c.add("status = ?", string(domain.TaskQueued))
		if len(busyProjects) > 0 {
			c.in("project", true, toAny(busyProjects))
		}
		t, err := queryOne[domain.Task](ctx, tx, "queued task",
			"SELECT data FROM tasks"+c.where()+
				" ORDER BY priority DESC, created_at ASC, id ASC LIMIT 1", c.args...)
		if err != nil {
			return err
		}
		t.Status = domain.TaskRunning
		t.Attempts++
		t.UpdatedAt = nowUTC()
		if err := tasks.save(ctx, tx, t); err != nil {
			return err
		}
		out = t
		return nil
	})
	if err != nil {
		return domain.Task{}, fmt.Errorf("claim task: %w", err)
	}
	return out, nil
}
