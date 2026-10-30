# Agent Conversations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Chat works like Claude Code for every agent. The orchestrator comes first, and any task
agent can be messaged, mid-run or after its run, until the agent, the orchestrator or the owner
closes its conversation.

**Architecture:** Run logs gain the run's prompt, owner messages and tool-call IDs, so one
transcript model covers every agent. The Claude harness switches to stream-json input and
exposes an optional `harness.Steerable`, which lets the engine write an owner's message into a
live run. Messages that cannot be delivered are stored as queued `ChatMessage`s. A follow-up run
picks them up and resumes the agent's session: it requeues the task, or creates a
`Source: "conversation"` child task after a merge. The web gets one `Transcript` component with
a Conversation/Terminal view, used by orchestrator and task-agent threads on the Chat page.

**Tech Stack:** Go 1.27, chi, SQLite (`internal/store/sqlite`), React + TypeScript +
TanStack Query, Vitest.

**Spec:** `docs/proposals/agent-conversations.md` (and the mockup next to it). Read both before
any task.

## Global Constraints

- Run Go through `mise exec --`. Before reporting a task done: `mise exec -- go vet ./...`,
  `test -z "$(mise exec -- gofmt -l internal cmd)"`, `mise exec -- go test -race -count=1 ./...`;
  for any `web/` change also `cd web && npm ci && npm run typecheck && npm test && npm run build`.
- No new dependencies (`go.mod`, `package.json` unchanged).
- Schema changes append to `migrations` in `internal/store/sqlite/migrations.go`; never edit an
  existing entry. This plan adds exactly two: entry 3 (Task 1) and entry 4 (Task 4).
- New Go tests go in new files (`*_conv_test.go` names given per task). Tests use `t.TempDir()`,
  the fake harness, stub scripts and `httptest`; no network, no fixed ports, nothing under `~`.
- Every new route declares its action with `s.read` / `s.operate`; reopen additionally rejects
  the API token with `userPrincipal`. Each route is added to `docs/openapi.yaml` and
  `docs/api.md` in the same task (`TestOpenAPISpecMatchesRoutes` enforces the first).
- Strict input stays: request structs are decoded with `readJSON` (unknown fields are errors).
- Owner text sent to an agent is headed exactly `## Message from the owner` and is not wrapped in
  `evidenceNote`.
- Only deterministic Go code commits, pushes, opens PRs or merges. Nothing in this plan adds a
  path from agent output to any of those.
- A run that ended denied, stalled, timed out, failed or cancelled never triggers delivery of
  queued messages. Only an explicit owner action (send, flush) does.
- Copy, verbatim: queued box `Queued · sends when the agent finishes this run`; task-agent hint
  `You're talking directly to this task's agent. For anything beyond this task, ask the
  orchestrator.`; old-run placeholder label `Task (prompt not recorded)`; the run timer shows
  `Working · 3m 41s · output 2s ago`.
- Each task updates the docs its behaviour changes (`docs/ui.md`, `docs/api.md`,
  `docs/openapi.yaml`, `docs/design.md`, README) in the same commit.

## Review Focus

1. **A message sent at the instant a run ends** (between the agent's last `result` line and its
   stdin closing, or just after the run is unregistered). The owner expects it to be either
   answered in that run or queued for the next one, never lost. Test:
   `TestSendRacingRunEndIsQueuedOrDelivered` (Task 3).
2. **A run that is denied while a message is queued.** The task blocks and the message stays
   queued; no follow-up starts until the owner presses Send now. Test:
   `TestQueuedMessageNotDeliveredAfterDenial` (Task 7).
3. **Editing or cancelling a queued message just as a run picks it up**, for example from a
   second tab. The edit is refused with 409 and the transcript shows the message as delivered.
   Test: `TestEditQueuedMessageAfterDeliveryConflicts` (Task 5).
4. **Cancelling a task that has queued messages.** The conversation closes, and the queued
   messages stay visible but are never delivered. Test:
   `TestCancelTaskClosesConversationAndKeepsQueue` (Task 8).
5. **Runs from before this change** (no `prompt` line, no tool IDs), and Codex runs (never any
   tool IDs). The transcript still renders, pairing results by order, and starts with the task
   body under `Task (prompt not recorded)`. Tests: `pairs results by order when tool_id is
   missing` and `uses the task body when no prompt line exists` (Task 9).

---

### Task 1: Prompt, owner and tool-ID log lines

**Files:**
- Modify: `internal/domain/domain.go` (LogKind consts, `LogLine`)
- Modify: `internal/harness/harness.go` (`Event`)
- Modify: `internal/harness/claude/parse.go` (`block`, `assistantEvents`, `toolResultEvents`)
- Modify: `internal/store/sqlite/migrations.go` (append entry 3), `internal/store/sqlite/runs.go`
  (`AppendRunLogs`, `ListRunLogs`)
- Modify: `internal/engine/supervise.go` (`flush` copies `ToolID`), `internal/engine/runner.go`,
  `internal/engine/chat.go` (log the prompt)
- Modify: `web/src/types.ts` (`LogKind`, `LogLine`), `web/src/components/LogViewer.tsx`
  (render `prompt`, `owner`)
