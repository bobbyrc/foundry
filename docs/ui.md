# Web UI design (v0)

The web UI is a control room: see what the factory is doing, answer what it needs from you,
and talk to the orchestrator. It is a client of `docs/api.md` and nothing else; native and
mobile apps should follow the same information architecture.

## Stack

Vite + React + TypeScript, react-router, @tanstack/react-query, marked + dompurify for
Markdown. No component library; one stylesheet with design tokens. `npm run build` writes to
`../internal/api/webdist`, which the Go binary embeds. `npm run dev` proxies `/api` to
`http://127.0.0.1:7777`.

## Tokens

Dark is the default; light follows `prefers-color-scheme: light`.

| Token | Dark | Light | Use |
|---|---|---|---|
| `--bg` | `#0e0f11` | `#f7f7f8` | page |
| `--panel` | `#16181c` | `#ffffff` | cards, sidebar |
| `--panel-2` | `#1d2026` | `#f1f2f4` | hover, inputs, user chat bubble |
| `--border` | `#2a2e36` | `#e2e4e8` | 1px borders |
| `--text` | `#e6e8eb` | `#16181c` | body |
| `--muted` | `#9aa1ac` | `#5d6470` | secondary text |
| `--accent` | `#f59e0b` | `#d97706` | brand (forge amber), primary buttons, focus ring |
| `--ok` | `#22c55e` | `#15803d` | done, healthy, low risk |
| `--warn` | `#eab308` | `#a16207` | medium risk, pending |
| `--danger` | `#ef4444` | `#dc2626` | blocked, failed, high risk, denial |
| `--info` | `#60a5fa` | `#2563eb` | running, deploying |
| `--review` | `#a78bfa` | `#7c3aed` | review |

Type: system UI stack, 14px base, 1.5 line height; headings 20/16/14px semibold. Monospace
(`ui-monospace, SFMono-Regular, Menlo, monospace`, 12.5px) for logs, IDs, SHAs, config.
Radius 8px on cards, 6px on controls, 999px on pills. Spacing scale 4/8/12/16/24/32.

## Shared components

- **StatusPill** — dot + label, colour by status: queued `--muted`, running `--info` with a
  pulsing dot, review `--review`, awaiting_approval `--accent`, merged and deploying `--info`,
  done `--ok`, blocked/failed `--danger`, cancelled `--muted`. Runs: starting/running `--info`,
  succeeded `--ok`, failed/stalled/timed_out/denied `--danger`, cancelled `--muted`.
  Deployments: pending/deploying/verifying `--info`, healthy `--ok`, failed/rolled_back `--danger`.
- **RiskBadge** — outlined pill: low `--ok`, medium `--warn`, high `--danger`.
- **RelTime** — "3m ago", full timestamp in `title`.
- **IdChip** — last 8 chars of an ID in monospace (IDs are UUIDv7, whose leading chars are a
  timestamp shared by IDs created close together), copies the full ID on click.
- **Empty** — muted centred line saying what would appear here and why it is empty.
- **Markdown** — marked + dompurify; code blocks in `--panel-2` monospace.
- Buttons: primary (accent fill, dark text), secondary (panel-2 fill), danger (danger outline).
  Every destructive action asks for confirmation inline (button turns into "Confirm?" for 3s).

## Shell

- Left sidebar 224px (`--panel`, right border): wordmark "foundry" in semibold with a 10px
  amber square before it; nav items with counts where useful: Overview, Chat, Tasks,
  Approvals (accent count badge when > 0), Runs, Deploys, Signals, Friction, Loops, Agents,
  Config. Active item: `--panel-2` background and a 2px accent bar on the left.
- Sidebar footer: the **pause switch** — a full-width button. Running: secondary
  "Pause factory". Paused: danger fill "Paused — no new runs. Resume". Below it the live
  indicator: green dot "live" when the event stream is connected, amber "reconnecting…".
- Main area: page header (title left, page actions right) then content, max width 1280px,
  24px padding.
- Under 760px wide the sidebar becomes a top bar (wordmark, approvals badge, menu button)
  with the nav in a slide-down drawer; tables become stacked cards (title row, then
  `label: value` lines).

## Auth

If no token is stored or any call returns 401, show a centred card: "Connect to foundry",
one password field "Token", help text "Run `foundry token` or read `<data_dir>/token`.", and
a Connect button. A `#token=<value>` URL fragment on load is stored in localStorage and
removed from the URL.

## Install on a phone

The UI is an installable web app (it has a web app manifest, `/manifest.webmanifest`, and an
amber-square icon, `/icon.svg`), so it can sit on a phone home screen and open without browser
chrome. Open foundry's URL on the phone first. From another network that needs remote access,
such as Tailscale, and the name you use must be in `server.allowed_hosts`, or the server answers
421. Then:

- iOS (Safari): Share → Add to Home Screen.
- Android (Chrome): menu → Install app (or Add to Home screen).

## Live data

One `EventSource` to `/api/v1/events?token=…&after=<last id>`, reconnecting with backoff
(1s → 30s). Events invalidate react-query keys: `task.*` → tasks, task detail, stats;
`run.updated` → runs, run detail; `approval.*` → approvals, stats; `deploy.updated` →
deployments; `signal.new` → signals; `friction.reported`/`issue.filed` → friction;
`chat.message` → that conversation. `run.log` and `chat.delta` are delivered to subscribers
directly (log viewer, chat stream) without refetching. Lists also refetch every 30s as a
fallback.

