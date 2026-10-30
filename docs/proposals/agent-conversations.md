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
all agents." And: "Until the orchestrator or an individual agent decides the agent is done, you
should be able to interact/steer, etc."

## Goals

- One chat experience for every agent: the orchestrator and each task's agent.
- A transcript that reads like Claude Code: the agent's prose, each tool call as a one-line row
  that expands to its output, edits with their added and removed line counts, live as it works.
- A **Conversation / Terminal** toggle on every transcript. Terminal is the same content as
  dense, timestamped monospace lines. It is a rendering, not a shell.
- Chat opens on the orchestrator and new conversations go to it. Task agents are reachable but
  sit lower: a quieter "Agents" section of the sidebar, and links from tasks, runs and the
  orchestrator's own transcript.
- You can type while an agent works, and, where its CLI supports it, the agent reads your
  message after its current step, as in Claude Code (see **Messages during a run**).
- An agent's conversation stays open until the agent, the orchestrator or the owner decides that
  agent is done (see **When a conversation closes**). Task status does not close it: you can
  reply to a triager's answer, or ask an implementer for more after its PR merged.

## Non-goals

- **Onett does not become an agent harness.** Onett keeps running agents in their CLI's
  non-interactive mode. Messages during a run use the CLI's own input stream
  (`claude -p --input-format stream-json`); continuing after a run uses the CLI's own resume
  (`claude --resume`, `codex exec resume`). Onett adds no protocol of its own and never decides
  when an agent should look at input.
- No pseudo-terminal, no rendering of an agent's own TUI, no interactive shell on a worktree, no
  xterm.js. The Terminal view is styled HTML.
- Agents reporting back to the owner without being asked (task finished, blocked, has a
  question). The owner wants all such messages to go through the orchestrator; that is a factory
  behaviour change and gets its own design later.
- Changing Retry. It keeps its current meaning: a fresh agent with a note.
- New dependencies.

## Anti-goals

These would make the work a failure even if it technically ran:

- Two input modes ("steer" versus "message"). There is one message box. When the message
  reaches the agent depends only on what the agent's CLI can do and whether it is running.
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
  its newest worktree. A task that needed no code change goes straight to done.
- **Agent reports.** An agent ends a run by writing `result.json` (`domain.RunResult`): a status
  (`completed`, `no_changes`, `blocked`, `failed`), a summary, risk, PR text, a review verdict,
  follow-up task suggestions and friction.
- **Run logs.** Every run's output is stored as `LogLine`s with a kind (`text`, `tool_use`,
  `tool_result`, `denial`, `system`, `stderr`, `error`). A `tool_use` line's text is
  `Name: <compact JSON input>`, truncated. Results follow their calls in order, but no ID links a
  result to its call. The prompt a run was started with is not stored.
- **Live updates.** One WebSocket per browser tab (`/api/v1/ws`). `run.log` and `chat.delta`
  reach a tab only for the runs and conversations it has subscribed to.
- **Cancel.** `CancelRun` stops a live run and blocks its task with "run cancelled by owner".

Checked on 2026-10-29 against the installed Claude Code CLI:

- `claude -p --input-format stream-json --output-format stream-json --verbose` accepts further
  user messages on stdin while it works. A message sent while a tool call was running was read
  after that call, inside the same turn (the single reply addressed both messages).
- In that mode the process does not exit after its reply (`result` line); it waits until stdin
  is closed. In the test the reply came at 11s and the process exited at 26s, when stdin closed.
- `claude -p --resume <id>` started in a different directory from the original session found the
  session and kept its context.

## Design

### One transcript model

A transcript is an ordered list of entries: owner messages, the prompt that started a run, and
the run's log lines. Both kinds of conversation produce it:

- **Orchestrator conversation:** the conversation's `ChatMessage`s, with each turn's run log
  placed after the owner message that started it (`ChatMessage.RunID` links them). The stored
  assistant reply stays the source of the final text; tool rows come from the run log.