- Modify: `docs/api.md`, `docs/openapi.yaml` (LogLine schema)
- Test: `internal/store/sqlite/runlogs_conv_test.go`, `internal/harness/claude/parse_conv_test.go`,
  `internal/engine/prompt_log_conv_test.go`, `internal/harness/claude/testdata/tool_ids.jsonl`

**Interfaces:**
- Produces: `domain.LogPrompt LogKind = "prompt"`, `domain.LogOwner LogKind = "owner"`;
  `LogLine.ToolID string \`json:"tool_id,omitempty"\``; `harness.Event.ToolID string`;
  `func (e *Engine) appendRunLog(ctx context.Context, runID string, kind domain.LogKind, text string)`
  (stores one line and emits `EvRunLog`, as `flush` does); web `LogKind` gains `'prompt' | 'owner'`,
  `LogLine.tool_id?: string`.

- [ ] **Step 1: Write the failing tests**
  - `TestRunLogToolIDRoundTrip`: `AppendRunLogs` with `{Kind: tool_use, ToolID: "toolu_1"}` then
    `ListRunLogs` returns `ToolID == "toolu_1"`; a line without one returns `""`.
  - `TestParseToolIDs`: replaying `testdata/tool_ids.jsonl` (an assistant `tool_use` block with
    `"id":"toolu_A"` and a user `tool_result` with `"tool_use_id":"toolu_A"`) yields a
    `tool_use` event and a `tool_result` event, both with `ToolID == "toolu_A"`.
  - `TestRunLogsStartWithPrompt`: a task run with the fake harness stores, as its first log line,
    `{Kind: "prompt", Text: <the exact prompt passed in harness.Spec.Prompt>}`; a chat turn
    stores the owner's message the same way.
- [ ] **Step 2: Run them and see them fail**
  Run: `mise exec -- go test -race -count=1 -run 'TestRunLogToolIDRoundTrip|TestParseToolIDs|TestRunLogsStartWithPrompt' ./internal/...`
  Expected: compile errors (`ToolID`, `LogPrompt` undefined).
- [ ] **Step 3: Implement**
  - Migration entry 3: `ALTER TABLE run_logs ADD COLUMN tool_id TEXT NOT NULL DEFAULT '';`.
    `AppendRunLogs` inserts and `ListRunLogs` selects `tool_id`.
  - `block` gains `ID string \`json:"id"\``. `assistantEvents` sets `ToolID: b.ID` on
    `tool_use`; `toolResultEvents` sets `ToolID: b.ToolUseID`.
  - `appendRunLog` lives in `supervise.go` next to `flush` and shares its store-and-emit code.
    `runTask` calls it with `LogPrompt` right after `h.Start` succeeds, and `chatTurn` does the
    same.
  - `LogViewer` renders `prompt` as a muted block labelled "Prompt" and `owner` as a line
    labelled "You".
- [ ] **Step 4: Run the tests and the full Go suite and web checks; all pass**
- [ ] **Step 5: Commit** `Record run prompts, owner messages and tool-call IDs in run logs`

### Task 2: Claude stream-json input and `Steerable`

**Files:**
- Modify: `internal/harness/harness.go`, `internal/harness/proc.go`
- Modify: `internal/harness/claude/claude.go`, `internal/harness/claude/parse.go`
- Modify: `internal/harness/fake/fake.go`
- Test: `internal/harness/proc_stdin_conv_test.go`, `internal/harness/claude/steer_conv_test.go`,
  `internal/harness/claude/testdata/two_results.jsonl`, `internal/harness/claude/testdata/echo_turns.sh`,
  `internal/harness/fake/steer_conv_test.go`

**Interfaces:**
- Produces:
  ```go
  // harness.go
  var ErrInputClosed = errors.New("harness: session input is closed")
  // Steerable is a Session that accepts more user input while it runs.
  type Steerable interface {
  	// Send writes one user message to the running agent. It returns ErrInputClosed once the
  	// session has stopped taking input; the caller then queues the message instead.
  	Send(text string) error
  }
  // proc.go
  ProcSpec.KeepStdinOpen bool // Stdin is written first; stdin stays open until CloseStdin
  func (p *Proc) WriteStdin(b []byte) error // ErrInputClosed after CloseStdin
  func (p *Proc) CloseStdin()               // idempotent
  // fake.go
  type SteerScript func(ctx context.Context, spec harness.Spec, inbox <-chan string, emit func(harness.Event)) (harness.Result, error)
  func NewSteerable(s SteerScript) harness.Harness // its sessions implement harness.Steerable
  ```
- Consumes: nothing from earlier tasks.

