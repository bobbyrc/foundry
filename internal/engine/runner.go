package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/harness"
	"github.com/bobbyrc/foundry/internal/scm"
	"github.com/bobbyrc/foundry/internal/workspace"
)

// startTask turns a claimed task into a live run. Everything that can fail before the agent
// starts is checked here, and the run row, its registration and its context are created
// synchronously, so the scheduler's count is right on its next pass. Only then does the run's
// goroutine start.
func (e *Engine) startTask(ctx context.Context, t domain.Task) {
	// The task is claimed, so every failure write below must land even if ctx is cancelled
	// (shutdown): bg does not cancel with ctx.
	bg := context.WithoutCancel(ctx)
	cfg := e.Config()
	p, ok := cfg.Projects[t.Project]
	if !ok {
		e.blockTask(bg, t.ID, fmt.Sprintf("unknown project %q", t.Project))
		return
	}
	h, agent, err := e.harnessFor(t.Project, t.Role)
	if err != nil {
		e.blockTask(bg, t.ID, "could not resolve agent "+t.Role+": "+err.Error())
		return
	}

	var parent *domain.Task
	if t.Kind == domain.KindReview {
		if t.ParentID == "" {
			e.blockTask(bg, t.ID, "review task has no parent task")
			return
		}
		pt, err := e.d.Store.GetTask(bg, t.ParentID)
		if err != nil {
			e.blockTask(bg, t.ID, "could not load the reviewed task: "+err.Error())
			return
		}
		parent = &pt
	}

	branch := t.Branch
	if branch == "" {
		if parent != nil && parent.Branch != "" {
			branch = parent.Branch
		} else {
			branch = branchName(t)
		}
	}
	t.Branch = branch

	runCtx, cancel := context.WithTimeout(ctx, cfg.Limits.RunTimeout.Duration)
	now := e.d.Now().UTC()
	run, err := e.d.Store.CreateRun(bg, domain.Run{
		TaskID:       t.ID,
		Project:      t.Project,
		Role:         t.Role,
		Harness:      agent.Harness,
		Model:        agent.Model,
		Status:       domain.RunStarting,
		Branch:       branch,
		StartedAt:    now,
		LastOutputAt: now,
	})
	if err != nil {
		cancel()
		e.blockTask(bg, t.ID, "could not create run: "+err.Error())
		return
	}
	ar := e.registerRun(run.ID, t.ID, t.Project, cancel)
	e.d.Bus.Emit(bg, domain.EvRunUpdated, run.Project, run.TaskID, run.ID,
		"run started: "+t.Role+" on "+t.Title, run)

	started := e.goTracked(func() {
		defer e.unregisterRun(run.ID)
		defer cancel()
		e.runTask(runCtx, t, run, ar, p, h, agent, parent)
	})
	if !started {
		// Shutting down: the run never starts. The task stays running; recover re-queues it.
		cancel()
		e.finishRun(bg, run.ID, domain.RunCancelled, errStopped.Error())
		e.unregisterRun(run.ID)
	}
}

// fromRunning guards the status writes a run makes about its own task: they land only while the
// task is still running, so a cancel or block made during the run is never overwritten.
var fromRunning = []domain.TaskStatus{domain.TaskRunning}

// finishRunning is setTaskFrom for a run's own outcome. It reports whether the write was made;
// false means the task moved on (or the write failed, which is logged).
func (e *Engine) finishRunning(ctx context.Context, id, message string, fn func(*domain.Task) error) bool {
	_, applied, err := e.setTaskFrom(ctx, id, fromRunning, message, fn)
	if err != nil {
		e.d.Logger.Error("update task", "task", id, "err", err)
		return false
	}
	return applied
}

// stillRunning reports whether the task is still running. A task the owner cancelled or blocked
// during the run must not be pushed or given a pull request.
func (e *Engine) stillRunning(ctx context.Context, id string) bool {
	t, err := e.d.Store.GetTask(ctx, id)
	if err != nil {
		e.d.Logger.Error("read task", "task", id, "err", err)
		return false
	}
	return t.Status == domain.TaskRunning
}

