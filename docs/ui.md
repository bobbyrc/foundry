# Web UI design (v0)

The web UI is a control room: see what the factory is doing, answer what it needs from you,
and talk to the orchestrator. It is a client of `docs/api.md` and nothing else; native and
mobile apps should follow the same information architecture.

## Stack

Vite + React + TypeScript, react-router, @tanstack/react-query, marked + dompurify for
Markdown. No component library; `src/styles.css` holds the design tokens and shared components, and a page with rules of its own imports a small CSS file next to it (`src/pages/TaskDetail.css`). `npm run build` writes to
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
| `--lamp-call` | `#f2c230` | same | waits on you: approvals; medium risk; the lit segment of the brand mark |
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

- Left rail 216px in `--board`: wordmark "onett" in Barlow Condensed after the brand mark, a
  loop in the text colour with one call-yellow lit segment (work going round the line); nav items: Overview, Projects, Chat, Tasks, Approvals, Runs, Deploys,
  Signals, Friction, Loops, Agents, Config, and Users (owners only). Approvals carries a
  yellow count badge when approvals wait; Tasks a red one when tasks are blocked (each count
  has screen-reader text). The active item is lit (`--board-2` background, white text).
- Project lamps: between the nav and the footer, one 26px row per project (its lamp square,
  name, and its lamp label in words, right-aligned: "1 needs you" or "1 blocked" in call or stop
  colour, "2 running", "idle", "paused" or "3 queued" in muted text), most urgent first,
  each linking to that project's tasks. More than eight projects show the eight most urgent and
  an "All projects" row. Hidden on Overview, which has the full board, and under 760px.
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

Until someone signs in, the page shows one sheet on the charcoal board. If onett has no
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
icon of the brand mark on charcoal, `/icon.svg`), so it can sit on a phone home screen
and open without browser chrome. Open onett's URL on the phone first. From another network
that needs remote access, such as Tailscale, and the name you use must be in
`server.allowed_hosts`, or the server answers 421. Then:

- iOS (Safari): Share → Add to Home Screen.
- Android (Chrome): menu → Install app (or Add to Home screen).

## Live data

Each tab keeps one WebSocket open, to `/api/v1/ws?after=<last id>` (`wss:` on an https page). A
WebSocket is a connection that stays open in both directions, so the server can push events and
the page can ask for subscriptions over it. The browser sends the session cookie with the
connection, and it starts only once someone is signed in. It reconnects with backoff (1s → 5s),
and at once when the tab is shown again or the network returns. The `onett` CLI uses SSE
(`/api/v1/events`) instead.

Events invalidate react-query keys: `task.*` → tasks, task detail, stats; `run.updated` → runs,
run detail; `approval.*` → approvals, stats; `deploy.updated` → deployments; `signal.new` →
signals; `friction.reported`/`issue.filed` → friction; `chat.message` → that conversation, every
task agent's conversation and the agents list; `conversation.updated` (a close or reopen) → that
task agent's conversation and the agents list. `run.updated` also refreshes the conversations,
the agents list and that run's cached log.

