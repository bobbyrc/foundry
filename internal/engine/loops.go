package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bobbyrc/foundry/internal/api"
	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
)

// Loop names. They are the names used in config, the API and LoopState.
const (
	loopIssueIntake = "issue_intake"
	loopPRShepherd  = "pr_shepherd"
	loopDeployWatch = "deploy_watch"
	loopProdWatch   = "prod_watch"
	loopFriction    = "friction"
)

// loopNames lists every loop, in the order they start.
var loopNames = []string{loopIssueIntake, loopPRShepherd, loopDeployWatch, loopProdWatch, loopFriction}

// loopWakeInterval is how often each loop checks which projects are due.
const loopWakeInterval = 5 * time.Second

// loopKickBuffer is how many kicks a loop can hold before further kicks are dropped.
const loopKickBuffer = 64

// loopFn runs one loop for one project. It returns an error for the loop state and the log.
type loopFn func(ctx context.Context, p *config.Project) error

// loopFuncs maps each loop name to its implementation.
func (e *Engine) loopFuncs() map[string]loopFn {
	return map[string]loopFn{
		loopIssueIntake: e.intakeLoop,
		loopPRShepherd:  e.shepherdLoop,
		loopDeployWatch: e.deployLoop,
		loopProdWatch:   e.prodLoop,
		loopFriction:    e.frictionLoop,
	}
}

// loopKicks holds each engine's kick channels. The Engine struct has no field for them, so they
// live here, keyed by engine.
var loopKicks = struct {
	sync.Mutex
	byEngine map[*Engine]map[string]chan string
}{byEngine: map[*Engine]map[string]chan string{}}

// loopKickChan returns the channel that carries project names to loop name, creating it on
// first use so a kick before startLoops is kept.
func (e *Engine) loopKickChan(name string) chan string {
	loopKicks.Lock()
	defer loopKicks.Unlock()
	chans := loopKicks.byEngine[e]
	if chans == nil {
		chans = map[string]chan string{}
		loopKicks.byEngine[e] = chans
	}
	ch := chans[name]
	if ch == nil {
		ch = make(chan string, loopKickBuffer)
		chans[name] = ch
	}
	return ch
}

// startLoops starts one goroutine per loop, tracked by e.wg. Each goroutine returns when ctx ends.
func (e *Engine) startLoops(ctx context.Context) {
	for _, name := range loopNames {
		name := name
		kicks := e.loopKickChan(name)
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			e.loopForever(ctx, name, kicks)
		}()
	}
}

// loopForever wakes every loopWakeInterval or when a project is kicked, and runs the loop for
// each project that is due.
func (e *Engine) loopForever(ctx context.Context, name string, kicks <-chan string) {
	ticker := time.NewTicker(loopWakeInterval)
	defer ticker.Stop()
	for {
		kicked := map[string]bool{}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case project := <-kicks:
			kicked[project] = true
		}
		for drained := false; !drained; {
			select {
			case project := <-kicks:
				kicked[project] = true
			default:
				drained = true
			}
		}
		e.loopTick(ctx, name, kicked)
	}
}

// loopTick runs loop name for every configured project that was kicked or whose interval has
// passed. Errors are already logged and saved by runLoopOnce.
func (e *Engine) loopTick(ctx context.Context, name string, kicked map[string]bool) {
	cfg := e.Config()
	now := e.d.Now()
	for _, project := range cfg.ProjectNames() {
		settings := loopSettingsFor(cfg.LoopsFor(project), name)
		if !settings.On() {
			continue
		}
		if !kicked[project] {
			st, err := e.d.Store.GetLoopState(ctx, name, project)
			if err != nil {
				e.d.Logger.Error("loop state", "loop", name, "project", project, "err", err)
				continue
			}
			if now.Sub(st.LastRunAt) < settings.Every.Duration {
				continue
			}
		}
		_ = e.runLoopOnce(ctx, name, project, false)
	}
}

// kickLoop asks a loop to run soon for one project. Never blocks.
func (e *Engine) kickLoop(name, project string) {
	select {
	case e.loopKickChan(name) <- project:
	default:
	}
}

// loopLock returns the mutex that serialises one loop for one project.
func (e *Engine) loopLock(name, project string) *sync.Mutex {
	key := name + "/" + project
	e.loopMu.Lock()
	defer e.loopMu.Unlock()
	m := e.loopLocks[key]
	if m == nil {
		m = &sync.Mutex{}
		e.loopLocks[key] = m
	}
	return m
}

