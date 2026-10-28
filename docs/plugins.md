# Plugins

Foundry has four kinds of plugin. Each kind is a Go interface with built-in implementations, and
you pick one per harness, project or notifier by its `type` in `foundry.yaml`:

- **Harnesses** run an agent CLI. Built in: `claude-code`, `codex`. Any other program: the `exec`
  type.
- **Deployers** release a project after a merge. Built in: `command` (shell commands) and `noop`.
  Deployers have no `exec` type; the `command` type plays that role.
- **Observers** report production problems. Built in: `sentry`. Any program: the `exec` type.
- **Notifiers** tell you about things that need attention. Built in: `log`, `ntfy`. Any program: the
  `exec` type.
- **Intake sources** turn an external queue of work into tasks. There are no built-in types: any
  program that prints JSON lines can be one. See [Intake sources](#intake-sources).

The `exec` types let you add a plugin in any language, without recompiling foundry. The source
interfaces are in `internal/harness`, `internal/deploy`, `internal/observe` and `internal/notify`.
The only source control implementation is GitHub (`internal/scm/github`), so there is no SCM plugin.

Every plugin is configured in `foundry.yaml`. The examples below are fragments of that file.

## Harness plugins (exec)

A harness runs one coding-agent CLI non-interactively and turns its output into a stream of events.
The `exec` harness runs any program as that CLI. Use it for an agent foundry does not ship with.

### Registering a plugin

Add a harness block to `foundry.yaml`. `command` is the program and its fixed leading arguments.
`args` and `env` are added to every session of that harness.

```yaml
harnesses:
  mytool:
    type: exec
    command: ["/path/to/plugin"]
    args: ["--quiet"]
    env:
      MYTOOL_HOME: /opt/mytool
```

A role that names `mytool` as its harness runs through this plugin:

```yaml
agents:
  implementer:
    harness: mytool
    model: fast-model        # passed to the plugin as FOUNDRY_MODEL
    prompt: builtin:implementer
```

### What foundry gives the plugin

For each session foundry starts the command with:

| Source                       | What                                                                                     |
|------------------------------|------------------------------------------------------------------------------------------|
| argv                         | `command`, then the harness `args`, then the role's `args`                               |
| working directory            | the run's git worktree                                                                   |
| stdin                        | the prompt (the same text as in `FOUNDRY_PROMPT_FILE`)                                   |
| `FOUNDRY_PROMPT_FILE`        | path to a file holding the task prompt                                                   |
| `FOUNDRY_INSTRUCTIONS_FILE`  | path to a file holding the system instructions (role, rules, outbox contract); empty if none |
| `FOUNDRY_MODEL`              | the model name from the role; empty if none                                              |
| `FOUNDRY_RESUME_SESSION`     | a session ID to continue; empty for a new session                                        |
| `env`                        | the harness `env`, then the role `env`, added to the inherited environment               |

The two prompt files are temporary. Foundry deletes them once the process has exited and its
result has been collected. The plugin must not expect them to exist after it exits.

Everything the agent does must happen inside the working directory. The agent reports back to
foundry through `.foundry-run/result.json` in the worktree (the outbox, described in
[design.md](design.md)). The protocol below is only for the live stream.

### Output protocol

The plugin writes to stdout, one JSON object per line. Each object has a `type`:

| `type`        | Meaning                                  | Fields used                       |
|---------------|------------------------------------------|-----------------------------------|
| `text`        | assistant prose                          | `text`                            |
| `tool_use`    | the agent called a tool                  | `text`, for example `shell: ls`   |
| `tool_result` | a tool returned                          | `text`                            |
| `denial`      | a tool call was refused                  | `text`, the refused call          |
| `system`      | bookkeeping shown in the log             | `text`                            |
| `error`       | something failed                         | `text`                            |
| `result`      | the final line; see below                | the fields below                  |

Any object may also carry `session_id`. Foundry remembers the latest one and returns it in the
result, so a later run can resume the session.

Lines that are not JSON are shown as `text`. Lines with an unknown `type` are shown as `system`.

The final line is `type: "result"`:

```json
{"type":"result","session_id":"abc123","text":"Fixed the parser and added tests.","cost_usd":0.12,"input_tokens":5400,"output_tokens":900,"is_error":false,"denials":[]}
```

| Field           | Meaning                                                              |
|-----------------|----------------------------------------------------------------------|
| `text`          | the agent's final message                                            |
| `session_id`    | the session to resume later                                          |
| `cost_usd`      | total cost in US dollars, if known                                   |
| `input_tokens`, `output_tokens` | token counts, if known                                 |
| `is_error`      | true if the run failed                                               |
| `denials`       | descriptions of refused tool calls; a denial stops the run's task    |

If the plugin exits without a result line, foundry uses the last `text` line as the final message.
The run is an error if the exit code is non-zero. The exit code is always recorded.

### Minimal example

This plugin wraps a hypothetical CLI, `mytool`, that prints its progress as plain text and accepts
the prompt on stdin. It needs `jq` to quote the progress lines:

```sh
#!/bin/sh
# /path/to/plugin: run mytool on the prompt and report in the exec protocol.
prompt=$(cat)   # the prompt arrives on stdin; it is also in $FOUNDRY_PROMPT_FILE
out=$(mktemp)
printf '{"type":"system","text":"mytool starting"}\n'
if mytool --model "${FOUNDRY_MODEL:-default}" "$prompt" >"$out" 2>&1; then
	failed=false
else
	failed=true
fi
jq -Rc '{type: "text", text: .}' "$out"
rm -f "$out"
printf '{"type":"result","text":"mytool finished","is_error":%s}\n' "$failed"
```

A real plugin can be as small as one line per event. Keep stdout for protocol lines only, and send
diagnostics to stderr. Stderr is shown in the run log as `stderr` lines.

## Deployers

A deployer releases a project after its PR merges. It is chosen per project by `deploy.type`. A
project without a `deploy` block has no deployer. Merged work is then not released, and foundry only
records the merge.

The deploy watch runs these steps for each merged change:

1. Calls `deploy` (for the `command` type), or does nothing (for `noop`), to create a release of the
   merge commit.
2. Polls the release until it is `ready`. A release that reports `error` or `canceled`, or that is
   still not ready after the deploy-level `timeout`, fails. Nothing new is serving yet, so a failed
   build is not rolled back.
3. Watches a health window for `verify_for` (default 10m). `health_url`, if set, must answer 2xx.
   Three failed health checks in a row, or a new production problem during the window, fail the
   release. A clean window marks it healthy.
4. A release that fails during the window is rolled back to the previous healthy release when
   `auto_rollback` is on (the default). Foundry then records the rollback, opens an `incident` task
   and notifies you.

These keys belong to the project's `deploy` block, not to the deployer's `options`:

```yaml
deploy:
  type: command
  options: { ... }           # type-specific, see below
  health_url: https://myapp.example.com/healthz   # empty skips the HTTP check
  verify_for: 10m            # how long a release is watched before it is called healthy
  timeout: 30m               # a release still building after this long is failed
  auto_rollback: true
```

Evidence for incidents. A release keeps the tail of its deploy command's output (the `command` type
keeps the last 4 KB of stdout and stderr). When a release fails, or a production problem opens an
incident, foundry also runs the project's `deploy.logs` command (`sh -c`, in the project's clone, 30
second limit) and keeps its last 8 KB, or the reason it failed. Both go into the incident's
`## Evidence` section. A failing logs command is reported there and does not stop the release from
being handled.

