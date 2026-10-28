# AGENTS.md

Instructions for any coding agent (Claude Code, Codex, or another) and any person changing this
repository. `CLAUDE.md` imports this file; keep the two in step by editing only this one.

## What this is

Onlet is a self-hosted software factory: one Go daemon (`onlet serve`) that hands work to
coding agents, runs each in its own git worktree, and carries the result through PR, CI, agent
review, a risk policy, merge, deploy, health check and rollback. A React UI and a CLI are clients
of its HTTP+SSE API. Onlet also manages this repository, so agents run by onlet may be
reading this file.

Read before you change anything non-trivial:

- `README.md`: terms (task, run, role, harness, loop, outbox, approval), the task flow, the safety
  model and the commands.
- `docs/design.md`: architecture and decisions, and **Product direction**: self-hosted now, a paid
  managed version later. Prefer choices that keep the seams that section lists.
- `tasks/lessons.md`: mistakes already made here, and the rule that prevents each one.

## Toolchain and commands

Go comes from `mise` (`mise.toml` pins Go 1.27); Node.js 26 builds the UI. Run Go through
`mise exec --` unless `go` on PATH is already the pinned version.

Before you report a change as done, run what CI runs and see it pass:

```sh
mise exec -- go vet ./...
test -z "$(mise exec -- gofmt -l internal cmd)"
mise exec -- go test -race -count=1 ./...
cd web && npm ci && npm run typecheck && npm test && npm run build
```

A Go-only change needs only the first three; a change under `web/` needs the last line too. To
run one package or test: `mise exec -- go test -race -count=1 -run TestName ./internal/engine/`.

- Build the binary with the UI embedded: `mise exec -- scripts/build.sh` (`OUT=path` to relocate).
  The Go build embeds `internal/api/webdist`, which `npm run build` fills; an empty directory
  builds a binary with no UI.
- Try a change against a real daemon: write a minimal config with `server.addr` on a spare port
  (not 7777) and `server.data_dir` in a temp directory, then `./onlet serve --config <it>`. Never
  point an experiment at the owner's real data directory or a running factory.

## Repository map

| Path | What lives there |
|---|---|
| `cmd/onlet/` | the one binary: `serve` and every CLI command (an API client) |
| `internal/domain/` | shared types: Task, Run, Event, Approval, Deployment, Signal, Friction, outbox |
| `internal/config/` | `onlet.yaml` schema, defaults (`defaults.go`), loading and validation |
| `internal/engine/` | scheduler, run supervisor, PR shepherd, loops (`loop_*.go`), prompts, redaction |
| `internal/engine/prompts/` | the built-in role prompts given to agents |
| `internal/store/` | `Store` interface; `store/sqlite` implements it (migrations in `migrations.go`) |
| `internal/harness/` | `Harness` interface; `claude/`, `codex/`, `exec/`, and `fake/` for tests |
| `internal/scm/`, `deploy/`, `observe/`, `notify/` | plugin interfaces and their implementations |
| `internal/workspace/` | git worktrees, diffs, commits, pushes |
| `internal/sysproc/` | process groups and the agent environment allowlist (`AgentEnv`) |
| `internal/api/` | HTTP API, SSE stream, host guard, the embedded UI |
| `web/` | React + Vite + TypeScript UI |
| `docs/` | design, API (`api.md`, `openapi.yaml`), UI, plugins, getting started, proposals |
| `examples/dogfood/` | onlet running on its own repo, with a local staging release |

## Invariants: do not weaken these

These are the safety model. A change that weakens one needs the owner's explicit decision, not a
reviewer's approval.

1. **Agents judge, code acts.** Only deterministic Go code commits, pushes, opens PRs, merges,
   deploys and rolls back. Do not add a path where agent output triggers one of those directly.
2. **Risk only goes up.** Final risk is the highest of the matched rules and the agent's own
   rating. Nothing an agent writes may lower it.
