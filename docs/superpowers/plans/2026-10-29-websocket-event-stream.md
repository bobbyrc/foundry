# WebSocket Event Stream Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The web UI receives live events over one WebSocket per tab, and gets `run.log` /
`chat.delta` only for the runs and conversations it subscribes to; SSE stays for the CLI.

**Architecture:** The SSE handler's replay / live / re-check loop moves into one function that
both transports call through a small `eventSink` interface. A new `/api/v1/ws` handler adds a
per-socket subscription set that filters live events. The web `EventStream` swaps
`EventSource` for `WebSocket` behind the same public API and gains `watch()`.

**Tech Stack:** Go 1.27, chi, `github.com/coder/websocket` v1.8.15, React + TypeScript, Vitest.

**Spec:** `docs/proposals/websocket-event-stream.md`. Read it before any task.

## Global Constraints

- Run Go through `mise exec --`. CI checks: `mise exec -- go vet ./...`,
  `test -z "$(mise exec -- gofmt -l internal cmd)"`, `mise exec -- go test -race -count=1 ./...`;
  for `web/`: `cd web && npm ci && npm run typecheck && npm test && npm run build`.
- Only new dependency: `github.com/coder/websocket@v1.8.15`. No other `go.mod` or
  `package.json` change.
- Never set `websocket.AcceptOptions.InsecureSkipVerify` or `OriginPatterns`; call
  `websocket.Accept(w, r, nil)`.
- Limits: read limit `4096` bytes, `64` subscriptions per socket, `10 * time.Second` per write.
- Message kinds are exactly `event`, `subscribed`, `unsubscribed`, `error` (server→client) and
  `subscribe`, `unsubscribe` (client→server); ID fields are exactly `run_id`, `conversation_id`.
- `GET /api/v1/events` (SSE) behaviour and its existing tests do not change.
- New Go tests go in new files. Tests never bind fixed ports (`httptest.NewServer` only).
- Match surrounding code: comments explain why, plain words.
- Do not commit to `main`; commit on the current branch (`websockets`).

## Review Focus

1. **Navigating away before the server acknowledges a subscription.** The released watch must
   not keep the status at `reconnecting`, and a late `subscribed` for it must be ignored.
   Test in Task 4.
2. **The daemon restarting while a run page is open.** The new socket has no subscriptions; the
   client must re-send them and stay `reconnecting` until all are acknowledged. Test in Task 4.
3. **A client message that isn't text or isn't JSON** (binary frame, `"hello"`, `{}`). It gets
   `error` and the socket stays open. Test in Task 2 (decode) and Task 3 (binary frame).
4. **The client vanishing without a close frame** (laptop sleeps). The hijacked connection does
   not cancel the request context, so the reader's error must end the stream and free the
   stream slot. Test in Task 3.
5. **A page on another port of the same host** (e.g. a dev server at `localhost:5173` talking to
   a daemon at `localhost:7777` directly) opening the socket with the user's cookie. Must be
   refused. Test in Task 3.

## Order and parallelism

Tasks 1, 2 and 4 touch disjoint files and can run at once. Task 3 needs 1 and 2. Task 5 needs 4.
Task 6 is the orchestrator's final check.

---

### Task 1: Shared stream loop

**Files:**
- Create: `internal/api/stream.go`
- Modify: `internal/api/sse.go` (`events` body moves out; `writeEvent`, `stillAllowed` stay)

**Interfaces:**
- Produces (exact):

```go
// eventSink writes to one open event stream: SSE for the CLI, a WebSocket for the UI.
type eventSink interface {
	send(e domain.Event) error // write one event
	flush() error              // push what was written to the client
	ping() error               // keep-alive; also flushes
}

// tryAcquireStream takes one of the maxEventStreams slots shared by every stream transport.
func (s *server) tryAcquireStream() (release func(), ok bool)

// streamEvents replays stored events with an ID above after (only when replay is true), then
// sends live events from ch that keep accepts, skipping IDs already sent. Every pingInterval it
// re-checks p with stillAllowed and pings. It returns when ctx ends, a write fails, the bus
// closes ch, or p may no longer read.
func (s *server) streamEvents(ctx context.Context, p principal, ch <-chan domain.Event,
	after int64, replay bool, sink eventSink, keep func(domain.Event) bool)
```