// runTask runs one agent session for a task and applies its outcome. The task's state is
// written with a context that survives cancellation, so a cancelled or timed-out run still
// records what happened; ctx governs only the agent and the git work.
func (e *Engine) runTask(ctx context.Context, t domain.Task, run domain.Run, ar *activeRun,
	p *config.Project, h harness.Harness, agent config.Agent, parent *domain.Task) {
	bg := context.WithoutCancel(ctx)
	cancelled := false
	defer func() {
		if cancelled {
			return
		}
		cur, err := e.d.Store.GetTask(bg, t.ID)
		if err == nil && cur.Status == domain.TaskRunning {
			e.requeueOrBlock(bg, t.ID, "internal error: run ended without an outcome", "")
		}
	}()

	// stopped handles a run that was cancelled, or ran out of time, before its agent started.
	// That is the run's outcome, not a setup failure, so it is never re-queued. It reports
	// whether it handled the run; the caller then returns.
	stopped := func() bool {
		if reason := reasonOf(ar); reason != "" {
			e.finishRun(bg, run.ID, domain.RunCancelled, reason)
			cancelled = true
			return true
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg := fmt.Sprintf("timed out after %s", e.Config().Limits.RunTimeout.Duration)
			e.finishRun(bg, run.ID, domain.RunTimedOut, msg)
			e.blockTask(bg, t.ID, msg)
			return true
		}
		return false
	}

	repo := e.repoFor(p)
	ws, err := e.d.Workspaces.Prepare(ctx, repo, t.Branch, run.ID)
	if err != nil {
		if stopped() {
			return
		}
		e.finishRun(bg, run.ID, domain.RunFailed, "workspace setup: "+err.Error())
		e.requeueOrBlock(bg, t.ID, "workspace setup failed", err.Error())
		return
	}
	// A branch with an open PR may have fallen behind the default branch. Merge the base in
	// before the agent starts, so its conflicts are its to resolve.
	var conflicts []string
	if t.PRNumber != 0 {
		conflicts, err = e.d.Workspaces.MergeBase(ctx, repo, ws)
		if err != nil {
			if stopped() {
				return
			}
			e.finishRun(bg, run.ID, domain.RunFailed, "merge base: "+err.Error())
			e.requeueOrBlock(bg, t.ID, "could not merge "+ws.BaseRef, err.Error())
			return
		}
	}

	_ = os.Remove(filepath.Join(ws.Dir, domain.OutboxDir, domain.ResultFile)) // no stale report
	if r, err := e.updateRun(bg, run.ID, func(r *domain.Run) error {
		r.Workdir, r.Branch = ws.Dir, ws.Branch
		return nil
	}); err == nil {
		run = r
	}

	instructions, err := e.buildInstructions(agent, p)
	if err != nil {
		e.finishRun(bg, run.ID, domain.RunFailed, "could not load role prompt: "+err.Error())
		e.blockTask(bg, t.ID, "could not load role prompt: "+err.Error())
		return
	}
	prompt := buildTaskPrompt(t, p, parent, conflicts)

	env := map[string]string{}
	for k, v := range e.d.AgentEnv {
		env[k] = v
	}
	for k, v := range p.Env {
		env[k] = v
	}
	env["FOUNDRY_RUN_ID"] = run.ID
	env["FOUNDRY_TASK_ID"] = t.ID
	env["FOUNDRY_PROJECT"] = t.Project
	env["FOUNDRY_ROLE"] = t.Role
	env["FOUNDRY_OUTBOX"] = filepath.Join(ws.Dir, domain.OutboxDir)
	env["FOUNDRY_BASE_REF"] = ws.BaseRef

	sess, err := h.Start(ctx, harness.Spec{
		RunID:        run.ID,
		Workdir:      ws.Dir,
		Prompt:       prompt,
		Instructions: instructions,
		Model:        agent.Model,
		Env:          env,
		Args:         agent.Args,
	})
	if err != nil {
		if stopped() {
			return
		}
		e.finishRun(bg, run.ID, domain.RunFailed, "could not start harness: "+err.Error())
		e.blockTask(bg, t.ID, "could not start harness: "+err.Error())
		return
	}
	if r, err := e.updateRun(bg, run.ID, func(r *domain.Run) error {
		r.Status = domain.RunRunning
		return nil
	}); err == nil {
		run = r
	}

	res, status, reason := e.supervise(ctx, run, sess, ar, nil)
	switch status {
	case domain.RunDenied:
		e.blockTask(bg, t.ID, "agent was denied: "+strings.TrimPrefix(reason, "denied: ")+
			". Foundry never retries a denial: adjust the agent's permissions or the task, then Retry.")
		return
	case domain.RunStalled, domain.RunTimedOut:
		e.blockTask(bg, t.ID, reason)
		return
	case domain.RunCancelled:
		cancelled = true // the canceller owns the task state
		return
	}

	result := readResult(ws.Dir, res.FinalText)
	e.recordFriction(bg, t, run, result.Friction)
	e.updateRun(bg, run.ID, func(r *domain.Run) error {
		r.Summary = result.Summary
		return nil
	})
	e.setTask(bg, t.ID, "agent reported "+string(result.Status), func(x *domain.Task) error {
		x.Summary = result.Summary
		return nil
	})

	if t.Kind == domain.KindReview {
		e.applyReview(bg, t, parent, ws, result)
		e.removeWorktree(bg, repo, ws)
		return
	}

	switch result.Status {
	case domain.ResultBlocked:
		reason := result.BlockedReason
		if strings.TrimSpace(reason) == "" {
			reason = result.Summary
		}
		e.blockTask(bg, t.ID, "agent needs a human: "+reason)
		return
	case domain.ResultFailed:
		e.requeueOrBlock(bg, t.ID, "agent reported failure", result.Summary)
		return
	}

	// completed or no_changes: commit leftovers, then publish or close out.
	commitMsg := result.PRTitle
	if commitMsg == "" {
		commitMsg = t.Title
	}
	if _, err := e.d.Workspaces.Commit(bg, ws, commitMsg); err != nil {
		if errors.Is(err, workspace.ErrUnresolvedConflicts) {
			e.requeueOrBlock(bg, t.ID, "unresolved merge conflicts", err.Error())
		} else {
			e.requeueOrBlock(bg, t.ID, "commit failed", err.Error())
		}
		return
	}
	head, err := e.d.Workspaces.HeadSHA(bg, ws)
	if err != nil {
		e.requeueOrBlock(bg, t.ID, "could not read HEAD", err.Error())
		return
	}
	if head == ws.StartSHA {
		var moved bool
		if t.PRNumber != 0 {
			moved = e.finishRunning(bg, t.ID, "no new changes; back to review", func(x *domain.Task) error {
				x.Status = domain.TaskReview
				return nil
			})
		} else {
			moved = e.finishRunning(bg, t.ID, "finished without code changes", func(x *domain.Task) error {
				x.Status = domain.TaskDone
				return nil
			})
		}
		if !moved {
			return // the task moved on during the run; nothing more to do
		}
	} else if !e.publish(bg, t, p, ws, result) {
		return // publish has requeued or blocked the task, or it moved on; keep the worktree
	}

	e.queueFollowups(bg, t, result)
	e.removeWorktree(bg, repo, ws)
}

