// Package client is the typed Go client for foundry's HTTP API (docs/api.md). The foundry CLI
// is its main user; every route in the API has one method here.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bobbyrc/foundry/internal/config"
	"github.com/bobbyrc/foundry/internal/domain"
)

// apiPrefix is the base path of every route. The health check is under it too.
const apiPrefix = "/api/v1"

// Client talks to one foundry server. BaseURL is the scheme, host and port, for example
// http://127.0.0.1:7777. Token is the bearer token from <data_dir>/token.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New returns a client for the server at baseURL. Requests time out after 60 seconds; the event
// stream is exempt.
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// APIError is a non-2xx response. Message is the server's {"error"} text.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("HTTP %d %s", e.Status, http.StatusText(e.Status))
	}
	return e.Message
}

// Health is the answer of GET /health.
type Health struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
}

// ControlState is the answer of the pause and resume routes.
type ControlState struct {
	Paused bool `json:"paused"`
}

// Stats is the answer of GET /stats.
type Stats struct {
	TasksByStatus    map[string]int `json:"tasks_by_status"`
	ActiveRuns       int            `json:"active_runs"`
	ApprovalsPending int            `json:"approvals_pending"`
	CostTodayUSD     float64        `json:"cost_today_usd"`
	Paused           bool           `json:"paused"`
}

// ProjectView is one entry of GET /projects: the project's settings plus its task counts.
type ProjectView struct {
	Name          string `json:"name"`
	Repo          string `json:"repo"`
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch"`
	Policy        string `json:"policy"`
	Paused        bool   `json:"paused"`
	// PausedBy says who paused the project: "config" (paused: true in foundry.yaml) or "owner"
	// (POST /projects/{name}/pause). Empty when it is not paused.
	PausedBy string         `json:"paused_by"`
	Counts   map[string]int `json:"counts"`
}

// ProjectControl is the answer of the project pause and resume routes.
type ProjectControl struct {
	Paused   bool   `json:"paused"`
	PausedBy string `json:"paused_by"`
}

// AgentView is one entry of GET /agents: a role and the harness it runs on.
type AgentView struct {
	Name        string   `json:"name"`
	Harness     string   `json:"harness"`
	HarnessType string   `json:"harness_type"`
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	Description string   `json:"description"`
	Kinds       []string `json:"kinds"`
}

// LoopView is one entry of GET /loops.
type LoopView struct {
	Name      string    `json:"name"`
	Project   string    `json:"project"`
	Enabled   bool      `json:"enabled"`
	Every     string    `json:"every"`
	LastRunAt time.Time `json:"last_run_at"`
	LastError string    `json:"last_error"`
}

