# Foundry HTTP API (v1)

Every client — the web UI, the `foundry` CLI, the orchestrator agent, future native and mobile
apps — uses this API. JSON field names are the `json` tags in `internal/domain` (Task, Run,
LogLine, Event, Approval, Deployment, Signal, Friction, Conversation, ChatMessage) and
`internal/config` (Config). Times are RFC 3339. The machine-readable OpenAPI 3.1 spec is [openapi.yaml](openapi.yaml).

## Auth and errors

- Base path `/api/v1`. Every route except `GET /api/v1/health` needs
  `Authorization: Bearer <token>`. `GET /events` also accepts `?token=` because browsers'
  `EventSource` cannot set headers.
- The token lives at `<data_dir>/token` (created on first start, mode 600).
- Errors: `{"error": "<message>"}` with 400 (invalid), 401, 404, 409 (conflict), 500.
- Request bodies are limited to 1 MB. A body with a JSON field the route does not define is
  rejected with 400, so check spelling against the shapes below.
- The web UI is served at `/` (single-page app; unknown non-API paths serve `index.html`).

## Routes

| Method & path | Body / query | Response |
|---|---|---|
| `GET /health` | | `{"ok":true,"version":"…"}` |
| `GET /events` | `?after=<id>&token=` | SSE stream (below) |
| `GET /events/history` | `?after=&project=&task_id=&limit=` (default 500) | `[]Event` |
| `GET /stats` | | `Stats` |
| `GET /control` | | `{"paused":bool}` |
| `POST /control/pause`, `POST /control/resume` | | `{"paused":bool}` |
| `GET /projects` | | `[]ProjectView` |
| `POST /projects/{name}/pause`, `POST /projects/{name}/resume` | | `{"paused":bool,"paused_by":""\|"config"\|"owner"}` (409 resuming a project paused in config) |
| `GET /tasks` | `?project=&status=queued,running&kind=&limit=` | `[]Task` |
| `POST /tasks` | `NewTask` | `Task` (201) |
| `GET /tasks/{id}` | | `TaskDetail` |
| `POST /tasks/{id}/cancel` | | 204 |
| `POST /tasks/{id}/retry` | `{"note":"…"}` (optional) | 204 |
| `GET /runs` | `?task_id=&project=&status=&limit=` | `[]Run` |
| `GET /runs/{id}` | | `Run` |
| `GET /runs/{id}/logs` | `?after=<seq>&limit=` (default 1000) | `[]LogLine` |
| `POST /runs/{id}/cancel` | | 204 |
| `GET /approvals` | `?status=pending` (default), `approved`, `rejected` or `all` | `[]Approval` |
| `POST /approvals/{id}/decide` | `{"approve":bool,"note":"…"}` (`approve` is required) | `Approval` |
| `GET /deployments` | `?project=&limit=` | `[]Deployment` |
| `GET /signals` | `?project=&limit=` | `[]Signal` |
| `GET /friction` | `?project=&limit=` | `[]Friction` |
| `GET /loops` | | `[]LoopView` |
| `POST /loops/{name}/run` | `?project=` | 202 |
| `GET /agents` | | `[]AgentView` |
| `GET /config` | | `Config` with every `env` value (projects and harnesses) and every deploy, observer or notifier option whose key contains `token`, `secret`, `password` or `key` replaced by `"***"` |
| `POST /config/reload` | | 204 |
| `GET /conversations` | | `[]Conversation` |
| `POST /conversations` | `{"title":"…","project":"…"}` | `Conversation` (201) |
| `GET /conversations/{id}` | | `{"conversation":Conversation,"messages":[]ChatMessage}` |
| `POST /conversations/{id}/messages` | `{"text":"…"}` | `ChatMessage` (202); 409 while busy |

`status` filters on task and run lists take a comma-separated list; `GET /approvals` takes one
value. `limit` is a non-negative integer, where 0 or absent means the route's default; a negative
or non-numeric value is a 400. Most list routes default to 200. The exceptions are
`GET /events/history` (500) and `GET /runs/{id}/logs` (1000).

`POST /tasks` rejects `"kind":"review"` with 400, since foundry creates review tasks itself. An
empty `kind` is stored as `feature`. A `risk` field is not part of `NewTask`, so it is rejected
with 400 like any unknown field.

`POST /approvals/{id}/decide` returns 400 `"approve is required"` when the body has no `approve`
field. `approve: false` rejects the approval.

`POST /projects/{name}/pause` and `/resume` change a project's runtime pause, which is saved and
survives restarts. A project paused by `paused: true` in foundry.yaml shows `paused_by: "config"`.
The API cannot resume it, so `/resume` returns 409 until the config changes. An unknown project is a
400. While a project is paused, the scheduler starts none of its runs and its loops create no tasks.

`POST /approvals/{id}/decide` returns 409 when the task is not awaiting approval. For a merge
approval it also returns 409 when the PR head changed since the approval was requested. In that
case the approval is recorded as rejected and the task goes back to review so foundry can re-check it.

### Shapes not in `internal/domain`

```jsonc
// NewTask
{"project":"foundry","kind":"feature","title":"…","body":"…",
 "role":"","priority":0,"parent_id":"","source":"api"}   // role "" = routing default

// TaskDetail
{"task":Task,"runs":[]Run,"approvals":[]Approval,"children":[]Task}

// ProjectView: the config.Project fields plus. "paused" is the effective state (true whenever
// "paused_by" is set), not the raw config value.
{"name":"…","repo":"…", "...": "...", "paused":false, "paused_by":"", // "config" or "owner"
 "counts":{"queued":0,"running":0,"review":0,"awaiting_approval":0,"blocked":0}}

// Stats
{"tasks_by_status":{"queued":3,...},"active_runs":1,"approvals_pending":0,
 "cost_today_usd":1.23,"daily_budget_usd":25,"paused":false}

// LoopView
{"name":"pr_shepherd","project":"foundry","enabled":true,"every":"1m",
 "last_run_at":"…","last_error":""}

// AgentView
{"name":"implementer","harness":"codex","harness_type":"codex","model":"",
 "prompt":"builtin:implementer","description":"…","kinds":["feature","bugfix"]}
```

## Event stream

`GET /api/v1/events?after=<id>` is a Server-Sent Events stream. With `after`, it first replays
stored events with a greater ID, then streams live ones. Each event is:

```text
id: 1234
event: task.updated
data: {"id":1234,"time":"…","type":"task.updated","task_id":"…","message":"…","data":{…}}

```

Live-only events (`run.log`, `chat.delta`) carry no `id:` line, since they are not stored. Their
JSON data has `"id":0`, so clients must not use it as a resume cursor.
The server sends `: ping` every 15s. Clients must ignore event types they do not know and
reconnect with the last `id` they saw. Event types are the `Ev*` constants in
`internal/domain/domain.go`.
