# Proposal: a WebSocket event stream for the web UI

Status: design agreed with the owner, 2026-10-29. Nothing here is implemented.

## Why

The web UI gets live updates from `GET /api/v1/events`, a Server-Sent Events (SSE) stream: one
long HTTP response the server keeps writing to. The server flushes after every event, so speed is
not the problem. The owner wants a two-way connection because three planned features need the
browser to talk back to the server:

1. **Pick what to stream.** Every tab now receives every event, including every agent's output
   (`run.log`) and every chat token (`chat.delta`) for the whole factory. Those two are the
   high-volume events. A tab should get them only for the runs and conversations it shows.
2. **Steer a running agent** (later, its own design): send text to an agent mid-run.
3. **A live terminal in a run** (later, its own design): an interactive shell on a run's worktree.

This proposal covers only the base: a WebSocket that replaces SSE for the web UI, plus
subscriptions (item 1). Steering and the terminal each get their own design on top of it.

A side effect: on plain HTTP/1.1 a browser allows about six connections per host across all
tabs, and each tab's SSE stream holds one. With six tabs open, a seventh tab's requests queue.
WebSockets do not count toward that limit.

## Goals

- The web UI uses one WebSocket per tab for every live event, with the same behaviour it has
  now: replay from the last seen event ID after a reconnect, backoff, the "live / reconnecting"
  indicator, a full refetch after a reconnect.
- `run.log` and `chat.delta` reach a tab only for runs and conversations it has subscribed to.
  Every other event type reaches every tab, as now.
- The socket is the place later features add their messages (steering), without a new
  connection per feature.
- No weaker security than the SSE stream (see **Security**).

## Non-goals

- Steering and the live terminal. Do not add message kinds for them now.
- The CLI. `onett` keeps using SSE (`internal/client/events.go`); `GET /api/v1/events` stays,
  unchanged in behaviour, for the CLI and for scripts (`curl`).
- Putting changed data into events so the UI can skip refetches. Worth doing; separate change.
- Filtering inside the event bus (`internal/events`). The socket handler filters; the `Bus`
  interface does not change.
- An SSE fallback in the UI. A reverse proxy must pass WebSocket upgrades; the docs say so.

## Anti-goals (failure even if it works)

- A second copy of the replay / live / re-check loop that drifts from the SSE one. The two
  transports share one loop.
- Any browser page on another site able to open the socket with the user's cookie.
- An agent able to use the socket for something its API token cannot do today.

## Design

### Endpoint

`GET /api/v1/ws`, registered with `s.read` in `internal/api/routes.go` (the "read" action, like
`/events`). Optional `?after=<id>` has the same meaning as on `/events`: replay stored events
with a greater ID first, then stream live ones.

### Messages (frozen; use these names exactly)

Every frame is one JSON text message with a `kind` field.

Server to client:

| kind | fields | meaning |
|---|---|---|
| `event` | `event`: the same JSON object SSE sends today (`domain.Event` as `writeEvent` encodes it) | one event |
| `subscribed` | `run_id` or `conversation_id` | the subscription is active; events for it follow |
| `unsubscribed` | `run_id` or `conversation_id` | the subscription is gone |
| `error` | `message` | a client message was rejected; the socket stays open |

Client to server:

| kind | fields | meaning |
|---|---|---|
| `subscribe` | exactly one of `run_id`, `conversation_id` | start receiving `run.log` for that run, or `chat.delta` for that conversation |
| `unsubscribe` | exactly one of `run_id`, `conversation_id` | stop |

Client messages are decoded strictly: an unknown `kind`, an unknown field, both IDs or neither
gets an `error` reply and is otherwise ignored. A frame that is not valid JSON or exceeds the
read limit closes the socket.

### Filtering

- `run.log` is sent only if the socket subscribed to its `run_id`.
- `chat.delta` is sent only if the socket subscribed to the `conversation_id` in its data.
- Every other type is sent as now. Both filtered types are live-only (never stored), so replay
  is unaffected.
- The server handles one socket's client messages in order and sends `subscribed` only after
  the subscription is in effect. So a client that waits for `subscribed` and then fetches the
  stored log misses nothing (it may see duplicates, which `LogViewer` already removes).

### One loop, two transports

Refactor the body of `events` in `internal/api/sse.go` (stream cap, replay in pages, skip
already-sent IDs, live loop, credential re-check every `pingInterval`) into one function that
both handlers call. It takes something that writes one event and something that sends a
keep-alive, and a filter that decides whether a live event is sent. SSE passes a filter that
sends everything. The exact shape is the builder's call. The SSE handler's behaviour and tests
must not change.

The socket handler runs two goroutines: the shared loop (writes) and a reader (client messages,
updating the socket's subscription set behind a mutex, and queueing the `subscribed` /
`unsubscribed` / `error` replies for the writer, since only one goroutine may write to the
connection). The keep-alive for the socket is a WebSocket ping (`Conn.Ping`), which also detects
a dead peer.

### Security

