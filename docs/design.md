# Foundry design (v0)

Foundry is a personal software factory: one daemon that takes work (from chat, GitHub issues,
production errors, its own friction reports), hands it to coding agents, and carries the result
through PR, merge, deploy and verification with as little human involvement as the risk allows.

Status: v0, written 2026-10-27 without a live design review. Every decision below is meant to be
cheap to revisit; the ones most worth a second look are marked **(review)**.

## Principles

1. **Agents do judgment, code does gates.** Agents write code, review diffs, triage issues and
   diagnose incidents. Deterministic Go code commits, pushes branches, opens PRs, decides risk,
   merges, deploys and rolls back. An agent can never lower a risk rating or merge its own work;
   it can only raise risk. Worker agents get no API token, so they cannot act on foundry at all;
   only the orchestrator (the agent you chat with) can, and only through the CLI.
2. **A permission denial or a stall is terminal for that run.** A run that is denied a tool, or
   produces no output for `limits.stall_timeout`, is stopped, its task becomes `blocked`, and you
   are notified with the exact reason. Foundry never retries a denial or rephrases around it.
3. **Everything is a plugin.** Harnesses, SCM, deployers, observers and notifiers are Go
   interfaces with built-in implementations. Harnesses, observers and notifiers also have an
   `exec` type that runs any external program speaking a small JSON protocol, and the `command`
   deployer runs any shell commands, so you can add a harness or provider in any language
   without recompiling. (The code host is GitHub only in v0.)
4. **Configuration is a file you own.** `foundry.yaml` (plus per-repo `.foundry/` overrides) is
   the source of truth for projects, agents, policies and loops. The database holds only runtime
   state (tasks, runs, events). Put the config in your dotfiles repo if you like.
5. **One API for every client.** The web UI is just a client of the HTTP+SSE API
   (`docs/api.md`); native and mobile apps later use the same API.
6. **Foundry manages itself.** The foundry repo is registered as a project, so friction that
   agents report becomes issues that agents fix — that is the self-improvement loop.

## Shape

```text
            chat / web UI / CLI / future apps
                         │  HTTP + SSE (/api/v1)
                ┌────────▼─────────┐
                │      foundryd    │  single Go binary, SQLite state
                │                  │
   loops ──────►│  task queue ──► scheduler ──► supervisor ──► harness (claude, codex, exec…)
 (issues, PRs,  │                  │                 │            runs in a git worktree
  deploys, prod,│                  │                 ▼
  friction)     │   policy ◄── shepherd: push, PR, CI, review, merge
                │                  │
                │   deploy watch ──► deployer + health + observers ──► rollback
                └──────────────────┘
```

### Concepts

- **Project** — a repository foundry manages: GitHub repo, local clone path, default branch,
  policy, deployer, observers. Defined in config.
- **Agent profile (role)** — a named recipe: which harness, which model, which prompt file,
  extra args. Built-in roles: `orchestrator` (the agent you chat with), `implementer`, `reviewer`,
  `triager`, `sre`, `improver`. Any role can be redefined or added in config.
- **Harness** — an adapter that runs one agent CLI non-interactively and streams its output:
  `claude-code`, `codex`, `exec` (anything else), `fake` (tests).
- **Task** — a unit of work for one role in one project, with a kind (`feature`, `bugfix`,
  `issue`, `incident`, `review`, `chore`, `improve`), a status, and a source used for dedupe
  (`github_issue:42`, `signal:<fingerprint>`, `chat:<conversation>`).
- **Run** — one attempt at a task by one harness session, in its own git worktree, with a
  streamed log, cost, and an outcome.
- **Outbox** — files an agent writes in `<worktree>/.foundry-run/` to report back:
  `result.json` (status, summary, self-assessed risk, PR title/body, follow-up tasks, friction).
  Files, not network calls, so it works inside sandboxed harnesses such as Codex's.
- **Policy** — deterministic rules mapping a diff (paths, size, labels, task kind) to a risk
  level, and the gates per risk level (auto-merge, review required, human approval required).
- **Loop** — a recurring job: issue intake, PR shepherd, deploy watch, prod watch, friction
  filing. Each can be enabled, disabled and tuned per project.
- **Signal** — a deduplicated production problem (e.g. a Sentry issue) from an observer.
- **Approval** — a request for your decision (merge a high-risk PR, act on a blocked task).

### Task lifecycle

```text
queued ─► running ─► review ─► merged ─► deploying ─► done
             │          │                     │
             │          └► awaiting_approval ─┘ (you approve in the UI → merge)
             ├► done        (no code change needed: question answered, triage finished)
             ├► blocked     (denied, stalled, agent says blocked, retries exhausted)
             └► failed / cancelled
```

