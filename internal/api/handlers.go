package api

import (
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/bobbyrc/onett/internal/config"
	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/store"
)

// defaultLimit is the page size of list routes when ?limit= is absent.
const defaultLimit = 200

// allTaskStatuses is every task status, so Stats always carries every key.
var allTaskStatuses = []domain.TaskStatus{
	domain.TaskQueued, domain.TaskRunning, domain.TaskReview, domain.TaskAwaitingApproval,
	domain.TaskMerged, domain.TaskDeploying, domain.TaskDone, domain.TaskBlocked,
	domain.TaskFailed, domain.TaskCancelled,
}

// projectCountStatuses are the statuses ProjectView.counts reports.
var projectCountStatuses = []domain.TaskStatus{
	domain.TaskQueued, domain.TaskRunning, domain.TaskReview,
	domain.TaskAwaitingApproval, domain.TaskBlocked,
}

// NewTask is the body of POST /tasks.
type NewTask struct {
	Project  string          `json:"project"`
	Kind     domain.TaskKind `json:"kind"`
	Title    string          `json:"title"`
	Body     string          `json:"body"`
	Role     string          `json:"role"`
	Priority int             `json:"priority"`
	ParentID string          `json:"parent_id"`
	Source   string          `json:"source"`
}

// TaskDetail is the body of GET /tasks/{id}.
type TaskDetail struct {
	Task      domain.Task       `json:"task"`
	Runs      []domain.Run      `json:"runs"`
	Approvals []domain.Approval `json:"approvals"`
	Children  []domain.Task     `json:"children"`
}

// ProjectView is a configured project plus its live task counts. Config values are redacted.
// Paused is the effective state: true whenever PausedBy is set. It shadows the config's own
// paused field, which is the same as PausedBy == "config".
// Observe and Deploy shadow the config's omitempty tags so the shape is stable: observe is
// always an array ([] when none) and deploy is an object or null.
type ProjectView struct {
	config.Project
	Observe  []config.Observer `json:"observe"`
	Deploy   *config.Deploy    `json:"deploy"`
	Paused   bool              `json:"paused"`
	PausedBy string            `json:"paused_by"`
	Counts   map[string]int    `json:"counts"`
}

// Stats is the body of GET /stats.
type Stats struct {
	TasksByStatus    map[string]int `json:"tasks_by_status"`
	ActiveRuns       int            `json:"active_runs"`
	ApprovalsPending int            `json:"approvals_pending"`
	CostTodayUSD     float64        `json:"cost_today_usd"`
	DailyBudgetUSD   float64        `json:"daily_budget_usd"`
	Paused           bool           `json:"paused"`
}

// AgentView describes one agent role as GET /agents shows it: where its settings come from,
// the instructions it runs with, and what it is used for. Anyone who can read the view sees
// PromptText and BuiltinPromptText in full, so prompts must never hold secrets.
type AgentView struct {
	Name        string             `json:"name"`
	Origin      config.AgentOrigin `json:"origin"`
	Disabled    bool               `json:"disabled"`
	Harness     string             `json:"harness"`
	HarnessType string             `json:"harness_type"`
	Model       string             `json:"model"`
	// Prompt is the reference the agent was configured with: "builtin:<name>" or a file path,
	// and "" when the instructions are written inline.
	Prompt string `json:"prompt"`
	// PromptText is the resolved instructions. It is empty when PromptError is set.
	PromptText  string `json:"prompt_text"`
	PromptError string `json:"prompt_error,omitempty"`
	// PromptUpdates is true when the agent runs the built-in prompt of its own name, so a new
	// onett release changes its instructions.
	PromptUpdates bool `json:"prompt_updates"`
	// BuiltinPromptText is the built-in prompt of this name, set only for a built-in whose
	// instructions the file replaces, so the owner can compare the two.
	BuiltinPromptText string            `json:"builtin_prompt_text,omitempty"`
	Description       string            `json:"description"`
	Args              []string          `json:"args"`
	Kinds             []string          `json:"kinds"`
	Uses              []config.AgentUse `json:"uses"`
}

// conversationDetail is the body of GET /conversations/{id}.
type conversationDetail struct {
	Conversation domain.Conversation  `json:"conversation"`
	Messages     []domain.ChatMessage `json:"messages"`
}