// publish pushes the run's branch, opens the pull request if there is none, and moves the task
// to review. It reports false when it re-queued or blocked the task instead.
func (e *Engine) publish(ctx context.Context, t domain.Task, p *config.Project, ws workspace.Workspace, result domain.RunResult) bool {
	if !e.stillRunning(ctx, t.ID) {
		return false // the owner cancelled or blocked the task during the run: push nothing
	}
	if err := e.d.Workspaces.Push(ctx, e.repoFor(p), ws); err != nil {
		e.requeueOrBlock(ctx, t.ID, "push failed", err.Error())
		return false
	}
	s, err := e.d.SCMFor(p)
	if err != nil {
		e.requeueOrBlock(ctx, t.ID, "could not reach the code host", err.Error())
		return false
	}

	var pr scm.PR
	if t.PRNumber != 0 {
		pr, err = s.GetPR(ctx, t.PRNumber)
		if err != nil {
			e.requeueOrBlock(ctx, t.ID, "could not read the pull request", err.Error())
			return false
		}
	} else {
		existing, ok, err := s.FindPR(ctx, ws.Branch)
		if err != nil {
			e.requeueOrBlock(ctx, t.ID, "could not look up the pull request", err.Error())
			return false
		}
		if ok {
			pr = existing
		} else {
			if !e.stillRunning(ctx, t.ID) {
				return false // moved on while the branch was pushed: open no pull request
			}
			title := result.PRTitle
			if title == "" {
				title = t.Title
			}
			opened, err := s.OpenPR(ctx, scm.PRSpec{
				Head:   ws.Branch,
				Base:   p.DefaultBranch,
				Title:  title,
				Body:   prBody(t, result),
				Labels: []string{"foundry"},
			})
			if err != nil {
				e.requeueOrBlock(ctx, t.ID, "could not open the pull request", err.Error())
				return false
			}
			pr = opened
			e.d.Bus.Emit(ctx, domain.EvPROpened, t.Project, t.ID, "",
				fmt.Sprintf("opened PR #%d", pr.Number), pr)
		}
	}

	if !e.finishRunning(ctx, t.ID, fmt.Sprintf("PR #%d ready for review", pr.Number), func(x *domain.Task) error {
		x.Status = domain.TaskReview
		x.Branch = ws.Branch
		x.PRNumber = pr.Number
		x.PRURL = pr.URL
		x.Risk = result.Risk
		x.RiskReasons = result.RiskReasons
		x.ApprovedSHA = "" // a new push invalidates any earlier review
		x.BlockedReason = ""
		return nil
	}) {
		return false
	}
	e.kickLoop("pr_shepherd", p.Name)
	return true
}

