package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/bobbyrc/foundry/internal/api"
	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/harness"
	"github.com/bobbyrc/foundry/internal/notify"
	"github.com/bobbyrc/foundry/internal/workspace"
)

// Config returns the current configuration. Callers must not mutate it.
func (e *Engine) Config() *config.Config {
	e.cfgMu.RLock()
	defer e.cfgMu.RUnlock()
	return e.cfg
}

// project returns the named project or an api.ErrInvalid error.
func (e *Engine) project(name string) (*config.Project, error) {
	p, ok := e.Config().Projects[name]
	if !ok {
		return nil, fmt.Errorf("%w: unknown project %q", api.ErrInvalid, name)
	}
	return p, nil
}

// repoFor describes a project's repository for the workspace manager.
func (e *Engine) repoFor(p *config.Project) workspace.Repo {
	return workspace.Repo{Name: p.Name, Remote: p.Repo, Path: p.Path, DefaultBranch: p.DefaultBranch}
}

// harnessFor resolves an agent role to a ready harness. The role is looked up among the roles
// that apply to project (its overrides included); project "" means the global roles.
func (e *Engine) harnessFor(project, agentName string) (harness.Harness, config.Agent, error) {
	cfg := e.Config()
	a, ok := cfg.AgentsFor(project)[agentName]
	if !ok {
		return nil, config.Agent{}, fmt.Errorf("unknown agent %q", agentName)
	}
	h, ok := cfg.Harness[a.Harness]
	if !ok {
		return nil, a, fmt.Errorf("agent %q: unknown harness %q", agentName, a.Harness)
	}
	hn, err := e.d.HarnessFor(a.Harness, h)
	return hn, a, err
}

// createTask fills defaults (role from routing, MaxAttempts, status queued, source "api"),
// stores the task, emits task.created and wakes the scheduler. Every task enters the system
// through here.
func (e *Engine) createTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	cfg := e.Config()
	if _, err := e.project(t.Project); err != nil {
		return domain.Task{}, err
	}
	if strings.TrimSpace(t.Title) == "" {
		return domain.Task{}, fmt.Errorf("%w: title is required", api.ErrInvalid)
	}
	if t.Kind == "" {
		t.Kind = domain.KindFeature
	}
	role, _, err := cfg.AgentForProject(t.Project, t.Kind, t.Role)
	if err != nil {
		return domain.Task{}, fmt.Errorf("%w: %v", api.ErrInvalid, err)
	}
	t.Role = role
	if t.MaxAttempts == 0 {
		t.MaxAttempts = cfg.Limits.MaxAttempts
	}
	if t.Source == "" {
		t.Source = "api"
	}
	t.Status = domain.TaskQueued
	t, err = e.d.Store.CreateTask(ctx, t)
	if err != nil {
		return domain.Task{}, err
	}
	e.d.Bus.Emit(ctx, domain.EvTaskCreated, t.Project, t.ID, "", fmt.Sprintf("queued %s task: %s", t.Kind, t.Title), t)
	e.wakeScheduler()
	return t, nil
}

// setTask updates a task, emits task.updated with message, and returns the new value.
func (e *Engine) setTask(ctx context.Context, id, message string, fn func(*domain.Task) error) (domain.Task, error) {
	t, err := e.d.Store.UpdateTask(ctx, id, fn)
	if err != nil {
		return t, err
	}
	e.d.Bus.Emit(ctx, domain.EvTaskUpdated, t.Project, t.ID, "", fmt.Sprintf("%s: %s", t.Title, message), t)
	if t.Status == domain.TaskQueued {
		e.wakeScheduler()
	}
	if t.Status.Terminal() {
		bg := context.WithoutCancel(ctx)
		e.goTracked(func() { e.reportToIssue(bg, t) })
	}
	return t, nil
}

// errSkip aborts a guarded update without an error; see setTaskFrom.
var errSkip = errors.New("skip")

// setTaskFrom is setTask guarded by the task's current status: fn runs only when the status
// is one of from, in the same transaction as the write. applied is false (and err nil) when
// the task had moved on — e.g. the owner cancelled it while a run or loop was working on it.
// Every automatic status change goes through this, so late outcomes never overwrite a
// decision made since.
func (e *Engine) setTaskFrom(ctx context.Context, id string, from []domain.TaskStatus, message string, fn func(*domain.Task) error) (domain.Task, bool, error) {
	t, err := e.setTask(ctx, id, message, func(t *domain.Task) error {
		if !slices.Contains(from, t.Status) {
			return errSkip
		}
		return fn(t)
	})
	if errors.Is(err, errSkip) {
		cur, gerr := e.d.Store.GetTask(ctx, id)
		if gerr == nil {
			e.d.Logger.Info("task moved on; update skipped", "task", id, "status", cur.Status, "update", message)
		}
		return cur, false, nil
	}
	return t, err == nil, err
}

// activeStatuses are the statuses foundry itself moves a task out of. blockTask and
// requeueOrBlock act only on these; done, failed, cancelled and blocked belong to the owner.
var activeStatuses = []domain.TaskStatus{
	domain.TaskQueued, domain.TaskRunning, domain.TaskReview, domain.TaskAwaitingApproval,
	domain.TaskMerged, domain.TaskDeploying,
}