### `command`

Runs shell commands (`sh -c`) in the project's local clone. Use it for any platform that has a CLI or
API you can call from a script.

A `deploy` or `rollback` command may start long-running processes, such as a service in the
background. They keep running after the command succeeds. If the command fails or hits its
`timeout`, foundry kills them. Redirect a background process's output (`> app.log 2>&1 &`) so it
does not hold the command's output pipe open.

| Option     | Required | Default | Meaning                                                              |
|------------|----------|---------|----------------------------------------------------------------------|
| `deploy`   | yes      |         | Creates a release of the ref in `FOUNDRY_REF`.                      |
| `status`   | no       |         | Reports the state of a release identified by `FOUNDRY_DEPLOY_ID`.   |
| `rollback` | no       |         | Restores a previous release. Without it, rollback is an error.      |
| `timeout`  | no       | `30m`   | Kills each command after this long. Go duration syntax, e.g. `10m`. |

Do not confuse the option `timeout` with the deploy-level `timeout` above. The option kills one
command that runs too long. The deploy-level setting fails a release that is still building after
that long.

Environment: each command inherits foundry's environment, then the project's `env` map, then:

- `deploy`: `FOUNDRY_REF` (the commit SHA to release).
- `status`: `FOUNDRY_DEPLOY_ID` (the ID that `deploy` returned).
- `rollback`: `FOUNDRY_ROLLBACK_ID` (the previous healthy release to restore) and
  `FOUNDRY_ROLLBACK_REF` (its commit SHA).

