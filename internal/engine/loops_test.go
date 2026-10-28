package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bobbyrc/foundry/internal/api"
	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/deploy"
	"github.com/bobbyrc/foundry/internal/domain"
	"github.com/bobbyrc/foundry/internal/events"
	"github.com/bobbyrc/foundry/internal/harness"
	"github.com/bobbyrc/foundry/internal/observe"
	"github.com/bobbyrc/foundry/internal/scm"
	scmfake "github.com/bobbyrc/foundry/internal/scm/fake"
	"github.com/bobbyrc/foundry/internal/store"
	"github.com/bobbyrc/foundry/internal/store/sqlite"
	"github.com/bobbyrc/foundry/internal/workspace"
)

// lpYAML is the config every loop test starts from. %[3]s is extra lines under project demo
// (indent four spaces); %[4]s is extra top-level lines (column zero).
const lpYAML = `server:
  data_dir: %[1]s
projects:
  demo:
    repo: bobbyrc/demo
    path: %[2]s
%[3]s%[4]s`

// lpDoc renders lpYAML for one project named demo.
func lpDoc(t *testing.T, path, projectExtra, topExtra string) string {
	t.Helper()
	return fmt.Sprintf(lpYAML, t.TempDir(), path, projectExtra, topExtra)
}

// lpPath is a project path for tests that never touch the repository.
func lpPath(t *testing.T) string {
	return filepath.Join(t.TempDir(), "demo")
}

// lpDeployer is a deploy.Deployer that records what it was asked to do. Tests set its fields.
type lpDeployer struct {
	mu          sync.Mutex
	seq         int
	deployed    []string
	rolledBack  [][2]string
	status      deploy.Status
	deployErr   error
	rollbackErr error
}

func (d *lpDeployer) Deploy(_ context.Context, ref string) (deploy.Handle, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deployed = append(d.deployed, ref)
	if d.deployErr != nil {
		return deploy.Handle{}, d.deployErr
	}
	d.seq++
	id := fmt.Sprintf("prov-%d", d.seq)
	return deploy.Handle{ID: id, URL: "https://demo.example/" + id}, nil
}

func (d *lpDeployer) Status(_ context.Context, _ string) (deploy.Status, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status, nil
}

func (d *lpDeployer) Rollback(_ context.Context, id, ref string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rolledBack = append(d.rolledBack, [2]string{id, ref})
	return d.rollbackErr
}

// lpObserver is an observe.Observer that returns canned observations, or an error.
type lpObserver struct {
	mu  sync.Mutex
	obs []domain.SignalObservation
	err error
}

func (o *lpObserver) Poll(_ context.Context, _ time.Time) ([]domain.SignalObservation, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return nil, o.err
	}
	return append([]domain.SignalObservation(nil), o.obs...), nil
}

// lpEnv is a real engine over an in-memory store, with a fake SCM, a recording deployer and a
// canned observer. Its clock is env.now and moves only when a test moves it.
type lpEnv struct {
	t    *testing.T
	ctx  context.Context
	e    *Engine
	st   *sqlite.Store
	fake *scmfake.Fake
	dep  *lpDeployer
	obs  *lpObserver
	now  time.Time
}