// blockTask stops a task until a human acts: status blocked with the exact reason, an event,
// and a high-priority notification. Use it for denials, stalls, timeouts, agent-reported
// blocks and exhausted retries. Never retry automatically after this. It does nothing to a
// task that is no longer active (e.g. cancelled by the owner).
func (e *Engine) blockTask(ctx context.Context, id, reason string) {
	e.blockTaskFrom(ctx, id, activeStatuses, reason)
}

// blockTaskFrom is blockTask limited to tasks whose status is one of from.
func (e *Engine) blockTaskFrom(ctx context.Context, id string, from []domain.TaskStatus, reason string) {
	t, applied, err := e.setTaskFrom(ctx, id, from, "blocked: "+reason, func(t *domain.Task) error {
		t.Status, t.BlockedReason = domain.TaskBlocked, reason
		return nil
	})
	if err != nil {
		e.d.Logger.Error("block task", "task", id, "err", err)
		return
	}
	if !applied {
		return
	}
	e.notify(ctx, notify.PriorityHigh, "Blocked: "+t.Title, reason, "/tasks/"+t.ID)
}

// requeueOrBlock re-queues a task for another attempt, appending context for the next agent to
// its body, or blocks it when attempts are exhausted.
func (e *Engine) requeueOrBlock(ctx context.Context, id, why, context string) {
	e.requeueOrBlockFrom(ctx, id, []domain.TaskStatus{domain.TaskRunning, domain.TaskReview, domain.TaskAwaitingApproval}, why, context)
}

// requeueOrBlockFrom is requeueOrBlock limited to tasks whose status is one of from. Both the
// re-queue and the block happen only while the task is in from, so a late outcome cannot
// re-queue a task that has been re-queued and is running again.
func (e *Engine) requeueOrBlockFrom(ctx context.Context, id string, from []domain.TaskStatus, why, context string) {
	t, err := e.d.Store.GetTask(ctx, id)
	if err != nil {
		e.d.Logger.Error("requeue task", "task", id, "err", err)
		return
	}
	if t.Attempts >= t.MaxAttempts {
		e.blockTaskFrom(ctx, id, from, fmt.Sprintf("%s (gave up after %d attempts)", why, t.Attempts))
		return
	}
	if _, _, err := e.setTaskFrom(ctx, id, from, "re-queued: "+why, func(t *domain.Task) error {
		t.Status = domain.TaskQueued
		if context != "" {
			t.Body = strings.TrimRight(t.Body, "\n") + fmt.Sprintf("\n\n## Attempt %d: %s\n\n%s\n", t.Attempts, why, context)
		}
		return nil
	}); err != nil {
		e.d.Logger.Error("requeue task", "task", id, "err", err)
	}
}

// notify sends a notification to every configured notifier and records it as an event.
// path is a web UI path such as "/tasks/<id>".
func (e *Engine) notify(ctx context.Context, prio notify.Priority, title, body, path string) {
	url := ""
	if path != "" {
		url = strings.TrimRight(e.Config().Server.PublicURL, "/") + path
	}
	e.d.Bus.Emit(ctx, domain.EvNotification, "", "", "", title, map[string]string{
		"priority": string(prio), "body": body, "url": url,
	})
	if err := e.d.Notifier.Notify(ctx, notify.Notification{Title: title, Body: body, Priority: prio, URL: url}); err != nil {
		e.d.Logger.Warn("notify", "err", err)
	}
}

// wakeScheduler asks the scheduler to look for work now. Never blocks.
func (e *Engine) wakeScheduler() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Paused reports the global pause switch.
func (e *Engine) Paused() bool { return e.paused.Load() }

// projectPaused reports whether nothing may start for the project: the factory is paused,
// the project is unknown, or it is paused in config or by the owner at runtime.
func (e *Engine) projectPaused(name string) bool {
	if e.Paused() {
		return true
	}
	return e.ProjectPausedBy(name) != "" || e.Config().Projects[name] == nil
}

// ProjectPausedBy implements api.Controller.
func (e *Engine) ProjectPausedBy(name string) string {
	if p := e.Config().Projects[name]; p != nil && p.Paused {
		return "config"
	}
	e.projMu.Lock()
	defer e.projMu.Unlock()
	if e.projPaused[name] {
		return "owner"
	}
	return ""
}

// cancelRun stops a live run, recording why. It reports whether the run was live.
func (e *Engine) cancelRun(runID, reason string) bool {
	e.runsMu.Lock()
	r, ok := e.active[runID]
	e.runsMu.Unlock()
	if !ok {
		return false
	}
	r.reason.CompareAndSwap(nil, reason)
	r.cancel()
	return true
}

func (e *Engine) cancelAllRuns(reason string) {
	e.runsMu.Lock()
	ids := make([]string, 0, len(e.active))
	for id := range e.active {
		ids = append(ids, id)
	}
	e.runsMu.Unlock()
	for _, id := range ids {
		e.cancelRun(id, reason)
	}
}

// branchName is the branch for a task: foundry/<first 8 of id>-<slug of title>.
func branchName(t domain.Task) string {
	var b strings.Builder
	for _, r := range strings.ToLower(t.Title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
		if b.Len() >= 40 {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	id := t.ID
	if len(id) > 8 {
		id = id[len(id)-8:] // UUIDv7's leading bytes are a timestamp; the tail is the unique part
	}
	if slug == "" {
		return "foundry/" + id
	}
	return "foundry/" + id + "-" + slug
}
