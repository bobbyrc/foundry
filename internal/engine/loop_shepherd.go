package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/bobbyrc/onlet/internal/config"
	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/notify"
	"github.com/bobbyrc/onlet/internal/policy"
	"github.com/bobbyrc/onlet/internal/scm"
	"github.com/bobbyrc/onlet/internal/store"
)

// shepherdStatuses are the statuses the shepherd works on. Every transition it makes is guarded
// by these, so a task that moved on while a network call was in flight is left alone.
var shepherdStatuses = []domain.TaskStatus{domain.TaskReview, domain.TaskAwaitingApproval}

// requireChecksLimit is how long a head may show no CI checks under require_checks before the
// task is blocked. Checks that never appear mean CI is not wired up, which a human must fix.
const requireChecksLimit = 30 * time.Minute

// notMergeableError is returned by mergeTask when the task is not waiting for a merge.
type notMergeableError struct{ status domain.TaskStatus }

func (e *notMergeableError) Error() string {
	return fmt.Sprintf("task is %s, not waiting for a merge", e.status)
}

// shepherdLoop moves each task in review or awaiting_approval through CI, review, policy and
// merge. Each task is handled on its own, and one task's error does not stop the others.
func (e *Engine) shepherdLoop(ctx context.Context, p *config.Project) error {
	s, err := e.d.SCMFor(p)
	if err != nil {
		return fmt.Errorf("pr shepherd: scm: %w", err)
	}
	tasks, err := e.d.Store.ListTasks(ctx, store.TaskFilter{
		Project:  p.Name,
		Statuses: shepherdStatuses,
		Limit:    1000,
	})
	if err != nil {
		return fmt.Errorf("pr shepherd: list tasks: %w", err)
	}
	var errs []error
	for _, t := range tasks {
		if err := e.shepherdTask(ctx, s, p, t); err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", t.ID, err))
		}
	}
	return errors.Join(errs...)
}

// freshShepherdTask re-reads a task after a network call. ok is false when the task is no longer
// in review or awaiting_approval, in which case the caller must stop.
func (e *Engine) freshShepherdTask(ctx context.Context, id string) (domain.Task, bool, error) {
	t, err := e.d.Store.GetTask(ctx, id)
	if err != nil {
		return t, false, fmt.Errorf("re-read task %s: %w", id, err)
	}
	return t, slices.Contains(shepherdStatuses, t.Status), nil
}