// lpNewEngine builds an engine from a full config document.
func lpNewEngine(t *testing.T, doc string) *lpEnv {
	t.Helper()
	cfg, err := config.Parse([]byte(doc), "")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	st, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	env := &lpEnv{
		t:    t,
		ctx:  context.Background(),
		st:   st,
		fake: scmfake.New(),
		dep:  &lpDeployer{},
		obs:  &lpObserver{},
		now:  time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	e, err := New(Deps{
		Config:     cfg,
		Store:      st,
		Bus:        events.NewBus(st, logger),
		Workspaces: workspace.New(t.TempDir()),
		Logger:     logger,
		SCMFor: func(*config.Project) (scm.SCM, error) {
			return env.fake, nil
		},
		HarnessFor: func(string, config.Harness) (harness.Harness, error) {
			return nil, errors.New("loop tests never start harnesses")
		},
		DeployerFor: func(*config.Project) (deploy.Deployer, error) {
			return env.dep, nil
		},
		ObserversFor: func(*config.Project) (map[string]observe.Observer, error) {
			return map[string]observe.Observer{"sentry": env.obs}, nil
		},
		Now: func() time.Time { return env.now },
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	env.e = e
	return env
}

// run runs a loop for project demo now, as a manual run, and fails the test on error.
func (env *lpEnv) run(loop string) {
	env.t.Helper()
	if err := env.e.RunLoop(env.ctx, loop, "demo"); err != nil {
		env.t.Fatalf("run %s: %v", loop, err)
	}
}

// tasks lists demo's tasks of kind, or of every kind when kind is "".
func (env *lpEnv) tasks(kind domain.TaskKind) []domain.Task {
	env.t.Helper()
	out, err := env.st.ListTasks(env.ctx, store.TaskFilter{Project: "demo", Kind: kind, Limit: 1000})
	if err != nil {
		env.t.Fatalf("list tasks: %v", err)
	}
	return out
}

// task reads one task by ID.
func (env *lpEnv) task(id string) domain.Task {
	env.t.Helper()
	t, err := env.st.GetTask(env.ctx, id)
	if err != nil {
		env.t.Fatalf("get task %s: %v", id, err)
	}
	return t
}

// lpHealth is an HTTP server whose status code the test controls.
type lpHealth struct {
	srv  *httptest.Server
	code atomic.Int32
}

func lpNewHealth(t *testing.T) *lpHealth {
	t.Helper()
	h := &lpHealth{}
	h.code.Store(http.StatusOK)
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(h.code.Load()))
	}))
	t.Cleanup(h.srv.Close)
	return h
}

// lpRepo is a bare remote, a seed clone that authors commits, and a clone the engine reads.
type lpRepo struct {
	seed, remote, clone string
}

// lpNewRepo creates a remote whose main branch has one commit, and a clone of it.
func lpNewRepo(t *testing.T) lpRepo {
	t.Helper()
	root := t.TempDir()
	r := lpRepo{
		seed:   filepath.Join(root, "seed"),
		remote: filepath.Join(root, "remote.git"),
		clone:  filepath.Join(root, "clone"),
	}
	lpGit(t, root, "init", "--bare", "--initial-branch=main", r.remote)
	lpGit(t, root, "init", "--initial-branch=main", r.seed)
	lpGit(t, r.seed, "remote", "add", "origin", r.remote)
	lpWrite(t, r.seed, "README.md", "demo\n")
	lpGit(t, r.seed, "add", "-A")
	lpGit(t, r.seed, "commit", "-m", "init")
	lpGit(t, r.seed, "push", "origin", "main")
	lpGit(t, root, "clone", r.remote, r.clone)
	return r
}

// pushBranch commits files on a new branch cut from main and pushes it to origin.
func (r lpRepo) pushBranch(t *testing.T, branch string, files map[string]string) {
	t.Helper()
	lpGit(t, r.seed, "checkout", "-B", branch, "main")
	for path, content := range files {
		lpWrite(t, r.seed, path, content)
	}
	lpGit(t, r.seed, "add", "-A")
	lpGit(t, r.seed, "commit", "-m", "work on "+branch)
	lpGit(t, r.seed, "push", "-f", "origin", branch)
	lpGit(t, r.seed, "checkout", "main")
}