- [ ] **Step 1: Run the SSE tests to record the baseline**

Run: `mise exec -- go test -race -count=1 -run 'TestSSE' ./internal/api/`
Expected: PASS.

- [ ] **Step 2: Implement `stream.go` and an `sseSink` in `sse.go`**

`sseSink{w io.Writer; f http.Flusher; log *slog.Logger}`: `send` calls the existing
`writeEvent`, `flush` calls `f.Flush()`, `ping` writes `": ping\n\n"` then flushes. `keep` is
applied to live events only (filtered types are never stored). Replay flushes once per page,
the live loop after each event, exactly as now. `events` keeps its order: Flusher check, stream
slot (503 + `Retry-After: 5`), principal, `after` parse, `Bus.Subscribe`, headers,
`": connected"`, then `s.streamEvents(..., func(domain.Event) bool { return true })`.

- [ ] **Step 3: Run the full API package**

Run: `mise exec -- go test -race -count=1 ./internal/api/ && mise exec -- go vet ./internal/api/`
Expected: PASS, no test file changed.

- [ ] **Step 4: Commit** — `git commit -m "Share the event stream loop between transports"`

---

### Task 2: Subscription protocol

**Files:**
- Create: `internal/api/wsproto.go`, `internal/api/wsproto_test.go`

**Interfaces:**
- Produces (exact):

```go
const (
	wsReadLimit        = 4096
	maxWSSubscriptions = 64
)

// wsOut is one server-to-client frame. Kind is "event", "subscribed", "unsubscribed" or "error".
type wsOut struct {
	Kind           string        `json:"kind"`
	Event          *domain.Event `json:"event,omitempty"`
	RunID          string        `json:"run_id,omitempty"`
	ConversationID string        `json:"conversation_id,omitempty"`
	Message        string        `json:"message,omitempty"`
}

// wsIn is one client-to-server frame.
type wsIn struct {
	Kind           string `json:"kind"`
	RunID          string `json:"run_id"`
	ConversationID string `json:"conversation_id"`
}

type wsSubscriptions struct { /* mu sync.Mutex; runs, convs map[string]bool */ }

func newWSSubscriptions() *wsSubscriptions
// handle decodes one client frame strictly, applies it, and returns the reply to send.
func (s *wsSubscriptions) handle(raw []byte) wsOut
// wants reports whether a live event goes to this socket.
func (s *wsSubscriptions) wants(e domain.Event) bool
```

- [ ] **Step 1: Write the failing tests**

`TestWSHandleReplies` (table, fresh set per row unless noted; check `Kind`, IDs, and that
`Message` is non-empty for errors):

| input | reply |
|---|---|
| `{"kind":"subscribe","run_id":"r1"}` | `subscribed`, `RunID:"r1"` |
| `{"kind":"subscribe","conversation_id":"c1"}` | `subscribed`, `ConversationID:"c1"` |
| `{"kind":"unsubscribe","run_id":"never"}` | `unsubscribed`, `RunID:"never"` (idempotent) |
| `{"kind":"subscribe","run_id":"r1","conversation_id":"c1"}` | `error` |
| `{"kind":"subscribe"}` and `{"kind":"subscribe","run_id":""}` | `error` |
| `{"kind":"steer","run_id":"r1"}` | `error` |
| `{"kind":"subscribe","run_id":"r1","x":1}` | `error` |
| `hello`, `{}`, `{"kind":"subscribe","run_id":"r1"} {}` | `error` |

`TestWSSubscriptionLimit`: 64 distinct subscribes (mix runs and conversations) all `subscribed`;
the 65th `error`; re-subscribing an existing ID still `subscribed`; after one `unsubscribe`, a
new ID is `subscribed`.

