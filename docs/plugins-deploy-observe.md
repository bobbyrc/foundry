# Deployers and observers

Foundry releases a project through a **deployer** and watches production through **observers**.
Both are chosen per project in `foundry.yaml` by their `type`.

## Deployers

### `command`

Runs shell commands (`sh -c`) in the project's local clone. Use it for any platform that has a CLI
or API you can call from a script. Each command is optional except `deploy`.

| Option     | Required | Default | Meaning                                                              |
|------------|----------|---------|----------------------------------------------------------------------|
| `deploy`   | yes      |         | Creates a release of the ref in `FOUNDRY_REF`.                       |
| `status`   | no       |         | Reports the state of a release identified by `FOUNDRY_DEPLOY_ID`.   |
| `rollback` | no       |         | Restores a previous release. Without it, rollback is an error.      |
| `timeout`  | no       | `30m`   | Kills each command after this long. Go duration syntax, e.g. `10m`. |

Environment: each command inherits foundry's environment, then the project's `env` map, then:

- `deploy`: `FOUNDRY_REF` (commit SHA to release)
- `status`: `FOUNDRY_DEPLOY_ID` (the ID returned by `deploy`)
- `rollback`: `FOUNDRY_ROLLBACK_ID` (the previous healthy release to restore) and
  `FOUNDRY_ROLLBACK_REF` (its commit SHA)

Output conventions. Foundry reads only the **last non-empty stdout line**. Earlier lines are
ignored, so print progress freely. On failure the error includes the tail of the output.

- `deploy` may end with a JSON object `{"id": "...", "url": "...", "state": "..."}`, or with a bare
  `https://` URL. Without an `id`, foundry generates one. Without a `status` command, the deploy is
  treated as finished and recorded as `ready` with its URL.
- `status` must end with a JSON object `{"state": "...", "url": "...", "message": "..."}`, or a bare
  state word. The states are `pending`, `building`, `ready`, `error` and `canceled`. Anything else is
  an error.
- A non-zero exit status from any command is a failure. The error includes the last 2 KB of output.

Vercel example. Vercel's `deploy` prints the deployment URL as its last line, so no status command
is needed:

```yaml
deploy:
  type: command
  options:
    deploy: vercel deploy --prod --yes
    rollback: vercel rollback $FOUNDRY_ROLLBACK_ID --yes
    timeout: 20m
```

For platforms that **deploy on push** (for example, a Vercel project connected to GitHub), foundry does
not create the release, so do not use `deploy` to trigger one. Use `type: noop`, which records
nothing and returns immediately, so the deploy watch still runs its health window. Or give a
`command` deployer a `status` command that finds the deployment for `FOUNDRY_REF` and reports its
state.

### `noop`

For projects released by something else, such as auto-deploy on push. `deploy` records nothing,
`status` always returns `ready`, and `rollback` does nothing. The health window and observers still
run after a merge, so a bad release is still caught.

```yaml
deploy:
  type: noop
```

## Observers

Each entry in a project's `observe` list is polled on the production watch interval. Each observer
has a `name`, which becomes the signal's `source`, and a `type`.

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

Example script that prints the last hour's errors from a log file as JSON Lines:

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
process environment. If neither has it, foundry refuses to start the observer and names the variable.
The token needs `project:read` on the project. Export it in the environment foundry runs in; never
put the token itself in `foundry.yaml`.

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