- `running` → the scheduler picked it and a run is live.
- Agents never commit (Codex's sandbox keeps `.git` read-only, so they could not anyway). When
  the run ends with changes, foundry commits them with the agent's PR title, pushes branch
  `foundry/<task-id-suffix>-<slug>` and opens a PR; the task moves to `review`.
- Before a re-run on a branch that already has a PR, foundry merges the default branch in. If
  that conflicts, the agent is given the conflicted files to resolve; leftover conflict
  markers fail the attempt instead of being committed.
- In `review` the shepherd waits for CI, dispatches a `reviewer` run when the policy requires
  review, then asks the policy: auto-merge → `merged`; otherwise → `awaiting_approval`. Every
  merge is pinned to the PR head commit that CI, review and the policy judged
  (`gh pr merge --match-head-commit`), and an approval is for one head: if the PR changes after
  you were asked, foundry withdraws the approval request and re-checks.
- Every automatic status change is guarded by the status it expects, atomically. A run or loop
  that finishes after you cancelled or blocked a task cannot revive it.
- An agent-reported failure, failing CI, a merge conflict or "request changes" from the
  reviewer re-queues the task for another implementer run on the same branch, up to
  `max_attempts`, then `blocked` (`running → queued → running …`; not drawn above).
- After merge, if the project has a deployer, the deploy watch takes it through `deploying`;
  a failed health window rolls back, opens an `incident` task, and notifies you.

### Run supervision

The supervisor owns every harness session:

- streams output into the run log (SSE to the UI) and updates `last_output_at`;
- kills the session after `stall_timeout` without output (status `stalled`) or `run_timeout`
  overall (`timed_out`);
- marks `denied` when the harness reports a permission denial;
- in every one of those cases sets the task `blocked` with the exact reason and notifies.

### Risk and auto-merge **(review)**

Risk is `low < medium < high`. Final risk = max(rules matched by the diff, agent's own rating).
Default policy: auto-merge `low` after green CI and an approving reviewer run; `medium` and
`high` need your approval. Default high-risk paths: migrations, auth, CI/deploy config, secrets,
infra, dependency manifests with major bumps, `.foundry/`; default size limit 400 changed lines.

### Deploy watch

`deployer.deploy(ref)` (or, for platforms that deploy on push such as Vercel, locate the
deployment for the merge commit) → poll until ready → health window (`verify_for`, default
10m): health URL must stay 2xx and observers must not report new signals above threshold for
this release. If verification fails: `deployer.rollback(previous healthy)`, record it, open an
`incident` task with the evidence, notify. A release that fails before it is ready (build
error, or still not ready after `deploy.timeout`) is failed without a rollback, since nothing
new is serving; it still opens an incident and blocks the task.

### Production watch

Each observer is polled every `prod_watch.every`. New signals (by fingerprint) above the
project's threshold become `incident` tasks for the `sre` role, deduped by fingerprint; a
signal that keeps firing after its fix merged reopens.

### Friction and self-improvement

Every agent prompt ends with the outbox contract, including a `friction` list: anything that
slowed the agent down (missing docs, flaky test, bad tooling, foundry itself). Foundry dedupes
friction by fingerprint and, when `friction.auto_file` is on, files a GitHub issue in the right
repo (the project, or foundry's own repo for factory friction) labelled `friction`. Issue intake
picks up issues labelled `foundry` (configurable), so turning a friction issue into work is one
label — or automatic with `friction.auto_queue`.

The retro loop (`loops.retro`, default every 24h) is the factory's retrospective. Each day, code
gathers the evidence of the last `lookback` (blocked and failed tasks with their reasons,
re-queued attempts, friction, incidents, review findings sent back) and files one improve task
for the improver agent with that evidence. The agent picks the most valuable root cause to fix,
files the rest as followups, and names duplicate friction issues in its summary, since foundry
never closes issues itself. Nothing is filed when there is no evidence. It is on only for
`self_project` unless `enabled` says otherwise.

### The orchestrator (main agent)

Chat messages go to a persistent `orchestrator` harness session (resumed by session ID per
conversation). It runs with the `foundry` CLI on its PATH and `FOUNDRY_API`/`FOUNDRY_TOKEN` in
its environment, so it delegates by running `foundry task create …`, inspects with `foundry
task list`, `foundry run logs`, and so on. Its answer streams back to the UI over SSE.

## Code layout

```text
cmd/foundry/          one binary: `foundry serve` (daemon) and CLI subcommands (API client)
internal/domain/      shared types: Task, Run, Event, Approval, Deployment, Signal, Friction, outbox
internal/config/      foundry.yaml schema, loading, defaults, validation
internal/store/       Store interface; store/sqlite implementation (modernc.org/sqlite, no cgo)
internal/events/      event bus: persist + fan-out to SSE subscribers
internal/harness/     Harness interface + registry; claude/, codex/, exec/, fake/
internal/workspace/   git worktrees, diff stats, commit, push
internal/scm/         SCM interface; scm/github (gh CLI)
internal/policy/      risk evaluation
internal/deploy/      Deployer interface; command deployer; HTTP health checks
internal/observe/     Observer interface; exec observer; sentry observer
internal/notify/      Notifier interface; ntfy, exec, log
internal/engine/      scheduler, supervisor, shepherd, loops, prompt assembly
internal/chat/        orchestrator conversations
internal/api/         chi HTTP API + SSE + embedded web UI
web/                  React + Vite + TypeScript UI (built into internal/api/webdist)
```

## Security defaults

- Binds `127.0.0.1:7777`. Every API call needs `Authorization: Bearer <token>`; the token is
  generated on first start at `<data_dir>/token` (mode 600). Expose remotely only over a VPN
  such as Tailscale.
- Agents never get push credentials from foundry; foundry pushes. Harness permissions are
  configured per agent profile (`claude-code` defaults to `acceptEdits` plus an allowed-tools
  list; `codex` defaults to `--approve-for-me`, its workspace-write sandbox with automatic review of escalations).
- Secrets for providers come from the environment or `~/.config/foundry/secrets.env`.

### Known limits of the harnesses

- Claude Code reports refused tool calls as `permission_denials`; foundry treats any as a
  denial. Codex does not report sandbox refusals in machine-readable form (only in prose), so
  for Codex the "denial is terminal" rule relies on the prompt ("if denied, report blocked")
  and the stall timeout.

## Not in v0

Native/mobile apps (API is ready for them), multi-machine workers, a config editor in the UI
(the UI shows config; edit the file), cost budgets beyond per-run limits, Vercel/Render native
deployers (use the `command` deployer with their CLIs meanwhile).