`run.log` goes only to tabs subscribed to that run, and `chat.delta` only to tabs subscribed to
that conversation; every other event goes to every tab. A page subscribes with `watch()` in
`web/src/events.ts`. The log viewer watches its run and, once the server confirms, fetches the
lines it missed. The chat thread watches the run that is live in it the same way. After a reconnect the live indicator
shows "live" only once the server has confirmed every subscription the page holds. A watch added
on a connection that is already open does not change the indicator. Lists also refetch every 30s
as a fallback.

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
- Right: **Live runs** on one shared time axis (`src/lib/runbar.ts`; 20m, 40m, 1h, 2h, ...
  whichever fits the longest run). Each run: task title (links to the run), Cancel, "role ·
  project", elapsed time and "output 12s ago", then a bar whose length is the elapsed time:
  green up to the last output, yellow after it once the run has been quiet for 5 minutes ("quiet
  for 7m" in the text). Then **Activity**: the event feed, newest first, in fixed columns:
  clock time, event type in words, message with one "task" (or "run") link. Ten rows show, with
  "Show all N" to expand in place. Live events prepend.
- Not built yet: a compact lamp row on pages other than Overview, and fuller window details on
  focus. The nav badges carry "needs you" and "blocked" everywhere meanwhile.

**Projects** (`/projects`) — one sheet per project, in a grid (one column on a phone). Each
sheet is a window of the Overview's board. A 10px lamp bar runs across its top in the colour the
Overview shows for that project: green while work runs, yellow when something waits on the
owner, red when work stopped itself, grey when idle or paused. Under the project name are the
repo (a muted link to `https://github.com/<repo>`) and the lamp's label in words, such as
"1 needs you". On the right is the pause toggle (secondary "Pause" / danger-fill
"Paused — Resume"). When the project is paused in onett.yaml, the toggle is replaced by a muted
"Paused in onett.yaml" label, because the file wins and the UI cannot unpause it. Below the name
is a row of five counts: queued, running, review, needs you (awaiting approval) and blocked. Each
non-zero count links to `/tasks`, filtered by that project and status. Blocked is red and needs
you is yellow. Zero counts are muted and are not links. One muted line follows with the default
branch, the deployer type (or "no deployer") and the observers (or "no observers"). The last line
is the loops: "Loops: 5 on, 2 off", where "Loops" links to `/loops`. If any loop's last run
failed, its name follows in red ("pr_shepherd failing"), and the error shows as a tooltip. The
sidebar gets **Projects** between Overview and Chat.

**Chat** (`/chat`, `/chat/:id`, `/chat/task/:taskId`) — talk to the orchestrator or to any task
agent, and read what an agent did as it works. With no conversation open, a centred block 640px
wide sits 8vh from the top: the heading "Talk to the orchestrator", a muted line on what it can and
cannot do, the composer, and three example prompts as chips. Sending creates a conversation and
opens it with the message already sent.

The 260px sidebar has a "+ New conversation" button (always the orchestrator; operators only), an
**Orchestrator** section of conversations (title, RelTime), then a quieter **Agents** section from
`GET /agent-conversations`. Each task with an agent shows its title, and under it a row with a lamp,
the role and a text label: a blinking green lamp and "working" while a run is live; a dim lamp and
"closed" for a closed conversation; a red lamp and "blocked" for a blocked task; a dim lamp and
the task's status otherwise. Under 760px the sidebar is a row of chips above the thread.

The header names who you are talking to ("Orchestrator", or the role such as "Implementer"), with
harness · model (from the latest run) and the conversation title, or for a task agent its task as
a link, its branch and status. On the right sit Close (task agents, operators, while open) and the
**Conversation / Terminal** switch; the choice is remembered in the browser. The transcript is the
same one the stored run logs give: owner messages as right-aligned sheets, agent prose as Markdown
under the agent's name, each tool call as one row (name, main argument, `+n −m` for edits) that
opens to its input and result, and a red DENIED row for a refused call. Terminal view shows one
line per entry with a time gutter, `›` for owner and prompt, `●` for prose and tool calls and `⎿` for
results clipped to three lines with "show more". A task agent's view shows each run in order, with a
divider where a retry started a new session; a run with no recorded prompt starts with the task body
labelled "Task (prompt not recorded)". Lines of the live run arrive as `run.log` events, so tool
rows appear as they happen. When an orchestrator turn created tasks, a chip under that turn shows
each task's lamp, title and status, with "open agent" (`/chat/task/:id`) and "task" links.

While a run is live a line shows "Working · 3m 41s · output 2s ago" with a link to the run and a
**Stop** button (Esc in the field does the same); both cancel the live run. The composer stays
enabled: a message sent to the orchestrator mid-run reaches it at once, and a message for a task
agent is queued. Queued messages show at the end in a dashed box, "Queued · sends when the agent
finishes this run", with edit and cancel (no longer possible once delivered). When messages are
queued and no run is live, for example after a failed run, **Send now** starts the follow-up.

A task agent's view adds a yellow note above the box, "You're talking directly to this task's
agent. For anything beyond this task, ask the orchestrator." (the last words link to `/chat`).
Close asks for a reason. A closed conversation replaces the composer with "Closed by <who>:
<reason>", shown as plain text, and a **Reopen** button for operators and owners. When the server
refuses a message (a 409) the composer shows its reason. Viewers read the thread and see a muted
line, "Viewers can read conversations but not send messages.", in place of the composer.

**Tasks** (`/tasks`) — filter bar: project select, status chips (multi-select; default
"active" = queued, running, review, awaiting_approval, merged, deploying, blocked), kind
select, text search (client-side over title). "New task" opens a modal: project, kind,
title, body (textarea, Markdown), role (optional; lists agents), priority (number). Table
columns: status, title (with kind tag and source muted under it), project, role, risk, PR
(#123 link), attempts (`1/3`), updated.

**Task detail** (`/tasks/:id`) — a header row with the title, StatusPill and the Cancel and Retry
actions (role-gated as before). Under it, one muted line: kind · project · role · from source, then
the IdChip and, once the task has run, an "Open conversation" link to `/chat/task/:id`. Below that is the task's line: seven lamps (Queued, Running, Review, Approval, Merged,
Deploying, Done) joined by a rule. Passed stations show the done lamp; the current one lights in
its tone (running blinks); a stop is red and its label reads Blocked, Failed or Cancelled; skipped
stations are hollow; stations not yet reached are dim. From 960px the page has two columns: the
main column, and a 300px spec sheet that stays in view. The sheet lists risk with its reasons,
pull request and branch, attempts, project, kind, role, source, parent, created, updated and ID.
Under 960px it is one column, with the sheet directly after "Needs you". The main column, top to
bottom: Needs you (only when something waits: the blocked reason with Retry and Cancel, a note
that the project is paused, and pending approvals with their decide controls); Summary and Body
(Markdown, when present); Runs (# is the attempt number in start order, then status, harness ·
model, started, duration, cost and link); Subtasks (when there are any); Decided approvals (one
compact row each: decision, title, time and note).

**Run detail** (`/runs/:id`) — header: "role · project" with the StatusPill, and Cancel run on
the right while the run is active (operators only). Under it, muted: harness · model, then the
task title as a link and "Open conversation" (`/chat/task/:taskId`). Then the time strip, the run's bar on its own axis: the same bar as the
Overview's live runs, drawn to the run's elapsed time, green up to the last output and yellow
after 5 minutes without output. The axis ticks sit above it, and at the right of the ticks the
elapsed time with "output 4s ago" or "quiet for 7m". A finished run is one solid bar and no
yellow: grey when it succeeded, red when it stopped itself (failed, stalled, timed out, denied),
light grey when cancelled. Under the strip, a row of facts (label above value): branch, cost,
tokens, started, duration, and exit code once the run has ended. "More" expands workdir, session
ID and run ID. A run's error shows in a red callout above the log. The log takes the rest of the
screen (at least 360px on a desktop; 60% of the screen height on a phone). Its toolbar has "Log",
the line count, and a "Follow" toggle (default on) that keeps the view at the newest line. Log
lines: `text` plain; `tool_use` with a green drawn chevron before it; `tool_result` muted,
collapsed to 3 lines with "show more"; `denial` on a red-tinted line with a "DENIED" tag before
the text; `prompt` (what onett gave the agent to start the run) a muted block labelled "Prompt"; `owner` (a message from the owner) a line labelled "You"; `stderr` muted italic; `error` red; `system` muted. The time gutter is 64px (52px on a
phone). Logs load from `/runs/:id/logs` and then append `run.log` events for this run,
deduplicated by `seq`.

**Approvals** (`/approvals`) — pending by default, with a Pending / All control in the header.
The list is at most 920px wide so the agent-written prose stays readable. Pending with nothing
waiting shows one muted line: "Nothing waits on you. Approvals appear here when a change needs
your sign-off." "All" adds decided approvals as cards, each with its status pill. Cards: title,
kind, project, task link, reason (Markdown), created; Approve (primary) / Reject (danger) with an
optional note field. Full width under 760px.

**Runs** (`/runs`) — table: status, task, role, harness · model, started, duration, cost.
The task cell shows the task's title as a link to the run's page, with a smaller line under it
giving the project and the task's short ID (a link to the task). Active runs (starting or
running) come first; the rest keep the server's order, newest first. Filters: project, status.
On a phone the status is the card's title row and the task comes second, because the stacked
table always takes the first column as the title.