// --- tasks ---

func (s *server) listTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := queryLimit(q, defaultLimit)
	if err != nil {
		s.fail(w, err)
		return
	}
	tasks, err := s.opts.Store.ListTasks(r.Context(), store.TaskFilter{
		Project:  q.Get("project"),
		Statuses: csvQuery[domain.TaskStatus](q, "status"),
		Kind:     domain.TaskKind(q.Get("kind")),
		Limit:    limit,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(tasks))
}

func (s *server) createTask(w http.ResponseWriter, r *http.Request) {
	var in NewTask
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	source := in.Source
	if source == "" {
		source = "api"
	}
	t, err := s.opts.Controller.CreateTask(r.Context(), domain.Task{
		Project:  in.Project,
		Kind:     in.Kind,
		Title:    in.Title,
		Body:     in.Body,
		Role:     in.Role,
		Priority: in.Priority,
		ParentID: in.ParentID,
		Source:   source,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *server) getTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	task, err := s.opts.Store.GetTask(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	runs, err := s.opts.Store.ListRuns(ctx, store.RunFilter{TaskID: id, Limit: defaultLimit})
	if err != nil {
		s.fail(w, err)
		return
	}
	all, err := s.opts.Store.ListApprovals(ctx, "")
	if err != nil {
		s.fail(w, err)
		return
	}
	var approvals []domain.Approval
	for _, a := range all {
		if a.TaskID == id {
			approvals = append(approvals, a)
		}
	}
	children, err := s.opts.Store.ListTasks(ctx, store.TaskFilter{ParentID: id, Limit: defaultLimit})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, TaskDetail{
		Task:      task,
		Runs:      nonNil(runs),
		Approvals: nonNil(approvals),
		Children:  nonNil(children),
	})
}

func (s *server) cancelTask(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Controller.CancelTask(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) retryTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Note string `json:"note"`
	}
	if err := readJSON(w, r, &body); err != nil && !errors.Is(err, io.EOF) {
		s.fail(w, err)
		return
	}
	if err := s.opts.Controller.RetryTask(r.Context(), chi.URLParam(r, "id"), body.Note); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- runs ---

func (s *server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := queryLimit(q, defaultLimit)
	if err != nil {
		s.fail(w, err)
		return
	}
	runs, err := s.opts.Store.ListRuns(r.Context(), store.RunFilter{
		TaskID:   q.Get("task_id"),
		Project:  q.Get("project"),
		Statuses: csvQuery[domain.RunStatus](q, "status"),
		Limit:    limit,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(runs))
}

func (s *server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.opts.Store.GetRun(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *server) runLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	q := r.URL.Query()
	after, err := queryInt64(q, "after")
	if err != nil {
		s.fail(w, err)
		return
	}
	limit, err := queryLimit(q, 1000)
	if err != nil {
		s.fail(w, err)
		return
	}
	// Check the run exists so an unknown ID is a 404, not an empty list.
	if _, err := s.opts.Store.GetRun(ctx, id); err != nil {
		s.fail(w, err)
		return
	}
	lines, err := s.opts.Store.ListRunLogs(ctx, id, after, limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(lines))
}

func (s *server) cancelRun(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Controller.CancelRun(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- approvals ---

func (s *server) listApprovals(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	var filter domain.ApprovalStatus
	switch status {
	case "", "pending":
		filter = domain.ApprovalPending
	case "all":
		filter = ""
	case string(domain.ApprovalApproved), string(domain.ApprovalRejected):
		filter = domain.ApprovalStatus(status)
	default:
		s.fail(w, invalidf("status must be pending, approved, rejected or all"))
		return
	}
	approvals, err := s.opts.Store.ListApprovals(r.Context(), filter)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(approvals))
}

func (s *server) decideApproval(w http.ResponseWriter, r *http.Request) {
	var body struct {
		// Approve is a pointer so that an omitted field is distinguishable from false. An omitted
		// approve must not silently reject the approval.
		Approve *bool  `json:"approve"`
		Note    string `json:"note"`
	}
	if err := readJSON(w, r, &body); err != nil {
		s.fail(w, err)
		return
	}
	if body.Approve == nil {
		writeErrorMsg(w, http.StatusBadRequest, "approve is required")
		return
	}
	a, err := s.opts.Controller.DecideApproval(r.Context(), chi.URLParam(r, "id"), *body.Approve, body.Note)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// --- deployments, signals, friction ---

func (s *server) listDeployments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := queryLimit(q, defaultLimit)
	if err != nil {
		s.fail(w, err)
		return
	}
	out, err := s.opts.Store.ListDeployments(r.Context(), q.Get("project"), limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(out))
}

func (s *server) listSignals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := queryLimit(q, defaultLimit)
	if err != nil {
		s.fail(w, err)
		return
	}
	out, err := s.opts.Store.ListSignals(r.Context(), q.Get("project"), limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(out))
}

func (s *server) listFriction(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := queryLimit(q, defaultLimit)
	if err != nil {
		s.fail(w, err)
		return
	}
	out, err := s.opts.Store.ListFriction(r.Context(), q.Get("project"), limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(out))
}

// --- projects, agents, stats ---

func (s *server) listProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := s.opts.Controller.Config().Redacted()
	names := sortedKeys(cfg.Projects)
	out := make([]ProjectView, 0, len(names))
	for _, name := range names {
		p := cfg.Projects[name]
		if p == nil {
			p = &config.Project{Name: name}
		}
		tasks, err := s.opts.Store.ListTasks(ctx, store.TaskFilter{
			Project:  name,
			Statuses: projectCountStatuses,
			Limit:    10000,
		})
		if err != nil {
			s.fail(w, err)
			return
		}
		counts := make(map[string]int, len(projectCountStatuses))
		for _, st := range projectCountStatuses {
			counts[string(st)] = 0
		}
		for _, t := range tasks {
			counts[string(t.Status)]++
		}
		pausedBy := s.opts.Controller.ProjectPausedBy(name)
		observe := p.Observe
		if observe == nil {
			observe = []config.Observer{}
		}
		out = append(out, ProjectView{
			Project:  *p,
			Observe:  observe,
			Deploy:   p.Deploy,
			Paused:   pausedBy != "",
			PausedBy: pausedBy,
			Counts:   counts,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// setProjectPaused pauses or resumes one project. The response reports the state after the call.
func (s *server) setProjectPaused(paused bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if err := s.opts.Controller.SetProjectPaused(r.Context(), name, paused); err != nil {
			s.fail(w, err)
			return
		}
		by := s.opts.Controller.ProjectPausedBy(name)
		writeJSON(w, http.StatusOK, map[string]any{"paused": by != "", "paused_by": by})
	}
}

// listAgents returns the global agent view, or with ?project= that project's merged view.
func (s *server) listAgents(w http.ResponseWriter, r *http.Request) {
	ctl := s.opts.Controller
	raw := ctl.Config()
	// The view is built from the redacted copy, so a value substituted for ${NAME} never
	// reaches the response. Prompt files are read through the raw agents, since a prompt path
	// may contain a substituted value that the redacted copy masks.
	cfg := raw.Redacted()
	project := r.URL.Query().Get("project")
	if project != "" && cfg.Projects[project] == nil {
		s.fail(w, invalidf("unknown project %q", project))
		return
	}
	agents := cfg.AgentsFor(project)
	rawAgents := raw.AgentsFor(project)
	uses := cfg.AgentUses(project)
	kindsByAgent := make(map[string][]string)
	for kind, agent := range cfg.Routing {
		kindsByAgent[agent] = append(kindsByAgent[agent], kind)
	}
	builtins := config.BuiltinAgents()
	out := make([]AgentView, 0, len(agents))
	for _, name := range sortedKeys(agents) {
		a := agents[name]
		kinds := kindsByAgent[name]
		sort.Strings(kinds)
		ownBuiltin := a.Prompt == "builtin:"+name && a.PromptText == ""
		v := AgentView{
			Name:          name,
			Origin:        a.Origin,
			Disabled:      a.Disabled,
			Harness:       a.Harness,
			HarnessType:   cfg.Harness[a.Harness].Type,
			Model:         a.Model,
			Prompt:        a.Prompt,
			PromptUpdates: strings.HasPrefix(a.Prompt, "builtin:") && a.PromptText == "",
			Description:   a.Description,
			Args:          nonNil(a.Args),
			Kinds:         nonNil(kinds),
			Uses:          nonNil(uses[name]),
		}
		// Inline text comes from the redacted copy; a file is read from its raw path.
		if a.PromptText != "" {
			v.PromptText = a.PromptText
		} else if text, err := ctl.AgentPrompt(rawAgents[name]); err != nil {
			v.PromptError = err.Error()
		} else {
			v.PromptText = text
		}
		if _, isBuiltin := builtins[name]; isBuiltin && !ownBuiltin {
			if text, err := ctl.BuiltinPrompt(name); err == nil {
				v.BuiltinPromptText = text
			}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tasks, err := s.opts.Store.ListTasks(ctx, store.TaskFilter{Limit: 100000})
	if err != nil {
		s.fail(w, err)
		return
	}
	byStatus := make(map[string]int, len(allTaskStatuses))
	for _, st := range allTaskStatuses {
		byStatus[string(st)] = 0
	}
	for _, t := range tasks {
		byStatus[string(t.Status)]++
	}
	active, err := s.opts.Store.ListRuns(ctx, store.RunFilter{
		Statuses: []domain.RunStatus{domain.RunStarting, domain.RunRunning},
		Limit:    10000,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	pending, err := s.opts.Store.ListApprovals(ctx, domain.ApprovalPending)
	if err != nil {
		s.fail(w, err)
		return
	}
	runs, err := s.opts.Store.ListRuns(ctx, store.RunFilter{Limit: 10000})
	if err != nil {
		s.fail(w, err)
		return
	}
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var cost float64
	for _, run := range runs {
		if !run.StartedAt.Before(midnight) {
			cost += run.CostUSD
		}
	}
	writeJSON(w, http.StatusOK, Stats{
		TasksByStatus:    byStatus,
		ActiveRuns:       len(active),
		ApprovalsPending: len(pending),
		CostTodayUSD:     cost,
		DailyBudgetUSD:   s.opts.Controller.Config().Limits.DailyBudgetUSD,
		Paused:           s.opts.Controller.Paused(),
	})
}

// --- control, config ---

func (s *server) getControl(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"paused": s.opts.Controller.Paused()})
}

func (s *server) setPaused(paused bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.opts.Controller.SetPaused(r.Context(), paused); err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"paused": s.opts.Controller.Paused()})
	}
}

func (s *server) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.opts.Controller.Config().Redacted())
}

func (s *server) reloadConfig(w http.ResponseWriter, r *http.Request) {
	if err := s.opts.Controller.ReloadConfig(r.Context()); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- loops ---

func (s *server) listLoops(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, nonNil(s.opts.Controller.Loops(r.Context())))
}

func (s *server) runLoop(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.opts.Controller.RunLoop(r.Context(), name, r.URL.Query().Get("project")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// --- conversations ---

func (s *server) listConversations(w http.ResponseWriter, r *http.Request) {
	out, err := s.opts.Store.ListConversations(r.Context(), defaultLimit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(out))
}

func (s *server) createConversation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title   string `json:"title"`
		Project string `json:"project"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if in.Project != "" {
		if _, ok := s.opts.Controller.Config().Projects[in.Project]; !ok {
			s.fail(w, invalidf("unknown project %q", in.Project))
			return
		}
	}
	c, err := s.opts.Store.CreateConversation(r.Context(), domain.Conversation{
		Title:   in.Title,
		Project: in.Project,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *server) getConversation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	c, err := s.opts.Store.GetConversation(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	msgs, err := s.opts.Store.ListChatMessages(ctx, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversationDetail{Conversation: c, Messages: nonNil(msgs)})
}

func (s *server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if in.Text == "" {
		s.fail(w, invalidf("text is required"))
		return
	}
	m, err := s.opts.Controller.SendChat(r.Context(), chi.URLParam(r, "id"), in.Text)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, m)
}

// --- events history ---

func (s *server) eventHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	after, err := queryInt64(q, "after")
	if err != nil {
		s.fail(w, err)
		return
	}
	limit, err := queryLimit(q, 0)
	if err != nil {
		s.fail(w, err)
		return
	}
	evs, err := s.opts.Store.ListEvents(r.Context(), store.EventFilter{
		AfterID: after,
		Project: q.Get("project"),
		TaskID:  q.Get("task_id"),
		Limit:   limit,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(evs))
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