// prBody is the pull request description: the agent's body (or its summary), a closing keyword
// when the task came from a GitHub issue, and a footer naming the task.
func prBody(t domain.Task, result domain.RunResult) string {
	body := result.PRBody
	if body == "" {
		body = result.Summary
	}
	if n, ok := strings.CutPrefix(t.SourceRef, "github_issue:"); ok {
		body += "\n\nFixes #" + n
	}
	return body + fmt.Sprintf("\n\n---\nOpened by foundry for task `%s` (%s).", t.ID, t.Role)
}

// applyReview acts on a reviewer's verdict. Approval records the reviewed head on the parent
// task; rejection re-queues the parent with the findings for its next implementer run.
func (e *Engine) applyReview(ctx context.Context, rt domain.Task, parent *domain.Task, ws workspace.Workspace, result domain.RunResult) {
	reviewed := ws.StartSHA
	if parent == nil {
		e.blockTask(ctx, rt.ID, "review has no parent task")
		return
	}
	if result.Review == nil || result.Status == domain.ResultFailed || result.Status == domain.ResultBlocked {
		e.requeueOrBlock(ctx, rt.ID, "review did not produce a verdict", result.Summary)
		return
	}

	if result.Review.Approve {
		_, applied, err := e.setTaskFrom(ctx, parent.ID, []domain.TaskStatus{domain.TaskReview},
			"approved by reviewer at "+reviewed, func(x *domain.Task) error {
				x.ApprovedSHA = reviewed
				if result.Risk != domain.RiskUnknown {
					base := x.Risk
					if base == domain.RiskUnknown {
						base = domain.RiskMedium
					}
					x.Risk = domain.MaxRisk(base, result.Risk)
					for _, why := range result.RiskReasons {
						x.RiskReasons = append(x.RiskReasons, "reviewer: "+why)
					}
				}
				return nil
			})
		if err != nil {
			e.d.Logger.Error("record approval", "task", parent.ID, "err", err)
			return
		}
		if !applied {
			// The parent moved on during the review, so this verdict no longer applies.
			e.finishRunning(ctx, rt.ID, "task moved on; verdict not applied", func(x *domain.Task) error {
				x.Status = domain.TaskDone
				return nil
			})
			return
		}
		e.finishRunning(ctx, rt.ID, "approved", func(x *domain.Task) error {
			x.Status = domain.TaskDone
			return nil
		})
		e.kickLoop("pr_shepherd", rt.Project)
		return
	}

	e.finishRunning(ctx, rt.ID, "changes requested", func(x *domain.Task) error {
		x.Status = domain.TaskDone
		return nil
	})
	e.requeueOrBlock(ctx, parent.ID, "reviewer requested changes", formatFindings(result.Review.Findings))
}

