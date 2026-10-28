package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/store"
)

// retroSectionLimit caps each evidence section at its newest items.
const retroSectionLimit = 15

// retroScanLimit is how many tasks and friction reports the retro loop reads before it filters
// them by time. Older rows beyond it are not considered.
const retroScanLimit = 1000

// retroReasonLimit caps a blocked reason, in characters.
const retroReasonLimit = 300

// retroInstruction opens every retrospective task body. The evidence follows it.
const retroInstruction = "Look at the evidence below. Fix the single most valuable underlying " +
	"problem (root cause, small and safe), and file followups for the rest. Consolidate duplicate " +
	"friction issues: for issues that describe the same problem, name the pair (the one to keep and " +
	"the duplicate) in your summary; do not call the GitHub API, because foundry closes nothing " +
	"itself. If nothing is worth changing, report no_changes and say why."

// retroLoop once a day gives the improver agent the evidence of what slowed the factory down
// over the look-back window, as one improve task. The code only gathers facts; the agent decides
// what matters. It creates nothing when there is no evidence or today's task already exists.
func (e *Engine) retroLoop(ctx context.Context, p *config.Project) error {
	now := e.d.Now()
	day := now.Local().Format("2006-01-02")
	ref := "retro:" + p.Name + ":" + day
	_, err := e.d.Store.FindTaskBySourceRef(ctx, p.Name, ref)
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("retro: find task: %w", err)
	}

	loops := e.Config().LoopsFor(p.Name)
	lookback := loops.Retro.Lookback.Duration
	if lookback == 0 {
		lookback = loops.Retro.Every.Duration
	}
	evidence, err := e.retroEvidence(ctx, p.Name, now.Add(-lookback))
	if err != nil {
		return fmt.Errorf("retro: evidence: %w", err)
	}
	if evidence == "" {
		return nil
	}

	_, err = e.createTask(ctx, domain.Task{
		Project:   p.Name,
		Kind:      domain.KindImprove,
		Title:     "Retrospective " + day + ": what slowed the factory down",
		Body:      retroInstruction + "\n\n" + evidence,
		Source:    "retro",
		SourceRef: ref,
		Priority:  -1,
	})
	return err
}

