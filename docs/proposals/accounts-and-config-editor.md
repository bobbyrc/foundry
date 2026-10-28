# Proposal: accounts, sign-in and the in-app config editor

Status: approved by the owner, 2026-10-28, and being built on branch `accounts-config`. The API
token keeps today's powers (including approving merges from chat or the CLI) except config and
user changes. Decisions made with the owner are marked **(decided)**.

## Why

Two things the owner asked for, which turn out to be one change:

1. Edit `onlet.yaml` from the web UI, with a visual editor by default and the raw file as an
   option.
2. Sign in with a username and password instead of pasting the API token.

They belong together because of the safety model. Today the browser, the CLI and the
orchestrator agent all hold the same API token (`<data_dir>/token`; the orchestrator gets it as
`ONLET_TOKEN`). A config-write endpoint behind that token would let the orchestrator agent
rewrite its own policy (lowering risk, breaking "risk only goes up") or add an `inherit_env`
entry that hands itself credentials (breaking "agents get no credentials"). So config writes
must need something agents never hold: a person signed in with a password.

## Who can do what

Two kinds of caller, told apart by how they authenticate:

| Caller | Authenticates with | Can |
|---|---|---|
| CLI and orchestrator agent | `Authorization: Bearer <API token>` (unchanged) | everything it can today; **never** config writes or user management |
| A person in the browser | session cookie from signing in | what their role allows |

Roles **(decided: users have roles)**:

| Role | Read pages and events | Act (approve, retry, cancel, pause, create tasks, chat, run loops, reload config) | Edit config, manage users |
|---|---|---|---|
| viewer | yes | no | no |
| operator | yes | yes | no |
| owner | yes | yes | yes |

There must always be at least one owner: removing or demoting the last owner is refused.

The browser signs in with a password only **(decided)**: the Connect page and `#token=` links go
away. The token keeps working for the CLI and for `GET /events?token=` (the orchestrator does not
use it, but scripts may).

## Sign-in

- **First run (decided: open setup page).** While no user exists, the UI shows "Create the owner
  account" (username, password twice). The first account is always an owner. The server creates
  it only if the users table is still empty, inside one transaction, so two racing visitors
  cannot both succeed. Risk, accepted by the owner: on a remotely reachable instance, whoever
  reaches it first claims it. The default bind is loopback, which limits this to the machine
  itself; `docs/getting-started.md` will say to create the account before exposing onlet.
- **Passwords** are hashed with PBKDF2-HMAC-SHA256 from Go's standard library
  (`crypto/pbkdf2`), 600,000 iterations, 16-byte random salt, stored as
  `pbkdf2-sha256$600000$<salt>$<hash>` (base64), so the iteration count can rise later. Minimum
  length 12. No new dependency.
- **Sessions**: a 32-byte random value in an `HttpOnly`, `SameSite=Strict` cookie named
  `onlet_session`, `Secure` when `public_url` is https. The database stores only its SHA-256,
  with the user, created and expiry times. Sessions last 30 days; signing out deletes the row;
  changing a password deletes that user's other sessions.
- **Cross-site requests**: state-changing requests that carry the cookie must pass Go's
  `http.CrossOriginProtection` (checks `Sec-Fetch-Site` / `Origin`). Bearer-token requests are
  not browser requests and skip it.
- **Guessing**: after 5 failed sign-ins for one username within 15 minutes, further attempts for
  that name answer 429 until the window passes (in memory; a restart clears it). Unknown
  usernames cost the same hashing time as known ones.
- **Forgotten password**: `onlet user reset-password <name>` on the machine, which writes the
  database directly (shell access to the machine already means full control of onlet).

## Config editing

The file stays the source of truth **(design principle 4)**; the UI edits the file.

- **Visual editor (default).** The server sends the file as a document tree *before* `${NAME}`
  expansion, so references show as `${GITHUB_TOKEN}`, never as their values, plus a schema of
  every setting: its path, type (string, number, boolean, duration, list, map of named entries,
  section) and help text. The help text comes from the doc comments in
  `internal/config/config.go` itself (the file is embedded and read with `go/parser`), so it can
  never drift from the code. The UI renders forms from the schema: a section per top-level key,
  and a list with add/rename/remove for named maps such as projects, agents and harnesses.