`TestWSWants`: with run `r1` and conversation `c1` subscribed —
`run.log` RunID `r1` → true; RunID `r2` → false; `chat.delta` Data
`{"conversation_id":"c1","text":"x"}` → true; `c2` → false; Data `not json` → false;
`task.updated` → true. After `unsubscribe` of `r1`, `run.log` `r1` → false.

- [ ] **Step 2: Run to see them fail**

Run: `mise exec -- go test -race -count=1 -run 'TestWS' ./internal/api/`
Expected: FAIL (undefined: `newWSSubscriptions`).

- [ ] **Step 3: Implement `wsproto.go`**

Decode with `json.NewDecoder(bytes.NewReader(raw))`, `DisallowUnknownFields`, then require a
second `Decode` to return `io.EOF` (rejects trailing data). Use `domain.EvRunLog` /
`domain.EvChatDelta` constants. The count limit is `len(runs)+len(convs)`.

- [ ] **Step 4: Run to see them pass** — same command, PASS.

- [ ] **Step 5: Commit** — `git commit -m "Add the WebSocket subscription protocol"`

---

### Task 3: WebSocket endpoint

**Files:**
- Create: `internal/api/ws.go`, `internal/api/ws_test.go`
- Modify: `internal/api/routes.go` (add `s.read(r, "/ws", s.eventsWS)` next to `/events`),
  `internal/api/server.go` (`requestLog`: log `/api/v1/ws` at debug like `/api/v1/events`),
  `go.mod`, `go.sum`, `docs/api.md`, `docs/openapi.yaml`, `docs/design.md`

**Interfaces:**
- Consumes: Task 1's `eventSink`, `tryAcquireStream`, `streamEvents`; Task 2's `wsOut`,
  `newWSSubscriptions`, `handle`, `wants`, `wsReadLimit`.
- Produces: `func (s *server) eventsWS(w http.ResponseWriter, r *http.Request)`;
  `const wsWriteTimeout = 10 * time.Second`; `wsSink` implementing `eventSink`.

- [ ] **Step 1: Add the dependency**

Run: `mise exec -- go get github.com/coder/websocket@v1.8.15`
Expected: `go.mod` gains one `require` line.

- [ ] **Step 2: Write the failing tests** (`ws_test.go`; `httptest.NewServer(x.h)` from
`newHarness`, dial with `websocket.Dial`, auth via `DialOptions.HTTPHeader`
`Authorization: Bearer testToken` unless noted; read frames with `wsjson.Read` under a 5 s
context; emit with `x.bus.Emit(ctx, typ, project, taskID, runID, message, data)`)

- `TestWSReplayThenLive`: two stored events; dial `?after=<first ID>`; first `event` frame is
  the second stored event; then an emitted `task.updated` arrives; no ID appears twice.
- `TestWSRunLogNeedsSubscription`: emit `run.log` for `r1`, then `task.updated` (marker); the
  next `event` is the marker. Send `subscribe r1`; read `subscribed` `r1`; emit `run.log` `r1`;
  next frame is that event. `unsubscribe r1`, read `unsubscribed`; emit `run.log` `r1` then a
  marker; next `event` is the marker.
- `TestWSChatDeltaNeedsSubscription`: the same for `chat.delta` with Data
  `{"conversation_id":"c1","text":"hi"}`.
- `TestWSBadMessagesKeepSocketOpen`: send `hello`, then a binary frame; each gets `error`; a
  following `subscribe r1` gets `subscribed`.
- `TestWSRefusesOtherOrigins`: `Origin: http://evil.example` → dial fails, response 403;
  `Origin: http://127.0.0.1:1` (same host, other port) → 403; `Origin` equal to the test
  server's URL → connects.
- `TestWSAuth`: no credential → 401; `?token=testToken` with no header → 401; a session cookie
  from `x.newUserSession(t, "ann", domain.RoleViewer)` (cookie `auth.SessionCookie`) → connects.
- `TestWSSignedOutLosesSocket`: build the server the way `streamServerWithPing` does
  (`internal/api/sse_auth_test.go`) but serving `s.eventsWS`, ping 50 ms; connect with a
  session; delete the session in the store; the next `Read` fails within 2 s.