// shepherdTask advances one task. Every return without an error means the task was either
// left to wait, re-queued, blocked, merged or sent for approval.
func (e *Engine) shepherdTask(ctx context.Context, s scm.SCM, p *config.Project, t domain.Task) error {
	if t.PRNumber == 0 {
		e.blockTask(ctx, t.ID, "in review without a pull request")
		return nil
	}
	pr, err := s.GetPR(ctx, t.PRNumber)
	if err != nil {
		return fmt.Errorf("get PR #%d: %w", t.PRNumber, err)
	}
	t, ok, err := e.freshShepherdTask(ctx, t.ID)
	if err != nil || !ok {
		return err
	}

	switch pr.State {
	case "merged":
		applied, err := e.markMerged(ctx, p, t, pr.MergeSHA, "merged outside onlet")
		if err != nil || !applied {
			return err
		}
		e.decidePendingApprovals(ctx, t.ID, domain.ApprovalApproved, "merged outside onlet")
		return nil
	case "closed":
		_, applied, err := e.setTaskFrom(ctx, t.ID, shepherdStatuses, "PR closed outside onlet", func(x *domain.Task) error {
			x.Status = domain.TaskCancelled
			return nil
		})
		if err != nil || !applied {
			return err
		}
		e.decidePendingApprovals(ctx, t.ID, domain.ApprovalRejected, "PR closed outside onlet")
		return nil
	}

	// Without a head commit nothing below can be pinned to what was judged, so never act on it.
	if pr.HeadSHA == "" {
		e.d.Logger.Warn("PR has no head commit; task skipped this pass", "task", t.ID, "pr", t.PRNumber)
		return nil
	}

	// A new head starts its own CI grace period, whoever pushed it.
	if t.CIHeadSHA != pr.HeadSHA || t.CIHeadSeenAt == nil {
		seen := e.d.Now().UTC()
		saved, applied, err := e.setTaskFrom(ctx, t.ID, shepherdStatuses,
			"new PR head "+deployShortRef(pr.HeadSHA), func(x *domain.Task) error {
				x.CIHeadSHA = pr.HeadSHA
				x.CIHeadSeenAt = &seen
				return nil
			})
		if err != nil || !applied {
			return err
		}
		t = saved
	}

	// Awaiting approval: the human decides, not the loop. Only check that the approval still
	// applies to this PR head.
	if t.Status == domain.TaskAwaitingApproval {
		return e.checkAwaitingApproval(ctx, t, pr)
	}

	// GitHub is still computing whether the PR merges cleanly; judge it on a later pass.
	if pr.Mergeable == "unknown" {
		return nil
	}
	if pr.Mergeable == "conflicting" {
		e.requeueOrBlock(ctx, t.ID, "merge conflict with "+p.DefaultBranch,
			"Rebase the branch onto origin/"+p.DefaultBranch+", resolve the conflicts, and make the checks pass again.")
		return nil
	}

	// Judge the commit we were asked about, not whatever the branch points at by now.
	files, err := e.d.Workspaces.Diff(ctx, e.repoFor(p), p.DefaultBranch, pr.HeadSHA)
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}
	if t, ok, err = e.freshShepherdTask(ctx, t.ID); err != nil || !ok {
		return err
	}
	d := policy.Evaluate(e.Config().PolicyFor(p.Name), policy.Input{
		Kind:             t.Kind,
		Files:            files,
		AgentRisk:        t.Risk,
		AgentRiskReasons: t.RiskReasons,
		Labels:           pr.Labels,
		Source:           t.Source,
	})
	if d.Risk != t.Risk || !slices.Equal(d.Reasons, t.RiskReasons) {
		saved, applied, err := e.setTaskFrom(ctx, t.ID, shepherdStatuses, "risk "+string(d.Risk), func(x *domain.Task) error {
			x.Risk = d.Risk
			x.RiskReasons = d.Reasons
			return nil
		})
		if err != nil || !applied {
			return err
		}
		t = saved
		msg := "risk " + string(d.Risk)
		if len(d.Reasons) > 0 {
			msg += ": " + strings.Join(d.Reasons, "; ")
		}
		e.d.Bus.Emit(ctx, domain.EvPolicyDecision, p.Name, t.ID, "", msg, d)
	}

	if d.RequireCI {
		switch pr.CI {
		case scm.CIPending:
			return nil
		case scm.CINone:
			// GitHub often registers checks a few seconds after a push, so "no checks" on a head
			// is not yet "no CI" until the head has been visible for the grace period.
			pol := e.Config().PolicyFor(p.Name)
			seen := e.d.Now()
			if t.CIHeadSeenAt != nil {
				seen = *t.CIHeadSeenAt
			}
			waited := e.d.Now().Sub(seen)
			if pol.RequireChecks {
				if waited < requireChecksLimit {
					return nil
				}
				e.blockTask(ctx, t.ID, fmt.Sprintf("no CI checks reported for %s (require_checks is on)", pr.HeadSHA))
				return nil
			}
			if waited < pol.CIGrace.Duration {
				return nil
			}
		case scm.CIFailure:
			log, lerr := s.CILog(ctx, t.PRNumber, 8000)
			if lerr != nil {
				log = fmt.Sprintf("(could not fetch the CI log: %v)", lerr)
			}
			if _, ok, err := e.freshShepherdTask(ctx, t.ID); err != nil || !ok {
				return err
			}
			e.requeueOrBlock(ctx, t.ID, "CI failed: "+strings.Join(pr.FailingCI, ", "),
				"```\n"+log+"\n```")
			return nil
		}
	}

	if d.RequireReview && t.ApprovedSHA != pr.HeadSHA {
		return e.ensureReview(ctx, p, t, pr)
	}

	if d.AutoMerge {
		// CI, review and policy were judged on pr.HeadSHA; merge only that commit.
		err := e.mergeTask(ctx, t.ID, pr.HeadSHA)
		var notMergeable *notMergeableError
		switch {
		case errors.As(err, &notMergeable):
			return nil // the task moved on since the re-read; the next pass sees where it is
		case err != nil:
			return e.mergeRefused(ctx, s, t, pr.HeadSHA, err)
		}
		return nil
	}
	return e.requestMergeApproval(ctx, p, t, pr, d)
}

