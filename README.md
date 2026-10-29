# onett

Onett is a personal software factory. It is a local daemon (a background program that keeps
running) that hands work to coding agents: Claude Code and Codex today, and any other agent CLI
through a plugin. For each piece of work it runs the agent in its own git checkout, opens a pull
request, waits for CI and an agent review, and applies a risk policy that decides whether the change
may merge without you. After a merge it can deploy, check the deploy, and roll back if the check
fails. It also watches production for errors and files the problems it finds as issues. You steer it
by chat, from a web UI, or with the `onett` command.

## Terms used below

- **Task**: one unit of work for one project, such as a feature, a bug fix or an incident.
- **Role** (agent profile): a named recipe for who does a task: which agent CLI runs it, which
  model, and which instructions. Built-in roles are `orchestrator` (the agent you chat with),
  `implementer`, `reviewer`, `triager`, `sre` and `improver`. You can edit a built-in role, turn it
  off with `disabled: true` (it cannot be turned off while a route still uses it, and the
  orchestrator cannot be turned off), or add a custom role. A role's instructions are a Markdown
  file (`prompt`) or written inline in the config (`prompt_text`). Inline instructions cannot use
  `${NAME}` references, because they are given to the agent, which never gets credentials.
- **Harness**: the adapter that runs one agent CLI non-interactively and streams its output back.
- **Run**: one attempt at a task by one agent session, in its own git worktree (a second checkout
  of the repository on its own branch).
- **Policy and risk**: rules that look at a diff and rate it `low`, `medium` or `high`. The policy
  decides which levels may merge on their own.
- **Loop**: a recurring background job, such as checking open PRs or polling production.
- **Outbox**: a folder in each worktree where the agent writes its report, `result.json`.
- **Approval**: a request for your decision, such as merging a `medium` or `high` risk PR.

## How a task flows

1. A task is created from chat, the CLI, the web UI, a GitHub issue with the `onett` label, a
   production error, or a friction report. It waits as `queued`.
2. The scheduler starts a run. The agent works in the worktree and writes its report. Agents never
   commit; onett does.
3. If the run changed code, onett commits it, pushes a `onett/...` branch and opens a PR. The
   task moves to `review`.
4. The PR shepherd (the loop that moves PRs along) waits for CI, starts a `reviewer` run when the
   policy requires one, and then asks the policy. `low` risk merges automatically once the reviewer
   approves. Otherwise the task
   moves to `awaiting_approval` and waits for you.
5. After a merge, a project with a deployer goes through `deploying`. A health window (default 10
   minutes) checks the release. A failed check rolls back, opens an `incident` task and notifies you.
6. The task becomes `done`.

What waits for you: approvals (`onett approvals`, then `onett approve`) and `blocked` tasks. A
task is blocked when a tool call is denied, when a run stalls, when the agent reports that it needs a
human, or when it has used up `max_attempts`. Retry it with a note, or cancel it.

## Safety model

- **Agents judge, code gates.** Agents write code, review diffs and triage issues. Deterministic Go
  code does everything that leaves the machine: commits, pushes, PRs, merges, deploys and rollbacks.
- **Agents cannot approve their own work.** Worker agents get no API token. Only the orchestrator
  does, and it uses the CLI.
- **People sign in; config and users change only through them.** The web UI signs people in with
  a username and password (roles: viewer reads, operator also acts on work, owner also edits
  `onett.yaml` and manages users). The API token, which the CLI and the orchestrator hold, can
  read and act but can never edit the config or users, so an agent cannot rewrite its own policy
  or environment. The first visit to a new instance creates the owner account, so create it
  before you make onett reachable from other machines. Behind an HTTPS proxy, set
  `server.public_url` to the `https://` address so the session cookie is marked Secure. This
  boundary is the API's: agents still run as your user, so one that can write `onett.yaml` or
  the database file directly is limited only by its harness until agents are isolated (see
  [docs/proposals/agent-isolation.md](docs/proposals/agent-isolation.md)).
- **Agents get a small environment.** An agent sees an allowlist (`PATH`, `HOME`, `USER`, `LANG`,
  ...), not onett's own environment, so `GH_TOKEN` and provider tokens do not reach it. To pass
  one through, name it in the harness's `inherit_env` list. Agents still run as your user and can
  read what your user can read; isolating them is an open decision, see
  [docs/proposals/agent-isolation.md](docs/proposals/agent-isolation.md).