**Deploys** (`/deploys`) — table: project, ref (short SHA mono), state, URL, task, started,
updated, notes.

**Signals** (`/signals`) — table: project, source, title (links to URL), level, count, last
seen, task link.

**Friction** (`/friction`) — table: project, title, count, issue (link), updated.

**Loops** (`/loops`) — a lamp grid: one row per loop name, one column per project. Loops that run
across all projects get an "all projects" column, placed first. Each cell has a 10px lamp, then
"every 30s" (the interval as configured), then on a second line "ran 3m ago" or "never". A disabled
loop reads "off · every 30s" in muted text, so it differs from a healthy loop by words as well as by
its grey lamp. The lamp is green when the last run worked, red when it failed, and grey when the
loop is disabled or has not run yet; the words are screen-reader text. A failed loop shows its error
in red on that second line, cut to one line, with the full text on hover. Operators see a "Run"
button in each cell; viewers do not. Above the grid, one muted line counts loops on, off and failing
(failing in red when any). Under 760px the grid becomes one sheet per loop name, with a row for each
project it runs in.

**Users** (`/users`, owners only; anyone else is sent to Overview) — a table of accounts:
username (your own row says "(you)"), role, added (relative time) and actions. The role is a
select that saves as soon as it changes; changing your own role reloads your session so the
menus match it. "Set password" opens a field under that row with Save and Cancel. "Remove"
asks for confirmation. "Add user" opens a sheet above the table with username, password and
role (default operator) and "Create user" or "Cancel". The server's message is shown under the
table when it refuses a change, such as demoting or removing the last owner.

