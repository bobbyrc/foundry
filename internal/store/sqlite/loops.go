package sqlite

import (
	"context"
	"errors"
	"fmt"

	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/store"
)

// GetLoopState returns the stored state of a loop, or a zero state with Name and Project set when
// none is stored.
func (s *Store) GetLoopState(ctx context.Context, name, project string) (domain.LoopState, error) {
	st, err := queryOne[domain.LoopState](ctx, s.db,
		fmt.Sprintf("loop state %s/%s", name, project),
		`SELECT data FROM loop_state WHERE name = ? AND project = ?`, name, project)
	if errors.Is(err, store.ErrNotFound) {
		return domain.LoopState{Name: name, Project: project}, nil
	}
	if err != nil {
		return domain.LoopState{}, err
	}
	return st, nil
}

// SaveLoopState stores s, replacing any state with the same name and project.
func (s *Store) SaveLoopState(ctx context.Context, st domain.LoopState) error {
	err := saveRow(ctx, s.db, "loop_state", []string{"name", "project"}, st,
		[]col{{"name", st.Name}, {"project", st.Project}})
	if err != nil {
		return fmt.Errorf("save loop state %s/%s: %w", st.Name, st.Project, err)
	}
	return nil
}

// ListLoopStates returns every stored loop state ordered by name, then project.
func (s *Store) ListLoopStates(ctx context.Context) ([]domain.LoopState, error) {
	out, err := queryDocs[domain.LoopState](ctx, s.db,
		`SELECT data FROM loop_state ORDER BY name, project`)
	if err != nil {
		return nil, fmt.Errorf("list loop states: %w", err)
	}
	return out, nil
}