- [ ] **Step 1: Write the failing tests**
  - `TestProcKeepStdinOpen`: run `/bin/sh -c 'while read l; do echo "$l"; done'` with
    `KeepStdinOpen`, `Stdin: "a\n"`. `WriteStdin([]byte("b\n"))` succeeds and lines `a`, `b`
    come back. After `CloseStdin()`, the process exits and `WriteStdin` returns
    `ErrInputClosed`. Calling `CloseStdin()` twice does not panic.
  - `TestParseTwoResults`: `two_results.jsonl` has two `result` lines (`usage.input_tokens` 10
    then 20, `total_cost_usd` 0.01 then 0.03, `result` "ONE" then "TWO"). Expected:
    `FinalText == "TWO"`, `CostUSD == 0.03`, `InputTokens == 30` (per-turn usage summed,
    including cache fields as today), `IsError` taken from the last line.
  - `TestClaudeSendDuringRun`: `cfg.Command` is `echo_turns.sh`, a stub that reads stdin
    lines and prints one `result` line per line it reads, with `"result"` set to that line's
    `message.content`. The session implements `harness.Steerable`; the prompt goes out as
    the first stdin line in the shape
    `{"type":"user","message":{"role":"user","content":"<prompt>"}}`; `buildArgs` includes
    `--input-format stream-json`; after the first `result` the harness closes stdin, the process
    exits, and `Wait()` returns. A `Send` after exit returns `ErrInputClosed`.
  - `TestFakeSteerableDeliversToInbox`: a `SteerScript` that reads one message from `inbox`
    and emits it as a text event; `Send("hi")` produces that event.
- [ ] **Step 2: Run them and see them fail** (`go test -run 'TestProc|TestParseTwoResults|TestClaudeSend|TestFakeSteerable' ./internal/harness/...`)
- [ ] **Step 3: Implement**
  - `proc.go`: with `KeepStdinOpen`, use `cmd.StdinPipe()` and write `Stdin` before
    returning. A mutex guards the writer and a closed flag. `Cancel` and process exit also mark
    stdin closed.
  - `claude.go`: `buildArgs` adds `--input-format stream-json` after `--output-format
    stream-json`. `Start` encodes the prompt as an input line (`json.Marshal`, never string
    concatenation) and sets `KeepStdinOpen`. It returns a `*session` that embeds `*harness.Proc`
    and implements `Send`. The parser gets an `onResult func()` hook, which the session sets to
    `proc.CloseStdin`, so stdin closes on every `result` line (spec, **Builder must verify**
    paragraph: a `Send` just before the close is still answered).
  - `parse.go`: accumulate per-result `usage` into the totals. Take `total_cost_usd`,
    `result` and `is_error` from the last result line. Denials keep appending.
  - Update `TestBuildArgsDefaults` and the other existing `buildArgs` expectations for the new
    flag.
- [ ] **Step 4: Run all harness tests and the full Go suite; all pass**
- [ ] **Step 5: Commit** `Run Claude with stream-json input and let callers send mid-run`

### Task 3: Deliver owner messages into a live run

**Files:**
- Modify: `internal/engine/engine.go` (`activeRun`), `internal/engine/runner.go`,
  `internal/engine/chat.go` (record the session on the active run)
- Create: `internal/engine/steer.go`
- Test: `internal/engine/steer_conv_test.go`

**Interfaces:**
- Consumes: `harness.Steerable`, `harness.ErrInputClosed` (Task 2); `appendRunLog`,
  `domain.LogOwner` (Task 1); `fake.NewSteerable` (Task 2).
- Produces:
  ```go
  var errRunNotLive = errors.New("run is not live")
  func (ar *activeRun) setSession(s harness.Session)      // called right after h.Start
  // deliverToRun sends ownerPrompt([]string{text}) to run runID's agent and logs the raw text
  // as an owner line. It returns
  // errRunNotLive when the run is not registered and harness.ErrInputClosed when the session
  // is not Steerable or has closed its input.
  func (e *Engine) deliverToRun(ctx context.Context, runID, text string) error
  func ownerPrompt(texts []string) string // "## Message from the owner\n\n" + texts joined by "\n\n"
  ```

- [ ] **Step 1: Write the failing tests**
  - `TestDeliverToLiveSteerableRun`: a `fake.NewSteerable` script waits on `inbox`.
    `deliverToRun` returns nil, the script receives `ownerPrompt([]string{"hi"})`, and the run
    log has a `kind: "owner"` line with text `hi`.
  - `TestDeliverToNonSteerableRun`: a `fake.New` session gives `harness.ErrInputClosed` and
    logs nothing.
  - `TestSendRacingRunEndIsQueuedOrDelivered`: call `deliverToRun` in a loop while the run
    finishes. Every call either returns nil and the message shows up in the script's inbox, or
    returns `errRunNotLive` / `ErrInputClosed`. No call returns nil without delivering.
- [ ] **Step 2: Run them and see them fail**
- [ ] **Step 3: Implement** `steer.go`. `activeRun` gets `sess atomic.Pointer[harness.Session]`
  or a mutex-guarded field, your choice. Look the run up under the same lock `registerRun`
  uses. `runTask` and `chatTurn` call `ar.setSession(sess)` right after `h.Start` succeeds.
- [ ] **Step 4: Run the engine tests and the full Go suite; all pass**
- [ ] **Step 5: Commit** `Deliver owner messages into live runs that accept input`

### Task 4: Conversation and message model for task agents

**Files:**
- Modify: `internal/domain/domain.go` (`Conversation`, `ChatMessage`, `Task`, `RunResult` in
  `outbox.go`, new types), `internal/store/store.go`, `internal/store/sqlite/chat.go`,
  `internal/store/sqlite/migrations.go` (append entry 4)
- Modify: `web/src/types.ts` (mirror every new field and type)
- Test: `internal/store/sqlite/chat_conv_test.go`

