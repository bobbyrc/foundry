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

The visual world is an **andon board**: the lamp board over a factory line that is green while
work flows, yellow when a station calls for help, and red when the line stopped itself. A
charcoal board carries the navigation and the Overview's lamp board; the page ground is a pale
machine-green enamel. Light is the default; dark follows `prefers-color-scheme: dark`. Structure
is drawn with 1px rules and square corners; content has no shadows.

| Token | Light | Dark | Use |
|---|---|---|---|
| `--bg` | `#e3eae4` | `#111614` | page ground |
| `--panel` | `#f5f8f5` | `#181e1b` | sheets: items, tables, meta grids |
| `--panel-2` | `#eaf0eb` | `#202723` | hover, code |
| `--input` | `#ffffff` | `#0d1210` | form fields |
| `--field` | `#6f7d76` | `#64736b` | form field outlines (3:1 against field and ground) |
| `--border` / `--rule` | `#b9c6bd` / `#cdd7d0` | `#3a4741` / `#2a332f` | outer rules / inner rules |
| `--text` / `--muted` | `#18201c` / `#4b5751` | `#e4ebe6` / `#a1aea7` | body / secondary text |
| `--board` | `#1f2422` | `#0a0d0c` | nav rail, phone top bar, lamp board |
| `--lamp-run` | `#2fa35a` | same | running, in review, merged, deploying |
| `--lamp-call` | `#f2c230` | same | waits on you: approvals; medium risk; brand square |
| `--lamp-stop` | `#d7342a` | same | stopped: blocked, failed, denied; high risk |
| `--lamp-done` | `#7f9d8c` | `#6f8e7d` | done, healthy |
| `--lamp-off` | `#3a423e` | `#2c3430` | idle, queued, cancelled, paused |

Status colours as text (`--ok`, `--warn`, `--danger`, ...) are darker or lighter versions of the
lamps chosen for 4.5:1 contrast on the ground. The primary button is ink-filled (`--accent` is
the text colour), not a lamp colour, so lamp colours keep their meaning.

Type: Barlow (text, 15px, line height 1.5) and Barlow Condensed (page titles 30px, section
labels and table headers in 13-15px caps, board window names), both self-hosted from
`src/assets/fonts/` (SIL Open Font License) because the CSP allows same-origin fonts only.
Monospace (`ui-monospace, SFMono-Regular, Menlo, monospace`, 13px) only for logs, IDs, SHAs and
config. Radius 0 on sheets, 2px on controls and pills. Numbers in tables and counts use tabular
figures. Motion: lamps switch, never fade: state changes are a two-step at 90ms, and running work
blinks on and off rather than pulsing; both stop under `prefers-reduced-motion`.

## Shared components

- **StatusPill**: a square lamp plus the status in words, on a 1px-ruled chip. Lamp by status:
  queued and cancelled off; running (blinking), review, merged and deploying run green;
  awaiting_approval call yellow; done done-green; blocked and failed stop red. Runs:
  starting/running run (blinking), succeeded done, failed/stalled/timed_out/denied stop,
  cancelled off. Deployments: pending/deploying/verifying run, healthy done, failed/rolled_back
  stop. The tone names in `src/lib/status.ts` are unchanged; the stylesheet maps them to lamps.
- **RiskBadge**: condensed caps on one fixed ramp, the same everywhere: low outlined, medium on
  call yellow, high on stop red, unrated as quiet muted text.
- **RelTime**: "3m ago", full timestamp in `title`.
- **IdChip**: last 8 chars of an ID in monospace (IDs are UUIDv7, whose leading chars are a
  timestamp shared by IDs created close together), copies the full ID on click.
- **Empty**: muted centred line in a dashed rule saying what would appear here and why it is
  empty.
- **Markdown**: marked + dompurify; code blocks in `--panel-2` monospace.
- **ApprovalCard**: a call item (see Overview). Under the title: status, kind, project, task,
  when requested. Then the gate: the task's risk badge and risk reasons, and the commit the
  approval covers (short SHA, PR link, "a new push withdraws this approval"). Then the reason
  (clamped to 3 lines), the decision (optional note, "Approve merge" for merge approvals or
  "Approve", and Reject), and only then "What changed" and the reviewer's summary, each clamped
  to 3 lines with "show more", so the decision stays in the first screen on a phone.