// runLoopOnce runs loop name for project and records the outcome in LoopState. It does nothing
// when the project is paused or, for a scheduled run, when the loop is disabled. A manual run
// ignores the enabled flag.
func (e *Engine) runLoopOnce(ctx context.Context, name, project string, manual bool) error {
	fn, ok := e.loopFuncs()[name]
	if !ok {
		return fmt.Errorf("%w: unknown loop %q", api.ErrInvalid, name)
	}
	if e.projectPaused(project) {
		return nil
	}
	cfg := e.Config()
	if !manual && !loopSettingsFor(cfg.LoopsFor(project), name).On() {
		return nil
	}
	p := cfg.Projects[project]

	lock := e.loopLock(name, project)
	lock.Lock()
	defer lock.Unlock()

	err := fn(ctx, p)

	// Reload before saving: the loop may have stored its own cursor.
	st, gerr := e.d.Store.GetLoopState(ctx, name, project)
	if gerr != nil {
		e.d.Logger.Error("loop state", "loop", name, "project", project, "err", gerr)
		return errors.Join(err, gerr)
	}
	prev := st.LastError
	st.LastRunAt = e.d.Now().UTC()
	st.LastError = ""
	if err != nil {
		st.LastError = err.Error()
	}
	if serr := e.d.Store.SaveLoopState(ctx, st); serr != nil {
		e.d.Logger.Error("save loop state", "loop", name, "project", project, "err", serr)
	}
	if err != nil {
		e.d.Logger.Error("loop failed", "loop", name, "project", project, "err", err)
		if st.LastError != prev {
			e.d.Bus.Emit(ctx, domain.EvLoopError, project, "", "",
				fmt.Sprintf("%s: %s", name, st.LastError),
				map[string]string{"loop": name, "error": st.LastError})
		}
	}
	return err
}

// Loops implements api.Controller. It returns one row per loop and project, sorted by loop name
// then project.
func (e *Engine) Loops(ctx context.Context) []api.LoopView {
	cfg := e.Config()
	states := map[string]domain.LoopState{}
	list, err := e.d.Store.ListLoopStates(ctx)
	if err != nil {
		e.d.Logger.Error("list loop states", "err", err)
	}
	for _, s := range list {
		states[s.Name+"/"+s.Project] = s
	}

	out := []api.LoopView{}
	for _, name := range loopNames {
		for _, project := range cfg.ProjectNames() {
			l := loopSettingsFor(cfg.LoopsFor(project), name)
			st := states[name+"/"+project]
			out = append(out, api.LoopView{
				Name:      name,
				Project:   project,
				Enabled:   l.On(),
				Every:     l.Every.Duration.String(),
				LastRunAt: st.LastRunAt,
				LastError: st.LastError,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Project < out[j].Project
	})
	return out
}

// RunLoop implements api.Controller. Project "" runs the loop for every project. It runs
// synchronously and returns the joined errors.
func (e *Engine) RunLoop(ctx context.Context, name, project string) error {
	if !isLoopName(name) {
		return fmt.Errorf("%w: unknown loop %q", api.ErrInvalid, name)
	}
	cfg := e.Config()
	targets := []string{project}
	if project == "" {
		targets = cfg.ProjectNames()
	} else if _, ok := cfg.Projects[project]; !ok {
		return fmt.Errorf("%w: unknown project %q", api.ErrInvalid, project)
	}
	var errs []error
	for _, t := range targets {
		if err := e.runLoopOnce(ctx, name, t, true); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t, err))
		}
	}
	return errors.Join(errs...)
}

func isLoopName(name string) bool {
	for _, n := range loopNames {
		if n == name {
			return true
		}
	}
	return false
}

// loopSettingsFor returns the settings of one loop from a project's effective loop config.
func loopSettingsFor(l config.Loops, name string) config.Loop {
	switch name {
	case loopIssueIntake:
		return l.IssueIntake.Loop
	case loopPRShepherd:
		return l.PRShepherd
	case loopDeployWatch:
		return l.DeployWatch
	case loopProdWatch:
		return l.ProdWatch.Loop
	case loopFriction:
		return l.Friction.Loop
	}
	return config.Loop{}
}

// isGitHubRepo reports whether the project's repo is owner/name on GitHub, rather than a local
// path or URL. Intake and friction only work on GitHub repositories.
func isGitHubRepo(p *config.Project) bool {
	if strings.HasPrefix(p.Repo, "/") || strings.HasPrefix(p.Repo, ".") || strings.Contains(p.Repo, ":") {
		return false
	}
	owner, name, ok := strings.Cut(p.Repo, "/")
	return ok && owner != "" && name != "" && !strings.Contains(name, "/")
}