The `status` command gets only `FOUNDRY_DEPLOY_ID`. It does not get `FOUNDRY_REF`, so it cannot look
up a deployment by commit. A command that needs the commit must do that work in `deploy`.

Output conventions. Foundry reads only the **last non-empty stdout line**. Earlier lines are
ignored, so print progress freely. On failure the error includes the tail of the output.

- `deploy` may end with a JSON object `{"id": "...", "url": "...", "state": "..."}`, or with a bare
  `https://` URL. Without an `id`, foundry generates one. Without a `status` command, the deploy is
  treated as finished and recorded as `ready` with its URL.
- `status` must end with a JSON object `{"state": "...", "url": "...", "message": "..."}`, or a bare
  state word. The states are `pending`, `building`, `ready`, `error` and `canceled`. Anything else is
  an error.
- A non-zero exit status from any command is a failure. The error includes the last 2 KB of output.

Vercel example. Vercel's `deploy` prints the deployment URL as its last line, so no status command is
needed:

```yaml
deploy:
  type: command
  options:
    deploy: vercel deploy --prod --yes
    rollback: vercel rollback $FOUNDRY_ROLLBACK_ID --yes
    timeout: 20m
```

**Platforms that deploy on push** (for example, a Vercel project connected to GitHub) release the
merge themselves. For these, `deploy` must not create a second release. Use one of two patterns:

- `type: noop`. Foundry records nothing and still runs the health window and the observers after the
  merge. This is the simple choice.
- A `command` deployer whose `deploy` command finds the platform's deployment for `FOUNDRY_REF` and
  prints its `{"id": ...}`, and whose `status` command reports that deployment's state from
  `FOUNDRY_DEPLOY_ID`. The `deploy` command is required and runs on every release, so it must be
  the lookup, not a create.

### `noop`

For projects released by something else, such as auto-deploy on push. `deploy` records nothing,
`status` always returns `ready`, and `rollback` does nothing. The health window and observers still
run after a merge, so a bad release is still caught.

```yaml
deploy:
  type: noop
```

## Observers

Each entry in a project's `observe` list is polled on the production watch interval (`loops.prod_watch.every`,
default 10m). Each observer has a `name`, which becomes the signal's `source`, and a `type`:

```yaml
observe:
  - name: sentry-prod        # unique within the project
    type: sentry
    options: { ... }         # type-specific
```

Each new problem above the project's threshold (`loops.prod_watch.min_level` and `min_count`) opens an
`incident` task for the `sre` role. Problems are deduplicated by fingerprint. A problem that keeps
firing after its fix merged reopens.

### `exec`

Runs a shell command that prints the production problems it sees. Foundry passes the time of the
last poll in `FOUNDRY_SINCE` (RFC 3339, UTC).

| Option    | Required | Default | Meaning                                        |
|-----------|----------|---------|------------------------------------------------|
| `command` | yes      |         | Shell command that prints observations.        |
| `timeout` | no       | `60s`   | Kills the command after this long.             |

Stdout is either a JSON array of observations, or JSON Lines with one per line. The first
non-space character tells them apart. Each observation has these fields:

```json
{"fingerprint": "sentry-111", "title": "TypeError: nil", "level": "error", "count": 12,
 "url": "https://example.com/issues/111", "culprit": "app.handler",
 "first_seen": "2026-01-01T00:00:00Z", "last_seen": "2026-01-02T12:00:00Z"}
```

Observations with an empty `fingerprint` are dropped. The fingerprint identifies a problem, so use
something stable, such as the error group ID. A non-zero exit is an error and includes the output
tail.

