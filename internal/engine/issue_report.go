package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/bobbyrc/onlet/internal/domain"
)

// issueReportMax caps the free text (summary or reason) copied into an issue comment.
const issueReportMax = 3000

// reportToIssue tells the GitHub issue behind an intake task how the task ended, when the task
// ended without a pull request. A task with a PR reports through the PR, whose "Fixes #N" closes
// the issue on merge. The comment is posted once per task and status: LoopState
// "issue_report:<task id>" keeps the status last reported. Failures are logged, never returned.
// It runs in its own goroutine (see setTask), so it never delays the status write.
func (e *Engine) reportToIssue(ctx context.Context, t domain.Task) {
	n, ok := issueNumberOf(t)
	if !ok || !t.Status.Terminal() || t.PRNumber != 0 {
		return
	}
	// Held from the re-read to the save, so a concurrent report of the same status sees the
	// cursor this one saves and does not post again. The lock is per task, so a slow comment on
	// one task does not delay reports for the others.
	mu, _ := e.issueReportLocks.LoadOrStore(t.ID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	// The status may have moved on since the write that started this goroutine.
	cur, err := e.d.Store.GetTask(ctx, t.ID)
	if err != nil {
		e.d.Logger.Error("issue report: read task", "task", t.ID, "err", err)
		return
	}
	if cur.Status != t.Status || cur.PRNumber != 0 {
		return
	}

	stateName := "issue_report:" + t.ID
	st, err := e.d.Store.GetLoopState(ctx, stateName, t.Project)
	if err != nil {
		e.d.Logger.Error("issue report: read state", "task", t.ID, "err", err)
		return
	}
	if st.Cursor == string(t.Status) {
		return
	}

	p, err := e.project(t.Project)
	if err != nil {
		e.d.Logger.Error("issue report", "task", t.ID, "err", err)
		return
	}
	s, err := e.d.SCMFor(p)
	if err != nil {
		e.d.Logger.Error("issue report: scm", "project", t.Project, "err", err)
		return
	}
	if err := s.CommentIssue(ctx, n, issueReportBody(t)); err != nil {
		e.d.Logger.Warn("issue report: comment", "project", t.Project, "issue", n, "err", err)
		return
	}

	st.Cursor = string(t.Status)
	if err := e.d.Store.SaveLoopState(ctx, st); err != nil {
		e.d.Logger.Error("issue report: save state", "task", t.ID, "err", err)
	}
}

// issueNumberOf returns the GitHub issue number of an intake task ("github_issue:<n>").
func issueNumberOf(t domain.Task) (int, bool) {
	rest, ok := strings.CutPrefix(t.SourceRef, "github_issue:")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil && n > 0
}

// issueReportBody is the comment for a terminal status without a PR.
func issueReportBody(t domain.Task) string {
	switch t.Status {
	case domain.TaskDone:
		return fmt.Sprintf("Onlet finished task `%s` without code changes:\n\n%s", t.ID, trimReport(t.Summary))
	case domain.TaskBlocked:
		return fmt.Sprintf("Onlet's task `%s` is blocked and needs the owner:\n\n%s", t.ID, trimReport(t.BlockedReason))
	case domain.TaskFailed:
		text := t.Summary
		if strings.TrimSpace(text) == "" {
			text = t.BlockedReason
		}
		return fmt.Sprintf("Onlet could not complete task `%s`:\n\n%s", t.ID, trimReport(text))
	default: // cancelled
		return fmt.Sprintf("The owner cancelled onlet's task `%s`.", t.ID)
	}
}

// trimReport returns s cut to issueReportMax characters, or a placeholder when s is blank.
func trimReport(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no details were recorded)"
	}
	if r := []rune(s); len(r) > issueReportMax {
		return string(r[:issueReportMax])
	}
	return s
}
