# Foundry v0 build — 2026-10-27 overnight session

Design: `docs/design.md`. API: `docs/api.md`. Frozen contracts (main agent only):
`internal/domain/*`, `internal/{store,events,harness,scm,deploy,observe,notify,policy}/<pkg>.go`,
`internal/config/config.go`, `internal/api/{controller,errors}.go`, `internal/engine/engine.go`.
Shared file: `go.mod`/`go.sum` — all deps pre-fetched; no agent edits them.

## Phase 1 (parallel, disjoint files)

- [x] U1 store/sqlite + events bus — `internal/store/sqlite/`, `internal/events/bus*.go`
- [x] U2 config loading/defaults/validation + `foundry.example.yaml` — `internal/config/` (not config.go)
- [x] U3 harness adapters claude-code, codex, exec, fake — `internal/harness/{proc.go,claude,codex,exec,fake}`
- [x] U4 git worktrees + GitHub SCM (gh) + fake SCM — `internal/workspace/`, `internal/scm/{github,fake}/`
- [x] U5 policy evaluation + notifiers — `internal/policy/evaluate*.go`, `internal/notify/*` (not notify.go)
- [x] U6 command/noop deployer, health check, exec/sentry observers — `internal/deploy/`, `internal/observe/`

## Phase 2 (after phase 1 contracts land)

- [x] Role prompts — `internal/engine/prompts/` (main agent: behaviour design)
- [x] U7 web UI — `web/` (main agent writes the design spec)
- [x] U8 API server — `internal/api/` (needs U1)
- [x] U9 engine: scheduler, runner, supervisor — `internal/engine/{scheduler,runner,supervise,prompt}.go`
- [x] U10 engine: loops — `internal/engine/loop_*.go`
- [x] U11 engine: controller + chat — `internal/engine/controller.go`, `internal/engine/chat.go`

## Phase 3

- [x] U12 `cmd/foundry`: serve wiring + CLI client
- [x] End-to-end test: fake harness + local bare repo + fake SCM through queued → merged → deployed
- [x] Real smoke: claude-code and codex harness on a throwaway local repo
- [x] Defect review (fresh agent), fix, final verification
- [x] README, docs/configuration.md, docs/plugins.md

## Review

Overnight 2026-10-27/28. Branch `init` (pushed), mirrored to `dogfood`, which is where the factory's
own PRs land. All verification is on the final tree: go vet, gofmt and `go test -race ./...`
pass, as do the web typecheck, 56 vitest tests and the build.

What exists: a Go daemon plus CLI and an installable web UI (PWA). It runs Claude Code and Codex
(and any `exec` harness) as roles. It takes work from GitHub issues, exec intake sources, chat
and the retro and friction loops. Agents work in worktrees and foundry opens the PRs. A
deterministic policy rates risk. A reviewer agent and CI gate every PR, and low-risk PRs
auto-merge. Each merge is released and its health verified, with automatic rollback, an incident
with evidence, and SRE triage. Production signals become incidents. Issues are closed on merge,
and friction becomes issues.

Proven live on this repo: 20 factory PRs merged on their own, every release health-checked on a
local staging copy, a rollback drill, a prod_watch drill, a Codex smoke run, orchestrator chat
(read-only questions and delegation), and the in-repo release pipeline (examples/dogfood).

Owner decisions and open items are the unchecked `OWNER:` lines below.

## Fix batch (from accepted units)