Example script that prints the errors written to a log file since the last poll, as JSON Lines:

```sh
#!/bin/sh
# Print one observation per error line written since FOUNDRY_SINCE.
# Assumes lines look like: 2026-01-02T12:00:00Z ERROR <fingerprint> <message>
since="$FOUNDRY_SINCE"
awk -v since="$since" '$2 == "ERROR" && $1 >= since {
  printf "{\"fingerprint\":\"%s\",\"title\":\"%s\",\"level\":\"error\",\"count\":1,\"last_seen\":\"%s\"}\n", $3, $4, $1
}' /var/log/myapp/app.log
```

Configured as `command: /usr/local/bin/myapp-errors.sh`. Comparing timestamps as strings works because
RFC 3339 UTC sorts lexically.

### `sentry`

Reads a Sentry project's unresolved issues through the REST API and reports those last seen since the
previous poll. It reads the issue list only; it never resolves or changes issues.

| Option      | Required | Default               | Meaning                                                                 |
|-------------|----------|-----------------------|-------------------------------------------------------------------------|
| `org`       | yes      |                       | Sentry organization slug.                                               |
| `project`   | yes      |                       | Sentry project slug.                                                    |
| `base_url`  | no       | `https://sentry.io`   | Use for self-hosted Sentry.                                             |
| `token_env` | no       | `SENTRY_AUTH_TOKEN`   | Name of the variable that holds the auth token (see below).             |
| `query`     | no       | `is:unresolved`       | Sentry issue search query.                                              |

The token is read from the project's `env` map under the name in `token_env`, then from foundry's
process environment. Foundry's process environment includes `~/.config/foundry/secrets.env`, so put
the token there. If neither has it, foundry refuses to start the observer and names the variable.
The token needs `project:read` on the project. Never put the token itself in `foundry.yaml`.

```yaml
observe:
  - name: sentry
    type: sentry
    options:
      org: acme
      project: web
      query: "is:unresolved level:error"
```

Each issue becomes an observation with fingerprint `sentry-<issue id>`, the issue title, level,
event count, permalink, culprit and first and last seen times. Issues last seen before `since` are
skipped.

## Notifiers

A notifier sends a message when something needs your attention: a blocked task, a rollback, an
approval. The `notify` list in `foundry.yaml` can hold several notifiers. Each one receives the
messages at or above its own `min_priority`.

```yaml
notify:
  - type: ntfy
    options:
      topic: foundry-example-7f3a
    min_priority: normal
  - type: log
```

`min_priority` is `low`, `normal` or `high`. Empty means `normal`. Blocked work, rollbacks and
approvals are `high`. If a notifier fails, foundry logs a warning and carries on. Notifications are
also recorded as events, so they show in the web UI's Activity feed.

### `log`

Writes each notification to the daemon's log, as an info-level `notification` line. It has no
options. `foundry init` adds it by default, so the notifications are visible even with no other
channel.

### `ntfy`

