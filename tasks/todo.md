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