- **Self-assigned work waits for you.** Follow-up and retro tasks (task sources `followup` and
  `retro`) are rated at least `medium` by a built-in policy rule, and so is any change to onett's
  own gates and agent instructions (`high`). Add rules with a policy's `extra_rules`. Setting
  `rules` instead **replaces** all the built-in rules, so copy back any you want to keep.
- **Daily budget.** `limits.daily_budget_usd` stops new task runs once today's reported cost
  reaches it. A run's cost is recorded when it ends, so runs in flight count only after they finish
  and the total can overshoot. Codex reports no cost, so it counts only if its
  harness has a `price` (per-million-token rates), which gives an estimate shown as `~$0.42`.
- **Risk only goes up.** Final risk is the highest of the rules the diff matches and the agent's own
  rating. An agent cannot lower it.
- **Merges are pinned to a commit.** A merge uses `gh pr merge --match-head-commit`, so it merges the
  exact commit that CI and review judged. An approval covers one commit. If the PR changes, the
  approval is withdrawn and the PR is checked again.
- **Denials and stalls stop, they do not retry.** A denied tool call, or no output for
  `limits.stall_timeout` (default 15 minutes), ends the run and blocks the task with the reason.
- **Pause switch.** `onett pause` stops new runs and loop actions. Running agents finish. A
  single project pauses with `onett project pause NAME` (`resume` undoes it) or on the web UI's
  Projects page; this survives restarts, but a `paused: true` in `onett.yaml` can only be cleared
  in the file. While paused, the `deploy_watch` and `prod_watch` loops keep running, so a release
  already in flight is still health-checked and rolled back.
- **Project setup and checks.** A project's `setup` commands run in each fresh worktree before the
  agent starts (install dependencies). Its `checks` (name, `run` shell command, optional `timeout`,
  default 10m) run after the agent finishes and before onett commits and pushes. A failing check
  re-queues the task with its output, like a CI failure, without spending a CI run. Both run with
  the agent's environment, not onett's, because they execute code the agent may have written.
  A policy's `require_checks: true` makes a PR with no CI checks wait (then block after 30 minutes)
  instead of counting as "no CI"; use it for repositories that always run CI.
- **Evidence is redacted, agent text cannot ping.** Deploy output, service logs, loop errors and
  production signals have secret values (env vars named like `*_TOKEN`, `*_SECRET`, `*_KEY`, and
  URLs with credentials) replaced with `***` before they are stored or shown to an agent. Agent text
  posted to GitHub has its @mentions neutralised, so an agent cannot notify people.
- **Local API.** It listens on `127.0.0.1:7777`. Every call needs the bearer token from `onett token`.
  Requests whose Host header is not local or configured are refused (DNS-rebinding guard).
- **Known limit with Codex.** Claude Code reports refused tool calls in machine-readable form.
  Codex reports sandbox refusals only in prose. For Codex, "a denial stops the task" therefore relies
  on the agent prompt ("if denied, report blocked") and on the stall timeout.

## Quick start

Prerequisites: `git`; `mise` (it installs the Go version in `mise.toml`); Node.js and npm (only for
the web UI build); `gh` logged in (`gh auth status`); and the `claude` and/or `codex` CLI installed
and logged in.

```sh
# Builds the web UI, then the binary with it embedded and the version stamped (OUT=path to relocate).
mise exec -- scripts/build.sh

# Manual fallback: (cd web && npm ci && npm run build) && go build -o onett ./cmd/onett
./onett init --project myproject --repo you/myproject   # writes ~/.config/onett/onett.yaml
./onett doctor     # checks your setup: config, git, gh, harness CLIs, daemon
./onett serve      # run the daemon; leave it running in its own terminal
./onett open       # open the web UI in your browser (second terminal)
./onett chat "What is running right now?"
```

Put `onett` on your PATH to drop the `./`. Before you point onett at a real repository, follow
[docs/getting-started.md](docs/getting-started.md): it starts with a throwaway repository and a
paused project.