- **Origin.** A WebSocket upgrade is a GET, and `requireAuth` only applies the cross-site check
  (`sameOrigin`) to non-GET methods; `http.CrossOriginProtection` treats GET as safe and always
  passes it. So the existing check does **not** protect this route. Keep the library's own
  Origin check on (`websocket.Accept` refuses an `Origin` whose host differs from the request
  `Host`), and add the hosts from `public_url` and `allowed_hosts` to `OriginPatterns` so the
  UI works behind a reverse proxy. Never set `InsecureSkipVerify`. A test proves a cross-origin
  upgrade is refused.
- **Credentials.** The session cookie authenticates browsers, as now. The API token is accepted
  only in the `Authorization` header (for tests and tools), never in the query string: the
  `?token=` exception in `tokenMatches` stays limited to `/api/v1/events`.
- **Re-check.** As on SSE, the principal is re-checked (`stillAllowed`) every `pingInterval`;
  a signed-out or demoted user loses the socket within one interval.
- **Limits.** Sockets and SSE streams share the existing `maxEventStreams` cap (`s.streams`).
  Read limit 4 KiB per client message. At most 64 subscriptions per socket (more gets an
  `error`). Each write has a 10 s timeout, so a stuck browser is dropped and the bus never
  waits on it. A socket dropped by the bus for falling behind closes, and the client reconnects
  with `?after=`.
- The host guard and the auth middleware run before the upgrade, unchanged.
- CSP: `connect-src 'self'` matches same-origin `ws:` and `wss:` in current browsers. Verify in
  Chrome, Firefox and Safari; if one refuses, stop and report rather than loosen the CSP.

### Library

`github.com/coder/websocket`: small, no dependencies, context-aware, origin check built in. This
adds to `go.mod` (medium risk; the reason is above). The dogfood factory builds with
`GOPROXY=off`, so after merging, warm its module cache as `docs/getting-started.md` describes.

### Web client

`web/src/events.ts` keeps its public API (`eventStream.start/stop`, `subscribe`,
`useLiveStatus`, `invalidateFor`, `keysFor`) so `App.tsx` and `Overview.tsx` do not change.
Inside, `EventSource` becomes `WebSocket` (URL from `location`, `ws:` or `wss:` to match the
page), with the same backoff, `online` / `visibilitychange` retry, 401 check via `getStats`, and
full refetch on reconnect.

New: `watch(target: { run_id: string } | { conversation_id: string }): Promise<() => void>`.
It sends `subscribe` (reference-counted, so two components watching one run send it once),
resolves when `subscribed` arrives, and returns a function that releases it (`unsubscribe` when
the count reaches zero). On every reconnect the client re-sends `subscribe` for every live
watch, and the status becomes `live` only once every one of them is acknowledged with
`subscribed`. Until then the status stays `reconnecting`. This matters because components run
their catch-up fetch when the status turns `live`: a line written between that fetch and an
unacknowledged subscription would otherwise be lost until a reload, because polling is off while
`live`.

`LogViewer.tsx` keeps loading the stored log at once, as now, and calls `watch` in parallel; when
`watch` resolves, it runs its existing catch-up (`getRunLogs` after the highest seq it has), so
nothing falls between the load and the subscription (`mergeLogLines` drops duplicates). The load
must not wait for the socket: if the socket never opens (a proxy that drops upgrades), the panel
still shows the stored log and the existing 3 s poll while the stream is down still runs.
`Chat.tsx` follows the same pattern for its conversation.

### Docs to update in the same change

`docs/api.md` (the endpoint and message tables), `docs/openapi.yaml` (the route, so
`TestOpenAPISpecMatchesRoutes` passes), `web/src/types.ts` (the message types), `docs/design.md`
("HTTP + SSE" becomes "HTTP, SSE for the CLI, WebSocket for the UI"; the reverse-proxy note must
say to forward `Upgrade` and `Connection` headers).

## Review note

`internal/api/routes.go` is a high-risk file under the built-in rule; the owner approves the PR.

## Tests

Go, in a new file `internal/api/ws_test.go` (httptest server, `coder/websocket` client):

- replay with `?after=` then live delivery, no duplicates;
- `run.log` delivered only after `subscribe` for that run, and not after `unsubscribe`;
  same for `chat.delta` by conversation;
- `subscribed` arrives before the first event it covers;
- unknown kind, unknown field, both IDs, neither ID each get `error` and the socket stays open;
  the 65th subscription gets `error`;
- a cross-origin upgrade is refused; an allowed host from `allowed_hosts` is accepted;
- no credential is 401; `?token=` is refused on this route;
- a signed-out user loses the socket within one (shortened) `pingInterval`;
- sockets and SSE streams share the stream cap;
- the existing SSE tests pass unchanged.

Web, Vitest with a fake `WebSocket`: resume from last ID, re-subscribe on reconnect, reference
counting, `watch` resolves on `subscribed`, status stays `reconnecting` until every watch is
re-acknowledged, a 401 stops the stream.

By hand: a throwaway daemon on a spare port, in a real browser at desktop and phone width:
a run's log streams live, chat streams, a second tab watching another run gets none of the
first's log lines (check the frames in devtools), killing and restarting the daemon reconnects
and catches up, seven tabs all load, and with WebSocket upgrades blocked (e.g. a proxy that
drops them) a run page still shows its stored log and polls.

## Left to the builder

The shape of the shared loop, file layout inside `internal/api`, the Go type names for messages,
and the exact wording of `error` messages.