// NewTask is the body of POST /tasks.
type NewTask struct {
	Project  string `json:"project"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Role     string `json:"role"`
	Priority int    `json:"priority"`
	ParentID string `json:"parent_id"`
	Source   string `json:"source"`
}

// TaskDetail is the answer of GET /tasks/{id}.
type TaskDetail struct {
	Task      domain.Task       `json:"task"`
	Runs      []domain.Run      `json:"runs"`
	Approvals []domain.Approval `json:"approvals"`
	Children  []domain.Task     `json:"children"`
}

// ConversationDetail is the answer of GET /conversations/{id}.
type ConversationDetail struct {
	Conversation domain.Conversation  `json:"conversation"`
	Messages     []domain.ChatMessage `json:"messages"`
}

// TaskFilter narrows GET /tasks. Zero fields are not sent.
type TaskFilter struct {
	Project string
	Status  []string
	Kind    string
	Limit   int
}

// RunFilter narrows GET /runs. Zero fields are not sent.
type RunFilter struct {
	TaskID  string
	Project string
	Status  []string
	Limit   int
}

// EventFilter narrows GET /events/history. Zero fields are not sent.
type EventFilter struct {
	After   int64
	Project string
	TaskID  string
	Limit   int
}

// Health calls GET /health.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var out Health
	err := c.do(ctx, http.MethodGet, "/health", nil, nil, &out)
	return out, err
}

// Stats calls GET /stats.
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	var out Stats
	err := c.do(ctx, http.MethodGet, "/stats", nil, nil, &out)
	return out, err
}

// Control calls GET /control.
func (c *Client) Control(ctx context.Context) (ControlState, error) {
	var out ControlState
	err := c.do(ctx, http.MethodGet, "/control", nil, nil, &out)
	return out, err
}

// Pause calls POST /control/pause.
func (c *Client) Pause(ctx context.Context) (ControlState, error) {
	var out ControlState
	err := c.do(ctx, http.MethodPost, "/control/pause", nil, nil, &out)
	return out, err
}

// Resume calls POST /control/resume.
func (c *Client) Resume(ctx context.Context) (ControlState, error) {
	var out ControlState
	err := c.do(ctx, http.MethodPost, "/control/resume", nil, nil, &out)
	return out, err
}

// PauseProject calls POST /projects/{name}/pause.
func (c *Client) PauseProject(ctx context.Context, name string) (ProjectControl, error) {
	var out ProjectControl
	err := c.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(name)+"/pause", nil, nil, &out)
	return out, err
}

// ResumeProject calls POST /projects/{name}/resume.
func (c *Client) ResumeProject(ctx context.Context, name string) (ProjectControl, error) {
	var out ProjectControl
	err := c.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(name)+"/resume", nil, nil, &out)
	return out, err
}

// EventHistory calls GET /events/history.
func (c *Client) EventHistory(ctx context.Context, f EventFilter) ([]domain.Event, error) {
	q := url.Values{}
	if f.After > 0 {
		q.Set("after", strconv.FormatInt(f.After, 10))
	}
	setIf(q, "project", f.Project)
	setIf(q, "task_id", f.TaskID)
	setLimit(q, f.Limit)
	var out []domain.Event
	err := c.do(ctx, http.MethodGet, "/events/history", q, nil, &out)
	return out, err
}

// Projects calls GET /projects.
func (c *Client) Projects(ctx context.Context) ([]ProjectView, error) {
	var out []ProjectView
	err := c.do(ctx, http.MethodGet, "/projects", nil, nil, &out)
	return out, err
}

// ListTasks calls GET /tasks.
func (c *Client) ListTasks(ctx context.Context, f TaskFilter) ([]domain.Task, error) {
	q := url.Values{}
	setIf(q, "project", f.Project)
	setIf(q, "status", strings.Join(f.Status, ","))
	setIf(q, "kind", f.Kind)
	setLimit(q, f.Limit)
	var out []domain.Task
	err := c.do(ctx, http.MethodGet, "/tasks", q, nil, &out)
	return out, err
}

// CreateTask calls POST /tasks.
func (c *Client) CreateTask(ctx context.Context, t NewTask) (domain.Task, error) {
	var out domain.Task
	err := c.do(ctx, http.MethodPost, "/tasks", nil, t, &out)
	return out, err
}

// GetTask calls GET /tasks/{id}.
func (c *Client) GetTask(ctx context.Context, id string) (TaskDetail, error) {
	var out TaskDetail
	err := c.do(ctx, http.MethodGet, "/tasks/"+url.PathEscape(id), nil, nil, &out)
	return out, err
}

// CancelTask calls POST /tasks/{id}/cancel.
func (c *Client) CancelTask(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/tasks/"+url.PathEscape(id)+"/cancel", nil, nil, nil)
}

// RetryTask calls POST /tasks/{id}/retry. An empty note is not sent.
func (c *Client) RetryTask(ctx context.Context, id, note string) error {
	var body any
	if note != "" {
		body = struct {
			Note string `json:"note"`
		}{note}
	}
	return c.do(ctx, http.MethodPost, "/tasks/"+url.PathEscape(id)+"/retry", nil, body, nil)
}

// ListRuns calls GET /runs.
func (c *Client) ListRuns(ctx context.Context, f RunFilter) ([]domain.Run, error) {
	q := url.Values{}
	setIf(q, "task_id", f.TaskID)
	setIf(q, "project", f.Project)
	setIf(q, "status", strings.Join(f.Status, ","))
	setLimit(q, f.Limit)
	var out []domain.Run
	err := c.do(ctx, http.MethodGet, "/runs", q, nil, &out)
	return out, err
}

// GetRun calls GET /runs/{id}.
func (c *Client) GetRun(ctx context.Context, id string) (domain.Run, error) {
	var out domain.Run
	err := c.do(ctx, http.MethodGet, "/runs/"+url.PathEscape(id), nil, nil, &out)
	return out, err
}

// RunLogs calls GET /runs/{id}/logs for lines with Seq greater than after. A limit of 0 uses
// the server default.
func (c *Client) RunLogs(ctx context.Context, id string, after int64, limit int) ([]domain.LogLine, error) {
	q := url.Values{"after": {strconv.FormatInt(after, 10)}}
	setLimit(q, limit)
	var out []domain.LogLine
	err := c.do(ctx, http.MethodGet, "/runs/"+url.PathEscape(id)+"/logs", q, nil, &out)
	return out, err
}

// CancelRun calls POST /runs/{id}/cancel.
func (c *Client) CancelRun(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/runs/"+url.PathEscape(id)+"/cancel", nil, nil, nil)
}

// ListApprovals calls GET /approvals. An empty status means the server default (pending);
// "all" lists every approval.
func (c *Client) ListApprovals(ctx context.Context, status string) ([]domain.Approval, error) {
	q := url.Values{}
	setIf(q, "status", status)
	var out []domain.Approval
	err := c.do(ctx, http.MethodGet, "/approvals", q, nil, &out)
	return out, err
}

// DecideApproval calls POST /approvals/{id}/decide.
func (c *Client) DecideApproval(ctx context.Context, id string, approve bool, note string) (domain.Approval, error) {
	body := struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}{approve, note}
	var out domain.Approval
	err := c.do(ctx, http.MethodPost, "/approvals/"+url.PathEscape(id)+"/decide", nil, body, &out)
	return out, err
}

// ListDeployments calls GET /deployments.
func (c *Client) ListDeployments(ctx context.Context, project string, limit int) ([]domain.Deployment, error) {
	q := url.Values{}
	setIf(q, "project", project)
	setLimit(q, limit)
	var out []domain.Deployment
	err := c.do(ctx, http.MethodGet, "/deployments", q, nil, &out)
	return out, err
}

// ListSignals calls GET /signals.
func (c *Client) ListSignals(ctx context.Context, project string, limit int) ([]domain.Signal, error) {
	q := url.Values{}
	setIf(q, "project", project)
	setLimit(q, limit)
	var out []domain.Signal
	err := c.do(ctx, http.MethodGet, "/signals", q, nil, &out)
	return out, err
}

// ListFriction calls GET /friction.
func (c *Client) ListFriction(ctx context.Context, project string, limit int) ([]domain.Friction, error) {
	q := url.Values{}
	setIf(q, "project", project)
	setLimit(q, limit)
	var out []domain.Friction
	err := c.do(ctx, http.MethodGet, "/friction", q, nil, &out)
	return out, err
}

// Loops calls GET /loops.
func (c *Client) Loops(ctx context.Context) ([]LoopView, error) {
	var out []LoopView
	err := c.do(ctx, http.MethodGet, "/loops", nil, nil, &out)
	return out, err
}

// RunLoop calls POST /loops/{name}/run. An empty project runs the loop for every project.
func (c *Client) RunLoop(ctx context.Context, name, project string) error {
	q := url.Values{}
	setIf(q, "project", project)
	return c.do(ctx, http.MethodPost, "/loops/"+url.PathEscape(name)+"/run", q, nil, nil)
}

// Agents calls GET /agents.
func (c *Client) Agents(ctx context.Context) ([]AgentView, error) {
	var out []AgentView
	err := c.do(ctx, http.MethodGet, "/agents", nil, nil, &out)
	return out, err
}

// Config calls GET /config. Every env value comes back as "***".
func (c *Client) Config(ctx context.Context) (*config.Config, error) {
	var out config.Config
	if err := c.do(ctx, http.MethodGet, "/config", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReloadConfig calls POST /config/reload.
func (c *Client) ReloadConfig(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/config/reload", nil, nil, nil)
}

// Conversations calls GET /conversations.
func (c *Client) Conversations(ctx context.Context) ([]domain.Conversation, error) {
	var out []domain.Conversation
	err := c.do(ctx, http.MethodGet, "/conversations", nil, nil, &out)
	return out, err
}

// CreateConversation calls POST /conversations.
func (c *Client) CreateConversation(ctx context.Context, title, project string) (domain.Conversation, error) {
	body := struct {
		Title   string `json:"title"`
		Project string `json:"project"`
	}{title, project}
	var out domain.Conversation
	err := c.do(ctx, http.MethodPost, "/conversations", nil, body, &out)
	return out, err
}

// GetConversation calls GET /conversations/{id}.
func (c *Client) GetConversation(ctx context.Context, id string) (ConversationDetail, error) {
	var out ConversationDetail
	err := c.do(ctx, http.MethodGet, "/conversations/"+url.PathEscape(id), nil, nil, &out)
	return out, err
}

// SendMessage calls POST /conversations/{id}/messages. It returns APIError with status 409
// while the orchestrator is still replying.
func (c *Client) SendMessage(ctx context.Context, conversationID, text string) (domain.ChatMessage, error) {
	body := struct {
		Text string `json:"text"`
	}{text}
	var out domain.ChatMessage
	err := c.do(ctx, http.MethodPost, "/conversations/"+url.PathEscape(conversationID)+"/messages", nil, body, &out)
	return out, err
}

// do sends one JSON request under /api/v1. A nil in sends no body; a nil out discards the
// response body (used for 204 responses).
func (c *Client) do(ctx context.Context, method, path string, q url.Values, in, out any) error {
	u := c.BaseURL + apiPrefix + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.authorize(req)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return decodeError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}
	return nil
}

func (c *Client) authorize(req *http.Request) {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// decodeError turns a non-2xx response into an *APIError.
func decodeError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	apiErr := &APIError{Status: resp.StatusCode}
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &body) == nil && body.Error != "" {
		apiErr.Message = body.Error
	} else {
		apiErr.Message = strings.TrimSpace(string(raw))
	}
	return apiErr
}

func setIf(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

func setLimit(q url.Values, limit int) {
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
}