**Agents** (`/agents`) — card grid of the configured roles. Each card shows the name, a badge
(`built-in`, `edited`, `custom` or `removed`: where its settings come from, or that it is switched
off), the description, the harness and its type, the model (or "harness default"), and **Used for**:
one line per task kind or route that uses the agent, with the project when one is named. The prompt
shows its first 8 lines with "Show full prompt" to expand it. An edited built-in that no longer
receives onett's prompt updates says so, and "Compare with built-in" puts the built-in text beside
the customised one (stacked on a phone). Enabled agents come first; removed ones sit under a
"Removed" heading. Anyone can read the page. An owner also sees:
"New agent", which opens a sheet with name (refused when taken), description, harness (the
configured harnesses), model, args (one per line), and the prompt, either a reference such as
`builtin:triager` or a path, or text written here. "Start from" copies another agent's settings
into the sheet, or starts blank. An enabled card has "Edit"; "Remove" (a built-in or edited agent is
switched off, a custom one is deleted); and "Reset to built-in" (an edited one). Remove and reset ask
for a second click ("Confirm?"). A removed card shows "Restore" only, with no Edit: restore it first,
then edit it. A refused change, such as removing an agent a route still uses, shows the server's
reason on the page and changes nothing.

**Config** (`/config`) — everyone sees the file path and the redacted config as syntax-highlighted
JSON (keys `--info`, strings `--ok`, numbers `--warn`), with the note "Only an owner can change
settings. Secrets are hidden." Operators also get a Reload button. An owner gets the editor, which
opens on the **Visual** tab. The top-level sections of `onett.yaml` sit in a list on the left (a
square `--lamp-call` after a section with unsaved edits, `--lamp-stop` after one with problems); the
active section's settings are on the right. A number, text or duration setting is an input; a
checkbox is "On"; clearing an input removes the key. A value written as `${NAME}` shows as text with
a note that onett reads that environment variable. A map is one card per entry, with Rename and
Remove (Remove asks first), and a form at the end to add one. A list has a row or a card per item,
with Remove, and an Add button. A value with no simple form is edited as JSON. Edits stay on the
page until saved: a bar at the bottom says how many there are, with Save and Discard. A save writes
nothing when the file changed on disk since the page opened (a notice offers to load the latest,
which discards your edits) or when the result would be invalid (the problems are listed above the
editor, and each is also shown under its field). Settings that take effect only after a restart are
named in a note until dismissed. The **YAML** tab edits the raw text of the file, with the same bar
and checks, and switching tabs with unsaved edits asks first. "Reload from file" makes the daemon
read the file again.
