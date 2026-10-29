package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onlet/internal/config"
	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/events"
	"github.com/bobbyrc/onlet/internal/store"
	"github.com/bobbyrc/onlet/internal/store/sqlite"
)

const testToken = "test-token-value"

// testRequest is httptest.NewRequest with a Host the default allowlist accepts. The default Host
// of httptest is example.com, which the host guard rejects.
func testRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Host = "localhost:7777"
	return r
}

// fakeCtl is a Controller that records calls. errs maps a method name to the error it returns.
type fakeCtl struct {
	mu          sync.Mutex
	cfg         *config.Config
	paused      bool
	errs        map[string]error
	calls       []string
	created     domain.Task
	loops       []LoopView
	decided     string
	loopRun     string
	chatConvID  string
	chatText    string
	reloadCalls int
	// projectPaused maps a project to its runtime pause owner ("owner") when set.
	projectPaused map[string]string
}

func (f *fakeCtl) note(name string) error {
	f.calls = append(f.calls, name)
	return f.errs[name]
}

func (f *fakeCtl) CreateTask(_ context.Context, t domain.Task) (domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = t
	if err := f.note("CreateTask"); err != nil {
		return domain.Task{}, err
	}
	t.ID = "task-new"
	t.Status = domain.TaskQueued
	return t, nil
}

func (f *fakeCtl) CancelTask(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.note("CancelTask:" + id)
}

func (f *fakeCtl) RetryTask(_ context.Context, id, note string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.note(fmt.Sprintf("RetryTask:%s:%s", id, note))
}

func (f *fakeCtl) CancelRun(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.note("CancelRun:" + id)
}

func (f *fakeCtl) DecideApproval(_ context.Context, id string, approve bool, note string) (domain.Approval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decided = fmt.Sprintf("%s:%t:%s", id, approve, note)
	if err := f.note("DecideApproval"); err != nil {
		return domain.Approval{}, err
	}
	status := domain.ApprovalRejected
	if approve {
		status = domain.ApprovalApproved
	}
	return domain.Approval{ID: id, Status: status, Note: note}, nil
}

func (f *fakeCtl) Loops(context.Context) []LoopView {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loops
}

func (f *fakeCtl) RunLoop(_ context.Context, name, project string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loopRun = name + "?project=" + project
	return f.note("RunLoop")
}

func (f *fakeCtl) Config() *config.Config {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg
}

func (f *fakeCtl) ReloadConfig(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reloadCalls++
	return f.note("ReloadConfig")
}

func (f *fakeCtl) Paused() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.paused
}

func (f *fakeCtl) SetPaused(_ context.Context, p bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = p
	return f.note(fmt.Sprintf("SetPaused:%t", p))
}

func (f *fakeCtl) ProjectPausedBy(project string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cfg != nil && f.cfg.Projects[project] != nil && f.cfg.Projects[project].Paused {
		return "config"
	}
	return f.projectPaused[project]
}

func (f *fakeCtl) SetProjectPaused(_ context.Context, project string, paused bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.note(fmt.Sprintf("SetProjectPaused:%s:%t", project, paused)); err != nil {
		return err
	}
	if f.cfg.Projects[project] == nil {
		return invalidf("unknown project %q", project)
	}
	if !paused && f.cfg.Projects[project].Paused {
		return fmt.Errorf("%w: project %q is paused in config", ErrConflict, project)
	}
	if f.projectPaused == nil {
		f.projectPaused = map[string]string{}
	}
	if paused {
		f.projectPaused[project] = "owner"
	} else {
		delete(f.projectPaused, project)
	}
	return nil
}

func (f *fakeCtl) SendChat(_ context.Context, convID, text string) (domain.ChatMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatConvID, f.chatText = convID, text
	if err := f.note("SendChat"); err != nil {
		return domain.ChatMessage{}, err
	}
	return domain.ChatMessage{ID: "msg-1", ConversationID: convID, Role: "user", Text: text}, nil
}

