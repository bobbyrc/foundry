# Proposal: agent conversations

Status: design agreed with the owner, 2026-10-29. Nothing here is implemented. The clickable
mockup the owner approved is `agent-conversations-mockup.html` next to this file (throwaway HTML,
not UI code; open it in a browser).

## Why

The owner wants chat to be onett's main surface, and to feel like using Claude Code or the Claude
desktop app: you talk to an agent, watch it work, and reply in the same session.

Today chat only reaches the orchestrator (the agent that turns requests into tasks), and its
replies are plain text: what it actually did (CLI calls, files read, tasks created) is only
visible on a separate run page. A task's agent (implementer, triager, reviewer) cannot be
talked to at all. Its work is a flat log on `/runs/:id`, and the only way to redirect it is
Retry, which starts a fresh agent with a note appended to the task body.

The owner's words: "Users should be able to message any agent, but should feel led to mostly
interact with the orchestrator. Chat should work like using Claude Code or the desktop app, for
all agents."

## Goals

- One chat experience for every agent: the orchestrator and each task's agent.
- A transcript that reads like Claude Code: the agent's prose, each tool call as a one-line row
  that expands to its output, edits with their added and removed line counts, live as it works.
- A **Conversation / Terminal** toggle on every transcript. Terminal is the same content as
  dense, timestamped monospace lines. It is a rendering, not a shell.
- Chat opens on the orchestrator and new conversations go to it. Task agents are reachable but
  sit lower: a quieter "Agents" section of the sidebar, and links from tasks, runs and the
  orchestrator's own transcript.
- You can type while an agent works. Like Claude Code, the message waits and goes in when the
  agent's current turn ends.
- Messaging a task's agent after its run ended continues that agent's session (see **Task agent
  follow-ups**).

## Non-goals

- **Onett does not become an agent harness.** No message is pushed into a running agent process.
  Onett keeps running agents non-interactively, one turn per run, and continuing a conversation
  uses the agent CLI's own resume (`claude --resume`, `codex exec resume`).
- No pseudo-terminal, no rendering of an agent's own TUI, no interactive shell on a worktree, no
  xterm.js. The Terminal view is styled HTML.
- Agents reporting back to the owner without being asked (task finished, blocked, has a
  question). The owner wants all such messages to go through the orchestrator; that is a factory
  behaviour change and gets its own design later.
- Changing Retry. It keeps its current meaning: a fresh agent with a note.
- New dependencies.

## Anti-goals

These would make the work a failure even if it technically ran:

- Two input modes ("steer" versus "message"). There is one message box with one behaviour.
- An agent messaging the owner directly.
- A message that changes code bypassing the normal pipeline (commit, push, CI, review, risk,
  approval).
- Weakening any invariant in `AGENTS.md`. In particular, a denial or stall must still end in a
  blocked task, and nothing automatic may restart the agent afterwards (invariant 4).

## How it works today (facts the design builds on)

- **Orchestrator chat** (`internal/engine/chat.go`). A `Conversation` holds the orchestrator's
  harness `SessionID`. Each owner message is one *turn*: onett starts one run (role
  `orchestrator`) that resumes that session, streams its text as `chat.delta` events, and stores
  the reply as a `ChatMessage` with the run's ID. The composer is disabled while a reply runs.
- **Task runs** (`internal/engine/runner.go`). Each run of a task gets a new git worktree named
  after the run ID, on the task's branch. Runs never resume an earlier session. When a run
  publishes or finishes cleanly its worktree is removed (the branch stays). A blocked task keeps
  its newest worktree.
- **Run logs.** Every run's output is stored as `LogLine`s with a kind (`text`, `tool_use`,
  `tool_result`, `denial`, `system`, `stderr`, `error`). A `tool_use` line's text is
  `Name: <compact JSON input>`, truncated. Results follow their calls in order, but no ID links a
  result to its call. The prompt a run was started with is not stored.
- **Live updates.** One WebSocket per browser tab (`/api/v1/ws`). `run.log` and `chat.delta`
  reach a tab only for the runs and conversations it has subscribed to.
- **Cancel.** `CancelRun` stops a live run and blocks its task with "run cancelled by owner".

## Design

### One transcript model

A transcript is an ordered list of entries: owner messages, the prompt that started a run, and
the run's log lines. Both kinds of conversation produce it:

- **Orchestrator conversation:** the conversation's `ChatMessage`s, with each turn's run log
  placed after the owner message that started it (`ChatMessage.RunID` links them). The stored
  assistant reply stays the source of the final text; tool rows come from the run log.
- **Task agent conversation:** the task's runs for its own role (`Run.Role == Task.Role`), oldest
  first. Each run begins with its prompt (the task prompt for the first run, the owner's
  message for a follow-up). When a run starts a new session instead of resuming one (the first
  run after a Retry), a divider says "New session" and why ("retried by owner").

  A review task is its own task with its own reviewer agent, so it is its own conversation.

Two small additions to stored logs make this possible:

- A new `LogKind` `prompt`: the run's prompt, written as the run's first log line. This is
  the task prompt or the owner's message that started the run. It does not include the role
  instructions (`Spec.Instructions`).
  Runs from before this change have no `prompt` line. Their transcript starts with the task body,
  labelled "Task (prompt not recorded)". (My call.)
- An optional tool-call ID on `LogLine` (`tool_id`, empty when the harness gives none), so a
  result attaches to its call even when an agent makes several calls at once. The Claude parser
  has the IDs (`tool_use.id`, `tool_result.tool_use_id`); the Codex parser sets it where its
  JSON has one. With no ID, the UI pairs a result with the nearest call before it that has no
  result yet. This needs a schema migration (append-only, per `AGENTS.md`).

### Sending a message

There is one message box. What a message does depends only on the target's state:

| Target | State | What happens |
|---|---|---|
| Orchestrator | idle | A turn starts at once, as today. |
| Orchestrator | replying | The message is queued and starts the next turn when this one ends. |
| Task agent | its run is live, or a follow-up is waiting for the scheduler | The message is queued (see below). |
| Task agent | task blocked, failed, in review or awaiting approval | A follow-up is queued for the scheduler (see **Task agent follow-ups**). |
| Task agent | task merged, deploying, done or cancelled, or queued with no run yet | Read-only. The box is replaced by a line that points to the orchestrator: "This task is merged. Ask the orchestrator for follow-up work." |

Done means read-only for every task, including a question task whose triager has just answered
and a review task whose verdict is in. This is the owner's decision: to follow up on an answer,
ask the orchestrator, which can create a new task.

**Queued messages.** A message sent to a busy agent is stored at once and shown at the end of the
transcript in a dashed box: "Queued · sends when the agent finishes this turn", with Edit and
Cancel. Several queued messages are joined, in order, into the next turn's prompt. They are
stored as `ChatMessage`s with `queued: true` until a run takes them, so they survive a reload
and every viewer sees them. Edit and Cancel work only while the message is still queued; a
message a run has taken cannot be edited or cancelled.

When a turn ends, queued messages start the next turn **only if the run ended normally**
(succeeded). If it ended denied, stalled, timed out, failed or cancelled, the task is blocked
exactly as today, and the queued messages stay queued under a "Send now" button. The owner must
act; onett never restarts an agent on its own after a denial or stall. A new message sent then
joins the queue and the whole queue goes as one turn, the same as pressing "Send now". This holds
for the orchestrator too, where a stopped turn blocks nothing.

If a task's run succeeds but the task lands in a read-only status (done, merged), its queued
messages are not sent. They stay in the transcript, marked "The task finished before this was
sent", with Cancel and an "Ask the orchestrator" link that opens a new orchestrator conversation
with the text in the message box. (My call.)

**Stop.** Esc in the box, or a Stop button while the agent works, cancels the current run
(`CancelRun`), as Claude Code's Esc interrupts a turn. For a task agent the task is blocked as
today, and the next message starts a follow-up.

**Who can send.** Operators and owners. Viewers read transcripts and see the existing
"Viewers can read conversations but not send messages." line.

### Task agent follow-ups

A follow-up is a new run of the task's role that resumes the session of the task's latest run
for that role, with the owner's message (or joined queued messages) as its prompt. The prompt is
headed so the agent knows it comes from a person and not from evidence: "## Message from the
owner".

- It goes through the scheduler like any run, so concurrency limits and run setup apply
  unchanged. Sending the message moves the task to queued with a compare-and-set from blocked,
  failed, review or awaiting_approval (invariant 6). Like Retry, this resets attempts, clears
  the blocked reason and cancels unfinished reviews of the old head (`cancelStaleReviews`). The
  owner's undelivered messages are the marker: when the runner starts a task that has any, it
  resumes the session with them as the prompt, marks them delivered, and does not build the
  usual task prompt. With none, it starts fresh as today. (My call.)
- "The session" is the one in the newest run for the task's role that recorded a `SessionID`. A
  run without one never got a session, so skipping it loses no context. If no run has one, the
  task cannot be continued (see below). (My call.)
- It runs on the task's branch, through the normal end of a run: outbox, deterministic commit and
  push, PR update, CI, review, risk, approval. A new push withdraws an existing approval, because
  merges are pinned to the approved commit (invariant 5). Nothing new may commit or push.