// formatFindings renders review findings as Markdown bullets for the next implementer run.
func formatFindings(findings []domain.ReviewFinding) string {
	if len(findings) == 0 {
		return "(the reviewer requested changes without listing findings)"
	}
	var b strings.Builder
	for _, f := range findings {
		loc := ""
		if f.File != "" {
			loc = "`" + f.File
			if f.Line > 0 {
				loc += fmt.Sprintf(":%d", f.Line)
			}
			loc += "` "
		}
		fmt.Fprintf(&b, "- **%s** %s%s\n", f.Severity, loc, f.Message)
	}
	return strings.TrimRight(b.String(), "\n")
}

// queueFollowups turns the agent's follow-up suggestions into tasks, at most three per run.
// Follow-ups do not spawn follow-ups.
func (e *Engine) queueFollowups(ctx context.Context, t domain.Task, result domain.RunResult) {
	if t.Source == "followup" {
		return
	}
	queued := 0
	for _, f := range result.Followups {
		if queued == 3 {
			break
		}
		if strings.TrimSpace(f.Title) == "" {
			continue
		}
		kind := f.Kind
		if kind == "" {
			kind = domain.KindChore
		}
		if _, err := e.createTask(ctx, domain.Task{
			Project:  t.Project,
			ParentID: t.ID,
			Kind:     kind,
			Title:    f.Title,
			Body:     f.Body,
			Source:   "followup",
			Priority: t.Priority - 1,
		}); err != nil {
			e.d.Logger.Warn("queue followup", "task", t.ID, "err", err)
			continue
		}
		queued++
	}
}

// recordFriction stores the run's friction reports. Factory friction goes to the self project
// when one is configured.
func (e *Engine) recordFriction(ctx context.Context, t domain.Task, run domain.Run, reports []domain.FrictionReport) {
	cfg := e.Config()
	for _, fr := range reports {
		if strings.TrimSpace(fr.Title) == "" {
			continue
		}
		target := t.Project
		if fr.Target == "foundry" && cfg.SelfProject != "" {
			target = cfg.SelfProject
		}
		f, _, err := e.d.Store.UpsertFriction(ctx, domain.Friction{
			Project:     target,
			RunID:       run.ID,
			Title:       fr.Title,
			Body:        fr.Body,
			Fingerprint: frictionFingerprint(fr.Title),
			Count:       1,
		})
		if err != nil {
			e.d.Logger.Error("record friction", "run", run.ID, "err", err)
			continue
		}
		e.d.Bus.Emit(ctx, domain.EvFrictionReported, target, t.ID, run.ID, "friction: "+fr.Title, f)
	}
}

// removeWorktree deletes a finished run's worktree. The branch is kept.
func (e *Engine) removeWorktree(ctx context.Context, repo workspace.Repo, ws workspace.Workspace) {
	if err := e.d.Workspaces.Remove(ctx, repo, ws); err != nil {
		e.d.Logger.Warn("remove worktree", "dir", ws.Dir, "err", err)
	}
}

// updateRun changes a run row and emits run.updated. It returns the stored run.
func (e *Engine) updateRun(ctx context.Context, id string, fn func(*domain.Run) error) (domain.Run, error) {
	r, err := e.d.Store.UpdateRun(ctx, id, fn)
	if err != nil {
		e.d.Logger.Error("update run", "run", id, "err", err)
		return r, err
	}
	e.d.Bus.Emit(ctx, domain.EvRunUpdated, r.Project, r.TaskID, r.ID, "run "+string(r.Status), r)
	return r, nil
}

// finishRun ends a run that failed before or outside supervision.
func (e *Engine) finishRun(ctx context.Context, id string, status domain.RunStatus, errMsg string) {
	now := e.d.Now().UTC()
	e.updateRun(ctx, id, func(r *domain.Run) error {
		r.Status, r.Error, r.EndedAt = status, errMsg, &now
		return nil
	})
}

// reasonOf returns why a run was cancelled, or "" if nothing has cancelled it.
func reasonOf(ar *activeRun) string {
	v, _ := ar.reason.Load().(string)
	return v
}