// BuiltinPrompt and AgentPrompt stand in for the engine's embedded prompts (the engine imports
// this package, so the test cannot import it). A built-in reads as "# Role: <name>".
func (f *fakeCtl) BuiltinPrompt(name string) (string, error) {
	if _, ok := config.BuiltinAgents()[name]; !ok {
		return "", fmt.Errorf("unknown builtin prompt %q", name)
	}
	return "# Role: " + name + "\n", nil
}

func (f *fakeCtl) AgentPrompt(a config.Agent) (string, error) {
	if a.PromptText != "" {
		return a.PromptText, nil
	}
	if name, ok := strings.CutPrefix(a.Prompt, "builtin:"); ok {
		return f.BuiltinPrompt(name)
	}
	b, err := os.ReadFile(a.Prompt)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func testConfig() *config.Config {
	return &config.Config{
		Projects: map[string]*config.Project{
			"onlet": {Repo: "bobbyrc/onlet", Env: map[string]string{"API_SECRET": "hunter2"}},
		},
		Harness: map[string]config.Harness{
			"codex":  {Type: "codex"},
			"claude": {Type: "claude-code"},
		},
		Agents: map[string]config.Agent{
			"implementer": {Harness: "codex", Prompt: "builtin:implementer", Description: "writes code"},
			"reviewer":    {Harness: "claude", Prompt: "builtin:reviewer", Model: "opus"},
		},
		Routing: map[string]string{
			"feature": "implementer",
			"bugfix":  "implementer",
			"review":  "reviewer",
		},
	}
}

type harness struct {
	h   http.Handler
	st  *sqlite.Store
	bus events.Bus
	ctl *fakeCtl
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewBus(st, logger)
	ctl := &fakeCtl{cfg: testConfig(), errs: map[string]error{}}
	h := NewServer(Options{
		Store:      st,
		Bus:        bus,
		Controller: ctl,
		Token:      testToken,
		Version:    "test-version",
		Web:        fstest.MapFS{},
		Logger:     logger,
	})
	return &harness{h: h, st: st, bus: bus, ctl: ctl}
}

// do sends a request through the handler. A non-empty body is sent as JSON.
func (x *harness) do(t *testing.T, method, target, body string, token string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := testRequest(method, target, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	x.h.ServeHTTP(rec, req)
	return rec
}

// call is do with the correct token.
func (x *harness) call(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	return x.do(t, method, target, body, testToken)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), v), "body: %s", rec.Body.String())
}