- `TestWSSharesStreamCap`: open `maxEventStreams` SSE streams (as `TestSSEConcurrentStreamLimit`
  does); a WebSocket dial gets 503. Close one SSE stream; within 2 s a dial succeeds.
- `TestWSOversizeFrameCloses`: a 5000-byte text frame closes the socket (the next `Read`
  fails).
- `TestWSClientVanishFreesSlot`: dial, then close the underlying connection without a close
  frame (`c.CloseNow()`); within 2 s, `maxEventStreams` new dials all succeed.

- [ ] **Step 3: Run to see them fail**

Run: `mise exec -- go test -race -count=1 -run 'TestWS' ./internal/api/`
Expected: FAIL (404 or undefined `eventsWS`).

- [ ] **Step 4: Implement `ws.go`**

Order in `eventsWS`: `principalFrom` (401) → `queryInt64(..., "after")` (`s.fail`) →
`tryAcquireStream` (503 + `Retry-After: 5`) → `Bus.Subscribe` → `websocket.Accept(w, r, nil)`
(on error return; it has written the response) → `c.SetReadLimit(wsReadLimit)` →
`ctx, stop := context.WithCancel(r.Context())`. Reader goroutine: loop `c.Read(ctx)`; on error
`stop()` and return; non-text frame → reply `error`; text → `subs.handle(data)`; write every
reply with `wsjson.Write` under a `wsWriteTimeout` context. Main goroutine:
`s.streamEvents(ctx, p, ch, after, r.URL.Query().Get("after") != "", wsSink{...}, subs.wants)`,
then `stop()` and `c.Close(websocket.StatusGoingAway, "stream ended")`. `wsSink.send` writes
`wsOut{Kind: "event", Event: &e}`; `flush` returns nil; `ping` is `c.Ping` under the write
timeout. A comment says why replies are written from the reader (the library allows concurrent
writes, and the set changes before the reply goes out).

- [ ] **Step 5: Run to see them pass**

Run: `mise exec -- go test -race -count=1 ./internal/api/`
Expected: PASS, including the unchanged SSE tests and `TestOpenAPISpecMatchesRoutes` once
Step 6 is done.

- [ ] **Step 6: Docs**

`docs/api.md`: add `GET /ws` to the route table and a "WebSocket stream" section after the SSE
one with the two message tables and the filtering rules from the spec. `docs/openapi.yaml`:
add `/ws` modelled on `/events` (a `101` response, query `after`). `docs/design.md`: where it
says "HTTP + SSE", say SSE serves the CLI and a WebSocket serves the UI; in the reverse-proxy
guidance, say to forward `Upgrade` and `Connection` and keep the `Host` header.

- [ ] **Step 7: CI checks** — the three Go commands from Global Constraints. All pass.

- [ ] **Step 8: Commit** — `git commit -m "Serve the UI event stream over a WebSocket"`

---

### Task 4: Web EventStream over WebSocket

**Files:**
- Modify: `web/src/events.ts`, `web/src/events.test.ts`, `web/src/types.ts`,
  `web/vite.config.ts` (proxy `'/api'` gains `ws: true`)

**Interfaces:**
- Produces (exact), in `types.ts`:

```ts
export type WsServerMessage =
  | { kind: 'event'; event: OnettEvent }
  | { kind: 'subscribed' | 'unsubscribed'; run_id?: string; conversation_id?: string }
  | { kind: 'error'; message: string };
export interface WsClientMessage {
  kind: 'subscribe' | 'unsubscribe';
  run_id?: string;
  conversation_id?: string;
}
```

and in `events.ts` (all existing exports keep their signatures):

```ts
export type WatchTarget = { run_id: string } | { conversation_id: string };
export interface Watch { ready: Promise<void>; release: () => void }
export function watch(target: WatchTarget): Watch; // delegates to eventStream.watch
```