- Its worktree is prepared like any run's, with a new path. Checked on 2026-10-29: `claude -p
  --resume <id>` started in a different directory from the original session found it and kept
  its context. Add a test with the fake harness that the follow-up passes the previous run's
  `SessionID` as `ResumeSessionID`.
- **Builder must verify:** `codex exec resume` takes no `--sandbox` or `--approve-for-me` flags
  (see `internal/harness/codex/codex.go`). Confirm that a resumed Codex session keeps the
  workspace-write sandbox it started with. Until that is shown, Codex task agents are read-only,
  with the reason in place of the message box.
- A harness with no sessions (the generic `exec` harness), or a run with no recorded session ID,
  cannot be continued. The box says so and offers Retry.

### The Chat page

As in the mockup:

- **Sidebar.** "New conversation" (always the orchestrator) on top, then **Orchestrator**
  conversations, then a quieter **Agents** section: tasks grouped by title, each with its agent
  sessions and a status lamp. It lists tasks that are active, plus those that ended in the last
  7 days (my call; the builder may tune it), newest first.
- **Header.** Who you are talking to, harness and model, and for a task agent its task, branch
  and status. The Conversation / Terminal toggle sits on the right. The choice is remembered per
  browser (local storage).
- **Conversation view.** Owner messages are right-aligned sheets, as now. Agent prose is rendered
  Markdown. Each tool call is one row: a chevron, the tool name, its main argument (command, path
  or pattern) in mono, and for edits, `+n −m`. Clicking the row shows the full input and result,
  redacted as stored. Denials keep the red "DENIED" treatment from the log viewer. When an
  orchestrator turn creates a task, a chip under the call shows the task's title and status with
  "open agent" and "task" links. The chip is found by matching the created task to the turn's run
  (the builder picks how: a recorded creator run on the task, or the `created task <id>` result
  line).
- **Task agent view.** A yellow note above the box: "You're talking directly to this task's
  agent. For anything beyond this task, ask the orchestrator." with a link.
- **Working line.** While the agent works: a blinking lamp, "Working · 3m 41s · output 2s ago",
  and the run's link.
- **Terminal view.** Each entry on one line: a 64px time gutter, a marker (`›` for owner and
  prompt, `●` for prose and tool calls, `⎿` for results), then the text in mono. Results are
  clipped to a few lines with "show more", as the log viewer does now.
- **Routes.** `/chat` and `/chat/:id` stay for orchestrator conversations. Task agents use
  `/chat/task/:taskId`. Task detail and run detail gain an "Open conversation" link. `/runs/:id`
  keeps its log viewer for audit.
- **Phone width.** As today: the sidebar becomes a row of chips above the thread.

### Live updates

The transcript subscribes to `run.log` for the run that is live in it (and still uses
`chat.delta` for the orchestrator's streaming text), over the existing WebSocket. A new event
for queued-message changes (or the existing `chat.message`) refreshes the queued box for every
viewer. No new connection is added.

### API

New or changed routes. The builder settles exact paths and payloads, keeps `docs/api.md`,
`docs/openapi.yaml` and `web/src/types.ts` in step, and declares each route's action (read for
GETs, operate for sends):

- Read a task agent's transcript: its runs with their log lines and owner messages.
- Send a message to a task agent (queues, starts a follow-up or is refused with 409 and the
  reason).
- Edit and cancel a queued message (409 once a run has taken it).
- `POST /conversations/{id}/messages` queues while the orchestrator is replying instead of
  refusing.
- Log line payloads gain `tool_id`, and `kind` gains `prompt`.

## Safety model check

- Invariant 1 (agents judge, code acts): a follow-up's changes go through the same deterministic
  publish path. No agent text triggers a commit, push, merge or deploy.
- Invariant 4 (denial or stall is terminal): queued messages are not delivered after a run that
  did not succeed. Only a person's explicit send restarts the agent.
- Invariant 5 (pinned merges): a follow-up push withdraws the old approval.
- Invariant 6 (compare-and-set): a follow-up transitions only from the four allowed statuses.
- Invariant 8 (evidence labelled): the owner's message is headed as a message from the owner;
  it is instruction from a person, not evidence, so it is not wrapped in `evidenceNote`.
- Invariant 12 (routes declare their action): sending needs operate, reading needs read.

The change touches `internal/api/routes.go` and probably `internal/engine/helpers.go`, both
high risk under the built-in rules, so the owner approves the PR.

## Left to the builder

- Storage details: the migrations for `tool_id` and the `queued` flag. Settled already (my call):
  task agent messages are `ChatMessage`s on a `Conversation` row with a new `task_id` column,
  created on the first message to that task's agent. The Agents sidebar lists tasks, not these
  rows, so a task appears there before anyone has messaged it.
- Exact route paths, payloads and the event used to refresh queued messages.
- How the orchestrator transcript finds tasks a turn created.
- Parsing each tool's input for its row (main argument, edit line counts), with a fallback to
  the raw text when the input was truncated.
- Component structure, provided one transcript component serves both kinds of conversation and
  both views, and the log viewer's line styles are reused rather than duplicated.
- The sidebar's time window and ordering.

## Rollout

One feature, but it splits cleanly into ordered pieces: (1) store the prompt and tool IDs in run
logs; (2) the shared transcript component and the Chat page for orchestrator conversations,
with the Terminal toggle; (3) queued messages and Stop; (4) task agent conversations, read-only;
(5) task agent follow-ups, with Codex task agents read-only until the Codex sandbox check above
has passed. Docs
(`docs/ui.md`, `docs/api.md`, `docs/design.md`, the README's chat section) change with the piece
that changes the behaviour they describe.