// mergeRefused handles a failed auto-merge at judged, the head the shepherd evaluated. A push to
// the PR after that evaluation is normal, and MergePR refuses the old head. In that case the task
// stays in review so the next pass judges the new head. Only a failure at an unchanged head blocks.
func (e *Engine) mergeRefused(ctx context.Context, s scm.SCM, t domain.Task, judged string, mergeErr error) error {
	pr, err := s.GetPR(ctx, t.PRNumber)
	if err != nil {
		e.d.Logger.Error("merge refused and PR could not be re-read; task stays in review",
			"task", t.ID, "pr", t.PRNumber, "merge_err", mergeErr, "err", err)
		return nil
	}
	if pr.HeadSHA != judged {
		e.d.Logger.Info("PR head moved before auto-merge; task stays in review for re-evaluation",
			"task", t.ID, "pr", t.PRNumber, "judged", judged, "head", pr.HeadSHA)
		return nil
	}
	e.blockTask(ctx, t.ID, "merge failed: "+mergeErr.Error())
	return nil
}

// ensureReview makes sure a reviewer run exists for the PR's current head commit, and waits for
// it. A review that ends without approving the head blocks the task, because nothing else would
// move it on. A review that was cancelled or failed is not a verdict: a new one is started.
func (e *Engine) ensureReview(ctx context.Context, p *config.Project, t domain.Task, pr scm.PR) error {
	ref := "review:" + t.ID + ":" + pr.HeadSHA
	children, err := e.d.Store.ListTasks(ctx, store.TaskFilter{
		Project:  p.Name,
		ParentID: t.ID,
		Kind:     domain.KindReview,
		Limit:    1000,
	})
	if err != nil {
		return fmt.Errorf("list reviews: %w", err)
	}
	for _, c := range children {
		if c.SourceRef != ref {
			continue
		}
		if c.Status == domain.TaskCancelled || c.Status == domain.TaskFailed {
			continue
		}
		switch {
		case c.Status == domain.TaskBlocked:
			e.blockTask(ctx, t.ID, "review could not complete: "+c.BlockedReason)
		case !c.Status.Terminal():
			// The reviewer is still running.
		default:
			e.blockTask(ctx, t.ID, fmt.Sprintf("review of %s ended as %s without approving it", pr.HeadSHA, c.Status))
		}
		return nil
	}
	_, err = e.createTask(ctx, domain.Task{
		Project:   p.Name,
		Kind:      domain.KindReview,
		Title:     "Review: " + t.Title,
		ParentID:  t.ID,
		Branch:    t.Branch,
		PRNumber:  t.PRNumber,
		PRURL:     t.PRURL,
		Source:    "shepherd",
		SourceRef: ref,
		Priority:  t.Priority + 1,
	})
	return err
}

// pendingApprovalsFor lists the pending approvals of one task.
func (e *Engine) pendingApprovalsFor(ctx context.Context, taskID string) ([]domain.Approval, error) {
	pending, err := e.d.Store.ListApprovals(ctx, domain.ApprovalPending)
	if err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	var out []domain.Approval
	for _, a := range pending {
		if a.TaskID == taskID {
			out = append(out, a)
		}
	}
	return out, nil
}

