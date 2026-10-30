// Package engine is onett's brain: the scheduler and run supervisor (scheduler.go,
// runner.go, supervise.go), the recurring loops (loop_*.go), and the api.Controller
// implementation (controller.go, chat.go). This file and helpers.go are the shared contract
// those files build on.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/deploy"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/events"
	"github.com/bobbyrc/onett/internal/harness"
	"github.com/bobbyrc/onett/internal/notify"
	"github.com/bobbyrc/onett/internal/observe"
	"github.com/bobbyrc/onett/internal/scm"
	"github.com/bobbyrc/onett/internal/store"
	"github.com/bobbyrc/onett/internal/workspace"
)

// Deps are the engine's collaborators. The *For factories exist so tests can inject fakes;
// cmd/onett wires the real implementations.
type Deps struct {
	Config     *config.Config
	Store      store.Store
	Bus        events.Bus
	Notifier   notify.Notifier
	Workspaces *workspace.Manager
	Logger     *slog.Logger

	SCMFor       func(p *config.Project) (scm.SCM, error)
	HarnessFor   func(name string, h config.Harness) (harness.Harness, error)
	DeployerFor  func(p *config.Project) (deploy.Deployer, error)
	ObserversFor func(p *config.Project) (map[string]observe.Observer, error)
	// ReloadConfig re-reads the config file (config.Load on the current path by default).
	ReloadConfig func(path string) (*config.Config, error)

	// AgentEnv is added to every agent session (e.g. PATH). It must not hold the API token:
	// worker agents may not act on onett, or one could approve its own work.
	AgentEnv map[string]string
	// OrchestratorEnv is added only to orchestrator (chat) sessions: ONETT_API and
	// ONETT_TOKEN, so the orchestrator can run the `onett` CLI.
	OrchestratorEnv map[string]string
	// Now defaults to time.Now.
	Now func() time.Time
	// SchedulerTick is how often the scheduler looks for work without being woken (default 5s).
	SchedulerTick time.Duration
}

// Engine runs the factory. Create with New, then Run.
type Engine struct {
	d Deps

	cfgMu sync.RWMutex
	cfg   *config.Config

	paused atomic.Bool
	wake   chan struct{}

	runsMu sync.Mutex
	active map[string]*activeRun // by run ID

	// loopMu serialises each (loop, project) pair so a manual RunLoop never overlaps a tick.
	loopMu    sync.Mutex
	loopLocks map[string]*sync.Mutex

	// kicks holds one channel per loop name; loops.go creates them in startLoops.
	kickMu sync.Mutex
	kicks  map[string]chan string

	// projPaused holds projects paused at runtime by the owner (persisted as LoopState
	// {Name: controlLoop, Project: name, Cursor: "paused"}). Guarded by projMu.
	projMu     sync.Mutex
	projPaused map[string]bool

	// budgetMu guards budgetDay, the local date the daily-budget notice was last sent.
	budgetMu  sync.Mutex
	budgetDay string

	// issueReportLocks holds one mutex per task ID (*sync.Mutex), making reportToIssue's
	// read-comment-save of its cursor one step per task. Each terminal write starts its own
	// goroutine, so two writes of the same status would otherwise both read an empty cursor and
	// both post. A single lock for all tasks would let one slow GitHub call hold up every report.
	issueReportLocks sync.Map

	// baseCtx is Run's context: work not tied to a request (chat replies) derives from it so
	// shutdown cancels it. Set at the start of Run; nil before.
	baseCtx context.Context

	// lifeMu guards stopped; goTracked refuses new goroutines once Run is shutting down.
	lifeMu  sync.Mutex
	stopped bool
	wg      sync.WaitGroup
}

// activeRun is a live harness session the engine can cancel.
type activeRun struct {
	runID   string
	taskID  string // "" for chat runs
	project string
	cancel  context.CancelFunc
	// reason is set before cancel to say why (shown in the run's error and the task's
	// blocked reason): "cancelled by user", "stalled: no output for 15m", ...
	reason atomic.Value // string

	// sess is the live harness session, set once Start succeeds; guarded by sessMu.
	sessMu sync.Mutex
	sess   harness.Session
}