- Buttons: primary (ink fill), secondary (ink outline), danger (stop-red outline), danger-fill,
  ghost. Every destructive action asks for confirmation inline (button turns into "Confirm?"
  for 3s).

## Shell

- Left rail 216px in `--board`: wordmark "onlet" in Barlow Condensed with a 12px call-yellow
  square before it; nav items: Overview, Projects, Chat, Tasks, Approvals, Runs, Deploys,
  Signals, Friction, Loops, Agents, Config, and Users (owners only). Approvals carries a
  yellow count badge when approvals wait; Tasks a red one when tasks are blocked (each count
  has screen-reader text). The active item is lit (`--board-2` background, white text).
- Rail footer: the **account panel**, then the **pause switch**. The account panel shows the
  signed-in username, the role in muted lower case, a "Sign out" link and a "Change password"
  link. Change password opens a dialog (current password, new password of at least 12
  characters, confirm); on success it says "Password changed." for three seconds. The dialog
  is drawn over the page, not inside the rail. The pause switch is the line's cord: a dark
  window with the line's state on the left (green lamp "Running", or red lamp "Paused" with
  "No new runs start." and a red rule) and the action on the right (an outlined "Pause", or a
  filled "Resume"; viewers see only the state). Below it the live
  indicator: green square "live" when the event stream is connected, yellow "reconnecting…".
- Main area: content up to 1360px wide, 28px top and 32px side padding.
- Under 760px wide the rail becomes a charcoal top bar (wordmark, "1 approval" and "1 blocked"
  badges in words, menu button) with the nav in a full-height drawer; tables become stacked
  sheets (title row, then `label: value` lines).


## Auth

Until someone signs in, the page shows one sheet on the charcoal board. If onlet has no
accounts yet, the sheet is **Create the owner account**: a username, a password typed twice
(at least 12 characters) and "Create account". That first account is the owner. Once accounts
exist, the sheet is **Sign in**: a username, a password and "Sign in". A wrong username or
password says so, and too many wrong attempts are refused for a few minutes.

Signing in sets a session cookie that the server marks HttpOnly, so page scripts cannot read
it. The browser sends it with every request and with the live event stream. Nothing is kept
in localStorage; the UI only remembers who is signed in (`src/auth.ts`). When a request
returns 401 during a session, the session has ended: the sheet comes back with "Your session
ended. Sign in again." A wrong password at sign-in never shows that line. Sign-out, and an
ended session, clear everything cached from the account so the next person does not see it.

Roles decide what the page draws. The server checks every request as well.

- **viewer** reads everything. No button that changes anything is drawn. In a chat, the
  composer is replaced by "Viewers can read conversations but not send messages."
- **operator** also approves and rejects, retries and cancels tasks and runs, pauses and
  resumes the factory or a project, creates tasks and chat conversations, sends chat messages
  and runs loops now.
- **owner** also manages accounts on the Users page.

A control a role may not use is left out, not shown greyed out.

## Install on a phone

The UI is an installable web app (it has a web app manifest, `/manifest.webmanifest`, and an
icon of a yellow lamp square on charcoal, `/icon.svg`), so it can sit on a phone home screen
and open without browser chrome. Open onlet's URL on the phone first. From another network
that needs remote access, such as Tailscale, and the name you use must be in
`server.allowed_hosts`, or the server answers 421. Then:

- iOS (Safari): Share → Add to Home Screen.
- Android (Chrome): menu → Install app (or Add to Home screen).

## Live data

One `EventSource` to `/api/v1/events?after=<last id>`, which the browser sends with the session
cookie. It starts only once someone is signed in. It reconnects with backoff (1s → 30s). Events invalidate react-query keys: `task.*` → tasks, task detail, stats;
`run.updated` → runs, run detail; `approval.*` → approvals, stats; `deploy.updated` →
deployments; `signal.new` → signals; `friction.reported`/`issue.filed` → friction;
`chat.message` → that conversation. `run.log` and `chat.delta` are delivered to subscribers
directly (log viewer, chat stream) without refetching. Lists also refetch every 30s as a
fallback.

## Pages

