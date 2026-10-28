package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/notify"
	"github.com/bobbyrc/foundry/internal/policy"
	"github.com/bobbyrc/foundry/internal/scm"
	"github.com/bobbyrc/foundry/internal/store"
)

// shepherdLoop moves each task in review or awaiting_approval through CI, review, policy and
// merge. Each task is handled on its own, and one task's error does not stop the others.
func (e *Engine) shepherdLoop(ctx context.Context, p *config.Project) error {
	s, err := e.d.SCMFor(p)
	if err != nil {
		return fmt.Errorf("pr shepherd: scm: %w", err)
	}
	tasks, err := e.d.Store.ListTasks(ctx, store.TaskFilter{
		Project:  p.Name,
		Statuses: []domain.TaskStatus{domain.TaskReview, domain.TaskAwaitingApproval},
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
	switch pr.State {
	case "merged":
		if err := e.markMerged(ctx, p, t, pr.MergeSHA, "merged outside foundry"); err != nil {
			return err
		}
		e.decidePendingApprovals(ctx, t.ID, domain.ApprovalApproved, "merged outside foundry")
		return nil
	case "closed":
		if _, err := e.setTask(ctx, t.ID, "PR closed outside foundry", func(x *domain.Task) error {
			x.Status = domain.TaskCancelled
			return nil
		}); err != nil {
			return err
		}
		e.decidePendingApprovals(ctx, t.ID, domain.ApprovalRejected, "PR closed outside foundry")
		return nil
	}

	// Awaiting approval: the human decides, not the loop.
	if t.Status == domain.TaskAwaitingApproval {
		return nil
	}

	if pr.Mergeable == "conflicting" {
		e.requeueOrBlock(ctx, t.ID, "merge conflict with "+p.DefaultBranch,
			"Rebase the branch onto origin/"+p.DefaultBranch+", resolve the conflicts, and make the checks pass again.")
		return nil
	}

	files, err := e.d.Workspaces.Diff(ctx, e.repoFor(p), p.DefaultBranch, t.Branch)
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}
	d := policy.Evaluate(e.Config().PolicyFor(p.Name), policy.Input{
		Kind:             t.Kind,
		Files:            files,
		AgentRisk:        t.Risk,
		AgentRiskReasons: t.RiskReasons,
		Labels:           pr.Labels,
	})
	if d.Risk != t.Risk || !slices.Equal(d.Reasons, t.RiskReasons) {
		saved, err := e.setTask(ctx, t.ID, "risk "+string(d.Risk), func(x *domain.Task) error {
			x.Risk = d.Risk
			x.RiskReasons = d.Reasons
			return nil
		})
		if err != nil {
			return err
		}
		t = saved
		e.d.Bus.Emit(ctx, domain.EvPolicyDecision, p.Name, t.ID, "",
			"risk "+string(d.Risk)+": "+strings.Join(d.Reasons, "; "), d)
	}

	if d.RequireCI {
		switch pr.CI {
		case scm.CIPending:
			return nil
		case scm.CIFailure:
			log, lerr := s.CILog(ctx, t.PRNumber, 8000)
			if lerr != nil {
				log = fmt.Sprintf("(could not fetch the CI log: %v)", lerr)
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
		if err := e.mergeTask(ctx, t); err != nil {
			e.blockTask(ctx, t.ID, "merge failed: "+err.Error())
		}
		return nil
	}
	return e.requestMergeApproval(ctx, p, t, pr, d)
}

// ensureReview makes sure a reviewer run exists for the PR's current head commit, and waits for
// it. A review that ends without approving the head blocks the task, because nothing else would
// move it on.
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

// requestMergeApproval asks a human to approve the merge, once. The task waits in
// awaiting_approval until then.
func (e *Engine) requestMergeApproval(ctx context.Context, p *config.Project, t domain.Task, pr scm.PR, d policy.Decision) error {
	pending, err := e.d.Store.ListApprovals(ctx, domain.ApprovalPending)
	if err != nil {
		return fmt.Errorf("list approvals: %w", err)
	}
	for _, a := range pending {
		if a.TaskID != t.ID {
			continue
		}
		if t.Status != domain.TaskAwaitingApproval {
			_, err := e.setTask(ctx, t.ID, "waiting for merge approval", func(x *domain.Task) error {
				x.Status = domain.TaskAwaitingApproval
				return nil
			})
			return err
		}
		return nil
	}

	reason := fmt.Sprintf("Risk **%s**: %s\n\n%s", d.Risk, strings.Join(d.Reasons, "; "), pr.URL)
	approval, err := e.d.Store.CreateApproval(ctx, domain.Approval{
		TaskID:  t.ID,
		Project: p.Name,
		Kind:    "merge",
		Title:   fmt.Sprintf("Merge: %s (#%d)", t.Title, pr.Number),
		Reason:  reason,
		Status:  domain.ApprovalPending,
	})
	if err != nil {
		return fmt.Errorf("create approval: %w", err)
	}
	e.d.Bus.Emit(ctx, domain.EvApprovalRequested, p.Name, t.ID, "", approval.Title, approval)
	if _, err := e.setTask(ctx, t.ID, "waiting for merge approval", func(x *domain.Task) error {
		x.Status = domain.TaskAwaitingApproval
		return nil
	}); err != nil {
		return err
	}
	e.notify(ctx, notify.PriorityHigh, "Approval needed: "+t.Title, reason, "/approvals")
	return nil
}

// mergeTask merges the task's PR, moves the task to merged, emits pr.merged and kicks the
// deploy watch. Used by the shepherd (auto-merge) and by approval decisions.
func (e *Engine) mergeTask(ctx context.Context, t domain.Task) error {
	p, err := e.project(t.Project)
	if err != nil {
		return err
	}
	s, err := e.d.SCMFor(p)
	if err != nil {
		return fmt.Errorf("scm: %w", err)
	}
	if err := s.MergePR(ctx, t.PRNumber); err != nil {
		return fmt.Errorf("merge PR #%d: %w", t.PRNumber, err)
	}
	pr, err := s.GetPR(ctx, t.PRNumber)
	if err != nil {
		return fmt.Errorf("PR #%d merged but could not be read back: %w", t.PRNumber, err)
	}
	if err := e.markMerged(ctx, p, t, pr.MergeSHA, fmt.Sprintf("merged PR #%d", t.PRNumber)); err != nil {
		return err
	}
	e.notify(ctx, notify.PriorityLow, "Merged: "+t.Title, pr.URL, "/tasks/"+t.ID)
	return nil
}

// markMerged records that a task's PR has merged, emits pr.merged and kicks the deploy watch.
func (e *Engine) markMerged(ctx context.Context, p *config.Project, t domain.Task, mergeSHA, message string) error {
	if _, err := e.setTask(ctx, t.ID, message, func(x *domain.Task) error {
		x.Status = domain.TaskMerged
		x.MergeSHA = mergeSHA
		return nil
	}); err != nil {
		return err
	}
	e.d.Bus.Emit(ctx, domain.EvPRMerged, p.Name, t.ID, "",
		fmt.Sprintf("PR #%d merged: %s", t.PRNumber, t.Title), map[string]string{"merge_sha": mergeSHA})
	e.kickLoop(loopDeployWatch, p.Name)
	return nil
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