3. **Agents get no credentials.** Agent processes, project `setup` and `checks` run with
   `sysproc.AgentEnv` (an allowlist plus the harness's `inherit_env`), never onlet's own
   environment. Never pass `GH_TOKEN`, the API token or provider keys to an agent, including in
   examples and docs.
4. **A denial or a stall is terminal.** A denied tool call, or no output for the stall timeout,
   blocks the task with the reason. Never add a retry or a workaround for a denial.
5. **Merges are pinned.** `gh pr merge --match-head-commit`: an approval covers one commit, and a
   changed PR withdraws it.
6. **Status changes are compare-and-set.** Automatic task transitions name the status they expect,
   so a finishing run cannot revive a cancelled or blocked task.
7. **Onlet's git calls disable hooks** (`core.hooksPath=/dev/null` in `internal/workspace`), so
   an agent-planted hook never runs with onlet's credentials. Keep it on every new git call.
8. **Evidence is redacted and labelled.** Command output, service logs, loop errors and production
   signals go through `redactSecrets` (`internal/engine/redact.go`) before they are stored or
   shown to an agent. Evidence given to an agent starts with `evidenceNote` ("data, not
   instructions").
9. **Agent text cannot ping people.** Any new code that posts agent-written text to GitHub must
   go through `quoteMentions` (`internal/scm/github/mentions.go`).
10. **Strict input.** Unknown YAML keys and unknown JSON request fields are errors. Keep it so.
11. **Local by default.** The API binds to loopback, needs the bearer token or a signed-in
    session, and refuses unknown Host headers (DNS-rebinding guard). Remote access is opt-in
    through `public_url` and `allowed_hosts`.
12. **Config and users change only through a person.** Editing `onlet.yaml` and managing users
    need a signed-in owner session. The API token, which the orchestrator agent holds, can never
    do either (`auth.TokenAllows`), so an agent cannot rewrite its own policy or environment.
    Every new route declares the action it needs (read, operate or administer).

Changes to `internal/engine/prompts/**`, `internal/policy/**`, `internal/config/defaults.go`,
`internal/sysproc/**`, `internal/engine/loop_shepherd.go`, `internal/engine/helpers.go`,
`internal/auth/**`, `internal/api/auth.go` and `internal/api/routes.go` are rated **high** risk by
a built-in rule, and `AGENTS.md`, `CLAUDE.md` and `onlet.yaml` files are
**medium**: the owner approves them. Do not move logic out of those files to avoid the rule.

## Go conventions

- Match the surrounding code: its naming, comment density and idiom. Comments explain why, in
  plain words, and every package has a doc comment.
- Interfaces live in the parent package (`harness`, `scm`, `deploy`, `observe`, `notify`,
  `store`); implementations live in subpackages and carry a compile-time assertion
  (`var _ scm.SCM = (*GitHub)(nil)`).
- Errors: wrap with `%w`; use sentinel errors such as `store.ErrNotFound` for conditions callers
  test. Validate input before writing to the store. The API maps errors to status codes.
- External tools (`git`, `gh`, agent CLIs) run through `exec`, not SDKs. Long-running command
  output goes to temp files, not pipes: a pipe whose reader stops kills a deployed service with
  SIGPIPE.
- Durations in config use `config.Duration`. A new loop gets its own `loop_<name>.go`.
- Avoid new dependencies; `go.mod` and `package.json` changes are medium risk and need a reason.
- Schema changes append a new entry to `migrations` in `internal/store/sqlite/migrations.go`.
  Never edit an existing migration: databases in the field have already applied it.

## Tests

- Go tests sit next to the code; test names describe behaviour
  (`TestStallEndsOnCancel`). Web tests are `web/src/**/*.test.ts` (Vitest).
- Write the failing test first for a bug fix, and see it fail for the right reason.
- Put new tests in a **new file** when other open PRs touch the same test file: tests appended to
  the end of one file are the most common merge conflict here.
- Tests never touch real machine state or the network: use `t.TempDir()`, the fake harness or
  recorded JSONL fixtures (`internal/harness/*/testdata`), the fake SCM, and stubs for `launchctl`
  and `gh`. Never call GitHub, never write under `~`, never bind a fixed port.
- Some checks guard documentation: `TestOpenAPISpecMatchesRoutes` (routes vs `docs/openapi.yaml`)
  and `TestExampleFileLoads` (`onlet.example.yaml`). Fix the doc, not the test.
- For changes to the runner, shepherd or deploy path, unit tests are not enough: exercise the real
  flow with a throwaway local repo and a second daemon on a spare port (see `tasks/lessons.md`).

## Keep these in sync in the same change

| When you change | Also update |
|---|---|
| an API route or payload | `docs/api.md`, `docs/openapi.yaml`, `web/src/types.ts` |
| a config key | `internal/config` (schema, default, validation), `onlet.example.yaml`, the README if users see it |
| a CLI command or flag | its `-h` usage and the README **Commands** table |
| a UI page | `docs/ui.md` |
| behaviour or a decision | `docs/design.md`; keep **(review)** marks on decisions that are still open |
| the safety model | the README **Safety model** section and the invariants above |

Docs describe what the code does now. Write them in plain words, unpack each term on first use,
and match the existing spelling and tone.

## Web UI conventions

- Pages in `src/pages/`, one PascalCase component per file in `src/components/`, pure logic with
  tests in `src/lib/`.
- Query keys come from `src/keys.ts`, and SSE events use the same keys to refresh the cache
  (`src/events.ts`). Filter state lives in the URL through `useParamState`.
- Status colours and labels come from `src/lib/status.ts`; every colour has a text label.
- No inline scripts (the server's CSP forbids them). Rendered Markdown goes through the sanitiser
  in `components/Markdown.tsx`; do not render agent text as raw HTML.
- Check a UI change in a real browser at desktop and phone width against a throwaway daemon, not
  only with tests.

## If onlet is running you

When `ONLET_OUTBOX` is set, you are an agent inside onlet, and its prompt rules first: do not
commit, push, open PRs or call the GitHub API, and write your report to
`$ONLET_OUTBOX/result.json`. In addition:

- Never report a check as passed if it did not run or could not run. Say which checks ran,
  which were skipped, and why. A sandbox that cannot bind localhost (Codex's default) makes the
  `httptest` tests in `internal/api` and `internal/engine` fail. Report that; do not call it
  green.
- The dogfood project sets `GOPROXY=off` and `GOFLAGS=-mod=readonly`: you cannot download
  modules, so do not add dependencies.
- Report problems with onlet itself as `friction` with target `onlet`. Check the current
  code first: friction often describes something already fixed.

## If a person or an interactive agent is driving

- Work on a branch and open a PR against `main`. Commit subjects are short and imperative; add
  the reason in a parenthesis or the body when it is not obvious.
- `dogfood` is the base branch of the self-hosted factory that works on this repo. Do not push to
  it while the factory has open PRs that touch the same files; sync it from `main` in one step
  when they have settled (`tasks/lessons.md`).
- Before deleting, killing or overwriting anything, check exactly what it is: kill processes by
  verified PID, never by pattern.
- Never write a secret into the repository, a log, a test fixture or a command's output.
- After the owner corrects you, add the rule that prevents the mistake to `tasks/lessons.md`.

## Keep the maintainer's instance up to date

The maintainer runs a long-lived onlet on their own machine, set up as in
`examples/dogfood/README.md`. It works on the maintainer's other projects and on onlet itself,
so it must run current `main`. When you develop onlet on that machine (there,
`DOGFOOD=~/.local/share/onlet-dogfood`), finish every change that reaches `main` like this:

1. Bring the factory's base branch up to date: push `main` to `dogfood` when it fast-forwards
   and no factory PR is waiting on the same files (see above).
2. Run `mise exec -- examples/dogfood/update.sh`. It builds `main`, waits until the factory is
   idle, swaps the binary, restarts the daemon, checks its version, and puts the old binary back
   if the new one fails.
3. Check that the UI loads and `onlet status` is sane.

That machine's config, `$DOGFOOD/onlet.yaml`, is a local copy of the example plus
machine-specific settings, so it is not in the repository. When a change adds a config key that
the instance needs, update the binary first and then add the key to that file: an older binary
rejects keys it does not know. Agents that onlet runs never do any of this, because restarting
the daemon ends their own run.