Publishes to a topic on [ntfy](https://ntfy.sh) for push notifications on your phone or desktop.

| Option      | Required | Default               | Meaning                                                                 |
|-------------|----------|-----------------------|-------------------------------------------------------------------------|
| `topic`     | yes      |                       | The topic to publish to. Anyone who knows the topic can read it, so pick a hard-to-guess name. |
| `server`    | no       | `https://ntfy.sh`     | Use for a self-hosted ntfy server.                                      |
| `token_env` | no       |                       | Name of an environment variable holding an access token, sent as a Bearer token. |

Priority maps to ntfy's scale: `low` is 2, `normal` is 3 and `high` is 5. The notification's title
and click URL (a link into the web UI) travel as headers.

### `exec`

Runs a shell command (`sh -c`) for each notification, so you can send to any channel with a script or
CLI. Foundry passes the message in environment variables:

| Variable                  | Value                                                  |
|---------------------------|--------------------------------------------------------|
| `FOUNDRY_NOTIFY_TITLE`    | the notification title                                 |
| `FOUNDRY_NOTIFY_BODY`     | the body text                                          |
| `FOUNDRY_NOTIFY_PRIORITY` | `low`, `normal` or `high`                              |
| `FOUNDRY_NOTIFY_URL`      | a link into the web UI, possibly empty                 |

| Option    | Required | Default | Meaning                                                       |
|-----------|----------|---------|---------------------------------------------------------------|
| `command` | yes      |         | The shell command to run for each notification.               |

The command has 30 seconds. A non-zero exit is an error, and it includes the command's output.

```yaml
notify:
  - type: exec
    options:
      command: 'my-notify-script --title "$FOUNDRY_NOTIFY_TITLE" --url "$FOUNDRY_NOTIFY_URL"'
    min_priority: high
```

## Intake sources

An intake source turns any external queue of work into foundry tasks: a phone app's inbox, a ticket
system, a chat bot, or a plain file. Foundry runs each source's command for every project on the
`exec_intake` loop (every 1m by default) and creates one task per new item the command prints.

Sources are listed under a project in `foundry.yaml`:

```yaml
projects:
  myapp:
    intake:
      - name: inbox                    # lowercase letters, digits, - and _; unique per project
        command: cat ~/foundry-inbox.jsonl
        kind: feature                  # default kind for items that name none (default feature)
        timeout: 60s                   # for one run of the command (default 60s)
```

### The protocol

- The command runs with `sh -c` in the project's clone, or in the data directory if the clone does
  not exist yet. It gets foundry's environment, the project's `env`, and `FOUNDRY_PROJECT` set to
  the project name.
- It prints zero or more items on stdout, one JSON object per line. Blank lines are ignored.

| Field      | Required | Meaning                                                                          |
|------------|----------|----------------------------------------------------------------------------------|
| `id`       | yes      | Your identifier for the item. Items are deduplicated by it (see below).          |
| `title`    | yes      | The task title.                                                                  |
| `body`     | no       | The task body. Foundry adds a footer naming the source and the id.               |
| `kind`     | no       | `feature`, `bugfix`, `chore`, `improve` or `question`. Anything else, or none, uses the source's `kind`. A line can never create a `review` or an `incident`. |
| `priority` | no       | Integer. Higher runs first.                                                      |
| `role`     | no       | An agent role name, as in `routing`. Empty uses the kind's routing.             |

```json
{"id": "2026-10-28-1", "title": "Add a /healthz endpoint", "body": "It returns 200 when the DB is up.", "kind": "feature", "priority": 1}
```

- A line that is not JSON, or lacks `id` or `title`, is skipped. Each run logs one warning that counts
  the skipped lines. The run still succeeds.
- A non-zero exit or a timeout is an error for that source. Its items from that run are not used,
  and the error shows as the loop's last error in the web UI. Other sources still run.
- Stdout is kept up to 1 MB; the rest is dropped. The last 4 KB of stderr is kept for errors.

### Dedupe by id

Each task records the source ref `intake:<name>:<id>`. When an item's ref already has a task, in any
state, the item is skipped. So the command may print its whole queue on every run: only new ids
create tasks. To queue the same work again, give it a new id.

### Example: a file queue

Append one JSON object per line to `~/foundry-inbox.jsonl`. Each line becomes a task within a
minute. Lines already in the file are not queued again, so keep the file append-only.

```yaml
intake:
  - name: inbox
    command: cat ~/foundry-inbox.jsonl
```

### Example: the Herdr phone app (sketch)

This is a sketch, not a tested integration. The `herdr gram` command and its JSON fields `id` and
`text` are assumptions. Verify them with `herdr gram list --queue` on your version before relying on
this. The script claims each message, so the phone app no longer shows it, and then prints it as an
item. A message that fails to claim is left in the queue and not printed.

```sh
#!/bin/sh
# SKETCH: verify the field names (.result.messages[], .id, .text) against your herdr version.
queue=$(herdr gram list --queue) || exit 1
echo "$queue" | jq -c '.result.messages[]' | while read -r msg; do
  id=$(echo "$msg" | jq -r '.id')
  herdr gram grab "$id" --as foundry >/dev/null 2>&1 || continue
  echo "$msg" | jq -c '{id: .id, title: (.text | split("\n")[0]), body: .text}'
done
```

Claiming a message before foundry creates its task means a crash in between loses the message. If
that matters, print the items first and claim them after a successful run.
