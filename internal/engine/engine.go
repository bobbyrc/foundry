// Package engine is foundry's brain: the scheduler and run supervisor (scheduler.go,
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

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/deploy"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/events"
	"github.com/bobbyrc/foundry/internal/harness"
	"github.com/bobbyrc/foundry/internal/notify"
	"github.com/bobbyrc/foundry/internal/observe"
	"github.com/bobbyrc/foundry/internal/scm"
	"github.com/bobbyrc/foundry/internal/store"
	"github.com/bobbyrc/foundry/internal/workspace"
)

// Deps are the engine's collaborators. The *For factories exist so tests can inject fakes;
// cmd/foundry wires the real implementations.
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

	// AgentEnv is added to every agent session: FOUNDRY_API, FOUNDRY_TOKEN, and PATH with the
	// foundry binary's directory first, so agents can run the `foundry` CLI.
	AgentEnv map[string]string
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

	wg sync.WaitGroup
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
		d:         d,
		cfg:       d.Config,
		wake:      make(chan struct{}, 1),
		active:    map[string]*activeRun{},
		loopLocks: map[string]*sync.Mutex{},
	}, nil
}

// Run recovers state left by a previous process, then runs the scheduler and every loop until
// ctx is cancelled. It cancels live runs and waits for them before returning.
func (e *Engine) Run(ctx context.Context) error {
	if err := e.recover(ctx); err != nil {
		return fmt.Errorf("engine recover: %w", err)
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runScheduler(ctx) // scheduler.go
	}()
	e.startLoops(ctx) // loops.go
	<-ctx.Done()
	e.cancelAllRuns("foundry is shutting down")
	e.wg.Wait()
	return nil
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

	runs, err := e.d.Store.ListRuns(ctx, store.RunFilter{
		Statuses: []domain.RunStatus{domain.RunStarting, domain.RunRunning}, Limit: 1000,
	})
	if err != nil {
		return err
	}
	for _, r := range runs {
		now := e.d.Now().UTC()
		if _, err := e.d.Store.UpdateRun(ctx, r.ID, func(r *domain.Run) error {
			r.Status, r.Error, r.EndedAt = domain.RunFailed, "foundry restarted during this run", &now
			return nil
		}); err != nil {
			return err
		}
		if r.TaskID == "" {
			continue
		}
		if _, err := e.setTask(ctx, r.TaskID, "re-queued after restart", func(t *domain.Task) error {
			if t.Status == domain.TaskRunning {
				t.Status = domain.TaskQueued
			}
			return nil
		}); err != nil && !errors.Is(err, store.ErrNotFound) {
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
