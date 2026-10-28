# Foundry v0 build — 2026-10-27 overnight session

Design: `docs/design.md`. API: `docs/api.md`. Frozen contracts (main agent only):
`internal/domain/*`, `internal/{store,events,harness,scm,deploy,observe,notify,policy}/<pkg>.go`,
`internal/config/config.go`, `internal/api/{controller,errors}.go`, `internal/engine/engine.go`.
Shared file: `go.mod`/`go.sum` — all deps pre-fetched; no agent edits them.

## Phase 1 (parallel, disjoint files)

- [ ] U1 store/sqlite + events bus — `internal/store/sqlite/`, `internal/events/bus*.go`
- [ ] U2 config loading/defaults/validation + `foundry.example.yaml` — `internal/config/` (not config.go)
- [ ] U3 harness adapters claude-code, codex, exec, fake — `internal/harness/{proc.go,claude,codex,exec,fake}`
- [ ] U4 git worktrees + GitHub SCM (gh) + fake SCM — `internal/workspace/`, `internal/scm/{github,fake}/`
- [ ] U5 policy evaluation + notifiers — `internal/policy/evaluate*.go`, `internal/notify/*` (not notify.go)
- [ ] U6 command/noop deployer, health check, exec/sentry observers — `internal/deploy/`, `internal/observe/`

## Phase 2 (after phase 1 contracts land)

- [ ] Role prompts — `internal/engine/prompts/` (main agent: behaviour design)
- [ ] U7 web UI — `web/` (main agent writes the design spec)
- [ ] U8 API server — `internal/api/` (needs U1)
- [ ] U9 engine: scheduler, runner, supervisor — `internal/engine/{scheduler,runner,supervise,prompt}.go`
- [ ] U10 engine: loops — `internal/engine/loop_*.go`
- [ ] U11 engine: controller + chat — `internal/engine/controller.go`, `internal/engine/chat.go`

## Phase 3

- [ ] U12 `cmd/foundry`: serve wiring + CLI client
- [ ] End-to-end test: fake harness + local bare repo + fake SCM through queued → merged → deployed
- [ ] Real smoke: claude-code and codex harness on a throwaway local repo
- [ ] Defect review (fresh agent), fix, final verification
- [ ] README, docs/configuration.md, docs/plugins.md

## Review

(filled in at the end)

## Fix batch (from accepted units)

- [x] policy.Evaluate: unrecognised agent risk string → treat as high (today it passes through)
- [x] config: unset `${NAME}` → load error naming the variable (decision: fail loudly on missing secrets)
- [x] foundry.example.yaml: "five built-in roles" lists six; max_attempts exhaustion → "blocked", not "failed"
- [x] merge docs/plugins-deploy-observe.md into docs/plugins.md
- [x] workspace.Manager: per-repo mutex (concurrent fetch/worktree-add on one clone collide on git's lock)
- [x] scm/github OpenPR: add labels with `gh pr edit`, not `gh issue edit`
- [x] web IdChip: UUIDv7 IDs share their first 8 chars (timestamp) — show the last 8
- [x] web: add web/public/.keep so every build recreates internal/api/webdist/.keep (embed needs it on fresh clones)
- [ ] web: visual + runtime pass against a live backend (main agent, Playwright screenshots)
- [x] cli: events --follow without --after starts from now (no history replay)

## Engine fix round (review findings, all confirmed by main agent) + live-test findings

- [x] R1 retries: Prepare removes any existing worktree holding the branch before `worktree add`
- [x] R2 guarded transitions: run outcomes only move tasks from `running`; applyReview only from `review`; re-check status before push/OpenPR; CancelTask cascades to children
- [x] R3 shepherd re-reads the task before merge/approval; DecideApproval requires `awaiting_approval`
- [x] R4 recover() re-queues every `running` task with no live run; startTask failure writes use WithoutCancel
- [x] R5 deploy timeout (config `deploy.timeout`, default 30m) fails a stuck release
- [x] R6 merges pinned to head SHA (`gh pr merge --match-head-commit`); Approval.HeadSHA; head changed since approval → back to review
- [x] R7 updateRun error must not zero the local run
- [x] R8 mergeable "unknown" → wait; merge read-back failure → leave in review (next pass sees merged)
- [x] R9 shepherd self-heals awaiting_approval without a pending approval
- [x] R10 blocked tasks can be cancelled; prod_watch treats blocked incidents as open
- [x] R11 API CreateTask rejects kind review and clears risk fields; worker agents get no FOUNDRY_TOKEN (orchestrator only)
- [x] R12 SendChat after shutdown returns an error instead of wg.Add after Wait
- [x] L1 codex default args: `--approve-for-me` alone (it conflicts with `--sandbox`) — every codex run currently fails
- [x] L2 agents never commit (codex sandboxes .git read-only even with --add-dir); before a re-run on an existing branch foundry merges origin/<default> and lists conflicted files in the prompt
- [x] L3 loop kick channels move from a package-level map into the Engine struct
- [x] L4 UI: closed mobile drawer renders through the page; "active" preset chip highlighting; search field alignment; show "project paused" on queued tasks
- [x] shepherd: pinned auto-merge refused because the PR head moved → leave in review and re-evaluate (block only if the head is unchanged)
- [x] README + docs/getting-started.md
- [x] (unblocked by owner) `foundry service` (launchd KeepAlive) + `foundry doctor` — written but its test run was denied by the auto-mode classifier ("Unauthorized Persistence"); parked UNTESTED on local branch `service-doctor` for the owner
- [x] UI polish: tool_use ▸ marker inline with its text; "—" for empty Task/Project on chat runs; workdir gets a full-width row
- [x] CI workflow for foundry itself (.github/workflows/ci.yml): go vet/test -race, gofmt, web typecheck/test/build
- [ ] cleanup: budget once-per-day state and any other package-level maps keyed by *Engine move into the Engine struct
- [x] docs/api.md corrections from the OpenAPI pass: history limit 500; approvals status=approved|rejected; live events carry id 0 in data; unknown JSON fields → 400; /config redacts option secrets and harness env
- [ ] docs: toolchains under Codex's sandbox — GOCACHE in /tmp, GOFLAGS=-mod=readonly -buildvcs=false, GOPROXY=off. Codex's sandbox blocks all network incl. binding localhost (httptest); options: use the Claude implementer for such repos, or the OWNER opts in to `-c sandbox_workspace_write.network_access=true` (the agent-side enable was refused as weakening the sandbox — owner decision)
- [x] CI grace: right after a push GitHub may report no checks yet (looks like "no CI" → passes). Treat CI "none" as pending until `policy.ci_grace` (default 3m) after foundry's last push (Task.PushedAt)
- [x] cosmetic: policy.decision message "risk low: " with no reasons → "risk low"
- [ ] re-runs on an existing PR should refresh its description (`gh pr edit --body`) with the latest summary — PR #1's body omitted the CI-fix test changes
- [x] after unit P lands: merge `dogfood` (PR #1: loop wake interval) back into `init`, keeping init's root-cause timeout fix over the loosened test timings
- [ ] SSE ?after=0 should replay from the start (store treats AfterID 0 as 'newest 500')
- [x] deploy/observe/notify: exit 0 with a lingering background child must stay a success (kill the group), not ErrWaitDelay → a good release failed/rolled back
- [ ] sysproc.Wait: also kill the leftover process group after a non-zero exit (no orphans either way)
- [ ] EnsureClone leaves the clone checked out on the remote HEAD (main), not the project's default_branch — the orchestrator reading project.path saw only main's README. Keep the clone's checkout at origin/<default_branch> (detached) after each fetch