- **Task agent conversation:** the runs of one agent session lineage, oldest first: the task's
  runs for its own role (`Run.Role == Task.Role`), followed by the runs of any conversation
  follow-up tasks (see **After a merge**). Each run begins with its prompt. Owner messages sent
  during a run appear where the agent received them. When a run starts a new session instead of
  resuming one (the first run after a Retry), a divider says "New session" and why ("retried by
  owner").

  A review task is its own task with its own reviewer agent, so it is its own conversation.

Two small additions to stored logs make this possible:

- A new `LogKind` `prompt`: the run's prompt, written as the run's first log line. This is
  the task prompt or the owner's message that started the run. It does not include the role
  instructions (`Spec.Instructions`). Runs from before this change have no `prompt` line. Their
  transcript starts with the task body, labelled "Task (prompt not recorded)". (My call.)
- A new `LogKind` `owner`: an owner message delivered to the agent during a run, logged when
  onett writes it to the agent's input.
- An optional tool-call ID on `LogLine` (`tool_id`, empty when the harness gives none), so a
  result attaches to its call even when an agent makes several calls at once. The Claude parser
  has the IDs (`tool_use.id`, `tool_result.tool_use_id`); the Codex parser sets it where its
  JSON has one. With no ID, the UI pairs a result with the nearest call before it that has no
  result yet. This needs a schema migration (append-only, per `AGENTS.md`).

### Sending a message

There is one message box. What a message does depends only on the target's state:

| Target | State | What happens |
|---|---|---|
| Any agent | its run is live and its harness accepts input | Delivered now; the agent reads it after its current step. |
| Any agent | its run is live and its harness does not accept input (Codex, `exec`) | Queued; it continues the session when the run ends. |
| Orchestrator | idle | A turn starts at once, as today. |
| Task agent | not running, conversation open | A follow-up run is queued for the scheduler (see **Follow-ups**). |
| Task agent | task queued and never run | Queued; it is added to the first run's prompt after the task prompt. |
| Task agent | conversation closed | Read-only, with the reason ("Closed by the orchestrator: merged and deployed") and Reopen (see **When a conversation closes**). |

**Queued messages.** A message that cannot be delivered yet is stored at once and shown at the
end of the transcript in a dashed box: "Queued · sends when the agent finishes this run", with
Edit and Cancel. Several queued messages are joined, in order, into the next prompt. They are
stored as `ChatMessage`s with `queued: true` until a run takes them, so they survive a reload and
every viewer sees them. Edit and Cancel work only while the message is still queued.

When a run ends, queued messages continue the session **only if the run ended normally**
(succeeded). If it ended denied, stalled, timed out, failed or cancelled, the task is blocked
exactly as today, and the queued messages stay queued under a "Send now" button. The owner must
act; onett never restarts an agent on its own after a denial or stall. A new message sent then
joins the queue and the whole queue goes as one prompt, the same as pressing "Send now". This
holds for the orchestrator too, where a stopped turn blocks nothing.

**Stop.** Esc in the box, or a Stop button while the agent works, cancels the current run
(`CancelRun`), as Claude Code's Esc interrupts a turn. For a task agent the task is blocked as
today, and the next message starts a follow-up.

**Who can send.** Operators and owners. Viewers read transcripts and see the existing
"Viewers can read conversations but not send messages." line.

### Messages during a run

The harness interface gains an optional capability, so harnesses that cannot take input are
unchanged:

```go
// Steerable is a Session that accepts more user input while it runs.
type Steerable interface {
	// Send writes one user message to the running agent. It returns ErrInputClosed once the
	// session has stopped taking input; the caller then queues the message instead.
	Send(text string) error
}
```

- The Claude harness starts every session with `--input-format stream-json` and writes the
  prompt to stdin as the first message instead of passing it as an argument. Its session
  implements `Steerable`.
- When the agent's turn ends (its `result` line), the harness closes stdin unless a message
  arrived in the meantime, and the process exits as it does today. A `Send` after stdin closed
  returns `ErrInputClosed`, and the engine queues the message for a follow-up. The supervisor's
  stall and timeout rules are unchanged: a message waiting to be read is not output.
- The orchestrator uses the same harness, so the same applies to its turns.
- Codex (`codex exec`) and `exec` sessions do not implement `Steerable`. Their messages queue.

This is the CLI's own documented input mode, not onett's protocol: onett writes a user message
and the CLI decides when the agent reads it. Two details are still open; see **Builder must
verify**.

### Follow-ups

A follow-up is a new run that resumes the session of the newest run in the conversation that
recorded a `SessionID`, with the owner's queued messages as its prompt, headed "## Message from
the owner" so the agent knows it comes from a person and not from evidence. A run without a
session ID never got a session, so skipping it loses no context; if no run has one, the
conversation cannot be continued and the box offers Retry instead. (My call.)

How the follow-up runs depends on where the task is:

- **Blocked, failed, in review or awaiting approval:** the task moves to queued with a
  compare-and-set from those statuses (invariant 6) and goes through the scheduler like any run,
  so concurrency limits and run setup apply unchanged. Like Retry, this resets attempts, clears
  the blocked reason and cancels unfinished reviews of the old head (`cancelStaleReviews`). The
  same transition sets an explicit marker on the task, `follow_up_pending`. When the runner
  starts a task with that marker, it clears the marker, resumes the session with the owner's
  messages as the prompt (plus any merge-conflict section the run needs), marks the messages
  delivered, and does not build the usual task prompt. Queued messages alone are not the marker:
  onett's automatic retry after a failed run builds the normal prompt with its attempt notes and
  leaves the queue alone, so a failed run never delivers the owner's messages by itself
  (invariant 4). The run ends through the normal pipeline: outbox, deterministic commit and
  push, PR update, CI, review, risk, approval. A new push withdraws an existing approval, because
  merges are pinned to the approved commit (invariant 5). (My call.)
- **After a merge** (merged, deploying, done with a merged PR): the merged task is not reopened.
  Onett creates a child task (`ParentID` = the merged task, `Source: "conversation"`, same
  project, kind and role, created with `follow_up_pending` set) whose first run resumes the
  same session on a new branch from the
  default branch and opens its own PR. The conversation carries on across both tasks. The
  existing `"followup"` source stays for agent-suggested follow-ups, which do not resume a
  session. (My call.)
- **Done without a PR** (a question answered, triage done): the task moves back to queued, as in
  the first case. With no code change it ends as done again; if the follow-up writes code, it
  goes through the PR pipeline like any task.
- **Cancelled:** cancelling a task closes its conversation (below), so there is no follow-up.

Codex: `codex exec resume` takes no `--sandbox` or `--approve-for-me` flags (see
`internal/harness/codex/codex.go`). Checked on 2026-10-29 with codex-cli 0.166.0: a resumed
session started with neither flag runs **read-only**. It could not write inside its own working
directory, and outside it was blocked too. With `-c sandbox_mode="workspace-write"` on the
resume command, it could write inside its working directory and still could not write outside
it. So the Codex harness passes that override on every resume unless the config already picks
a sandbox, and Codex task agents can be continued like Claude ones. This also fixes resumed
orchestrator turns on Codex, which today silently run read-only.

### When a conversation closes

A task agent's conversation is open from its first run until one of these closes it:

- **The agent decides.** `RunResult` gains `finished` (bool). The agent sets it when nothing more
  can come of this conversation. Role prompts say when: a triager that answered fully with no
  open questions sets it; an implementer does not, because its work still goes through review
  and merge.
- **The orchestrator decides.** A new operate route and CLI command closes a task agent's
  conversation with a reason. The orchestrator prompt says to use it when the owner says the
  work is finished, or when a task is merged and deployed healthy and the owner has nothing
  open on it.
- **The owner decides.** A Close button in the conversation header, or cancelling the task.

A closed conversation is read-only and shows who closed it and why. The owner (not an agent) can
Reopen it, which only clears the closed mark. (Reopen is my call: an escape hatch so an agent's
own `finished` cannot lock the owner out.) Closing never changes the task's status, and task
status never closes a conversation, except Cancel.

The orchestrator's own conversations do not close; they work as today.

The Agents sidebar shows open conversations first, then closed ones from the last 7 days (my
call; the builder may tune it).

### The Chat page

As in the mockup:

- **Sidebar.** "New conversation" (always the orchestrator) on top, then **Orchestrator**
  conversations, then a quieter **Agents** section: tasks grouped by title, each with its agent
  session and a status lamp.
- **Header.** Who you are talking to, harness and model, and for a task agent its task, branch,
  status and Close. The Conversation / Terminal toggle sits on the right. The choice is
  remembered per browser (local storage).
- **Conversation view.** Owner messages are right-aligned sheets, as now. Agent prose is rendered
  Markdown. Each tool call is one row: a chevron, the tool name, its main argument (command, path
  or pattern) in mono, and for edits, `+n −m`. Clicking the row shows the full input and result,
  redacted as stored. Denials keep the red "DENIED" treatment from the log viewer. When an
  orchestrator turn creates a task, a chip under the call shows the task's title and status with
  "open agent" and "task" links. The chip is found by matching the created task to the turn's run
  (the builder picks how, for example a recorded creator run on the task).
- **Task agent view.** A yellow note above the box: "You're talking directly to this task's
  agent. For anything beyond this task, ask the orchestrator." with a link.
- **Working line.** While the agent works: a blinking lamp, "Working · 3m 41s · output 2s ago",
  and the run's link. A message delivered mid-run shows as a normal owner message with a small
  "read after current step" mark until the agent's next output; a queued one shows the dashed
  box.
- **Terminal view.** Each entry on one line: a 64px time gutter, a marker (`›` for owner and
  prompt, `●` for prose and tool calls, `⎿` for results), then the text in mono. Results are
  clipped to a few lines with "show more", as the log viewer does now.
- **Routes.** `/chat` and `/chat/:id` stay for orchestrator conversations. Task agents use
  `/chat/task/:taskId` (a conversation follow-up task opens its root task's conversation). Task
  detail and run detail gain an "Open conversation" link. `/runs/:id` keeps its log viewer for
  audit.
- **Phone width.** As today: the sidebar becomes a row of chips above the thread.

### Live updates

The transcript subscribes to `run.log` for the run that is live in it (and still uses
`chat.delta` for the orchestrator's streaming text), over the existing WebSocket. A new event
for message and conversation changes (queued, delivered, closed, reopened), or the existing
`chat.message`, refreshes every viewer. No new connection is added.

### API

New or changed routes. The builder settles exact paths and payloads, keeps `docs/api.md`,
`docs/openapi.yaml` and `web/src/types.ts` in step, and declares each route's action (read for
GETs, operate for everything else):

- Read a task agent's conversation: its runs with their log lines, owner messages, and its open
  or closed state.
- Send a message to a task agent (delivers, queues, starts a follow-up, or 409 with the reason
  when closed).
- Edit and cancel a queued message (409 once a run has taken it).
- Close (with a reason) and reopen a task agent's conversation. Reopen is refused to the API
  token, which the orchestrator holds, so only a signed-in person reopens.
- `POST /conversations/{id}/messages` delivers or queues while the orchestrator is replying
  instead of refusing.
- Log line payloads gain `tool_id`, and `kind` gains `prompt` and `owner`. `RunResult` gains
  `finished`.

## Safety model check

- Invariant 1 (agents judge, code acts): a follow-up's changes go through the same deterministic
  publish path. An agent's `finished` and the orchestrator's close only mark a conversation; they
  commit, push, merge and deploy nothing.
- Invariant 3 (no credentials): the stream-json input carries only the prompt and owner
  messages. The process environment is unchanged (`sysproc.AgentEnv`).
- Invariant 4 (denial or stall is terminal): queued messages are not delivered after a run that
  did not succeed, and a denial still ends the run even if a message is waiting. Only a person's
  explicit send restarts the agent.
- Invariant 5 (pinned merges): a follow-up push withdraws the old approval. A post-merge
  follow-up opens a new PR that needs its own approval.
- Invariant 6 (compare-and-set): a follow-up requeues a task only from the listed statuses.
- Invariant 8 (evidence labelled): the owner's message is headed as a message from the owner;
  it is instruction from a person, not evidence, so it is not wrapped in `evidenceNote`.
- Invariant 12 (routes declare their action): sending and closing need operate; reopening needs
  a signed-in person.

The change touches `internal/engine/prompts/**`, `internal/api/routes.go` and probably
`internal/engine/helpers.go`, all high risk under the built-in rules, so the owner approves the
PR.

## Checked against the real CLIs

Everything this section once listed for the builder to verify is settled. Codex is under
**Follow-ups**. Claude was settled on 2026-10-29 against the installed Claude Code CLI: the input message shape is one JSON
line per message, `{"type":"user","message":{"role":"user","content":"<text>"}}`. A message
written after a `result` line starts another turn in the same process, which emits another
`result` line (`result_index` counts them; `total_cost_usd` is cumulative; `usage` is per turn).
A message written immediately before stdin closes is still read and answered before the process
exits. So the harness closes stdin on every `result` line, a `Send` before the close is always
answered, and a `Send` after it returns `ErrInputClosed`.

## Left to the builder

- Storage details: the migrations for `tool_id`, the `queued` flag and the closed state.
  Settled already (my call): task agent messages are `ChatMessage`s on a `Conversation` row with
  a new `task_id` column (the root task of the session lineage), created on the first message.
  The Agents sidebar lists tasks, not these rows, so a task appears there before anyone has
  messaged it.
- Exact route paths, payloads, CLI command name and the event used to refresh conversations.
- How the orchestrator transcript finds tasks a turn created.
- Parsing each tool's input for its row (main argument, edit line counts), with a fallback to
  the raw text when the input was truncated.
- Component structure, provided one transcript component serves both kinds of conversation and
  both views, and the log viewer's line styles are reused rather than duplicated.
- Role prompt wording for `finished` and for the orchestrator's close.

## Rollout

One feature in ordered pieces: (1) store the prompt, owner and tool IDs in run logs; (2) the
shared transcript component and the Chat page for orchestrator conversations, with the Terminal
toggle; (3) the Claude harness on stream-json input with `Steerable`, used first by the
orchestrator, plus queued messages and Stop; (4) task agent conversations, read-only; (5) task
agent messages and follow-ups, including after a merge; (6) closing and reopening, `finished`,
and the prompt changes; and, early, the Codex resume sandbox override. Docs (`docs/ui.md`, `docs/api.md`, `docs/design.md`, the README's chat section)
change with the piece that changes the behaviour they describe.