// retroEvidence builds the Markdown evidence sections for project since the given time. It returns
// "" when every section is empty.
func (e *Engine) retroEvidence(ctx context.Context, project string, since time.Time) (string, error) {
	tasks, err := e.d.Store.ListTasks(ctx, store.TaskFilter{Project: project, Limit: retroScanLimit})
	if err != nil {
		return "", fmt.Errorf("list tasks: %w", err)
	}
	friction, err := e.d.Store.ListFriction(ctx, project, retroScanLimit)
	if err != nil {
		return "", fmt.Errorf("list friction: %w", err)
	}
	reviews, err := e.retroReviewFindings(ctx, tasks, since)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	section := func(heading string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", heading, strings.Join(items, "\n"))
	}
	section("Blocked or failed tasks", retroBlocked(tasks, since))
	section("Re-queued tasks", retroRequeued(tasks, since))
	section("Friction", retroFriction(friction, since))
	section("Incidents", retroIncidents(tasks, since))
	section("Review findings sent back", reviews)
	if b.Len() == 0 {
		return "", nil
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

// retroBlocked lists blocked and failed tasks updated since the given time, newest first.
func retroBlocked(tasks []domain.Task, since time.Time) []string {
	var picked []domain.Task
	for _, t := range tasks {
		if (t.Status == domain.TaskBlocked || t.Status == domain.TaskFailed) && !t.UpdatedAt.Before(since) {
			picked = append(picked, t)
		}
	}
	sortNewest(picked, func(t domain.Task) time.Time { return t.UpdatedAt })
	var out []string
	for _, t := range capped(picked) {
		line := fmt.Sprintf("- %s (id %s, status %s)", t.Title, t.ID, t.Status)
		if reason := retroOneLine(t.BlockedReason, retroReasonLimit); reason != "" {
			line += "\n  - blocked_reason: " + reason
		}
		out = append(out, line)
	}
	return out
}

// retroRequeued lists tasks re-queued since the given time, with the headings of their attempts.
func retroRequeued(tasks []domain.Task, since time.Time) []string {
	var picked []domain.Task
	for _, t := range tasks {
		if strings.Contains(t.Body, "## Attempt") && !t.UpdatedAt.Before(since) {
			picked = append(picked, t)
		}
	}
	sortNewest(picked, func(t domain.Task) time.Time { return t.UpdatedAt })
	var out []string
	for _, t := range capped(picked) {
		line := fmt.Sprintf("- %s (id %s)", t.Title, t.ID)
		for _, l := range strings.Split(t.Body, "\n") {
			if strings.HasPrefix(l, "## Attempt") {
				line += "\n  - " + strings.TrimSpace(strings.TrimPrefix(l, "##"))
			}
		}
		out = append(out, line)
	}
	return out
}

// retroFriction lists friction reports updated since the given time, newest first.
func retroFriction(friction []domain.Friction, since time.Time) []string {
	var picked []domain.Friction
	for _, f := range friction {
		if !f.UpdatedAt.Before(since) {
			picked = append(picked, f)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].UpdatedAt.After(picked[j].UpdatedAt) })
	var out []string
	for _, f := range capped(picked) {
		issue := f.IssueURL
		if issue == "" {
			issue = "none"
		}
		out = append(out, fmt.Sprintf("- %s (count %d, issue %s)", f.Title, f.Count, issue))
	}
	return out
}

// retroIncidents lists incident tasks created since the given time, newest first.
func retroIncidents(tasks []domain.Task, since time.Time) []string {
	var picked []domain.Task
	for _, t := range tasks {
		if t.Kind == domain.KindIncident && !t.CreatedAt.Before(since) {
			picked = append(picked, t)
		}
	}
	sortNewest(picked, func(t domain.Task) time.Time { return t.CreatedAt })
	var out []string
	for _, t := range capped(picked) {
		out = append(out, fmt.Sprintf("- %s (status %s)", t.Title, t.Status))
	}
	return out
}

// retroReviewFindings lists review tasks done since the given time whose parent was re-queued
// because the reviewer requested changes, naming the parent. Parents are read from the store.
func (e *Engine) retroReviewFindings(ctx context.Context, tasks []domain.Task, since time.Time) ([]string, error) {
	var picked []domain.Task
	for _, t := range tasks {
		if t.Kind == domain.KindReview && t.Status == domain.TaskDone && t.ParentID != "" && !t.UpdatedAt.Before(since) {
			picked = append(picked, t)
		}
	}
	sortNewest(picked, func(t domain.Task) time.Time { return t.UpdatedAt })
	var out []string
	seen := map[string]bool{}
	for _, r := range picked {
		if len(out) == retroSectionLimit {
			break
		}
		// A parent gets one review per PR head; picked is newest first, so the first wins.
		if seen[r.ParentID] {
			continue
		}
		seen[r.ParentID] = true
		parent, err := e.d.Store.GetTask(ctx, r.ParentID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("get parent task: %w", err)
		}
		if strings.Contains(parent.Body, "reviewer requested changes") {
			out = append(out, fmt.Sprintf("- %s (id %s)", parent.Title, parent.ID))
		}
	}
	return out, nil
}

// sortNewest sorts items by their time, newest first.
func sortNewest[T any](items []T, at func(T) time.Time) {
	sort.SliceStable(items, func(i, j int) bool { return at(items[i]).After(at(items[j])) })
}

// capped returns at most retroSectionLimit items from the front of items.
func capped[T any](items []T) []T {
	if len(items) > retroSectionLimit {
		return items[:retroSectionLimit]
	}
	return items
}

// retroOneLine collapses whitespace in s and clips it to limit characters.
func retroOneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "..."
}