- [ ] **Step 1: Write the failing tests** (replace `FakeEventSource` with a `FakeWebSocket`
stub: `instances`, `url`, `sent: string[]`, `readyState`, `onopen/onmessage/onclose`, helpers
to deliver a server message; keep every existing reconnect test, re-expressed with it)

- URL: page `http:` → `ws://<host>/api/v1/ws`; after an event with id 7 and a reconnect, the URL
  ends `?after=7`. (`https:` → `wss:` if `location` can be stubbed.)
- With no watches, status is `live` on open; reconnect calls `invalidateQueries` once.
- `watch({run_id:'r1'})` twice sends one `subscribe`; `ready` resolves on `subscribed` `r1`;
  releasing one sends nothing; releasing the second sends `unsubscribe`.
- With a watch, open alone leaves status `reconnecting`; it becomes `live` on `subscribed`.
- After close and reopen, `subscribe` is re-sent for every live watch, and status stays
  `reconnecting` until each is acknowledged.
- Release before `subscribed` arrives: status becomes `live` without waiting for it; a late
  `subscribed` for it changes nothing and throws nothing.
- A watch created while disconnected sends `subscribe` on the next open.
- `event` frames reach `subscribe()` listeners as `OnettEvent`; `error` frames do not.
- A 401 from `getStats` during recovery stops the stream (existing test, kept).

- [ ] **Step 2: Run to see them fail**

Run: `cd web && npm test -- events`
Expected: FAIL.

- [ ] **Step 3: Implement**

Replace `EventSource` with `WebSocket`; the URL scheme follows `location.protocol`. Keep
backoff, `retryNow`, `recover`, `lastId`. Handle disconnects in `onclose` only (browsers fire
`error` then `close`). Watches live in a `Map` keyed `run:<id>` / `conversation:<id>` holding
the target, a count, an `acked` flag and pending resolvers; all `acked` reset on close. `live`
means: socket open and every entry acked. Drop `EVENT_TYPES` if nothing else imports it.

- [ ] **Step 4: Run to see them pass** — `cd web && npm run typecheck && npm test`, PASS.

- [ ] **Step 5: Commit** — `git commit -m "Move the web event stream to a WebSocket"`

---

### Task 5: Pages watch what they show

**Files:**
- Modify: `web/src/components/LogViewer.tsx`, `web/src/pages/Chat.tsx`

**Interfaces:**
- Consumes: Task 4's `watch`.

- [ ] **Step 1: LogViewer**

Leave the initial load, poll and reconnect effects as they are. Add an effect keyed on `runId`:
`const w = watch({ run_id: runId })`; when `w.ready` resolves and the effect is still current,
call `catchUp()`; cleanup calls `w.release()`. Do not gate the initial load on the socket.

- [ ] **Step 2: Chat**

In `Thread`, an effect keyed on `conversationId` that returns
`watch({ conversation_id: conversationId }).release`.

- [ ] **Step 3: Checks** — `cd web && npm run typecheck && npm test && npm run build`, PASS.

- [ ] **Step 4: Commit** — `git commit -m "Subscribe run logs and chat to the streams they show"`

---

### Task 6: Final verification (orchestrator)

- [ ] All CI commands from Global Constraints on the settled tree.
- [ ] `mise exec -- scripts/build.sh` with `OUT` in a temp dir; a config with `server.addr` on a
  spare port and `server.data_dir` in a temp dir; `./onett serve --config <it>`.
- [ ] In a real browser at desktop and phone width: a run's log streams live; chat streams; a
  second tab on another run receives none of the first run's `run.log` frames (devtools →
  WS frames); kill and restart the daemon and the open run page reconnects and catches up;
  seven tabs all load; with WebSocket upgrades blocked, a run page still shows its stored log
  and polls.
- [ ] CSP `connect-src 'self'` allows the socket in Chrome, Firefox and Safari. If one refuses,
  stop and report; do not loosen the CSP.
- [ ] `onett` CLI log streaming still works (SSE).
- [ ] After merge: warm the dogfood factory's module cache for `github.com/coder/websocket`
  (it builds with `GOPROXY=off`; see `docs/getting-started.md`).