- [x] policy.Evaluate: unrecognised agent risk string → treat as high (today it passes through)
- [x] config: unset `${NAME}` → load error naming the variable (decision: fail loudly on missing secrets)
- [x] foundry.example.yaml: "five built-in roles" lists six; max_attempts exhaustion → "blocked", not "failed"
- [x] merge docs/plugins-deploy-observe.md into docs/plugins.md
- [x] workspace.Manager: per-repo mutex (concurrent fetch/worktree-add on one clone collide on git's lock)
- [x] scm/github OpenPR: add labels with `gh pr edit`, not `gh issue edit`
- [x] web IdChip: UUIDv7 IDs share their first 8 chars (timestamp) — show the last 8
- [x] web: add web/public/.keep so every build recreates internal/api/webdist/.keep (embed needs it on fresh clones)
- [x] web: visual + runtime pass against a live backend (main agent, Playwright screenshots) — desktop and 390px phone width, on live dogfood data
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
- [x] cleanup: budget once-per-day state and any other package-level maps keyed by *Engine move into the Engine struct (budget state is on Engine; only the deployHealth tally remains package-level, keyed by unique deployment IDs)
- [x] docs/api.md corrections from the OpenAPI pass: history limit 500; approvals status=approved|rejected; live events carry id 0 in data; unknown JSON fields → 400; /config redacts option secrets and harness env
- [x] docs: toolchains under Codex's sandbox — GOCACHE in /tmp, GOFLAGS=-mod=readonly -buildvcs=false, GOPROXY=off. Codex's sandbox blocks all network incl. binding localhost (httptest); options: use the Claude implementer for such repos, or the OWNER opts in to `-c sandbox_workspace_write.network_access=true` (the agent-side enable was refused as weakening the sandbox — owner decision)
- [x] CI grace: right after a push GitHub may report no checks yet (looks like "no CI" → passes). Treat CI "none" as pending until `policy.ci_grace` (default 3m) after foundry's last push (Task.PushedAt)
- [x] cosmetic: policy.decision message "risk low: " with no reasons → "risk low"
- [x] re-runs on an existing PR should refresh its description (`gh pr edit --body`) with the latest summary — PR #1's body omitted the CI-fix test changes
- [x] after unit P lands: merge `dogfood` (PR #1: loop wake interval) back into `init`, keeping init's root-cause timeout fix over the loosened test timings
- [x] SSE ?after=0 should replay from the start (store treats AfterID 0 as 'newest 500') — already done (EventFilter.FromStart)
- [x] deploy/observe/notify: exit 0 with a lingering background child must stay a success (kill the group), not ErrWaitDelay → a good release failed/rolled back
- [x] sysproc.Wait: also kill the leftover process group after a non-zero exit (no orphans either way)
- [x] EnsureClone leaves the clone checked out on the remote HEAD (main), not the project's default_branch — the orchestrator reading project.path saw only main's README. Keep the clone's checkout at origin/<default_branch> (detached) after each fetch
- [ ] friction: semantic duplicates need judgment — periodically queue a triager task "consolidate duplicate friction issues" (close dupes with a link)
- [x] openapi.yaml + web/src/types.ts: add 'retro' to the loop names / loops config shape
- [x] OWNER (done): approve or reject dogfood PR #14 (retro's implementer-prompt change; high risk = foundry's own instructions). My approval attempt was refused as self-approval — correctly; it is yours. My read: safe and useful; one hint (GOMODCACHE→/tmp with GOPROXY=off) is slightly off. — owner rejected #14 (2026-10-28); PR closed
- [ ] OWNER: friction #4, #5, #6 are all the Codex sandbox (no localhost listeners, no module cache); they resolve with your Codex network decision. #3 closed as a duplicate of #5
- [x] per-project `checks:` — commands foundry runs in the worktree after the agent and before commit/push (generic replacement for the cancelled Go-specific follow-ups); failure re-queues with the output
- [x] policy rule criterion `sources` + default rule: work foundry assigned itself (followup, retro) is at least medium — tonight 2 of 2 retro follow-ups had design problems (owner may relax)
- [x] approval card: include the task's latest agent summary and the reviewer's verdict/summary (so the owner can decide without opening GitHub); spacing between PR link and Note label

## Third review round (all confirmed by main agent; fixed and committed)
- [x] follow-up kinds allowlisted (an implementer could file a `review` follow-up on its own PR)
- [x] stale review rejection ignored unless parent in review at the reviewed head
- [x] CI grace from when the shepherd first saw the head (human pushes too); `require_checks`
- [x] policy diff pinned to the judged head SHA; empty head never passes
- [x] cancelled/failed reviews don't condemn a head; owner Retry cancels stale reviews
- [x] pause keeps deploy_watch (finish/rollback) and prod_watch running; manual loop run on paused project → 409
- [x] chat replies tied to shutdown; crash recovery respects max attempts
- [x] cancelled incidents stay dismissed; retro excludes its own work; review findings deduped
- [ ] known limit: budget counts a run's cost only when it ends (in-flight runs count $0)
- [ ] idea: herdr gram intake — pick up owner-queued work posted from the Herdr phone app (herdr gram grab) as foundry tasks
- [x] issue-sourced tasks must report back: when a github_issue task ends without a PR (no_changes / blocked / failed / cancelled) comment the outcome on the issue (the intake comment promises it); once per outcome
- [ ] later: comments/PRs post as the owner's gh account — consider a bot account or GitHub App for clear provenance
- [x] pause messages (CLI, engine event, UI pause button text, docs/api.md) still say 'no loops act' — pause stops new work, but deploy_watch/prod_watch keep running
- [x] incidents need evidence: attach the deploy command's output tail and an optional `deploy.logs` (path or command) tail to "Release failed" incidents and to prod_watch incidents (the first real staging incident had only the health error)
- [x] REAL BUG (found by the first real staging deploy): sysproc.Wait kills the command's whole process group unconditionally (dogfood #7), so a deploy script that starts a long-running service has it killed on return. Deployers must leave processes started by a SUCCESSFUL deploy running; notifiers/observers keep killing leftovers. Outcome: the factory's SRE agent concluded 'config/port issue, not a regression' — wrong, because the incident had no logs (it couldn't see the service start then vanish). Fixing on init after the evidence change lands.
- [x] deploy/rollback output to temp files, not pipes (b167957): a service that keeps printing after the deploy returned died of SIGPIPE
- [x] ROLLBACK TEST (real): killed staging during PR #30's verify window → release e697fd5 rolled_back, staging back on healthy 8f8fefa within ~1.5 min, incident opened. The SRE's diagnosis was stale (no logs: daemon predated the evidence change); induced incident cancelled, its PR #31 closed. Dogfood daemon now runs 3f1ecbd with `deploy.logs` set
- [x] closed fixed friction issues #25 (CI/web build) and #27 (nohup service killed)
- [x] factory queue: #28 (health-failure reason names URL, count, elapsed; spec in the body) and #33 (intake includes OWNER/MEMBER/COLLABORATOR comments; public repo, so others are ignored)
- [x] OWNER (done): PR #19 — recommend REJECT: superseded by the example on init (its example put GH_TOKEN in inherit_env, handing agents foundry's GitHub token). It also conflicts with dogfood (foundry.example.yaml); approve → the factory must rebase it first, or reject — owner rejected #19 (2026-10-28); PR closed
- [x] friction #32: the dogfood setup now lives in examples/dogfood/ (config, release and observer scripts). The scripts run from foundry's clone, so agents can change them by PR (rated high). The live daemon runs on it (scratchpad/dogfood/run-daemon.sh)
- [x] factory merged #34 (#33: intake includes OWNER/MEMBER/COLLABORATOR comments) and #35 (#28: health-failure reason names URL, count, elapsed). Both auto-merged at low risk, released and verified healthy on staging, and their issues closed automatically
- [x] defect review of 47ee9be..b167957 (two fresh reviewers) → fixed: wildcard bind address broke the CLI, foundry open and orchestrator chat (Host guard 421) → LocalURL; public_url validated; exec intake over 1 MB is an error; secrets redacted from deploy/logs evidence and failure notes; deploy stdout bounded to a 64 KB tail; issue outcome comments serialised (no double post)
- [x] SECURITY: checks ran with foundry's full env (GH_TOKEN), and a check such as `go test` runs agent-written code. Checks and the new `setup` commands now run with the agent's environment
- [x] project `setup` commands (friction #20): run in each fresh worktree before the agent. The dogfood project now runs `npm ci` for web/
- [x] factory: #36 Deploys page shows a release's evidence log (PR #38, auto-merged, released healthy). Its run used the web setup and ran vitest/build in the worktree; friction #20 closed
- [x] cleanup (dropped): the deployHealth tally is keyed by unique deployment IDs, so the package-level map cannot cause cross-talk; not worth a run
- [ ] known gaps (low): prod_watch runs deploy.logs synchronously (30s max per new incident); an issue-outcome comment due at shutdown is not retried. (@mentions in agent text are now neutralised.)
- [x] UI pass on live dogfood data (Playwright): tables split short words mid-word ("foundr/y", "#3/0") → #37 → factory PR #39 (overflow-wrap instead of word-break), verified in a screenshot
- [x] merged tasks blocked by a failed release now become done when a later healthy release contains their merge commit (git merge-base --is-ancestor). Three such stale tasks (#23, #24, #30) should resolve on the next healthy release
- [x] factory: #40 wider release-log modal (PR #41, auto-merged, released healthy). Its release resolved the 3 stale blocked tasks: Overview shows Blocked 0
- [x] agent text posted to GitHub has @mentions outside code wrapped in backticks (no pings from a public repo)
- [x] redaction: SESSION removed from the secret words (it mangled paths containing CLAUDE_CODE_SESSION_ID)
- [x] orchestrator chat verified live (56s, correct counts; it repeated stale incident diagnoses, which resolved once the blocked tasks did)
- [x] PR #26 (Codex cost estimate): an independent review found flaky tests (reproduced: 2 of 120 runs failed under load) and unchecked .inf/.nan prices. Fixed both on the PR branch and merged dogfood into it (full suite and 57 web tests pass). foundry re-reviews it, then it returns to the owner for approval. Recommend APPROVE
- [x] OWNER (done): PR #14 — recommend REJECT as written: its Go cache advice belongs in project env/setup, and GOMODCACHE=/tmp with GOPROXY=off breaks module resolution. Its rule "never report a skipped check as passed; rate it medium; file friction" is worth adding to the implementer prompt by hand — rejected; the "never report a skipped check as passed" rule is now in AGENTS.md
- [x] #42 → PR #43 was the first release through the in-repo release script (22cccf4, healthy); friction #32 closed
- [x] the dogfood factory moved out of the session scratch dir to ~/.local/share/foundry-dogfood (data, clone, binary; staging rebuilt there). Start it with ~/.local/share/foundry-dogfood/run-daemon.sh; the CLI needs DOGFOOD=~/.local/share/foundry-dogfood FOUNDRY_CONFIG=$DOGFOOD/clone/examples/dogfood/foundry.yaml. The old scratch copy is untouched (backup)
- [ ] OWNER: to keep the daemon running across reboots, `foundry service install` (launchd) — not done for you
- [x] leaked worktrees: every run that ended without publishing left its worktree (4 found; ~100 MB each once setup runs npm ci). A sweep at start and after each run removes them; the newest worktree of a blocked task is kept for inspection
- [x] DRILL (real): a labelled synthetic ERROR line in the staging log → prod_watch signal → incident task → SRE run read the log evidence, found the drill, closed it with no code change and no PR (~1 min). The production-error loop works end to end; until then it had never fired (zero signals all night)
- [x] UI at phone width (390px): tables become cards, nav collapses to a menu, approval cards readable
- [x] REVIEW ROUND 2 (fresh reviewer over c54e18e..b4d762d), all findings confirmed and fixed except one:
  - mention quoting was bypassable (lone or escaped backticks, mismatched runs, multi-line spans) → now a zero-width space after the @ everywhere but fenced code
  - redaction missed loop errors (LastError, loop.error events, logs) and observer signals; a secret cut at a tail boundary left a fragment; URL passwords printed alone → all fixed
  - shipped-task resolution marked reverted commits done and fetched once per task → one fetch, reverted work stays blocked
  - setup failure output went into the next agent's task body (it might "fix" an outage by editing package.json) → output goes on the run instead
  - issue-report lock was global across a gh call → per task
  - NOT FIXED (owner decision): an agent that can run `git config` in its worktree can set core.fsmonitor or filters, which foundry's own git commands then run with foundry's environment (GH_TOKEN). Recorded in docs/proposals/agent-isolation.md; the fix is isolation (separate user or containers)
- [x] Codex smoke (local throwaway repo, separate daemon :7801): the Codex harness ran, setup and checks passed with the agent environment, and the agent edited the file
- [x] BUG found by the smoke: a task whose earlier attempt pushed commits but failed to open the PR was later marked done when the next attempt reported no changes — work stranded on an unmerged branch. Fixed: a no-change attempt publishes when the branch is not contained in the base (test fails without the fix)
- [x] `foundry project add NAME --repo OWNER/NAME`: adds a project to an existing config by inserting text (every other byte kept), validated before writing
- [x] DRILL 2 (evidence path): killed staging while #44's release (76d97bb, PR #45 `foundry deploys`) was verifying → rolled back to 22cccf4 in 1m30s. The incident carried the new reason text (URL, 3 consecutive failures, elapsed), the deploy output tail and the staging log tail. With that evidence the SRE agent rebuilt the released binary, found it healthy, and concluded correctly that the process was killed after start (last night, without evidence, it blamed the wrong cause). Its follow-up investigation task was cancelled (the kill was mine)
- [ ] the #44 task stays blocked by that drill until the next healthy release, which marks it done automatically (approving #26 would trigger one)
- [x] orchestrator chat delegation verified on a throwaway project: it queued a correctly scoped chore via the CLI in 28s and did not do the work itself (minor: it did not notice the factory was paused)
- [x] REVIEW ROUND 3 (fresh reviewer over 8488593..35e2d79): no way found for the sweep to delete a live or foreign worktree, or for the stranded-branch fix to publish a task that should be done. Fixed: mention neutralising (fences in lists or after HTML, unclosed fences, \@ and entity forms); project add could truncate a trailing block scalar → it now never steps into one, and a semantic guard refuses any insertion that changes existing settings; manual loop-run errors returned unredacted (the orchestrator holds a token) → redacted wrapper; reverted reverts; needless fetch. Skipped (cosmetic or narrow): ZWSP inside URLs and paths; stale base right after Prepare
- [x] PR #47 init → main opened for the owner (not merged): summary, safety gap, decisions
- [x] Friction page shows which filed issues are closed (the friction loop refreshes issue states) and lists open ones by default: 5 open of 11
- [x] friction #46 (agent-reported): incident evidence is now labelled as data, not instructions; the SRE noticed my drill's log line read like an instruction
- [x] retention: events older than 30 days and run logs older than 90 days are pruned daily (server.retain_events / retain_run_logs). The dogfood DB was 3.4 MB after one busy night, about 1 GB a year without it
- [x] stray test daemon from early in the night (scratchpad/run, random port) stopped by verified pid
- [x] factory budget: today's dogfood spend is about $21 of the $25 daily cap, and the day lasts until midnight, so I stopped queueing factory work at 04:25 to leave headroom for the owner
- [x] docs audit (CLI help and config vs every doc): 11 mismatches fixed, including two small CLI bugs (`task -h` and `run -h` errored; `project pause -h` treated "-h" as a project name)
- [x] `foundry task create` warns on stderr when the factory or project is paused, so the orchestrator relays it


## Morning, 2026-10-28

- [x] owner approved PR #26 (merged, released healthy); #47 (init → main), #48 (product direction), #49 (Connect button fix), #50 (AGENTS.md) merged
- [x] UI reachable over Tailscale (`tailscale serve --https=7797`); the instance's local config `$DOGFOOD/foundry.yaml` sets `public_url`
- [x] `examples/dogfood/update.sh`: one-step update of the maintainer's instance to `main`; AGENTS.md says to run it after every change to `main`. Tested live (47a21fe → 68d139d, then a no-op run); the undo-on-failed-health path is untested
- [x] stale branches deleted (factory branches of cancelled/rejected tasks, merged `init`, local `service-doctor`)

## UI redesign "Andon Board" and accounts — 2026-10-28 (branch `ui-rework`)

Product facts: `PRODUCT.md`. Direction contract: `.impeccable/surfaces/web-src-pages-overview-tsx.md`.
Shared files (one agent at a time): `web/src/styles.css`, `web/src/components/Shell.tsx`,
`docs/ui.md`, `web/src/types.ts`.

Stream A — visual world (main agent sets the system, then delegates page rollout)
- [x] Fonts self-hosted (`web/src/assets/fonts/`, Barlow + Barlow Condensed, OFL)
- [x] Lamp and run-bar rules with tests (`web/src/lib/lamp.ts`, `web/src/lib/runbar.ts`)
- [x] Tokens, base components and shell in `styles.css`; `Shell.tsx`; `AndonBoard.tsx`
- [x] Overview rebuilt to the contract; checked desktop, dark and 390px against a seeded throwaway daemon, and on a built binary (CSP, fonts, live stream)
- [x] Finish review (fresh agent): fix, then fixes 1-7 resolved; DESIGN.md via documenter; `docs/ui.md` rewritten
- [ ] Deferred: compact lamp row on non-Overview pages; window details on focus; replace the ▸ glyph
      marker in `LogViewer.tsx` with a drawn icon (Run detail rollout)
- [ ] Rollout: Task detail, Run detail, Chat, Approvals, phone layout, then remaining pages

Stream B — accounts, sign-in and in-app config editor (branch `accounts-config`)
Design: `docs/proposals/accounts-and-config-editor.md` (approved). Frozen by the main agent:
`internal/domain/user.go`, the user/session methods and errors in `internal/store/store.go`,
the API table in the proposal, and the `internal/auth` and `internal/config` APIs in the unit
prompts.
- [x] U1 store: migration 2, users.go + tests — `internal/store/sqlite/`
- [x] U2 auth: password, session, limiter, roles + tests — `internal/auth/`
- [x] U3 config: document tree, changes on the YAML tree, schema (9 types incl. `any` for options
      maps), atomic write — `internal/config/`
- [x] U5 web: sign-in/setup, session client, account panel, Users page, role-aware actions
- [x] U4 API (after U1-U3): middleware, routes, cmd/foundry switch to config.WriteFileAtomic,
      `foundry user reset-password`, docs/api.md, openapi.yaml
- [x] U6 web (after U5): config page — visual editor + raw tab
- [x] Docs: invariants in AGENTS.md, README safety model, docs/design.md, getting-started
- [x] Verify on a throwaway daemon (setup, sign-in, viewer, users, visual save keeps comments,
      token 403 on config/users); defect review: 7 of 10 fixed, 3 documented (setup page open by
      decision, Secure cookie behind a proxy needs public_url, file access needs agent isolation)
- [ ] PR, CI, owner decides merge (after merge the dogfood instance asks for an owner account)