**Interfaces:**
- Produces (Go; JSON tags are the wire format):
  ```go
  // Conversation, added fields
  TaskID       string     `json:"task_id,omitempty"`       // root task of a task agent conversation; "" = orchestrator
  ActiveRunID  string     `json:"active_run_id,omitempty"` // orchestrator turn in flight
  ClosedAt     *time.Time `json:"closed_at,omitempty"`
  ClosedBy     string     `json:"closed_by,omitempty"`     // "agent", "orchestrator" or "owner:<username>"
  ClosedReason string     `json:"closed_reason,omitempty"`
  // ChatMessage, added field
  Queued bool `json:"queued,omitempty"`
  // Task, added field
  CreatedByRun string `json:"created_by_run,omitempty"`
  // RunResult, added field
  Finished bool `json:"finished,omitempty"`
  const SourceConversation = "conversation" // Task.Source of a post-merge follow-up
  type TaskConversation struct {
  	Task         Task          `json:"task"`          // the root task
  	Conversation *Conversation `json:"conversation,omitempty"`
  	Tasks        []Task        `json:"tasks"`         // root then conversation follow-ups, oldest first
  	Runs         []Run         `json:"runs"`          // the agent's runs across Tasks, oldest first
  	Messages     []ChatMessage `json:"messages"`
  	LiveRunID    string        `json:"live_run_id,omitempty"`
  	CanSend      bool          `json:"can_send"`
  	Reason       string        `json:"reason,omitempty"` // why CanSend is false
  }
  type AgentConversation struct {
  	TaskID     string     `json:"task_id"`
  	Title      string     `json:"title"`
  	Project    string     `json:"project"`
  	Role       string     `json:"role"`
  	TaskStatus TaskStatus `json:"task_status"`
  	Live       bool       `json:"live"`
  	Closed     bool       `json:"closed"`
  	UpdatedAt  time.Time  `json:"updated_at"`
  }
  // store.Store additions
  GetConversationByTask(ctx context.Context, taskID string) (domain.Conversation, error) // store.ErrNotFound if none
  ListTaskConversations(ctx context.Context, limit int) ([]domain.Conversation, error)
  UpdateChatMessage(ctx context.Context, id string, fn func(*domain.ChatMessage) error) (domain.ChatMessage, error)
  DeleteChatMessage(ctx context.Context, id string) error
  ```
  `ListConversations` now returns only orchestrator conversations (`task_id = ''`).

- [ ] **Step 1: Write the failing tests**
  - `TestConversationByTask`: create a conversation with `TaskID: "t1"`. `GetConversationByTask("t1")`
    finds it, `GetConversationByTask("t2")` returns `store.ErrNotFound`, `ListConversations`
    leaves it out, and `ListTaskConversations` includes it.
  - `TestUpdateAndDeleteChatMessage`: append a message with `Queued: true`, update it to
    `Queued: false, RunID: "r1"`, read it back, delete it, and confirm `ListChatMessages` no
    longer returns it. Deleting an unknown ID returns `store.ErrNotFound`.
- [ ] **Step 2: Run and see them fail**
- [ ] **Step 3: Implement.** Migration entry 4:
  `ALTER TABLE conversations ADD COLUMN task_id TEXT NOT NULL DEFAULT ''; CREATE INDEX
  conversations_task_id ON conversations (task_id);`. The conversations entity writes `task_id`
  from the struct. Mirror the types in `web/src/types.ts`.
- [ ] **Step 4: Full Go suite and web checks pass**
- [ ] **Step 5: Commit** `Add the conversation model for task agents and queued messages`

### Task 5: Orchestrator conversations: mid-run delivery, queue and Stop

**Files:**
- Modify: `internal/engine/controller.go` (`SendChat`, new methods), `internal/engine/chat.go`
- Modify: `internal/api/controller.go`, `internal/api/handlers.go`, `internal/api/routes.go`
- Modify: `docs/api.md`, `docs/openapi.yaml`
- Test: `internal/engine/chat_queue_conv_test.go`, `internal/api/chat_queue_conv_test.go`

**Interfaces:**
- Consumes: `deliverToRun`, `ownerPrompt` (Task 3); `Conversation.ActiveRunID`, `ChatMessage.Queued`,
  `UpdateChatMessage`, `DeleteChatMessage` (Task 4).
- Produces (also added to `api.Controller`):
  ```go
  func (e *Engine) FlushChat(ctx context.Context, conversationID string) error
  func (e *Engine) EditQueuedMessage(ctx context.Context, id, text string) (domain.ChatMessage, error) // ErrConflict once delivered
  func (e *Engine) CancelQueuedMessage(ctx context.Context, id string) error                          // ErrConflict once delivered
  type chatTurnResult struct{ Reply, RunID, SessionID string; Status domain.RunStatus } // chatTurn's new return
  ```
  Routes: `s.operate(r, http.MethodPost, "/conversations/{id}/flush", s.flushConversation)`,
  `s.operate(r, http.MethodPatch, "/messages/{id}", s.editMessage)` (body `{"text": string}`),
  `s.operate(r, http.MethodDelete, "/messages/{id}", s.cancelMessage)`. Every message change
  emits `EvChatMessage` with data `{"conversation_id", "message"}`.

