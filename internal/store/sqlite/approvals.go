package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/bobbyrc/onett/internal/domain"
)

var approvals = entity[domain.Approval]{
	name:  "approval",
	table: "approvals",
	id:    func(a *domain.Approval) *string { return &a.ID },
	cols: func(a domain.Approval) []col {
		return []col{
			{"id", a.ID},
			{"status", string(a.Status)},
			{"task_id", a.TaskID},
			{"created_at", nanos(a.CreatedAt)},
		}
	},
}

var deployments = entity[domain.Deployment]{
	name:  "deployment",
	table: "deployments",
	id:    func(d *domain.Deployment) *string { return &d.ID },
	stamp: func(d *domain.Deployment, now time.Time) { d.UpdatedAt = now },
	cols: func(d domain.Deployment) []col {
		return []col{
			{"id", d.ID},
			{"project", d.Project},
			{"state", string(d.State)},
			{"started_at", nanos(d.StartedAt)},
		}
	},
}

// CreateApproval stores a, assigning ID and CreatedAt when empty.
func (s *Store) CreateApproval(ctx context.Context, a domain.Approval) (domain.Approval, error) {
	if a.ID == "" {
		a.ID = newID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = nowUTC()
	}
	if err := approvals.insert(ctx, s.db, a); err != nil {
		return domain.Approval{}, fmt.Errorf("create approval: %w", err)
	}
	return a, nil
}

// GetApproval returns the approval with id, or ErrNotFound.
func (s *Store) GetApproval(ctx context.Context, id string) (domain.Approval, error) {
	return approvals.get(ctx, s.db, id)
}

// ListApprovals returns approvals with the given status, or all when status is "", newest first.
func (s *Store) ListApprovals(ctx context.Context, status domain.ApprovalStatus) ([]domain.Approval, error) {
	var c cond
	if status != "" {
		c.add("status = ?", string(status))
	}
	out, err := queryDocs[domain.Approval](ctx, s.db,
		"SELECT data FROM approvals"+c.where()+" ORDER BY created_at DESC, id DESC", c.args...)
	if err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	return out, nil
}

// UpdateApproval applies fn to the approval with id in one transaction.
func (s *Store) UpdateApproval(ctx context.Context, id string, fn func(*domain.Approval) error) (domain.Approval, error) {
	return approvals.update(ctx, s.db, id, fn)
}

// CreateDeployment stores d, assigning ID, StartedAt and UpdatedAt when empty.
func (s *Store) CreateDeployment(ctx context.Context, d domain.Deployment) (domain.Deployment, error) {
	if d.ID == "" {
		d.ID = newID()
	}
	now := nowUTC()
	if d.StartedAt.IsZero() {
		d.StartedAt = now
	}
	if d.UpdatedAt.IsZero() {
		d.UpdatedAt = now
	}
	if err := deployments.insert(ctx, s.db, d); err != nil {
		return domain.Deployment{}, fmt.Errorf("create deployment: %w", err)
	}
	return d, nil
}

// GetDeployment returns the deployment with id, or ErrNotFound.
func (s *Store) GetDeployment(ctx context.Context, id string) (domain.Deployment, error) {
	return deployments.get(ctx, s.db, id)
}

// ListDeployments returns project's deployments newest first (all projects when project is "").
func (s *Store) ListDeployments(ctx context.Context, project string, limit int) ([]domain.Deployment, error) {
	var c cond
	if project != "" {
		c.add("project = ?", project)
	}
	args := append(c.args, limitOr(limit, 200))
	out, err := queryDocs[domain.Deployment](ctx, s.db,
		"SELECT data FROM deployments"+c.where()+" ORDER BY started_at DESC, id DESC LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	return out, nil
}

// UpdateDeployment applies fn to the deployment with id in one transaction.
func (s *Store) UpdateDeployment(ctx context.Context, id string, fn func(*domain.Deployment) error) (domain.Deployment, error) {
	return deployments.update(ctx, s.db, id, fn)
}

// LastHealthyDeployment returns the newest healthy deployment of project, or ErrNotFound.
func (s *Store) LastHealthyDeployment(ctx context.Context, project string) (domain.Deployment, error) {
	return queryOne[domain.Deployment](ctx, s.db,
		fmt.Sprintf("healthy deployment of %s", project),
		`SELECT data FROM deployments WHERE project = ? AND state = ?
		 ORDER BY started_at DESC, id DESC LIMIT 1`, project, string(domain.DeployHealthy))
}
