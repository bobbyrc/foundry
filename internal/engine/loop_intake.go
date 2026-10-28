package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bobbyrc/onlet/internal/config"
	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/scm"
	"github.com/bobbyrc/onlet/internal/store"
)

// intakeLoop turns open GitHub issues carrying the intake label into tasks, once each.
func (e *Engine) intakeLoop(ctx context.Context, p *config.Project) error {
	if !isGitHubRepo(p) {
		return nil
	}
	label := e.Config().LoopsFor(p.Name).IssueIntake.Label
	s, err := e.d.SCMFor(p)
	if err != nil {
		return fmt.Errorf("issue intake: scm: %w", err)
	}
	issues, err := s.ListIssues(ctx, label, "open")
	if err != nil {
		return fmt.Errorf("issue intake: list issues: %w", err)
	}

	var errs []error
	for _, is := range issues {
		ref := fmt.Sprintf("github_issue:%d", is.Number)
		_, err := e.d.Store.FindTaskBySourceRef(ctx, p.Name, ref)
		switch {
		case err == nil:
			continue
		case !errors.Is(err, store.ErrNotFound):
			errs = append(errs, fmt.Errorf("issue #%d: %w", is.Number, err))
			continue
		}

		body := fmt.Sprintf("%s%s\n\n---\nGitHub issue #%d: %s", is.Body, trustedComments(is.Comments), is.Number, is.URL)
		t, err := e.createTask(ctx, domain.Task{
			Project:   p.Name,
			Kind:      domain.KindIssue,
			Title:     is.Title,
			Body:      body,
			Source:    "github_issue",
			SourceRef: ref,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("issue #%d: %w", is.Number, err))
			continue
		}
		msg := fmt.Sprintf("Onlet queued this as task `%s`. It will open a pull request that fixes this issue, or explain here why not.", t.ID)
		if err := s.CommentIssue(ctx, is.Number, msg); err != nil {
			e.d.Logger.Warn("comment on issue", "project", p.Name, "issue", is.Number, "err", err)
		}
	}
	return errors.Join(errs...)
}

// trustedComments renders the comments written by someone with write access to the repo
// (OWNER, MEMBER, COLLABORATOR); everyone else's comments are left out because the repo may
// be public. It returns "" when there are none.
func trustedComments(comments []scm.IssueComment) string {
	var b strings.Builder
	for _, c := range comments {
		switch c.Association {
		case "OWNER", "MEMBER", "COLLABORATOR":
		default:
			continue
		}
		if b.Len() == 0 {
			b.WriteString("\n\nComments from maintainers:")
		}
		fmt.Fprintf(&b, "\n\nComment by @%s (%s, %s):\n%s", c.Author, c.Association,
			c.CreatedAt.Format(time.RFC3339), c.Body)
	}
	return b.String()
}