`SendChat` behaviour, in order: (1) the turn is live: try `deliverToRun(ctx, conv.ActiveRunID, text)`;
on success, store the message with `RunID` set. (2) otherwise, if busy, store it with `Queued:
true`. (3) idle: join the queued messages plus this one, mark them delivered, and start a turn.
`chatReply` sets and clears `ActiveRunID` alongside `Busy`. After a turn whose `Status` is
`RunSucceeded`, if queued messages exist, it starts the next turn with them; after any other
status it leaves them queued. `FlushChat` starts a turn from the queue when idle, and is a
no-op with an empty queue.

- [ ] **Step 1: Write the failing tests** (engine, fake harnesses)
  - `TestChatMessageWhileBusyIsDeliveredToSteerableTurn`
  - `TestChatMessageWhileBusyIsQueuedForNonSteerableTurn`, then `TestQueuedChatSentAfterSuccessfulTurn`:
    the next turn's prompt is `ownerPrompt([...queued texts in order])`, and the messages are no
    longer queued and carry that turn's run ID.
  - `TestQueuedChatHeldAfterCancelledTurn`: after `CancelRun` on the active turn, the messages
    stay queued. A new `SendChat` sends them all plus the new text as one turn.
  - `TestEditQueuedMessageAfterDeliveryConflicts`: editing or cancelling a delivered message
    returns an error wrapping `api.ErrConflict`.
  - API: `TestMessageRoutes` checks PATCH and DELETE on `/messages/{id}` (200/204, 409 once
    delivered), and `POST /conversations/{id}/flush` (202). A viewer session gets 403.
- [ ] **Step 2: Run and see them fail**
- [ ] **Step 3: Implement as described above**
- [ ] **Step 4: Full Go suite passes (includes `TestOpenAPISpecMatchesRoutes`)**
- [ ] **Step 5: Commit** `Let the owner message the orchestrator while it works`

### Task 6: Task agent conversations, read side

**Files:**
- Create: `internal/engine/conversation.go`
- Modify: `internal/api/controller.go`, `internal/api/handlers.go`, `internal/api/routes.go`,
  `internal/api/tasks.go` or `handlers.go` (`createTask` records `created_by_run`),
  `cmd/onett/` HTTP client (send the header)
- Modify: `docs/api.md`, `docs/openapi.yaml`
- Test: `internal/engine/conversation_conv_test.go`, `internal/api/conversation_conv_test.go`

**Interfaces:**
- Consumes: Task 4 types and store methods.
- Produces:
  ```go
  func (e *Engine) TaskConversation(ctx context.Context, taskID string) (domain.TaskConversation, error)
  func (e *Engine) AgentConversations(ctx context.Context) ([]domain.AgentConversation, error)
  func (e *Engine) conversationRoot(ctx context.Context, t domain.Task) (domain.Task, error) // follows ParentID while Source == domain.SourceConversation
  func (e *Engine) resumeSession(ctx context.Context, root domain.Task) (domain.Run, bool)  // newest lineage run of root.Role with SessionID != ""
  ```
  Routes: `s.read(r, "/tasks/{id}/conversation", s.taskConversation)`,
  `s.read(r, "/agent-conversations", s.agentConversations)`.
  `createTask` sets `Task.CreatedByRun` from the request header `X-Onett-Run-ID` only when the
  caller is the API token. The CLI sends that header with the value of `ONETT_RUN_ID` when it is
  set.

`CanSend`/`Reason` rules, in order: a viewer is shown the existing viewer line client-side,
so it is not decided here. Closed → `false`, `"Closed by <ClosedBy>: <ClosedReason>"`. No run
with a session, and nothing live → `false`, `"This agent has no session to continue. Retry the
task instead."`. The last run's harness (`Run.Harness` is the config name; look up its
`Type` in `cfg.Harness`, the `map[string]config.Harness`) has type `codex` and nothing is live → `false`, `"Codex agents
can't be continued yet: a resumed Codex session's sandbox is unverified."`. Task `queued` with
no runs → `true` (messages queue). Otherwise `true`. `AgentConversations` lists root tasks that
have at least one run: open ones (not closed) first, then ones closed or updated in the last 7
days, newest first, at most 100.

- [ ] **Step 1: Write the failing tests**
  - `TestTaskConversationLineage`: a root task with two runs and a `SourceConversation` child
    with one run gives `Tasks` (root, child), `Runs` (all three, oldest first), and
    `TaskConversation(childID)` returns the same root.
  - `TestTaskConversationCanSendReasons`: one case per rule above.
  - `TestAgentConversationsOrder`
  - API: `TestCreateTaskRecordsCreatorRun`: the header is honoured for the token and ignored
    for a signed-in user. `GET /tasks/{id}/conversation` returns 200, or 404 for an unknown task.
- [ ] **Step 2: Run and see them fail**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Full Go suite passes**
- [ ] **Step 5: Commit** `Read a task agent's conversation across its runs`

### Task 7: Messaging task agents and follow-up runs

**Files:**
- Modify: `internal/engine/conversation.go`, `internal/engine/runner.go`, `internal/engine/controller.go`
- Modify: `internal/api/controller.go`, `internal/api/handlers.go`, `internal/api/routes.go`
- Modify: `docs/api.md`, `docs/openapi.yaml`, `docs/design.md` (follow-ups and queued messages)
- Test: `internal/engine/followup_conv_test.go`, `internal/api/task_messages_conv_test.go`

