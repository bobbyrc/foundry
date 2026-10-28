package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/deploy"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/notify"
	"github.com/bobbyrc/foundry/internal/store"
)

const (
	// deployHealthAttempts is how many consecutive failed health checks roll a release back.
	deployHealthAttempts = 3
	// deployHealthTimeout bounds one health check.
	deployHealthTimeout = 10 * time.Second
	// deployProbeSignalLimit is how many signals are read when judging a release.
	deployProbeSignalLimit = 200
	// deployProbeTitles is how many signal titles go into a failure reason.
	deployProbeTitles = 3
)

// deployHealthTally counts consecutive failed health checks per deployment ID. It lives in
// process memory on purpose: a restart resets the count, which only delays a rollback.
type deployHealthTally struct {
	sync.Mutex
	n map[string]int
}

var deployHealth = &deployHealthTally{n: map[string]int{}}

// fail records one failed check and returns the consecutive count.
func (h *deployHealthTally) fail(id string) int {
	h.Lock()
	defer h.Unlock()
	h.n[id]++
	return h.n[id]
}

// pass clears the count for a deployment.
func (h *deployHealthTally) pass(id string) {
	h.Lock()
	defer h.Unlock()
	delete(h.n, id)
}

// deployLoop releases merged work one task at a time and verifies each release until it is
// healthy, or rolls it back.
func (e *Engine) deployLoop(ctx context.Context, p *config.Project) error {
	if p.Deploy == nil {
		return e.finishUndeployed(ctx, p)
	}
	if e.d.DeployerFor == nil {
		return errors.New("deploy watch: no deployer factory")
	}
	dep, err := e.d.DeployerFor(p)
	if err != nil {
		return fmt.Errorf("deploy watch: deployer: %w", err)
	}
	releases, err := e.d.Store.ListDeployments(ctx, p.Name, 20)
	if err != nil {
		return fmt.Errorf("deploy watch: list deployments: %w", err)
	}
	for _, d := range releases { // newest first
		switch d.State {
		case domain.DeployPending, domain.DeployDeploying, domain.DeployVerifying:
			return e.advanceRelease(ctx, p, dep, d)
		}
	}
	return e.startRelease(ctx, p, dep)
}