**Overview** (`/`): the andon board, then the work.
- **Board**: a charcoal band with one window per project (from `/projects`). A window's lamp
  bar is lit by what the project needs (`src/lib/lamp.ts`): red when a task is blocked, yellow
  when approvals wait, off when paused, green when tasks run or are in review, off when only
  queued or idle. Under the project name the reason in words ("1 blocked", "2 need you",
  "3 running") and the other non-zero counts. A window links to the project's tasks. At the
  right end, the line window: "Line running" or "Line paused", active runs, queued tasks and cost
  today. Empty: "No projects yet." On a phone the board is a compact strip (two windows across,
  no detail lines, the line window on one row) so the first decision fits in the first screen.
- Below, two columns (one on a phone, board windows two across):
- Left: **Needs you**, with "N waiting · oldest 14m" beside the heading. Pending approvals
  (ApprovalCard) and blocked tasks, each a call item: a sheet with a lit lamp before the title
  (yellow for approvals, red for blocked), a muted line under it, then the body. Blocked items:
  "Blocked · project · attempt 2 of 3 · 11m ago", the `blocked_reason` in a stop callout, Retry
  with note and Cancel. Empty: "Nothing needs you. The factory is handling everything in flight."
- Right: **Live runs** on one shared time axis (`src/lib/runbar.ts`; 10m, 20m, 30m, 1h, ...
  whichever fits the longest run). Each run: task title (links to the run), Cancel, "role ·
  project", elapsed time and "output 12s ago", then a bar whose length is the elapsed time:
  green up to the last output, yellow after it once the run has been quiet for 5 minutes ("quiet
  for 7m" in the text). Then **Activity**: the event feed, newest first, in fixed columns:
  clock time, event type in words, message with one "task" (or "run") link. Ten rows show, with
  "Show all N" to expand in place. Live events prepend.
- Not built yet: a compact lamp row on pages other than Overview, and fuller window details on
  focus. The nav badges carry "needs you" and "blocked" everywhere meanwhile.

**Projects** (`/projects`) — one card per project, in a responsive grid (one column on
mobile). Card header: project name (semibold), repo as a muted link to
`https://github.com/<repo>`, and on the right a pause toggle (secondary "Pause" / danger-fill
"Paused — Resume"). When the project is paused in onlet.yaml, the toggle is replaced by a
muted "Paused in onlet.yaml" label (the file wins; the UI cannot unpause it). Body: a row of
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

**Users** (`/users`, owners only; anyone else is sent to Overview) — a table of accounts:
username (your own row says "(you)"), role, added (relative time) and actions. The role is a
select that saves as soon as it changes; changing your own role reloads your session so the
menus match it. "Set password" opens a field under that row with Save and Cancel. "Remove"
asks for confirmation. "Add user" opens a sheet above the table with username, password and
role (default operator) and "Create user" or "Cancel". The server's message is shown under the
table when it refuses a change, such as demoting or removing the last owner.

**Agents** (`/agents`) — card grid: name, description, harness and its type, model,
prompt, and the task kinds routed to it.

**Config** (`/config`) — everyone sees the file path and the redacted config as syntax-highlighted
JSON (keys `--info`, strings `--ok`, numbers `--warn`), with the note "Only an owner can change
settings. Secrets are hidden." Operators also get a Reload button. An owner gets the editor, which
opens on the **Visual** tab. The top-level sections of `onlet.yaml` sit in a list on the left (a
square `--lamp-call` after a section with unsaved edits, `--lamp-stop` after one with problems); the
active section's settings are on the right. A number, text or duration setting is an input; a
checkbox is "On"; clearing an input removes the key. A value written as `${NAME}` shows as text with
a note that onlet reads that environment variable. A map is one card per entry, with Rename and
Remove (Remove asks first), and a form at the end to add one. A list has a row or a card per item,
with Remove, and an Add button. A value with no simple form is edited as JSON. Edits stay on the
page until saved: a bar at the bottom says how many there are, with Save and Discard. A save writes
nothing when the file changed on disk since the page opened (a notice offers to load the latest,
which discards your edits) or when the result would be invalid (the problems are listed above the
editor, and each is also shown under its field). Settings that take effect only after a restart are
named in a note until dismissed. The **YAML** tab edits the raw text of the file, with the same bar
and checks, and switching tabs with unsaved edits asks first. "Reload from file" makes the daemon
read the file again.