- **Saving a visual edit** sends a list of changes (`set` a path to a value, `delete` a path).
  The server applies them to the parsed YAML tree, which keeps comments, then encodes it.
  Formatting may be normalised (indentation, quoting); comments and key order are kept.
- **Raw editor (option).** A text editor on the file as written. Saving sends the whole text.
- **Every save**, either way: the request carries the version (SHA-256) of the file it was based
  on; if the file changed since, the save is refused with 409 and the UI offers to reload. The new
  text is parsed and validated exactly like a load (`config.Parse`: unknown keys and invalid
  values are errors, listed by setting path) before anything is written. Then it is written
  atomically (temp file and rename, as `onlet project add` already does) and the running
  daemon reloads it. Settings read only at start-up (`server.addr`, `data_dir`, `allowed_hosts`,
  `notify`) are reported back as "takes effect after a restart".
- **Who**: only an owner session reads the raw file or the document tree, and only an owner
  session saves. Viewers and operators keep today's redacted, read-only view.

## API (frozen for implementation)

All under `/api/v1`, JSON, unknown fields rejected.

| Method and path | Who | Body | Response |
|---|---|---|---|
| `GET /auth/state` | anyone | | `{"setup_required": bool, "user": User\|null}` |
| `POST /auth/setup` | anyone, only while no user exists | `{"username","password"}` | `User` and sets cookie; 409 once a user exists |
| `POST /auth/login` | anyone | `{"username","password"}` | `User` and sets cookie; 401 bad credentials; 429 locked |
| `POST /auth/logout` | session | | 204, clears cookie |
| `POST /auth/password` | session | `{"current","new"}` | 204 |
| `GET /users` | owner | | `[User]` |
| `POST /users` | owner | `{"username","password","role"}` | `User` |
| `POST /users/{name}/role` | owner | `{"role"}` | `User`; 409 for the last owner |
| `POST /users/{name}/password` | owner | `{"password"}` | 204 |
| `DELETE /users/{name}` | owner | | 204; 409 for the last owner |
| `GET /config/document` | owner | | `{"path","version","document","schema"}` |
| `PATCH /config/document` | owner | `{"version","changes":[{"op":"set"\|"delete","path":[...],"value":any}]}` | `ConfigSaved` |
| `GET /config/file` | owner | | `{"path","version","text"}` |
| `PUT /config/file` | owner | `{"version","text"}` | `ConfigSaved` |

`User` is `{"username","role","created_at"}`. `ConfigSaved` is `{"version","restart_required":[paths]}`.
Validation failures answer 422 `{"error": "...", "problems": [{"path","message"}]}`.
`GET /health` stays open; every other existing route keeps working with the token and now also
with a session (viewer: GET only; operator and owner: all of today's routes).

## What stays open

- The boundary is the API's. Agents run as the owner's OS user, so an agent that can write
  `onlet.yaml` or `onlet.db` directly can still change them (then `POST /config/reload` is
  an Operate action). Closing that needs agent isolation (`docs/proposals/agent-isolation.md`).
- The first-run setup page is open until the owner account exists (accepted).
- The session cookie is marked Secure from `public_url` or a direct TLS connection; behind an
  HTTPS proxy, `public_url` must be set to the https address.

## What changes in the rules

- AGENTS.md invariant 11 ("needs the bearer token") becomes "needs the bearer token or a signed-in
  session"; a new invariant: "Config and users change only through a signed-in owner session. The
  API token, which agents can hold, can never change them."
- README **Safety model** and `docs/design.md` (the managed-version seam "the single API token
  becomes accounts and sessions" is now built) are updated in the same change.

## Build plan

Units with disjoint files, after this proposal is approved:

1. Store: migration 2 (`users`, `sessions`), `domain.User`, store methods.
2. `internal/auth`: password hashing, session tokens, roles and permission checks, sign-in limiter.
3. `internal/config`: document tree, change application on the YAML tree, schema with help text,
   atomic write.
4. API: middleware (cookie or bearer, role per route, cross-origin check), the routes above,
   `docs/api.md`, `docs/openapi.yaml`.
5. Web: setup and sign-in pages, session handling, users page, role-aware actions.
6. Web: the config page (visual editor and raw tab).
7. CLI `onlet user reset-password`, docs, invariants.