// New validates deps and returns an engine. It does not start anything.
func New(d Deps) (*Engine, error) {
	if d.Config == nil || d.Store == nil || d.Bus == nil || d.Workspaces == nil {
		return nil, errors.New("engine: Config, Store, Bus and Workspaces are required")
	}
	if d.SCMFor == nil || d.HarnessFor == nil {
		return nil, errors.New("engine: SCMFor and HarnessFor are required")
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Notifier == nil {
		d.Notifier = noopNotifier{}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.SchedulerTick == 0 {
		d.SchedulerTick = 5 * time.Second
	}
	return &Engine{
		d:          d,
		cfg:        d.Config,
		wake:       make(chan struct{}, 1),
		active:     map[string]*activeRun{},
		loopLocks:  map[string]*sync.Mutex{},
		kicks:      map[string]chan string{},
		projPaused: map[string]bool{},
	}, nil
}

// Run recovers state left by a previous process, then runs the scheduler and every loop until
// ctx is cancelled. It cancels live runs and waits for them before returning.
func (e *Engine) Run(ctx context.Context) error {
	e.lifeMu.Lock()
	e.baseCtx = ctx
	e.lifeMu.Unlock()
	if err := e.recover(ctx); err != nil {
		return fmt.Errorf("engine recover: %w", err)
	}
	// Worktrees from runs that ended in an earlier process are swept in the background, so a
	// large project's cleanup does not delay the scheduler.
	for _, p := range e.Config().Projects {
		e.goTracked(func() { e.sweepWorktrees(ctx, p) })
	}
	e.goTracked(func() { e.housekeepLoop(ctx) }) // housekeeping.go
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runScheduler(ctx) // scheduler.go
	}()
	e.startLoops(ctx) // loops.go
	<-ctx.Done()
	e.lifeMu.Lock()
	e.stopped = true
	e.lifeMu.Unlock()
	e.cancelAllRuns("onett is shutting down")
	e.wg.Wait()
	return nil
}

// errStopped is returned by work refused because the engine is shutting down.
var errStopped = errors.New("onett is shutting down")

// goTracked runs fn in a goroutine that Run waits for. It returns false (and does not run fn)
// once Run has begun shutting down, so no goroutine starts after wg.Wait.
func (e *Engine) goTracked(fn func()) bool {
	e.lifeMu.Lock()
	defer e.lifeMu.Unlock()
	if e.stopped {
		return false
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		fn()
	}()
	return true
}

// recover fixes state a crashed or restarted process left behind: runs still marked live are
// failed, their running tasks re-queued, busy conversations released, and the pause switch
// restored.
func (e *Engine) recover(ctx context.Context) error {
	st, err := e.d.Store.GetLoopState(ctx, controlLoop, "")
	if err != nil {
		return err
	}
	e.paused.Store(st.Cursor == "paused")
	states, err := e.d.Store.ListLoopStates(ctx)
	if err != nil {
		return err
	}
	e.projMu.Lock()
	for _, s := range states {
		if s.Name == controlLoop && s.Project != "" && s.Cursor == "paused" {
			e.projPaused[s.Project] = true
		}
	}
	e.projMu.Unlock()

	runs, err := e.d.Store.ListRuns(ctx, store.RunFilter{
		Statuses: []domain.RunStatus{domain.RunStarting, domain.RunRunning}, Limit: 1000,
	})
	if err != nil {
		return err
	}
	for _, r := range runs {
		now := e.d.Now().UTC()
		if _, err := e.d.Store.UpdateRun(ctx, r.ID, func(r *domain.Run) error {
			r.Status, r.Error, r.EndedAt = domain.RunFailed, "onett restarted during this run", &now
			return nil
		}); err != nil {
			return err
		}
	}

	// No run is live at startup, so every running task is orphaned — including one claimed by
	// the scheduler just before a crash, before its run row existed.
	tasks, err := e.d.Store.ListTasks(ctx, store.TaskFilter{
		Statuses: []domain.TaskStatus{domain.TaskRunning}, Limit: 10000,
	})
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.Attempts >= t.MaxAttempts {
			// The attempt that was running counted, so this task has no attempts left.
			e.blockTaskFrom(context.WithoutCancel(ctx), t.ID, []domain.TaskStatus{domain.TaskRunning},
				"onett restarted during the last attempt and attempts are exhausted")
			continue
		}
		if _, _, err := e.setTaskFrom(ctx, t.ID, []domain.TaskStatus{domain.TaskRunning},
			"re-queued after restart", func(t *domain.Task) error {
				t.Status = domain.TaskQueued
				return nil
			}); err != nil {
			return err
		}
	}

	convs, err := e.d.Store.ListConversations(ctx, 1000)
	if err != nil {
		return err
	}
	for _, c := range convs {
		if c.Busy {
			if _, err := e.d.Store.UpdateConversation(ctx, c.ID, func(c *domain.Conversation) error {
				c.Busy = false
				c.ActiveRunID = ""
				return nil
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// controlLoop is the LoopState name that persists the pause switch (Cursor "paused").
const controlLoop = "control"

type noopNotifier struct{}

func (noopNotifier) Notify(context.Context, notify.Notification) error { return nil }
