package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/scm"
)

// frictionLoop files a GitHub issue for each friction report that has reached its threshold and
// has no issue yet. An existing issue with the same title is linked instead of duplicated.
func (e *Engine) frictionLoop(ctx context.Context, p *config.Project) error {
	loops := e.Config().LoopsFor(p.Name)
	if !isGitHubRepo(p) || !loops.FrictionAutoFile() {
		return nil
	}
	s, err := e.d.SCMFor(p)
	if err != nil {
		return fmt.Errorf("friction: scm: %w", err)
	}
	items, err := e.d.Store.ListFriction(ctx, p.Name, 200)
	if err != nil {
		return fmt.Errorf("friction: list: %w", err)
	}
	var errs []error
	for _, f := range items {
		if f.IssueURL != "" || f.Count < loops.Friction.MinCount {
			continue
		}
		if err := e.fileFriction(ctx, s, p, f, loops); err != nil {
			errs = append(errs, fmt.Errorf("friction %s: %w", f.ID, err))
		}
	}
	return errors.Join(errs...)
}

// fileFriction links friction f to an issue with the same title, or creates one, and records
// the issue URL on f.
func (e *Engine) fileFriction(ctx context.Context, s scm.SCM, p *config.Project, f domain.Friction, loops config.Loops) error {
	matches, err := s.SearchIssues(ctx, f.Title)
	if err != nil {
		return fmt.Errorf("search issues: %w", err)
	}
	url := ""
	for _, m := range matches {
		if strings.EqualFold(m.Title, f.Title) {
			url = m.URL
			break
		}
	}
	if url == "" {
		labels := []string{loops.Friction.Label}
		if loops.Friction.AutoQueue {
			labels = append(labels, loops.IssueIntake.Label)
		}
		body := f.Body + fmt.Sprintf("\n\n---\nReported by foundry agents %d time(s); first in run `%s`.", f.Count, f.RunID)
		issue, err := s.CreateIssue(ctx, scm.IssueSpec{Title: f.Title, Body: body, Labels: labels})
		if err != nil {
			return fmt.Errorf("create issue: %w", err)
		}
		url = issue.URL
		e.d.Bus.Emit(ctx, domain.EvIssueFiled, p.Name, "", "",
			fmt.Sprintf("filed issue #%d: %s", issue.Number, f.Title),
			map[string]any{"number": issue.Number, "url": issue.URL, "title": f.Title})
	}
	_, err = e.d.Store.UpdateFriction(ctx, f.ID, func(x *domain.Friction) error {
		x.IssueURL = url
		return nil
	})
	return err
}