// finishUndeployed marks merged tasks done for a project that has no deployer.
func (e *Engine) finishUndeployed(ctx context.Context, p *config.Project) error {
	merged, err := e.d.Store.ListTasks(ctx, store.TaskFilter{
		Project: p.Name, Statuses: []domain.TaskStatus{domain.TaskMerged}, Limit: 1000,
	})
	if err != nil {
		return fmt.Errorf("deploy watch: list merged tasks: %w", err)
	}
	var errs []error
	for _, t := range merged {
		if _, err := e.setTask(ctx, t.ID, "merged; no deployer configured", func(x *domain.Task) error {
			x.Status = domain.TaskDone
			return nil
		}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// startRelease deploys the oldest merged task's merge commit.
func (e *Engine) startRelease(ctx context.Context, p *config.Project, dep deploy.Deployer) error {
	merged, err := e.d.Store.ListTasks(ctx, store.TaskFilter{
		Project: p.Name, Statuses: []domain.TaskStatus{domain.TaskMerged}, Limit: 1000,
	})
	if err != nil {
		return fmt.Errorf("deploy watch: list merged tasks: %w", err)
	}
	if len(merged) == 0 {
		return nil
	}
	oldest := merged[len(merged)-1] // lists are newest first
	if oldest.MergeSHA == "" {
		return fmt.Errorf("task %s is merged without a merge commit", oldest.ID)
	}
	now := e.d.Now().UTC()
	d, err := e.d.Store.CreateDeployment(ctx, domain.Deployment{
		Project:   p.Name,
		TaskID:    oldest.ID,
		Ref:       oldest.MergeSHA,
		State:     domain.DeployPending,
		StartedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("create deployment: %w", err)
	}
	e.d.Bus.Emit(ctx, domain.EvDeployUpdated, p.Name, d.TaskID, "", "deploy "+deployShortRef(d.Ref)+" pending", d)

	handle, err := dep.Deploy(ctx, d.Ref)
	if err != nil {
		return e.failRelease(ctx, p, dep, d, "deploy failed to start: "+err.Error(), false)
	}
	if _, err := e.setDeployment(ctx, d.ID, func(x *domain.Deployment) error {
		x.ProviderID = handle.ID
		x.URL = handle.URL
		x.State = domain.DeployDeploying
		return nil
	}); err != nil {
		return err
	}
	_, err = e.setTask(ctx, oldest.ID, "deploying "+deployShortRef(d.Ref), func(x *domain.Task) error {
		x.Status = domain.TaskDeploying
		return nil
	})
	return err
}

// advanceRelease moves an active release along.
func (e *Engine) advanceRelease(ctx context.Context, p *config.Project, dep deploy.Deployer, d domain.Deployment) error {
	switch d.State {
	case domain.DeployPending:
		// Only a crash between recording and starting the release leaves a pending one.
		return e.failRelease(ctx, p, dep, d, "deploy was interrupted before it started", false)
	case domain.DeployDeploying:
		return e.advanceDeploying(ctx, p, dep, d)
	default:
		return e.advanceVerifying(ctx, p, dep, d)
	}
}

// advanceDeploying waits for the provider to finish building the release.
func (e *Engine) advanceDeploying(ctx context.Context, p *config.Project, dep deploy.Deployer, d domain.Deployment) error {
	st, err := dep.Status(ctx, d.ProviderID)
	if err != nil {
		return fmt.Errorf("deploy %s status: %w", d.ID, err)
	}
	switch st.State {
	case deploy.StateReady:
		now := e.d.Now().UTC()
		_, err := e.setDeployment(ctx, d.ID, func(x *domain.Deployment) error {
			x.State = domain.DeployVerifying
			x.VerifyingSince = &now
			if st.URL != "" {
				x.URL = st.URL
			}
			return nil
		})
		return err
	case deploy.StateError, deploy.StateCanceled:
		return e.failRelease(ctx, p, dep, d, "deploy "+string(st.State)+": "+st.Message, false)
	}
	return nil
}

// advanceVerifying runs the health window. Three failed health checks in a row, or a new
// production signal during the window, fail the release. A clean window marks it healthy.
func (e *Engine) advanceVerifying(ctx context.Context, p *config.Project, dep deploy.Deployer, d domain.Deployment) error {
	dp := p.Deploy
	if dp.HealthURL != "" {
		if err := deploy.CheckHealth(ctx, dp.HealthURL, deployHealthTimeout); err != nil {
			if deployHealth.fail(d.ID) >= deployHealthAttempts {
				deployHealth.pass(d.ID)
				return e.failRelease(ctx, p, dep, d, "health check failing: "+err.Error(), true)
			}
			return nil
		}
		deployHealth.pass(d.ID)
	}

	if d.VerifyingSince == nil {
		return fmt.Errorf("deployment %s is verifying without a start time", d.ID)
	}
	since := *d.VerifyingSince
	signals, err := e.d.Store.ListSignals(ctx, p.Name, deployProbeSignalLimit)
	if err != nil {
		return fmt.Errorf("list signals: %w", err)
	}
	minRank := signalLevelRank(e.Config().LoopsFor(p.Name).ProdWatch.MinLevel)
	var titles []string
	for _, s := range signals {
		if s.CreatedAt.Before(since) || signalLevelRank(s.Level) < minRank {
			continue
		}
		titles = append(titles, s.Title)
	}
	if len(titles) > 0 {
		if len(titles) > deployProbeTitles {
			titles = titles[:deployProbeTitles]
		}
		return e.failRelease(ctx, p, dep, d,
			"new production errors during verification: "+strings.Join(titles, "; "), true)
	}

	if e.d.Now().Sub(since) < dp.VerifyFor.Duration {
		return nil
	}
	healthy, err := e.setDeployment(ctx, d.ID, func(x *domain.Deployment) error {
		x.State = domain.DeployHealthy
		return nil
	})
	if err != nil {
		return err
	}
	label := deployShortRef(d.Ref)
	if d.TaskID != "" {
		t, err := e.d.Store.GetTask(ctx, d.TaskID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("get task %s: %w", d.TaskID, err)
		}
		if err == nil {
			label = t.Title
			if _, err := e.setTask(ctx, t.ID, "deployed and verified", func(x *domain.Task) error {
				x.Status = domain.TaskDone
				return nil
			}); err != nil {
				return err
			}
		}
	}
	e.notify(ctx, notify.PriorityLow, "Deployed: "+label, healthy.URL, "/deploys")
	return nil
}

// failRelease marks a release failed or rolled back, opens an incident task with the evidence,
// blocks the merged task it came from, and notifies. It rolls back to the last healthy release
// when rollbackEligible is set and the project allows rollback.
func (e *Engine) failRelease(ctx context.Context, p *config.Project, dep deploy.Deployer, d domain.Deployment, reason string, rollbackEligible bool) error {
	notes := reason
	state := domain.DeployFailed
	if rollbackEligible && p.Deploy.RollbackEnabled() {
		prev, err := e.d.Store.LastHealthyDeployment(ctx, p.Name)
		switch {
		case err == nil:
			if rerr := dep.Rollback(ctx, prev.ProviderID, prev.Ref); rerr != nil {
				notes += "; ROLLBACK FAILED: " + rerr.Error()
			} else {
				state = domain.DeployRolledBack
				notes += "; rolled back to " + deployShortRef(prev.Ref)
			}
		case errors.Is(err, store.ErrNotFound):
			notes += "; no healthy release to roll back to"
		default:
			notes += "; ROLLBACK FAILED: could not find the last healthy release: " + err.Error()
		}
	}

	failed, err := e.setDeployment(ctx, d.ID, func(x *domain.Deployment) error {
		x.State = state
		x.Notes = notes
		return nil
	})
	if err != nil {
		return err
	}

	var errs []error
	var task *domain.Task
	label := deployShortRef(d.Ref)
	if d.TaskID != "" {
		t, err := e.d.Store.GetTask(ctx, d.TaskID)
		switch {
		case err == nil:
			task = &t
			label = t.Title
		case !errors.Is(err, store.ErrNotFound):
			errs = append(errs, fmt.Errorf("get task %s: %w", d.TaskID, err))
		}
	}

	body := fmt.Sprintf("Release of %s failed.\n\n- Outcome: %s\n- Reason: %s\n- Deployment: %s\n- Ref: %s\n- URL: %s\n",
		label, notes, reason, d.ID, d.Ref, failed.URL)
	if task != nil {
		body += fmt.Sprintf("- Merged task: %s (PR #%d %s)\n", task.ID, task.PRNumber, task.PRURL)
	}
	incident, err := e.createTask(ctx, domain.Task{
		Project:   p.Name,
		Kind:      domain.KindIncident,
		Title:     "Release failed: " + label,
		Body:      body,
		Source:    "deploy",
		SourceRef: "deploy:" + d.ID,
		Priority:  10,
	})
	incidentID := ""
	if err != nil {
		errs = append(errs, fmt.Errorf("open incident for deployment %s: %w", d.ID, err))
	} else {
		incidentID = incident.ID
	}

	if task != nil {
		e.blockTask(ctx, task.ID, "release failed: "+notes+"; incident task "+incidentID)
	}
	e.notify(ctx, notify.PriorityHigh, "Release failed: "+label, notes, "/deploys")
	return errors.Join(errs...)
}

// setDeployment applies fn to a deployment, stamps UpdatedAt, and emits deploy.updated.
func (e *Engine) setDeployment(ctx context.Context, id string, fn func(*domain.Deployment) error) (domain.Deployment, error) {
	now := e.d.Now().UTC()
	saved, err := e.d.Store.UpdateDeployment(ctx, id, func(x *domain.Deployment) error {
		x.UpdatedAt = now
		return fn(x)
	})
	if err != nil {
		return saved, fmt.Errorf("update deployment %s: %w", id, err)
	}
	e.d.Bus.Emit(ctx, domain.EvDeployUpdated, saved.Project, saved.TaskID, "",
		"deploy "+deployShortRef(saved.Ref)+" is "+string(saved.State), saved)
	return saved, nil
}

// deployShortRef shortens a commit SHA for messages.
func deployShortRef(ref string) string {
	if len(ref) > 7 {
		return ref[:7]
	}
	return ref
}