// checkAwaitingApproval handles a task waiting for a human. With no pending approval, or with one
// for a different PR head, the task goes back to review so it is judged again. The human's
// approval is only for the head it was asked about.
func (e *Engine) checkAwaitingApproval(ctx context.Context, t domain.Task, pr scm.PR) error {
	pending, err := e.pendingApprovalsFor(ctx, t.ID)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		_, _, err := e.setTaskFrom(ctx, t.ID, []domain.TaskStatus{domain.TaskAwaitingApproval},
			"no merge approval is pending; re-evaluating", func(x *domain.Task) error {
				x.Status = domain.TaskReview
				return nil
			})
		return err
	}
	stale := false
	for _, a := range pending {
		if a.HeadSHA != pr.HeadSHA {
			stale = true
		}
	}
	if !stale {
		return nil
	}
	_, applied, err := e.setTaskFrom(ctx, t.ID, []domain.TaskStatus{domain.TaskAwaitingApproval},
		"pull request changed; re-evaluating", func(x *domain.Task) error {
			x.Status = domain.TaskReview
			return nil
		})
	if err != nil || !applied {
		return err
	}
	e.decidePendingApprovals(ctx, t.ID, domain.ApprovalRejected, "superseded: the pull request changed")
	return nil
}

// requestMergeApproval asks a human to approve the merge, once. The task moves to
// awaiting_approval first; the approval is created only if that move applied, so a task that
// moved on is never asked about.
func (e *Engine) requestMergeApproval(ctx context.Context, p *config.Project, t domain.Task, pr scm.PR, d policy.Decision) error {
	_, applied, err := e.setTaskFrom(ctx, t.ID, []domain.TaskStatus{domain.TaskReview}, "waiting for merge approval", func(x *domain.Task) error {
		x.Status = domain.TaskAwaitingApproval
		return nil
	})
	if err != nil || !applied {
		return err
	}

	pending, err := e.pendingApprovalsFor(ctx, t.ID)
	if err != nil {
		return e.backToReview(ctx, t.ID, err)
	}
	if len(pending) > 0 {
		return nil // already asked; checkAwaitingApproval judges the head on the next pass
	}

	reason := fmt.Sprintf("Risk **%s**: %s\n\n%s", d.Risk, strings.Join(d.Reasons, "; "), pr.URL)
	approval, err := e.d.Store.CreateApproval(ctx, domain.Approval{
		TaskID:  t.ID,
		Project: p.Name,
		Kind:    "merge",
		Title:   fmt.Sprintf("Merge: %s (#%d)", t.Title, pr.Number),
		Reason:  reason,
		HeadSHA: pr.HeadSHA,
		Status:  domain.ApprovalPending,
	})
	if err != nil {
		return e.backToReview(ctx, t.ID, fmt.Errorf("create approval: %w", err))
	}
	e.d.Bus.Emit(ctx, domain.EvApprovalRequested, p.Name, t.ID, "", approval.Title, approval)
	e.notify(ctx, notify.PriorityHigh, "Approval needed: "+t.Title, reason, "/approvals")
	return nil
}

// backToReview undoes requestMergeApproval's move to awaiting_approval after the approval could
// not be created. It returns cause, with any failure of the undo logged.
func (e *Engine) backToReview(ctx context.Context, taskID string, cause error) error {
	if _, _, err := e.setTaskFrom(ctx, taskID, []domain.TaskStatus{domain.TaskAwaitingApproval},
		"could not request approval; back to review", func(x *domain.Task) error {
			x.Status = domain.TaskReview
			return nil
		}); err != nil {
		e.d.Logger.Error("move task back to review", "task", taskID, "err", err)
	}
	return cause
}

