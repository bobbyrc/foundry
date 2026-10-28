# Getting started

This walks you from a fresh checkout of foundry to a first merged pull request on a throwaway
repository. Do it on a test repository first. Foundry opens PRs, merges them and, if you configure
it, deploys, so the first run should touch nothing you care about.

Terms used here are defined in the [README](../README.md#terms-used-below). In short: a **task**
is a unit of work, a **run** is one agent attempt at it, a **role** picks the agent and its
instructions, and an **approval** is a decision foundry is waiting on from you.

## 1. Check the prerequisites

- `git`, and `gh` logged in to GitHub (`gh auth status`). Foundry uses `gh` to open and merge PRs.
- `mise`. The repository's `mise.toml` pins the Go version, and `mise exec --` runs commands with it.
- Node.js and npm. They are needed only to build the web UI.
- The agent CLIs you plan to use. The default `implementer` role runs on Codex, and the
  `orchestrator` and `reviewer` roles run on Claude Code. Install and log in to both (`codex` and
  `claude`), or change the roles to use only one.

## 2. Build foundry

From the repository root:

```sh
(cd web && npm install && npm run build)           # writes the UI into internal/api/webdist
mise exec -- go build -o foundry ./cmd/foundry     # embeds the UI into the binary
```

Build the UI first. The Go build embeds whatever is in `internal/api/webdist` at build time, and that
directory is empty in git. Without the UI build, the daemon runs but serves no web page. Move the
binary somewhere on your PATH, or keep using `./foundry`.

Check it:

```sh
./foundry version
./foundry help
```

## 3. Create a throwaway repository

```sh
gh repo create foundry-trial --private --add-readme --clone
cd foundry-trial
pwd    # note this path; you will need it in step 4
```

`--add-readme` gives the repository a `main` branch, which foundry expects by default.

## 4. Write the config

```sh
./foundry init --project trial --repo YOUR_GITHUB_USER/foundry-trial --path /absolute/path/to/foundry-trial
```

This writes `~/.config/foundry/foundry.yaml`. It refuses to overwrite an existing file. The starter
file has a `log` notifier and one project. Open it in your editor and add `paused: true` to the
`trial` project, so the factory starts nothing until you are ready:

```yaml
projects:
  trial:
    repo: "YOUR_GITHUB_USER/foundry-trial"
    path: "/absolute/path/to/foundry-trial"
    paused: true        # nothing starts for this project until you set this to false
```

`paused` is a key of the project, next to `repo` and `path`. The `--path` you pass must point at the
clone from step 3. Foundry runs agents there, so it must be a real git checkout.

Every other key is optional and has a default. `foundry.example.yaml` in the repository lists every
option with its default.

## 5. Start the daemon

In its own terminal:

```sh
./foundry serve
```

It keeps running and prints the address it listens on (`127.0.0.1:7777` by default). Leave it
running. Stop it with Ctrl-C.

In a second terminal:

```sh
./foundry status       # factory state, pending approvals, blocked tasks and running runs
./foundry open         # open the web UI, already signed in with the API token
```

`foundry status` reads "Factory: running" because the factory is not paused. The project is paused
separately, so no run for `trial` will start yet.

The CLI finds the daemon through the config file's `server` address and reads the token from the
daemon's data directory (`~/.local/share/foundry/token` by default). On another machine, set
`FOUNDRY_API` and `FOUNDRY_TOKEN` instead.

## 6. Create a small task while the project is paused

```sh
./foundry task create --project trial --kind chore \
  --title "Add a usage section to the README" \
  --body "Add a short section to README.md that says how to run the tests. Keep it to five lines."
```

The task is created and stays `queued`. A paused project is skipped when work is claimed, so nothing
runs yet. Check the task:

```sh
./foundry task list --project trial
```

## 7. Unpause the project and watch the run

Set `paused: false` for `trial` in `~/.config/foundry/foundry.yaml`, then reload the config:

```sh
./foundry config --reload
```

Foundry re-reads the file without a restart. From here the work runs on its own. Watch it:

```sh
./foundry status                      # "Running" lists the active run and its ID
./foundry run list --project trial    # every run, with status
./foundry run logs RUN_ID --follow    # stream the agent's output; Ctrl-C stops following
./foundry events --follow             # the live event stream, all projects
```

In the web UI, the Overview page shows active runs and the **Needs you** list. Tasks shows every
task and its status. Runs opens a run's full log.

A run ends with one of these outcomes:

- **Changes made**: foundry commits them, pushes a `foundry/...` branch, opens a PR and moves the
  task to `review`.
- **No changes needed**: the task is done, and the summary explains why.
- **Failed, or the agent reports `failed`**: foundry queues the task again for another attempt,
  until `max_attempts` is used up. Then the task is blocked. The same happens when CI fails or the
  reviewer asks for changes.
- **Denied or stalled**: the task is blocked at once, with the reason, and you are notified.

Once the PR exists, the PR shepherd waits for CI and runs a reviewer agent. Your trial repository
needs no CI. Foundry treats "no checks" as passing, so the reviewer is the only gate. A small docs
change is usually rated `low` risk. If the agent and the policy both agree, the PR merges on its
own after the reviewer approves. Otherwise it waits for you, as in step 8.

## 8. Answer approvals

A `medium` or `high` risk change does not merge on its own. The task moves to `awaiting_approval`,
and the request appears in the web UI's **Needs you** list and in:

```sh
./foundry approvals                                   # pending approvals
./foundry approve APPROVAL_ID                         # approve: the PR merges
./foundry approve APPROVAL_ID --reject --note "Split this into two PRs"
```

An approval covers one commit on the PR. If the PR changes after you were asked, the approval is
withdrawn and foundry checks the PR again.

## 9. Retry or cancel a blocked task

A blocked task has stopped and will not start again on its own. Look at the reason first:

```sh
./foundry task show TASK_ID           # shows the blocked reason and the run history
```

Then either give the agent guidance and retry, or cancel:

```sh
./foundry task retry TASK_ID --note "The README is at the repo root, not in docs/."
./foundry task cancel TASK_ID
```

The `--note` is added to the task body for the next attempt. Foundry also marks a task blocked after
`limits.max_attempts` (default 3) failed attempts.

## 10. Add a deployer (optional)

A deployer releases a project after its PR merges. Without one, a merge is the end of the task. Two
options:

**Nothing is released by foundry** (for example, the platform deploys on push). Use `noop`. Foundry
still runs the health window and the observers after each merge.

```yaml
projects:
  trial:
    # ...
    deploy:
      type: noop
```

**A shell command releases it.** Foundry runs the `deploy` command in the project's clone, with the
commit to release in `FOUNDRY_REF`. A failed health window runs `rollback` with the previous healthy
release in `FOUNDRY_ROLLBACK_ID`:

```yaml
projects:
  myapp:
    # ...
    deploy:
      type: command
      options:
        deploy: "vercel deploy --prod --yes"
        rollback: "vercel rollback $FOUNDRY_ROLLBACK_ID --yes"
      health_url: https://myapp.example.com/healthz   # must answer 2xx during verify_for
      verify_for: 10m          # watch a release this long before calling it healthy
      timeout: 30m             # fail a release still building after this long
      auto_rollback: true
```

Every option, and how deployers read command output, is in [plugins.md](plugins.md#deployers).

## 11. Add a Sentry observer (optional)

An observer reports production problems. The prod watch loop polls every observer (every 10 minutes
by default) and opens an `incident` task for each new problem above the threshold. The Sentry
observer needs an auth token with `project:read`:

```yaml
projects:
  myapp:
    # ...
    observe:
      - name: sentry-prod
        type: sentry
        options:
          org: acme
          project: myapp
          # token_env defaults to SENTRY_AUTH_TOKEN
```

Put the token in `secrets.env` (next step), not in the YAML. Details, including self-hosted Sentry
and the query option, are in [plugins.md](plugins.md#sentry).

## 12. Keep secrets out of the config

Foundry reads `~/.config/foundry/secrets.env` when `foundry serve` starts, before it reads the
config. Each line is `KEY=value`. Blank lines and lines starting with `#` are skipped, and one pair of
matching quotes around a value is removed.

```sh
mkdir -p ~/.config/foundry
touch ~/.config/foundry/secrets.env
chmod 600 ~/.config/foundry/secrets.env
code ~/.config/foundry/secrets.env    # add lines like SENTRY_AUTH_TOKEN=...
```

- A variable already set in the environment wins over the file.
- In the config, `${NAME}` is replaced with the value of `NAME`. If `NAME` is not set, `foundry serve`
  refuses to start and names the variable. A bare `$NAME` is left alone, so it can be used in shell
  commands such as the rollback above.
- Restart `foundry serve` after you change `secrets.env`. It is read only at startup.

## 13. Pause and resume

There are two switches:

```sh
./foundry pause      # the whole factory: nothing new starts, and loops stop acting
./foundry resume
```

For one project, set `paused: true` under it and run `./foundry config --reload`. Pausing does not
stop agents that are already running. They finish, and nothing new starts.

## Troubleshooting

- **`no config at ...: run foundry init`**: the CLI cannot find the config file. Run `foundry init`, or
  pass `--config` / set `FOUNDRY_CONFIG`.
- **`no token: set FOUNDRY_TOKEN or run on the machine running foundry serve`**: the CLI cannot read
  the token. Run the command on the machine that runs the daemon, or set `FOUNDRY_API` and
  `FOUNDRY_TOKEN`.
- **`undefined environment variable ${NAME}`**: the config refers to a variable that is not set.
  Add it to `secrets.env` and restart `foundry serve`.
- **A task is `blocked` with a denial**: read the run log with `./foundry run logs RUN_ID`. The agent's
  tool call was refused. Fix the cause (for example, a missing permission or a path outside the
  checkout) and retry with a note.
