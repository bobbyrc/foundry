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
- [ ] merge docs/plugins-deploy-observe.md into docs/plugins.md
- [x] workspace.Manager: per-repo mutex (concurrent fetch/worktree-add on one clone collide on git's lock)
- [x] scm/github OpenPR: add labels with `gh pr edit`, not `gh issue edit`
- [ ] web IdChip: UUIDv7 IDs share their first 8 chars (timestamp) — show the last 8
- [ ] web: add web/public/.keep so every build recreates internal/api/webdist/.keep (embed needs it on fresh clones)
- [ ] web: visual + runtime pass against a live backend (main agent, Playwright screenshots)