To use the web UI on a phone, add it to the home screen; see [docs/ui.md](docs/ui.md#install-on-a-phone).

### Commands

Run `onett <command> -h` for a command's flags. For `task`, `run` and `project`, put the verb first,
as in `onett task create -h`.

| Command | What it does |
|---------|--------------|
| `serve` | run the daemon: API, web UI and engine |
| `init` | write a starter onett.yaml |
| `token` | print the API token |
| `open` | open the web UI (sign in there) |
| `status` | what is running, and what needs you |
| `projects` | list managed projects |
| `project add\|pause\|resume` | add a project to the config, or pause or resume one |
| `agents [--all]` | list agent roles; `--all` also shows removed (disabled) ones |
| `config` | print the redacted config, or `--reload` it |
| `task` | create, list, show, cancel and retry tasks |
| `run` | list, show, cancel and print logs of agent runs |
| `deploys` | list releases, or show one with its log |
| `approvals` | list approvals waiting for a decision |
| `approve` | approve or reject an approval |
| `events` | show history, or follow the event stream |
| `loops` | list recurring loops |
| `loop run NAME` | run a loop now |
| `user reset-password NAME` | give a user a new random password and sign them out everywhere |
| `pause` | stop starting runs and loops |
| `resume` | resume a paused factory |
| `chat` | message the orchestrator and print its reply |
| `service` | install, uninstall or check the launchd service that keeps serve running |
| `doctor` | check the setup: config, git, gh, harness CLIs, projects and the daemon |
| `version` | print the onett version |

### Run it as a service

On macOS, `onett service install` keeps the daemon running in the background and restarts it if it
crashes. It uses launchd, the macOS service manager, which reads a plist file (an XML settings file)
from `~/Library/LaunchAgents`. The daemon's log is `onett.log` in the config's data directory
(`server.data_dir`, `~/.local/share/onett` by default).

```sh
./onett service install     # write the plist, start the daemon, keep it alive
./onett service status      # running (with pid) or installed/not installed, and the log path
./onett service uninstall   # stop the daemon and remove the plist
```

Run `service install` once, and do not also keep `onett serve` running in a terminal: two daemons
cannot both listen on the same address.

launchd starts the daemon without your shell's environment. `service install` therefore records,
in the plist, the `PATH` of the shell you run it from and every variable your config references
as `${NAME}`. The plist is readable only by you, because those values can be secrets. Install
from a shell where `onett serve` works. To change a recorded value, run `service uninstall`
and then `service install` again.

### Retro loop and friction

Agents list anything that slowed them down as `friction`. Onett files it as a GitHub issue and
dedupes by title: a near-duplicate of an open issue becomes a comment on that issue. The retro loop
(`loops.retro`, every 24h) hands the last day's blocked and failed tasks, friction, incidents and
review rejections to the `improver` role as one task. It is on by default only for the project named
by `self_project`; set `enabled: true` or `false` to override.

## Repository layout

```text
cmd/onett/      one binary: `onett serve` (daemon) and the CLI commands (API client)
internal/domain/  shared types: Task, Run, Event, Approval, Deployment, Signal, Friction, outbox
internal/config/  onett.yaml schema, loading, defaults, validation
internal/store/   Store interface; store/sqlite implementation (SQLite, no cgo)
internal/events/  event bus: stores events and fans them out to SSE subscribers
internal/harness/ Harness interface and registry; claude/, codex/, exec/, fake/
internal/workspace/ git worktrees, diff stats, commit, push
internal/scm/     SCM interface; scm/github uses the gh CLI
internal/policy/  risk evaluation
internal/deploy/  Deployer interface; command and noop deployers; HTTP health checks
internal/observe/ Observer interface; exec and sentry observers
internal/notify/  Notifier interface; log, ntfy and exec notifiers
internal/engine/  scheduler, supervisor, PR shepherd, loops, prompt assembly
internal/chat/    orchestrator conversations
internal/api/     HTTP API, SSE stream and the embedded web UI
web/              React + Vite + TypeScript UI (built into internal/api/webdist)
docs/             design, API, UI and plugin documentation
```

## Documentation

- [docs/getting-started.md](docs/getting-started.md): from nothing to a first merged PR.
- [docs/design.md](docs/design.md): the architecture, task lifecycle, policy and known limits.
- [docs/api.md](docs/api.md): the HTTP API and event stream.
- [docs/ui.md](docs/ui.md): the web UI pages and design.
- [docs/plugins.md](docs/plugins.md): writing harnesses, deployers, observers and notifiers.
- [onett.example.yaml](onett.example.yaml): every config option, annotated.
- [examples/dogfood/README.md](examples/dogfood/README.md): run onett on its own repo, with a local staging release.