## Pages

**Overview** (`/`) — a row of five stat cards: Active runs, Queued, Awaiting approval,
Blocked, Cost today; each links to the filtered list. Below, two columns (one column on
mobile):
- Left: **Needs you** — pending approvals (title, reason, Approve / Reject with optional
  note) and blocked tasks (title, `blocked_reason` in a danger callout, Retry with note,
  Cancel). Empty: "Nothing needs you. The factory is handling everything in flight."
  Then **Live runs** — each active run: role · project, task title, elapsed time, "last
  output 12s ago" (turns `--warn` after 5 minutes), Cancel.
- Right: **Activity** — the event feed, newest first, each row: type tag (muted mono), the
  event message, RelTime, links to task/run when present. Live events prepend.

**Projects** (`/projects`) — one card per project, in a responsive grid (one column on
mobile). Card header: project name (semibold), repo as a muted link to
`https://github.com/<repo>`, and on the right a pause toggle (secondary "Pause" / danger-fill
"Paused — Resume"). When the project is paused in foundry.yaml, the toggle is replaced by a
muted "Paused in foundry.yaml" label (the file wins; the UI cannot unpause it). Body: a row of
counts (queued, running, review, awaiting approval, blocked — each a link to `/tasks` filtered
by project and that status; zero counts muted), then muted lines for the default branch, the
deployer type (or "no deployer") and observers (or "no observers"). Footer: the project's
loops as small pills (name, a dot coloured by health: `--ok` when last run had no error,
`--danger` with the error as a tooltip, `--muted` when disabled or never run). The sidebar
gets **Projects** between Overview and Chat.

**Chat** (`/chat`, `/chat/:id`) — conversation list on the left (240px, "New chat" button,
items show title and RelTime), thread on the right. User messages: right-aligned
`--panel-2` bubbles. Assistant messages: left-aligned, no bubble, rendered Markdown. While
the orchestrator replies, `chat.delta` text streams into a provisional assistant message
with a blinking caret, and a muted line "Orchestrator is working · view run" links to its
run. Composer at the bottom: auto-growing textarea, Enter sends, Shift+Enter newline,
disabled while busy. Empty thread: three example prompts as clickable chips ("What's
running right now?", "Fix the flaky test in …", "Triage open issues in …").

**Tasks** (`/tasks`) — filter bar: project select, status chips (multi-select; default
"active" = queued, running, review, awaiting_approval, merged, deploying, blocked), kind
select, text search (client-side over title). "New task" opens a modal: project, kind,
title, body (textarea, Markdown), role (optional; lists agents), priority (number). Table
columns: status, title (with kind tag and source muted under it), project, role, risk, PR
(#123 link), attempts (`1/3`), updated.

**Task detail** (`/tasks/:id`) — header: title, StatusPill, actions Cancel and Retry (with
note). Danger callout with `blocked_reason` when blocked. Meta grid: project, kind, role,
source and source_ref, branch (mono), PR link, risk badge with reasons as a list, attempts,
created, updated, IdChip. Sections: Summary (latest agent summary), Body (Markdown),
Approvals (with decide controls), Subtasks (children), Runs (status, harness · model,
started, duration, cost, link).

**Run detail** (`/runs/:id`) — header: role · harness · model, StatusPill, Cancel while
active. Meta: task link, project, branch, workdir, session ID, exit code, cost, tokens,
started/ended, error (danger callout). Log viewer fills the rest: monospace lines with a
time gutter; `text` plain; `tool_use` prefixed with an accent "▸"; `tool_result` muted,
collapsed to 3 lines with "show more"; `denial` on a danger-tinted background with a
"DENIED" tag; `stderr` muted italic; `error` danger; `system` muted. A "Follow" toggle
(default on) keeps the view scrolled to the bottom. Logs load from `/runs/:id/logs` and
then append `run.log` events for this run, deduplicated by `seq`.

**Approvals** (`/approvals`) — pending by default, toggle "All". Cards: title, kind,
project, task link, reason (Markdown), created; Approve (primary) / Reject (danger) with an
optional note field.

**Runs** (`/runs`) — table: status, role, harness · model, project, task, started, duration,
cost. Filters: project, status.

**Deploys** (`/deploys`) — table: project, ref (short SHA mono), state, URL, task, started,
updated, notes.

**Signals** (`/signals`) — table: project, source, title (links to URL), level, count, last
seen, task link.

**Friction** (`/friction`) — table: project, title, count, issue (link), updated.

**Loops** (`/loops`) — table: name, project, enabled, every, last run, last error (danger,
truncated with title tooltip); "Run now" per row.

**Agents** (`/agents`) — card grid: name, description, harness and its type, model,
prompt, and the task kinds routed to it.

**Config** (`/config`) — the file path, a Reload button, and the redacted config as
syntax-highlighted JSON (keys `--info`, strings `--ok`, numbers `--warn`). Note above it:
"Edit this file and press Reload. Secrets are hidden."