func (x *harness) makeTask(t *testing.T, project string, status domain.TaskStatus, parent string) domain.Task {
	t.Helper()
	tk, err := x.st.CreateTask(context.Background(), domain.Task{
		Project: project, Kind: domain.KindFeature, Title: "t", Status: status, ParentID: parent,
		Source: "api", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	require.NoError(t, err)
	return tk
}

func TestAuth(t *testing.T) {
	x := newHarness(t)

	rec := x.do(t, http.MethodGet, "/api/v1/stats", "", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.JSONEq(t, `{"error":"unauthorized"}`, rec.Body.String())

	rec = x.do(t, http.MethodGet, "/api/v1/stats", "", "wrong")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = x.do(t, http.MethodGet, "/api/v1/stats?token="+testToken, "", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "?token= is accepted only on /events")

	rec = x.do(t, http.MethodGet, "/api/v1/health", "", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"ok":true,"version":"test-version"}`, rec.Body.String())
}

func TestAuthEmptyConfiguredToken(t *testing.T) {
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	h := NewServer(Options{Store: st, Bus: events.NewBus(st, nil), Controller: &fakeCtl{cfg: testConfig()}, Web: fstest.MapFS{}})
	req := testRequest(http.MethodGet, "/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUnknownAPIPathIsJSON404(t *testing.T) {
	x := newHarness(t)
	rec := x.call(t, http.MethodGet, "/api/v1/nope", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"error":"not found"}`, rec.Body.String())

	rec = x.do(t, http.MethodGet, "/api/nope", "", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestCreateTask(t *testing.T) {
	x := newHarness(t)
	body := `{"project":"onlet","kind":"feature","title":"Add x","body":"do it","role":"implementer",
		"priority":5,"parent_id":"parent-1"}`
	rec := x.call(t, http.MethodPost, "/api/v1/tasks", body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var got domain.Task
	decode(t, rec, &got)
	assert.Equal(t, "task-new", got.ID)

	c := x.ctl.created
	assert.Equal(t, "onlet", c.Project)
	assert.Equal(t, domain.KindFeature, c.Kind)
	assert.Equal(t, "Add x", c.Title)
	assert.Equal(t, "do it", c.Body)
	assert.Equal(t, "implementer", c.Role)
	assert.Equal(t, 5, c.Priority)
	assert.Equal(t, "parent-1", c.ParentID)
	assert.Equal(t, "api", c.Source, "source defaults to api")
}

func TestCreateTaskRejectsUnknownField(t *testing.T) {
	x := newHarness(t)
	rec := x.call(t, http.MethodPost, "/api/v1/tasks", `{"project":"onlet","bogus":1}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, x.ctl.calls, "controller must not run on a bad body")
}

func TestListTasksStatusFilter(t *testing.T) {
	x := newHarness(t)
	x.makeTask(t, "onlet", domain.TaskQueued, "")
	x.makeTask(t, "onlet", domain.TaskRunning, "")
	x.makeTask(t, "onlet", domain.TaskDone, "")

	rec := x.call(t, http.MethodGet, "/api/v1/tasks?status=queued,running", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var tasks []domain.Task
	decode(t, rec, &tasks)
	assert.Len(t, tasks, 2)

	rec = x.call(t, http.MethodGet, "/api/v1/tasks?project=other", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `[]`, rec.Body.String(), "empty lists are [] not null")

	rec = x.call(t, http.MethodGet, "/api/v1/tasks?limit=abc", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTaskDetail(t *testing.T) {
	x := newHarness(t)
	parent := x.makeTask(t, "onlet", domain.TaskRunning, "")
	child := x.makeTask(t, "onlet", domain.TaskQueued, parent.ID)
	other := x.makeTask(t, "onlet", domain.TaskQueued, "")
	ctx := context.Background()
	_, err := x.st.CreateRun(ctx, domain.Run{TaskID: parent.ID, Project: "onlet", Status: domain.RunRunning, StartedAt: time.Now()})
	require.NoError(t, err)
	_, err = x.st.CreateApproval(ctx, domain.Approval{TaskID: parent.ID, Project: "onlet", Kind: "merge", Status: domain.ApprovalPending})
	require.NoError(t, err)
	_, err = x.st.CreateApproval(ctx, domain.Approval{TaskID: other.ID, Project: "onlet", Kind: "merge", Status: domain.ApprovalPending})
	require.NoError(t, err)

	rec := x.call(t, http.MethodGet, "/api/v1/tasks/"+parent.ID, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var det struct {
		Task      domain.Task       `json:"task"`
		Runs      []domain.Run      `json:"runs"`
		Approvals []domain.Approval `json:"approvals"`
		Children  []domain.Task     `json:"children"`
	}
	decode(t, rec, &det)
	assert.Equal(t, parent.ID, det.Task.ID)
	assert.Len(t, det.Runs, 1)
	assert.Len(t, det.Approvals, 1, "only approvals for this task")
	require.Len(t, det.Children, 1)
	assert.Equal(t, child.ID, det.Children[0].ID)

	rec = x.call(t, http.MethodGet, "/api/v1/tasks/"+child.ID, "")
	assert.JSONEq(t, `[]`, toJSON(t, rec, "runs"), "no runs gives []")
	assert.JSONEq(t, `[]`, toJSON(t, rec, "children"))

	rec = x.call(t, http.MethodGet, "/api/v1/tasks/missing", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// toJSON returns the JSON text of one top-level key of a response body.
func toJSON(t *testing.T, rec *httptest.ResponseRecorder, key string) string {
	t.Helper()
	var m map[string]json.RawMessage
	decode(t, rec, &m)
	return string(m[key])
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		msg    string
	}{
		{"not found", fmt.Errorf("task x: %w", store.ErrNotFound), http.StatusNotFound, "task x: not found"},
		{"invalid", fmt.Errorf("%w: bad project", ErrInvalid), http.StatusBadRequest, "invalid request: bad project"},
		{"conflict", fmt.Errorf("%w: busy", ErrConflict), http.StatusConflict, "conflict: busy"},
		{"internal", errors.New("disk on fire"), http.StatusInternalServerError, "internal error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := newHarness(t)
			x.ctl.errs["CancelTask:t1"] = tc.err
			rec := x.call(t, http.MethodPost, "/api/v1/tasks/t1/cancel", "")
			assert.Equal(t, tc.status, rec.Code)
			assert.JSONEq(t, fmt.Sprintf(`{"error":%q}`, tc.msg), rec.Body.String())
		})
	}
}

func TestCancelAndRetry(t *testing.T) {
	x := newHarness(t)
	rec := x.call(t, http.MethodPost, "/api/v1/tasks/t1/cancel", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)

	rec = x.call(t, http.MethodPost, "/api/v1/tasks/t1/retry", "")
	assert.Equal(t, http.StatusNoContent, rec.Code, "retry body is optional")

	rec = x.call(t, http.MethodPost, "/api/v1/tasks/t1/retry", `{"note":"try again"}`)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	rec = x.call(t, http.MethodPost, "/api/v1/runs/r1/cancel", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)

	assert.Equal(t, []string{
		"CancelTask:t1", "RetryTask:t1:", "RetryTask:t1:try again", "CancelRun:r1",
	}, x.ctl.calls)
}

func TestApprovals(t *testing.T) {
	x := newHarness(t)
	ctx := context.Background()
	_, err := x.st.CreateApproval(ctx, domain.Approval{TaskID: "t1", Project: "onlet", Kind: "merge", Status: domain.ApprovalPending})
	require.NoError(t, err)
	_, err = x.st.CreateApproval(ctx, domain.Approval{TaskID: "t2", Project: "onlet", Kind: "merge", Status: domain.ApprovalApproved})
	require.NoError(t, err)

	var pending []domain.Approval
	rec := x.call(t, http.MethodGet, "/api/v1/approvals", "")
	require.Equal(t, http.StatusOK, rec.Code)
	decode(t, rec, &pending)
	assert.Len(t, pending, 1, "default is pending")

	var all []domain.Approval
	rec = x.call(t, http.MethodGet, "/api/v1/approvals?status=all", "")
	decode(t, rec, &all)
	assert.Len(t, all, 2)

	rec = x.call(t, http.MethodGet, "/api/v1/approvals?status=bogus", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = x.call(t, http.MethodPost, "/api/v1/approvals/"+pending[0].ID+"/decide", `{"approve":true,"note":"ship it"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var dec domain.Approval
	decode(t, rec, &dec)
	assert.Equal(t, domain.ApprovalApproved, dec.Status)
	assert.Equal(t, pending[0].ID+":true:ship it", x.ctl.decided)
}

func TestDecideApprovalRequiresApprove(t *testing.T) {
	for _, body := range []string{`{}`, `{"note":"no decision"}`, `{"approve":null}`} {
		x := newHarness(t)
		rec := x.call(t, http.MethodPost, "/api/v1/approvals/a1/decide", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
		assert.JSONEq(t, `{"error":"approve is required"}`, rec.Body.String(), body)
		assert.Empty(t, x.ctl.decided, "an omitted approve must not reach the controller: %s", body)
	}

	x := newHarness(t)
	rec := x.call(t, http.MethodPost, "/api/v1/approvals/a1/decide", `{"approve":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "a1:false:", x.ctl.decided, "approve:false is a rejection, not a missing field")
}

func TestProjectPauseAndResume(t *testing.T) {
	x := newHarness(t)

	rec := x.call(t, http.MethodPost, "/api/v1/projects/onlet/pause", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"paused":true,"paused_by":"owner"}`, rec.Body.String())

	var projects []struct {
		Name     string `json:"name"`
		Paused   bool   `json:"paused"`
		PausedBy string `json:"paused_by"`
	}
	rec = x.call(t, http.MethodGet, "/api/v1/projects", "")
	require.Equal(t, http.StatusOK, rec.Code)
	decode(t, rec, &projects)
	require.Len(t, projects, 1)
	assert.True(t, projects[0].Paused)
	assert.Equal(t, "owner", projects[0].PausedBy)

	rec = x.call(t, http.MethodPost, "/api/v1/projects/onlet/resume", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"paused":false,"paused_by":""}`, rec.Body.String())
	rec = x.call(t, http.MethodGet, "/api/v1/projects", "")
	decode(t, rec, &projects)
	assert.False(t, projects[0].Paused)
	assert.Empty(t, projects[0].PausedBy)

	assert.Equal(t, []string{
		"SetProjectPaused:onlet:true", "SetProjectPaused:onlet:false",
	}, x.ctl.calls)

	rec = x.call(t, http.MethodPost, "/api/v1/projects/nope/pause", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code, "unknown project is ErrInvalid")
}

func TestProjectResumeConfigPausedConflicts(t *testing.T) {
	x := newHarness(t)
	x.ctl.cfg.Projects["onlet"].Paused = true

	var projects []struct {
		Paused   bool   `json:"paused"`
		PausedBy string `json:"paused_by"`
	}
	rec := x.call(t, http.MethodGet, "/api/v1/projects", "")
	decode(t, rec, &projects)
	require.Len(t, projects, 1)
	assert.True(t, projects[0].Paused, "a project paused in config is paused")
	assert.Equal(t, "config", projects[0].PausedBy)

	rec = x.call(t, http.MethodPost, "/api/v1/projects/onlet/resume", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.JSONEq(t, `{"error":"conflict: project \"onlet\" is paused in config"}`, rec.Body.String())
}

func TestProjectsCountsAndRedaction(t *testing.T) {
	x := newHarness(t)
	x.makeTask(t, "onlet", domain.TaskQueued, "")
	x.makeTask(t, "onlet", domain.TaskQueued, "")
	x.makeTask(t, "onlet", domain.TaskRunning, "")
	x.makeTask(t, "onlet", domain.TaskDone, "") // not counted
	x.makeTask(t, "other", domain.TaskBlocked, "")

	rec := x.call(t, http.MethodGet, "/api/v1/projects", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "hunter2", "project env values must be redacted")

	var projects []struct {
		Name   string            `json:"name"`
		Repo   string            `json:"repo"`
		Env    map[string]string `json:"env"`
		Counts map[string]int    `json:"counts"`
	}
	decode(t, rec, &projects)
	require.Len(t, projects, 1)
	assert.Equal(t, "onlet", projects[0].Name)
	assert.Equal(t, "bobbyrc/onlet", projects[0].Repo)
	assert.Equal(t, "***", projects[0].Env["API_SECRET"])
	assert.Equal(t, 2, projects[0].Counts["queued"])
	assert.Equal(t, 1, projects[0].Counts["running"])
	assert.Equal(t, 0, projects[0].Counts["blocked"])
	assert.Contains(t, projects[0].Counts, "awaiting_approval")
	assert.Contains(t, projects[0].Counts, "review")
}

func TestStats(t *testing.T) {
	x := newHarness(t)
	ctx := context.Background()
	x.makeTask(t, "onlet", domain.TaskQueued, "")
	x.makeTask(t, "onlet", domain.TaskDone, "")
	_, err := x.st.CreateRun(ctx, domain.Run{TaskID: "t", Project: "onlet", Status: domain.RunRunning,
		StartedAt: time.Now(), CostUSD: 1.5})
	require.NoError(t, err)
	_, err = x.st.CreateRun(ctx, domain.Run{TaskID: "t", Project: "onlet", Status: domain.RunSucceeded,
		StartedAt: time.Now().Add(-48 * time.Hour), CostUSD: 10})
	require.NoError(t, err)
	_, err = x.st.CreateApproval(ctx, domain.Approval{TaskID: "t", Project: "onlet", Kind: "merge", Status: domain.ApprovalPending})
	require.NoError(t, err)
	x.ctl.paused = true
	x.ctl.cfg.Limits.DailyBudgetUSD = 25

	rec := x.call(t, http.MethodGet, "/api/v1/stats", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var s Stats
	decode(t, rec, &s)
	assert.Equal(t, 1, s.TasksByStatus["queued"])
	assert.Equal(t, 1, s.TasksByStatus["done"])
	assert.Equal(t, 0, s.TasksByStatus["failed"])
	assert.Equal(t, 1, s.ActiveRuns)
	assert.Equal(t, 1, s.ApprovalsPending)
	assert.InDelta(t, 1.5, s.CostTodayUSD, 1e-9, "runs from before today are excluded")
	assert.Equal(t, 25.0, s.DailyBudgetUSD)
	assert.True(t, s.Paused)
}

func TestAgentsKinds(t *testing.T) {
	x := newHarness(t)
	rec := x.call(t, http.MethodGet, "/api/v1/agents", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var agents []AgentView
	decode(t, rec, &agents)
	require.Len(t, agents, 2)
	assert.Equal(t, "implementer", agents[0].Name)
	assert.Equal(t, "codex", agents[0].HarnessType)
	assert.Equal(t, []string{"bugfix", "feature"}, agents[0].Kinds)
	assert.Equal(t, "reviewer", agents[1].Name)
	assert.Equal(t, "claude-code", agents[1].HarnessType)
	assert.Equal(t, "opus", agents[1].Model)
	assert.Equal(t, []string{"review"}, agents[1].Kinds)
}

func TestConfigRedacted(t *testing.T) {
	x := newHarness(t)
	rec := x.call(t, http.MethodGet, "/api/v1/config", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "hunter2")
	assert.Contains(t, rec.Body.String(), `"API_SECRET":"***"`)
	assert.Equal(t, "hunter2", x.ctl.cfg.Projects["onlet"].Env["API_SECRET"], "the live config is not modified")

	rec = x.call(t, http.MethodPost, "/api/v1/config/reload", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, 1, x.ctl.reloadCalls)
}

func TestControl(t *testing.T) {
	x := newHarness(t)
	rec := x.call(t, http.MethodPost, "/api/v1/control/pause", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"paused":true}`, rec.Body.String())

	rec = x.call(t, http.MethodGet, "/api/v1/control", "")
	assert.JSONEq(t, `{"paused":true}`, rec.Body.String())

	rec = x.call(t, http.MethodPost, "/api/v1/control/resume", "")
	assert.JSONEq(t, `{"paused":false}`, rec.Body.String())
}

func TestLoops(t *testing.T) {
	x := newHarness(t)
	x.ctl.loops = []LoopView{{Name: "pr_shepherd", Project: "onlet", Enabled: true, Every: "1m"}}
	rec := x.call(t, http.MethodGet, "/api/v1/loops", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var loops []LoopView
	decode(t, rec, &loops)
	require.Len(t, loops, 1)
	assert.Equal(t, "pr_shepherd", loops[0].Name)

	rec = x.call(t, http.MethodPost, "/api/v1/loops/pr_shepherd/run?project=onlet", "")
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, "pr_shepherd?project=onlet", x.ctl.loopRun)
}

func TestRunsAndLogs(t *testing.T) {
	x := newHarness(t)
	ctx := context.Background()
	run, err := x.st.CreateRun(ctx, domain.Run{TaskID: "t1", Project: "onlet", Status: domain.RunRunning, StartedAt: time.Now()})
	require.NoError(t, err)
	_, err = x.st.AppendRunLogs(ctx, run.ID, []domain.LogLine{
		{Kind: domain.LogText, Text: "one"}, {Kind: domain.LogText, Text: "two"}, {Kind: domain.LogText, Text: "three"},
	})
	require.NoError(t, err)

	rec := x.call(t, http.MethodGet, "/api/v1/runs?task_id=t1&status=running", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), run.ID)

	rec = x.call(t, http.MethodGet, "/api/v1/runs/"+run.ID+"/logs?after=1&limit=1", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var lines []domain.LogLine
	decode(t, rec, &lines)
	require.Len(t, lines, 1)
	assert.Equal(t, "two", lines[0].Text)

	rec = x.call(t, http.MethodGet, "/api/v1/runs/nope/logs", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = x.call(t, http.MethodGet, "/api/v1/runs/nope", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeploymentsSignalsFriction(t *testing.T) {
	x := newHarness(t)
	for _, p := range []string{"/api/v1/deployments", "/api/v1/signals", "/api/v1/friction"} {
		rec := x.call(t, http.MethodGet, p+"?project=onlet&limit=5", "")
		assert.Equal(t, http.StatusOK, rec.Code, p)
		assert.JSONEq(t, `[]`, rec.Body.String(), p)
	}
}

func TestEventsHistory(t *testing.T) {
	x := newHarness(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, err := x.bus.Publish(ctx, domain.Event{Type: domain.EvTaskCreated, Project: "onlet", Message: "m"})
		require.NoError(t, err)
	}
	rec := x.call(t, http.MethodGet, "/api/v1/events/history?after=1&project=onlet", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var evs []domain.Event
	decode(t, rec, &evs)
	require.Len(t, evs, 2)
	assert.EqualValues(t, 2, evs[0].ID)
}

func TestEventsHistoryAfterZeroIsNewest(t *testing.T) {
	x := newHarness(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, err := x.bus.Publish(ctx, domain.Event{Type: domain.EvTaskCreated, Message: "m"})
		require.NoError(t, err)
	}
	rec := x.call(t, http.MethodGet, "/api/v1/events/history?after=0&limit=2", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var evs []domain.Event
	decode(t, rec, &evs)
	require.Len(t, evs, 2)
	assert.EqualValues(t, 4, evs[0].ID, "after=0 on history still means the newest page")
	assert.EqualValues(t, 5, evs[1].ID)
}

func TestConversations(t *testing.T) {
	x := newHarness(t)

	rec := x.call(t, http.MethodPost, "/api/v1/conversations", `{"title":"Plan","project":"nope"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "unknown project is invalid")

	rec = x.call(t, http.MethodPost, "/api/v1/conversations", `{"title":"Plan","project":"onlet"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var conv domain.Conversation
	decode(t, rec, &conv)
	require.NotEmpty(t, conv.ID)

	rec = x.call(t, http.MethodGet, "/api/v1/conversations", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), conv.ID)

	rec = x.call(t, http.MethodGet, "/api/v1/conversations/"+conv.ID, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `[]`, toJSON(t, rec, "messages"))

	rec = x.call(t, http.MethodPost, "/api/v1/conversations/"+conv.ID+"/messages", `{"text":"hello"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Equal(t, conv.ID, x.ctl.chatConvID)
	assert.Equal(t, "hello", x.ctl.chatText)

	x.ctl.errs["SendChat"] = fmt.Errorf("busy: %w", ErrConflict)
	rec = x.call(t, http.MethodPost, "/api/v1/conversations/"+conv.ID+"/messages", `{"text":"again"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)

	rec = x.call(t, http.MethodGet, "/api/v1/conversations/missing", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSPAFallbackAndNotBuilt(t *testing.T) {
	st, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	web := fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	h := NewServer(Options{Store: st, Bus: events.NewBus(st, nil), Controller: &fakeCtl{cfg: testConfig()}, Web: web})

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, testRequest(http.MethodGet, path, nil))
		return rec
	}
	rec := get("/tasks/abc")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "<html>app</html>", rec.Body.String())
	assert.Equal(t, "<html>app</html>", get("/").Body.String())
	assert.Equal(t, "console.log(1)", get("/assets/app.js").Body.String())

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, testRequest(http.MethodPost, "/tasks/abc", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	h = NewServer(Options{Store: st, Bus: events.NewBus(st, nil), Controller: &fakeCtl{cfg: testConfig()}, Web: fstest.MapFS{}})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, testRequest(http.MethodGet, "/tasks/abc", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "The web UI is not built")
}

func TestProjectViewShapeWithoutObserversOrDeployer(t *testing.T) {
	x := newHarness(t)
	rec := x.call(t, http.MethodGet, "/api/v1/projects", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var projects []map[string]json.RawMessage
	decode(t, rec, &projects)
	require.Len(t, projects, 1)
	assert.JSONEq(t, `[]`, string(projects[0]["observe"]), "observe is an array even when empty")
	assert.JSONEq(t, `null`, string(projects[0]["deploy"]), "deploy is null when unset")
	assert.Contains(t, rec.Body.String(), `"observe":[]`)
	assert.Contains(t, rec.Body.String(), `"deploy":null`)
}