// lpGit runs git in dir and fails the test on error.
func lpGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{
		"-c", "user.name=foundry-test",
		"-c", "user.email=test@example.invalid",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// lpWrite writes a file under dir, creating parent directories.
func lpWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoopsListsEveryLoopAndProject(t *testing.T) {
	doc := fmt.Sprintf(`server:
  data_dir: %s
projects:
  demo:
    repo: bobbyrc/demo
    path: %s
  other:
    repo: bobbyrc/other
    path: %s
    loops:
      pr_shepherd:
        enabled: false
`, t.TempDir(), lpPath(t), lpPath(t))
	env := lpNewEngine(t, doc)

	rows := env.e.Loops(env.ctx)
	if len(rows) != 10 {
		t.Fatalf("got %d rows, want 5 loops x 2 projects", len(rows))
	}
	var keys []string
	for _, r := range rows {
		keys = append(keys, r.Name+"/"+r.Project)
	}
	wantFirst := []string{"deploy_watch/demo", "deploy_watch/other", "friction/demo"}
	for i, w := range wantFirst {
		if keys[i] != w {
			t.Fatalf("row %d = %s, want %s (rows sorted by name then project)", i, keys[i], w)
		}
	}
	byKey := map[string]api.LoopView{}
	for _, r := range rows {
		byKey[r.Name+"/"+r.Project] = r
	}
	if v := byKey["pr_shepherd/other"]; v.Enabled {
		t.Fatalf("pr_shepherd/other should be disabled by its project override: %+v", v)
	}
	if v := byKey["pr_shepherd/demo"]; !v.Enabled || v.Every != "1m0s" {
		t.Fatalf("pr_shepherd/demo = %+v, want enabled every 1m0s", v)
	}
	if v := byKey["issue_intake/demo"]; v.Every != "5m0s" {
		t.Fatalf("issue_intake/demo every = %q, want 5m0s", v.Every)
	}
	if v := byKey["deploy_watch/demo"]; v.Every != "30s" {
		t.Fatalf("deploy_watch/demo every = %q, want 30s", v.Every)
	}
}

func TestLoopRunRejectsUnknownNames(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))

	if err := env.e.RunLoop(env.ctx, "nope", ""); !errors.Is(err, api.ErrInvalid) {
		t.Fatalf("unknown loop: err = %v, want api.ErrInvalid", err)
	}
	if err := env.e.RunLoop(env.ctx, loopPRShepherd, "missing"); !errors.Is(err, api.ErrInvalid) {
		t.Fatalf("unknown project: err = %v, want api.ErrInvalid", err)
	}
}

func TestLoopRunRecordsErrorOnce(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	env.obs.err = errors.New("sentry is down")

	if err := env.e.RunLoop(env.ctx, loopProdWatch, "demo"); err == nil {
		t.Fatal("RunLoop returned nil, want the observer error")
	}
	if err := env.e.RunLoop(env.ctx, loopProdWatch, "demo"); err == nil {
		t.Fatal("second RunLoop returned nil, want the observer error")
	}

	var row api.LoopView
	for _, r := range env.e.Loops(env.ctx) {
		if r.Name == loopProdWatch && r.Project == "demo" {
			row = r
		}
	}
	if row.LastError == "" || row.LastRunAt.IsZero() {
		t.Fatalf("prod_watch row = %+v, want LastError and LastRunAt set", row)
	}

	evs, err := env.st.ListEvents(env.ctx, store.EventFilter{Project: "demo", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range evs {
		if ev.Type == domain.EvLoopError {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("got %d loop.error events for the same error twice, want 1", n)
	}
}

func TestLoopScheduledRunSkipsDisabledLoop(t *testing.T) {
	doc := fmt.Sprintf(`server:
  data_dir: %s
projects:
  demo:
    repo: bobbyrc/demo
    path: %s
    loops:
      friction:
        enabled: false
`, t.TempDir(), lpPath(t))
	env := lpNewEngine(t, doc)

	if err := env.e.runLoopOnce(env.ctx, loopFriction, "demo", false); err != nil {
		t.Fatalf("scheduled run of a disabled loop: %v", err)
	}
	for _, r := range env.e.Loops(env.ctx) {
		if r.Name == loopFriction && r.Project == "demo" && !r.LastRunAt.IsZero() {
			t.Fatalf("disabled loop ran on schedule: %+v", r)
		}
	}
}

func TestLoopKickIsRemembered(t *testing.T) {
	env := lpNewEngine(t, lpDoc(t, lpPath(t), "", ""))
	env.e.kickLoop(loopDeployWatch, "demo")
	env.e.kickLoop(loopDeployWatch, "demo")
	if got := len(env.e.loopKickChan(loopDeployWatch)); got != 2 {
		t.Fatalf("buffered kicks = %d, want 2", got)
	}
}
