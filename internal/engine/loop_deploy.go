package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/deploy"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/notify"
	"github.com/bobbyrc/foundry/internal/store"
	"github.com/bobbyrc/foundry/internal/sysproc"
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
	// defaultDeployTimeout fails a release still building after this long, when the config sets none.
	defaultDeployTimeout = 30 * time.Minute
	// serviceLogsTail is how many trailing bytes of the project's deploy.logs output are kept.
	serviceLogsTail = 8 << 10
	// evidenceCap is how many trailing bytes of a release's evidence go into an incident body.
	evidenceCap = 12 << 10
)

// serviceLogsTimeout bounds one run of the project's deploy.logs command. It is a variable so
// tests can shorten it.
var serviceLogsTimeout = 30 * time.Second

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
	if e.projectPaused(p.Name) {
		return nil // paused: releases in flight were advanced above, but nothing new starts
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
		if _, _, err := e.setTaskFrom(ctx, t.ID, []domain.TaskStatus{domain.TaskMerged}, "merged; no deployer configured", func(x *domain.Task) error {
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
		x.Log = deployOutputEvidence(redactSecrets(handle.Output, evidenceEnv(p)))
		x.State = domain.DeployDeploying
		return nil
	}); err != nil {
		return err
	}
	_, _, err = e.setTaskFrom(ctx, oldest.ID, []domain.TaskStatus{domain.TaskMerged}, "deploying "+deployShortRef(d.Ref), func(x *domain.Task) error {
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
	if timeout := deployTimeout(p); e.d.Now().Sub(d.StartedAt) > timeout {
		// Nothing new is serving yet, so there is nothing to roll back to.
		return e.failRelease(ctx, p, dep, d, fmt.Sprintf("deploy still not ready after %s", timeout), false)
	}
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
				reason := fmt.Sprintf("health check failing at %s: %d consecutive failed checks", dp.HealthURL, deployHealthAttempts)
				if d.VerifyingSince != nil {
					reason += fmt.Sprintf(", %s after verification started", e.d.Now().Sub(*d.VerifyingSince).Round(time.Second))
				}
				return e.failRelease(ctx, p, dep, d, reason+": "+err.Error(), true)
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
			if _, _, err := e.setTaskFrom(ctx, t.ID, []domain.TaskStatus{domain.TaskDeploying}, "deployed and verified", func(x *domain.Task) error {
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
	// Service logs are read before any rollback, so they show the release that failed.
	logs := serviceLogsEvidence(ctx, p)
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

	// One redaction point, before anything is stored or sent. The reason is deploy output, and the
	// rollback error can carry the output tail of a failed rollback command, so both are covered.
	env := evidenceEnv(p)
	reason = redactSecrets(reason, env)
	notes = redactSecrets(notes, env)

	failed, err := e.setDeployment(ctx, d.ID, func(x *domain.Deployment) error {
		x.State = state
		x.Notes = notes
		x.Log = appendEvidence(x.Log, logs)
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
	body += evidenceSection(failed.Log)
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

// deployTimeout is how long a release may take to build: the project's deploy timeout, or 30m
// when it is unset.
func deployTimeout(p *config.Project) time.Duration {
	if p.Deploy == nil || p.Deploy.Timeout.Duration == 0 {
		return defaultDeployTimeout
	}
	return p.Deploy.Timeout.Duration
}

// deployShortRef shortens a commit SHA for messages.
func deployShortRef(ref string) string {
	if len(ref) > 7 {
		return ref[:7]
	}
	return ref
}

// deployOutputEvidence is the first part of a release's log: the tail of its deploy command's
// output. A release whose command printed nothing has no log.
func deployOutputEvidence(output string) string {
	if output == "" {
		return ""
	}
	return "== deploy output ==\n" + output
}

// serviceLogsEvidence runs the project's deploy.logs command and returns its section of the
// evidence: the last 8KB of its output, plus the reason if it failed or timed out. It returns ""
// when the project has no logs command. A failing logs command never fails the caller. The whole
// section is redacted, so the command line in its header is covered too.
func serviceLogsEvidence(ctx context.Context, p *config.Project) string {
	if p.Deploy == nil || p.Deploy.Logs == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("== service logs (" + p.Deploy.Logs + ") ==\n")
	out, err := runServiceLogs(ctx, p)
	b.WriteString(out)
	if err != nil {
		if out != "" && !strings.HasSuffix(out, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("logs command failed: " + err.Error() + "\n")
	} else if out == "" {
		b.WriteString("(no output)\n")
	}
	return redactSecrets(b.String(), evidenceEnv(p))
}

// runServiceLogs runs the project's deploy.logs command with sh -c in the project directory. The
// environment is foundry's, then the project's env. It returns the last 8KB of stdout and stderr
// together, and the error if the command failed or ran past serviceLogsTimeout.
func runServiceLogs(ctx context.Context, p *config.Project) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, serviceLogsTimeout)
	defer cancel()

	out := &tailBuffer{max: serviceLogsTail}
	cmd := exec.CommandContext(ctx, "sh", "-c", p.Deploy.Logs)
	cmd.Dir = p.Path
	cmd.Env = append(os.Environ(), projectEnvPairs(p.Env)...)
	// Stdout and Stderr are the same writer, so os/exec calls it from one goroutine at a time.
	cmd.Stdout = out
	cmd.Stderr = out
	sysproc.Configure(cmd)
	err := cmd.Start()
	if err == nil {
		err = sysproc.Wait(cmd)
	}
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("timed out after %s", serviceLogsTimeout)
	}
	return strings.TrimRight(out.String(), "\n"), err
}

// projectEnvPairs turns a project's env into sorted KEY=value pairs.
func projectEnvPairs(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+env[k])
	}
	return pairs
}

// tailBuffer keeps the last max bytes written to it. It is not safe for concurrent use.
type tailBuffer struct {
	max int
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	if len(b.buf) > 2*b.max {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-b.max:]...)
	}
	return len(p), nil
}

// String returns the last max bytes written, starting on a rune boundary.
func (b *tailBuffer) String() string {
	return lastBytes(string(b.buf), b.max)
}

// lastBytes returns the last n bytes of s, starting on a rune boundary.
func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}

// appendEvidence adds a section to a release's log, on its own line.
func appendEvidence(log, section string) string {
	switch {
	case section == "":
		return log
	case log == "":
		return section
	case strings.HasSuffix(log, "\n"):
		return log + section
	default:
		return log + "\n" + section
	}
}

// evidenceSection renders evidence as a "## Evidence" section of an incident body, in a code
// fence that a backtick run inside the evidence cannot close early. Past evidenceCap only the
// newest output is kept, and the section says so. It returns "" for no evidence.
func evidenceSection(evidence string) string {
	if evidence == "" {
		return ""
	}
	const truncated = "...(earlier output truncated)\n"
	text := evidence
	if len(text) > evidenceCap {
		text = truncated + lastBytes(evidence, evidenceCap-len(truncated))
	}
	fence := strings.Repeat("`", max(3, longestBacktickRun(text)+1))
	return "\n## Evidence\n\n" + fence + "\n" + text + "\n" + fence + "\n"
}

// longestBacktickRun returns the length of the longest run of backticks in s.
func longestBacktickRun(s string) int {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}