**Interfaces:**
- Consumes: Tasks 3, 4, 6.
- Produces:
  ```go
  func (e *Engine) SendTaskMessage(ctx context.Context, taskID, text string) (domain.ChatMessage, error)
  func (e *Engine) FlushTaskMessages(ctx context.Context, taskID string) error
  func (e *Engine) continueConversation(ctx context.Context, rootID string) // starts a follow-up from queued messages if the rules allow
  ```
  Routes: `s.operate(r, http.MethodPost, "/tasks/{id}/messages", s.sendTaskMessage)` (body
  `{"text": string}`, 202 with the ChatMessage, 409 with the `Reason` when it can't be sent),
  `s.operate(r, http.MethodPost, "/tasks/{id}/conversation/flush", s.flushTaskMessages)`.

`SendTaskMessage` runs in this order. It finds or creates the root's conversation
(`TaskID = root.ID`, `Title = root.Title`), then refuses with `ErrConflict` and the Task 6 reason
when `CanSend` is false. If a lineage run is live, it tries `deliverToRun`: on success the
message is stored with `RunID`; on `ErrInputClosed` or `errRunNotLive` it is stored `Queued`.
If nothing is live, it stores the message `Queued` and calls `continueConversation`.
`continueConversation` acts on the newest lineage task:
- blocked, failed, review, awaiting_approval, or done with `PRNumber == 0`: `setTaskFrom` to
  queued, `Attempts = 0`, `BlockedReason = ""`, then `cancelStaleReviews` and a scheduler wake.
- merged, deploying, or done with a PR: `createTask` a child with `ParentID = newest.ID`,
  `Source = SourceConversation`, the same project, kind and role, and
  `Title = "Follow-up: " + root.Title`. Its `Body` repeats the root body.
- queued or running: nothing; the scheduler or the end of the run picks the messages up.

In `runTask`, before `buildTaskPrompt`: if the root conversation has queued messages and
`resumeSession` finds a session, the prompt is `ownerPrompt(those texts)`, the spec's
`ResumeSessionID` is that run's `SessionID`, and the messages are marked delivered with this
run's ID. If there is no session yet (the task's first run), the queued texts are appended to
the task prompt after a blank line, headed `## Message from the owner`. At the end of
`runTask`, when the run `Status` is `RunSucceeded`, call `continueConversation(root.ID)`; it
does nothing if no messages are queued. Do not call it for any other status. A conversation
child's branch comes from `branchName(t)`, because only review tasks inherit their parent's
branch (`runner.go:42-62`).