// mergeTask merges a task's PR at headSHA, moves the task to merged, emits pr.merged and kicks
// the deploy watch. The shepherd (auto-merge) and approval decisions both call it. The task must
// be in review or awaiting_approval; otherwise it returns a notMergeableError and merges nothing.
func (e *Engine) mergeTask(ctx context.Context, taskID, headSHA string) error {
	t, err := e.d.Store.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("get task %s: %w", taskID, err)
	}
	if t.Status != domain.TaskReview && t.Status != domain.TaskAwaitingApproval {
		return &notMergeableError{status: t.Status}
	}
	p, err := e.project(t.Project)
	if err != nil {
		return err
	}
	s, err := e.d.SCMFor(p)
	if err != nil {
		return fmt.Errorf("scm: %w", err)
	}
	if err := s.MergePR(ctx, t.PRNumber, headSHA); err != nil {
		return fmt.Errorf("merge PR #%d: %w", t.PRNumber, err)
	}
	pr, err := s.GetPR(ctx, t.PRNumber)
	if err != nil {
		// The merge happened. Leave the task alone; the next shepherd pass sees the PR merged.
		e.d.Logger.Error("PR merged but could not be read back", "task", taskID, "pr", t.PRNumber, "err", err)
		return nil
	}
	applied, err := e.markMerged(ctx, p, t, pr.MergeSHA, fmt.Sprintf("merged PR #%d", t.PRNumber))
	if err != nil || !applied {
		return err
	}
	e.notify(ctx, notify.PriorityLow, "Merged: "+t.Title, pr.URL, "/tasks/"+t.ID)
	return nil
}

// markMerged moves a task whose PR has merged to merged, emits pr.merged and kicks the deploy
// watch. applied is false when the task had already moved on (for example, cancelled).
func (e *Engine) markMerged(ctx context.Context, p *config.Project, t domain.Task, mergeSHA, message string) (bool, error) {
	_, applied, err := e.setTaskFrom(ctx, t.ID, shepherdStatuses, message, func(x *domain.Task) error {
		x.Status = domain.TaskMerged
		x.MergeSHA = mergeSHA
		return nil
	})
	if err != nil || !applied {
		return false, err
	}
	e.d.Bus.Emit(ctx, domain.EvPRMerged, p.Name, t.ID, "",
		fmt.Sprintf("PR #%d merged: %s", t.PRNumber, t.Title), map[string]string{"merge_sha": mergeSHA})
	e.kickLoop(loopDeployWatch, p.Name)
	e.closeMergedIssue(ctx, p, t)
	return true, nil
}

// closeMergedIssue closes the GitHub issue behind an intake task whose PR has merged. GitHub
// closes it by itself only when the PR merges into the repository's default branch, so fixes
// that land on another branch would leave it open. It runs after the task has moved to merged,
// and a failure is logged, never returned.
func (e *Engine) closeMergedIssue(ctx context.Context, p *config.Project, t domain.Task) {
	n, ok := issueNumberOf(t)
	if !ok {
		return
	}
	s, err := e.d.SCMFor(p)
	if err != nil {
		e.d.Logger.Error("close merged issue: scm", "task", t.ID, "project", p.Name, "err", err)
		return
	}
	comment := fmt.Sprintf("Fixed by #%d (merged into `%s`).", t.PRNumber, p.DefaultBranch)
	if err := s.CloseIssue(ctx, n, comment); err != nil {
		e.d.Logger.Warn("close merged issue", "task", t.ID, "issue", n, "err", err)
	}
}

// decidePendingApprovals resolves every pending approval of a task with status and note. A
// failed update is logged and skipped.
func (e *Engine) decidePendingApprovals(ctx context.Context, taskID string, status domain.ApprovalStatus, note string) {
	pending, err := e.d.Store.ListApprovals(ctx, domain.ApprovalPending)
	if err != nil {
		e.d.Logger.Error("list approvals", "task", taskID, "err", err)
		return
	}
	for _, a := range pending {
		if a.TaskID != taskID {
			continue
		}
		now := e.d.Now().UTC()
		out, err := e.d.Store.UpdateApproval(ctx, a.ID, func(x *domain.Approval) error {
			x.Status = status
			x.Note = note
			x.DecidedAt = &now
			return nil
		})
		if err != nil {
			e.d.Logger.Error("decide approval", "approval", a.ID, "err", err)
			continue
		}
		e.d.Bus.Emit(ctx, domain.EvApprovalDecided, out.Project, out.TaskID, "",
			fmt.Sprintf("%s: %s", out.Title, status), out)
	}
}