- [ ] **Step 1: Write the failing tests** (fake harness records each `harness.Spec`)
  - `TestMessageToBlockedTaskResumesSession`: the task requeues, and the next run's
    `ResumeSessionID` is the previous run's `SessionID` with prompt `ownerPrompt([text])`.
  - `TestMessageDuringNonSteerableRunQueuesThenFollowsUp`
  - `TestQueuedMessageNotDeliveredAfterDenial`: the run is denied, so the task blocks and the
    message stays `Queued`; after `FlushTaskMessages` the follow-up starts.
  - `TestMessageAfterMergeCreatesConversationChild`: the child has `Source ==
    "conversation"`, its own branch (not the parent's) and the resumed session ID.
  - `TestMessageToQuestionTaskDoneWithoutPRRequeues`
  - `TestMessageToNeverRunTaskJoinsFirstPrompt`
  - API: 202 / 409 with reason / 403 for a viewer.
- [ ] **Step 2: Run and see them fail**
- [ ] **Step 3: Implement**
- [ ] **Step 4: Full Go suite passes**
- [ ] **Step 5: Real-flow check (AGENTS.md: runner changes need it).** Build onett
  (`mise exec -- scripts/build.sh`, `OUT` in a temp dir). Run a second daemon with a temp
  `data_dir` on a spare port and a throwaway local git repo as its project, using the real
  Claude harness. Send a task, wait for review, message the agent through
  `POST /tasks/{id}/messages`, and confirm the follow-up run resumes the session (its run log
  starts with a `prompt` line holding the owner text, and its `session_id` matches) and pushes
  a new commit. Never use port 7777 or the owner's data directory.
- [ ] **Step 6: Commit** `Message task agents and continue their sessions`

### Task 8: Closing, reopening and `finished`

**Files:**
- Modify: `internal/engine/conversation.go`, `internal/engine/runner.go` (after `readResult`),
  `internal/engine/controller.go` (`CancelTask`)
- Modify: `internal/api/controller.go`, `internal/api/handlers.go`, `internal/api/routes.go`,
  `cmd/onett/commands.go` (new `task close-conversation` subcommand, its `-h`), README
  **Commands** table
- Modify: `internal/engine/prompts/common.md` (report schema gains `"finished": false` and one
  sentence), `triager.md`, `implementer.md`, `orchestrator.md`
- Modify: `docs/api.md`, `docs/openapi.yaml`
- Test: `internal/engine/close_conv_test.go`, `internal/api/close_conv_test.go`

**Interfaces:**
- Consumes: Tasks 4, 6, 7.
- Produces:
  ```go
  func (e *Engine) CloseTaskConversation(ctx context.Context, taskID, by, reason string) error
  func (e *Engine) ReopenTaskConversation(ctx context.Context, taskID string) error
  ```
  Routes: `s.operate(r, http.MethodPost, "/tasks/{id}/conversation/close", s.closeTaskConversation)`
  (body `{"reason": string}`; `by` is `"orchestrator"` for the API token and `"owner:<username>"`
  for a person), and `s.operate(r, http.MethodPost, "/tasks/{id}/conversation/reopen",
  s.reopenTaskConversation)`, which calls `userPrincipal` first so the token gets 403. Both emit
  `"conversation.updated"` (new `domain.EvConversationUpdated`) with data `{"task_id"}`. CLI:
  `onett task close-conversation ID --reason R`.

Prompt text, added verbatim:
- common.md, after the JSON block: ``Set `"finished": true` only when nothing more can come of
  this conversation: the owner has what they asked for and you have no open questions. It
  closes your conversation; it does not change the task.``
- triager.md: ``When your answer is complete and you have no open questions, set `"finished": true`.``
- implementer.md: ``Leave `finished` unset: your change still goes through review and merge.``
- orchestrator.md, under "What you can do": ``- `onett task close-conversation ID --reason R` —
  close a task agent's conversation when the owner says the work is finished, or when its task
  is merged and deployed and the owner has nothing open on it.``

- [ ] **Step 1: Write the failing tests**
  - `TestFinishedClosesConversation`: a fake run reporting `finished: true` sets `ClosedBy ==
    "agent"` and leaves the task status as the run decided.
  - `TestCancelTaskClosesConversationAndKeepsQueue`: with a queued message, cancelling the task
    closes the conversation with `ClosedBy == "owner:<user>"`; the message stays queued and is
    never delivered.
  - `TestReopenClearsClosedOnly`
  - API: the token gets 403 on reopen and 200 on close; a viewer gets 403 on both.
- [ ] **Step 2: Run and see them fail**
- [ ] **Step 3: Implement.** Closing an already-closed conversation returns `ErrConflict`, and so
  does reopening an open one. Closing creates the conversation row if none exists yet.
- [ ] **Step 4: Full Go suite passes; `onett task close-conversation -h` prints its usage**
- [ ] **Step 5: Commit** `Close and reopen agent conversations`

### Task 9: Transcript model (web, pure logic)

**Files:**
- Create: `web/src/lib/transcript.ts`, `web/src/lib/transcript.test.ts`

**Interfaces:**
- Consumes: web types from Tasks 1 and 4.
- Produces:
  ```ts
  export interface ToolSummary { name: string; arg: string; added?: number; removed?: number }
  export function parseTool(text: string): ToolSummary // input "Name: {compact json}"
  export type Entry =
    | { kind: 'owner'; text: string; time: string; queued?: boolean; messageId?: string }
    | { kind: 'prompt'; text: string; time: string; label: string } // label "Prompt" or "Task (prompt not recorded)"
    | { kind: 'text'; text: string; time: string }
    | { kind: 'tool'; tool: ToolSummary; input: string; result?: string; resultError?: boolean; time: string }
    | { kind: 'denial' | 'error' | 'system' | 'stderr'; text: string; time: string }
    | { kind: 'divider'; text: string; time: string } // "New session · retried by owner"
  export function runEntries(lines: LogLine[], fallbackPrompt?: string): Entry[]
  export function orchestratorEntries(messages: ChatMessage[], logsByRun: Record<string, LogLine[]>): Entry[]
  export function taskEntries(conv: TaskConversation, logsByRun: Record<string, LogLine[]>): Entry[]
  ```
  `parseTool`'s main argument comes from the first present key among `command`, `file_path`,
  `path`, `pattern`, `url`, `query`, `description`. For an `Edit`, `added` and `removed` count
  the lines in `new_string` and `old_string`. Unparseable (for example, truncated) JSON gives
  `arg` as the raw text after `": "`, with no counts.

- [ ] **Step 1: Write the failing tests** (Vitest, `describe`/`it` like `format.test.ts`):
  `parses Bash command`, `counts edit lines`, `falls back on truncated input`, `pairs results
  by tool_id`, `pairs results by order when tool_id is missing`, `uses the task body when no
  prompt line exists`, `puts orchestrator turn logs after their message`, `marks a new session
  with a divider`, `shows queued messages last`.
- [ ] **Step 2: `cd web && npm test` fails**
- [ ] **Step 3: Implement**
- [ ] **Step 4: `npm run typecheck && npm test` pass**
- [ ] **Step 5: Commit** `Build agent transcripts from messages and run logs`

### Task 10: Transcript component and Terminal view

**Files:**
- Create: `web/src/runLogs.ts` (hook extracted from `LogViewer`), `web/src/components/Transcript.tsx`,
  `web/src/components/Transcript.css`
- Modify: `web/src/components/LogViewer.tsx` (use the hook)
- Test: `web/src/lib/transcriptView.test.ts`

**Interfaces:**
- Consumes: Task 9.
- Produces:
  ```ts
  export function useRunLogLines(runId: string, active: boolean): { lines: LogLine[]; loading: boolean; error?: Error } // runLogs.ts: LogViewer's paging, run.log subscription, watch() and polling, unchanged
  export type TranscriptView = 'conversation' | 'terminal'
  export function readTranscriptView(): TranscriptView           // lib/transcriptView.ts, localStorage key 'onett.transcriptView', try/catch, default 'conversation'
  export function writeTranscriptView(v: TranscriptView): void
  export function Transcript(props: { entries: Entry[]; view: TranscriptView }): JSX.Element
  export function ViewToggle(props: { view: TranscriptView; onChange(v: TranscriptView): void }): JSX.Element
  ```
  Conversation view is laid out as in the mockup (`docs/proposals/agent-conversations-mockup.html`):
  a tool row expands on click, with the full input and result in `pre`; denials keep LogViewer's
  `DENIED` tag. Terminal view has a 64px time gutter (52px under 760px), markers `›` (owner,
  prompt), `●` (text, tool), `⎿` (result), and results clipped to 3 lines with "show more".
  Colours come from the existing tokens in `styles.css`; there are no inline scripts and no raw
  HTML (prose goes through `Markdown`).

- [ ] **Step 1: Write the failing test** `transcriptView.test.ts`: the default is conversation;
  a written value reads back; a throwing localStorage falls back to conversation.
- [ ] **Step 2: Fails** → **Step 3: Implement** → **Step 4: typecheck, test and build pass; RunDetail still shows its log**
- [ ] **Step 5: Commit** `Add the shared transcript component with a terminal view`

### Task 11: Chat page for every agent

**Files:**
- Modify: `web/src/pages/Chat.tsx`, `web/src/pages/Chat.css`, `web/src/App.tsx` (route
  `/chat/task/:taskId`), `web/src/api.ts`, `web/src/queries.ts`, `web/src/keys.ts`,
  `web/src/events.ts` (`keysFor`: `chat.message` → conversation, task conversation, agent
  conversations; `conversation.updated` → task conversation, agent conversations),
  `web/src/pages/TaskDetail.tsx`, `web/src/pages/RunDetail.tsx` ("Open conversation" link)
- Modify: `docs/ui.md` (Chat, Task detail, Run detail), README chat section
- Test: `web/src/lib/chatState.test.ts`, `web/src/lib/chatState.ts`

**Interfaces:**
- Consumes: Tasks 5–10.
- Produces:
  - `api.ts`: `editMessage(id, text)`, `cancelMessage(id)`, `flushConversation(id)`,
    `listAgentConversations()`, `getTaskConversation(taskId)`, `sendTaskMessage(taskId, text)`,
    `flushTaskMessages(taskId)`, `closeTaskConversation(taskId, reason)`,
    `reopenTaskConversation(taskId)`.
  - `keys.ts`: `agentConversations: ['agent-conversations'] as const`,
    `taskConversation: (id: string) => ['task-conversation', id] as const`.
  - `lib/chatState.ts`: `export function composerState(input: { canSend: boolean; reason?: string; viewer: boolean; working: boolean; queued: number }): { enabled: boolean; placeholder: string; note?: string }`
    The placeholder is `Message the orchestrator…` or `Message the <role>…`. Viewers get the
    existing viewer line, and a closed conversation gets its reason.

Behaviour, as in the mockup and spec **The Chat page**:
- The sidebar has New conversation, then **Orchestrator** conversations, then an **Agents**
  section from `listAgentConversations`, grouped by task, each with a lamp: running blinks,
  closed is dim, blocked is red.
- The composer stays enabled while the agent works. Queued messages show in the dashed box with
  Edit and Cancel, and a "Send now" button appears when the last run did not succeed.
- Stop (a button, or Esc in the field) calls `cancelRun` on the live run.
- The header has the view toggle and, for task agents, Close and Reopen (Reopen only for
  signed-in operators and owners).
- Task agents show the yellow hint with a link to `/chat`, and a closed conversation shows its
  reason with Reopen.
- When an orchestrator turn created tasks (`created_by_run` matches one of the conversation's
  runs), a chip appears under that turn with the task's lamp, title, an "open agent" link to
  `/chat/task/:id` and a "task" link.
- Under 760px the sidebar collapses to chips, as today.

- [ ] **Step 1: Write the failing test** `chatState.test.ts`, covering each `composerState` case
  (viewer, closed, working, idle, queued).
- [ ] **Step 2: Fails** → **Step 3: Implement** → **Step 4: typecheck, test and build pass**
- [ ] **Step 5: Browser check (AGENTS.md).** Run a throwaway daemon (temp `data_dir`, spare
  port) built with the UI. At 1280px and at 390px, check: an orchestrator conversation with
  tool rows, the Terminal toggle surviving a reload, a task agent conversation, queue and Stop,
  and close and reopen. Take screenshots and look at them.
- [ ] **Step 6: Commit** `Make Chat work like Claude Code for every agent`

### Task 12: Final verification

- [ ] Run the full CI set from Global Constraints on the settled branch and see every command
  pass.
- [ ] Re-run Task 7 Step 5's real-flow check, plus one mid-run message to a live Claude task
  agent: its log shows an `owner` line, and its reply addresses the message.
- [ ] Update the spec's Status line to `Implemented` and list anything deferred (the Codex
  sandbox check, if still open).
- [ ] Commit `Mark agent conversations implemented`
