# Customizable Agents (Phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Owners can add, edit, remove, restore and reset agents from the Agents page or
`onlet.yaml`, and every agent card shows its full prompt and every way it is used.

**Architecture:** `onlet.yaml` stays the only store. Config gains `disabled` and `prompt_text`
on agents, a computed origin, and a computed list of uses; validation refuses removals that
something depends on. The engine reads inline prompts and refuses disabled agents. `GET /agents`
returns the richer view; the UI writes through the existing owner-only `PATCH /config/document`.

**Tech Stack:** Go 1.27 (`mise exec --`), React + Vite + TypeScript, Vitest.

**Spec:** `docs/proposals/customizable-agents.md` (Phase 1 only; phase 2 gets its own plan).

## Global Constraints

- All `AGENTS.md` invariants hold. Agent edits are config edits: owner session only; the API
  token never gets them (invariant 12). No new write route.
- Unknown YAML keys and unknown JSON request fields stay errors (invariant 10).
- `internal/engine/prompts/**`, `internal/config/defaults.go`, `internal/engine/helpers.go`,
  `internal/api/routes.go` are high risk. Do not move logic out of them to dodge the rule.
- New Go tests go in **new files** (other PRs touch the existing test files).
- Tests never touch `~`, the network or a fixed port.
- Keep in sync in the same task: `docs/api.md`, `docs/openapi.yaml`, `web/src/types.ts` (API);
  `onlet.example.yaml`, README (config keys); README **Commands** table (CLI flag); `docs/ui.md`
  (UI page); `docs/design.md` (behaviour).
- Error messages start with the dotted path of the setting at fault, then `: `.
- The Agents page manages **global** agent entries only. Per-project overrides
  (`projects.<p>.agents`) stay in the Config editor and YAML.

## Review Focus

1. **Removing a built-in that a default route still uses** (e.g. `reviewer`, routed `review` by
   default): validation refuses with a message naming the route and the fix; the pipeline never
   silently loses PR reviews. Pinned in Task 1.
2. **A queued or retried task whose role was disabled afterwards**: the run does not start; the
   task blocks with `agent "<name>" is disabled`. Pinned in Task 2.
3. **An agent whose prompt file is missing or unreadable**: `GET /agents` still returns 200, with
   `prompt_error` on that agent; the card shows the error instead of the prompt. Pinned in Task 3.
4. **The config file changed since the owner opened the page** (409 stale version): the editor
   says so and keeps the form, using the Config page's existing `saveFailure` handling. Pinned
   in Task 4.
5. **Remove then Restore of a built-in that had no entry in the file** leaves the file as it
   was: no `{}` or `disabled: false` residue. Pinned in Tasks 1 and 4.

---

### Task 1: Config: disabled, inline prompts, origin, uses, validation

**Files:**
- Modify: `internal/config/config.go` (Agent struct, around line 97)
- Modify: `internal/config/defaults.go` (`applyAgentDefaults`, `applyRoutingDefaults`)
- Modify: `internal/config/load.go` (`AgentsFor` ~229, `AgentForProject` ~273)
- Modify: `internal/config/validate.go` (agents ~41, routing ~51, projects ~89-110)
- Create: `internal/config/agents.go` (origin type, `AgentUse`, `AgentUses`, `KindUse`)
- Test: `internal/config/agents_test.go` (new)
- Docs: `onlet.example.yaml`, README (roles paragraph), `docs/design.md` (agent profile entry)

**Interfaces:**
- Produces (frozen; later tasks use these exact names):
  ```go
  // in config.go, Agent gains:
  PromptText string      `yaml:"prompt_text" json:"prompt_text,omitempty"`
  Disabled   bool        `yaml:"disabled" json:"disabled,omitempty"`
  Origin     AgentOrigin `yaml:"-" json:"origin"`

  // in agents.go:
  type AgentOrigin string
  const (
      OriginBuiltin AgentOrigin = "builtin"
      OriginEdited  AgentOrigin = "edited"
      OriginCustom  AgentOrigin = "custom"
  )
  type AgentUse struct {
      Kind        string `json:"kind,omitempty"`    // task kind; "" for chat and delegation
      Project     string `json:"project,omitempty"` // set when the route is a project's own
      Description string `json:"description"`
  }
  func (c *Config) AgentUses(project string) map[string][]AgentUse
  func KindUse(kind string) string

  // in defaults.go:
  func BuiltinAgents() map[string]Agent // fresh map each call, prompts as written ("builtin:<name>")
  func (c *Config) IsDefaultRoute(kind string) bool // true when routing[kind] was filled by defaults
  ```
- `AgentsFor` still returns disabled agents (the API lists them). `AgentForProject` refuses them.

- [ ] **Step 1: Write the failing tests** in `agents_test.go`, parsing YAML through the same
  helper the existing config tests use:
  - `TestBuiltinNotInFileHasBuiltinOrigin`: empty `agents:` → `c.Agents["reviewer"].Origin == OriginBuiltin`.
  - `TestBuiltinOverrideIsEditedAndReplacesWhole`: `agents: {reviewer: {harness: claude, prompt: builtin:reviewer}}`
    → Origin `OriginEdited`, `Model == ""` (no field merge with the built-in's `sonnet`).
  - `TestDisabledOnlyEntryKeepsBuiltinSettings`: `agents: {improver: {disabled: true}}` plus
    `routing: {improve: implementer}` → `Disabled`, `Harness == "codex"`,
    `Prompt == "builtin:improver"`, Origin `OriginBuiltin`, and `Validate()` returns nil.
  - `TestCustomAgentOrigin`: `agents: {docs: {harness: claude, prompt_text: "Write docs."}}` →
    `OriginCustom`, `PromptText == "Write docs."`, `Prompt == ""`.
  - `TestPromptAndPromptTextBothSetIsError`: error contains `agents.docs: set prompt or prompt_text, not both`.
  - `TestDisablingOrchestratorIsError`: error contains
    `agents.orchestrator.disabled: chat needs the orchestrator; edit it instead of removing it`.
  - `TestDisabledAgentOnDefaultRouteIsError`: `agents: {reviewer: {disabled: true}}` → error equals
    (as one of the joined lines) `agents.reviewer.disabled: routing.review (built-in default) still uses it (reviews every pull request before merge); route review to another agent first`.
  - `TestDisabledAgentOnExplicitRouteIsError`: `routing: {feature: docs}` with `docs` disabled →
    `agents.docs.disabled: routing.feature still uses it (builds features); route feature to another agent first`.
  - `TestProjectDisableBlockedByInheritedRoute`: project `app` overrides `reviewer: {disabled: true}`
    → `projects.app.agents.reviewer.disabled: routing.review (built-in default) still uses it (reviews every pull request before merge); set projects.app.routing.review to another agent first`.
  - `TestProjectDisableAllowedWhenRerouted`: same plus `projects.app.routing.review: implementer` → no error.
  - `TestProjectPromptTextClearsInheritedPrompt`: project override `reviewer: {prompt_text: "x"}` →
    `AgentsFor("app")["reviewer"]` has `PromptText "x"`, `Prompt ""`.
  - `TestAgentForProjectRefusesDisabled`: explicit role `docs` (disabled) → error contains `agent "docs" is disabled`.
  - `TestAgentUsesGlobal`: default config → `uses["orchestrator"][0] == {Description: "the agent you chat with"}`;
    `uses["reviewer"]` contains `{Kind: "review", Description: "reviews every pull request before merge"}`;
    every enabled non-orchestrator agent's **last** use is `{Description: "the orchestrator may also hand it work by name"}`;
    an agent with no routes has exactly that one use; a disabled agent has none.
  - `TestAgentUsesProjectRoute`: global view includes `{Kind: "feature", Project: "app", ...}` for an
    agent only `projects.app.routing.feature` names; `AgentUses("app")` shows the effective
    route for `app` with `Project: "app"` only where the project's own routing set it.
  - `TestSchemaOmitsOrigin`: `Schema()` has no `origin` field under agents, and has `disabled` and `prompt_text`.
- [ ] **Step 2: Run them and see them fail**
  Run: `mise exec -- go test -race -count=1 -run 'Origin|Disabled|PromptText|Prompt|AgentUses|AgentFor|Schema' ./internal/config/`
  Expected: compile failure (undefined `OriginBuiltin`, `AgentUses`, fields).
- [ ] **Step 3: Implement.**
  - Agent fields as frozen above, each with a doc comment (the schema reads them for help text).
  - `defaults.go`: lift the defaults literal into `BuiltinAgents()` (stays in this file);
    `applyAgentDefaults` sets Origin: absent → copy, `OriginBuiltin`; present with a built-in name
    and every field zero except `Disabled` → built-in copy with `Disabled: true`, `OriginBuiltin`;
    present with a built-in name otherwise → `OriginEdited`; other names → `OriginCustom`.
    `resolvePrompt` is unchanged (empty `Prompt` is already left alone).
  - `applyRoutingDefaults` records filled kinds in an unexported `defaultRoutes map[string]bool`
    on Config (`yaml:"-" json:"-"`); `IsDefaultRoute` reads it.
  - `AgentsFor`: project override `Disabled` true → `base.Disabled = true`; `Prompt` set → also
    clear `PromptText`; `PromptText` set → set it and clear `Prompt`. A project-only agent gets
    `OriginCustom`.
  - `AgentForProject`: after lookup, a disabled agent → `fmt.Errorf("config: agent %q is disabled", name)`.
  - `validate.go`: replace the "prompt must not be empty" checks with: both set → the "not both"
    message; neither set → existing `.prompt: must not be empty`. Add the orchestrator rule (global
    and `projects.<p>.agents.orchestrator.disabled`). For routes: for each kind, the effective agent
    (global view: `c.Routing`; project view: project routing else global) must not be disabled;
    message templates exactly as in the tests, with ` (built-in default)` after the route only
    when `IsDefaultRoute(kind)` and the route is the global one. A project override that sets both
    `prompt` and `prompt_text` → `projects.<p>.agents.<a>: set prompt or prompt_text, not both`.
  - `agents.go`: `KindUse` strings, exactly:
    feature `builds features`; bugfix `fixes bugs`; issue `works on GitHub issues`; chore
    `does chores`; incident `handles failed deploys and production signals`; review
    `reviews every pull request before merge`; improve
    `fixes friction and review rejections (retrospectives)`; question `answers questions and triages issues`.
    `AgentUses` order per agent: chat use (orchestrator only), then kind uses sorted by kind then
    project, then the delegation use (enabled non-orchestrator agents only).
- [ ] **Step 4: Run the package tests and see them pass**
  Run: `mise exec -- go test -race -count=1 ./internal/config/`
  Expected: PASS (including the existing `validate_test.go`, `schema_test.go` and `TestExampleFileLoads` wherever it lives).
- [ ] **Step 5: Docs.** In `onlet.example.yaml`, under `agents`, add commented examples of
  `disabled: true` on a built-in and a custom agent with `prompt_text: |`. README roles paragraph:
  built-ins can be edited or removed (`disabled: true`), custom agents can be added, prompts can
  be inline. `docs/design.md` agent-profile entry: origin, disabled, prompt_text, and that
  removal is refused while a route uses the agent. Run `mise exec -- go test -race -count=1 ./...`
  for the example-file test. Expected: PASS.
- [ ] **Step 6: Commit** `git commit -m "Let agents be removed and carry inline prompts"` (with the
  repo's co-author trailer).

### Task 2: Engine: inline prompts, exported prompt lookups, refuse disabled agents

**Files:**
- Modify: `internal/engine/prompt.go` (`loadPrompt`, `buildInstructions`)
- Modify: `internal/engine/chat.go:58`
- Modify: `internal/engine/helpers.go:41` (`harnessFor`)
- Test: `internal/engine/agent_prompt_test.go` (new)

**Interfaces:**
- Consumes: `config.Agent.PromptText`, `config.Agent.Disabled` (Task 1).
- Produces:
  ```go
  // AgentPrompt is the agent's own instructions: PromptText, else the builtin or file Prompt.
  func (e *Engine) AgentPrompt(a config.Agent) (string, error)
  // BuiltinPrompt is the embedded prompts/<name>.md, or an error for an unknown name.
  func (e *Engine) BuiltinPrompt(name string) (string, error)
  ```

- [ ] **Step 1: Write the failing tests:**
  - `TestAgentPromptPrefersInlineText`: `AgentPrompt(config.Agent{PromptText: "Do X."})` == `"Do X."`.
  - `TestAgentPromptReadsBuiltin`: `AgentPrompt(config.Agent{Prompt: "builtin:reviewer"})` starts with `# Role: reviewer`.
  - `TestBuiltinPromptUnknown`: `BuiltinPrompt("nope")` errors with `unknown builtin prompt "nope"`.
  - `TestBuildInstructionsUsesPromptText`: instructions for `{PromptText: "Inline role."}` start with `Inline role.` and contain the common prompt.
  - `TestDisabledRoleBlocksRun`: with the fake harness, queue a task with role `docs`, then reload
    config with `docs: {disabled: true, ...}` (routing elsewhere); the task ends `blocked` and its
    reason contains `agent "docs" is disabled`; the fake harness was never started. Follow the
    existing engine tests' fixture pattern for config reload and the fake harness.
- [ ] **Step 2: Run and see them fail**
  Run: `mise exec -- go test -race -count=1 -run 'AgentPrompt|BuiltinPrompt|PromptText|DisabledRole' ./internal/engine/`
  Expected: compile failure, then (after stubs) `TestDisabledRoleBlocksRun` fails because the run starts.
- [ ] **Step 3: Implement.** `BuiltinPrompt` is the builtin branch of `loadPrompt`; `AgentPrompt`
  returns `PromptText` when non-empty, else `loadPrompt(a.Prompt)`. `buildInstructions` and
  `chat.go` call `AgentPrompt`. `harnessFor`: a disabled agent → `fmt.Errorf("agent %q is disabled", agentName)`.
  Confirm where the runner turns a `harnessFor` error into a task status; if it fails rather than
  blocks, make it block (a disabled role is a config decision, not a transient failure), with the
  error as the reason. If that path is not obvious, stop and report instead of guessing.
- [ ] **Step 4: Run and see them pass**
  Run: `mise exec -- go test -race -count=1 ./internal/engine/`
  Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "Read inline agent prompts and refuse disabled agents"`.

### Task 3: API, client and CLI: the richer agent view

**Files:**
- Modify: `internal/api/controller.go` (Controller interface)
- Modify: `internal/api/handlers.go:78` (`AgentView`), `:421` (`listAgents`)
- Modify: the api tests' fake controller (find it with `grep -rn "func (.*) SendChat" internal/api`)
- Modify: `internal/client/client.go:379` (`Agents` takes a project)
- Modify: `cmd/onlet/commands.go:171` (`cmdAgents`)
- Docs: `docs/api.md`, `docs/openapi.yaml`, README **Commands** table
- Test: `internal/api/agents_view_test.go`, `cmd/onlet/agents_cmd_test.go` (new)

**Interfaces:**
- Consumes: Task 1 (`Origin`, `Disabled`, `AgentUses`, `BuiltinAgents`), Task 2 (`AgentPrompt`, `BuiltinPrompt`).
- Produces (frozen for Task 4; JSON names exact):
  ```go
  // Controller gains:
  AgentPrompt(a config.Agent) (string, error)
  BuiltinPrompt(name string) (string, error)

  type AgentView struct {
      Name              string             `json:"name"`
      Origin            config.AgentOrigin `json:"origin"`
      Disabled          bool               `json:"disabled"`
      Harness           string             `json:"harness"`
      HarnessType       string             `json:"harness_type"`
      Model             string             `json:"model"`
      Prompt            string             `json:"prompt"`              // ref: builtin:<name> or a path; "" when inline
      PromptText        string             `json:"prompt_text"`         // resolved instructions
      PromptError       string             `json:"prompt_error,omitempty"`
      PromptUpdates     bool               `json:"prompt_updates"`      // prompt is builtin:<own name>
      BuiltinPromptText string             `json:"builtin_prompt_text,omitempty"` // edited built-ins whose prompt was replaced
      Description       string             `json:"description"`
      Args              []string           `json:"args"`
      Kinds             []string           `json:"kinds"`               // kept: global kinds routed here
      Uses              []config.AgentUse  `json:"uses"`
  }
  ```
  `GET /agents?project=<p>`: that project's merged view (`AgentsFor(p)`, `AgentUses(p)`); unknown
  project → 400 via `api.ErrInvalid`. No query → global view. Route permission unchanged (read).

- [ ] **Step 1: Write the failing tests** (`agents_view_test.go`, using the existing httptest
  server helpers):
  - `TestAgentsViewShowsOriginPromptAndUses`: default config → `reviewer` has `origin "builtin"`,
    `prompt_updates true`, `prompt_text` starting `# Role: reviewer`, `uses` containing the review use.
  - `TestAgentsViewEditedPromptShowsBuiltinForCompare`: `reviewer: {harness: claude, prompt_text: "Mine."}`
    → `origin "edited"`, `prompt_updates false`, `prompt_text "Mine."`, `builtin_prompt_text` starts `# Role: reviewer`.
  - `TestAgentsViewMissingPromptFileStill200`: an agent whose `prompt` is a path under `t.TempDir()`
    that does not exist → 200, that agent has `prompt_error` non-empty and `prompt_text ""`.
  - `TestAgentsViewListsDisabled`: `improver: {disabled: true}` → present with `disabled true`, `uses []`.
  - `TestAgentsViewProjectQuery`: `?project=app` with an app override reflects it; `?project=nope` → 400.
  - CLI (`agents_cmd_test.go`, stubbed API as existing CLI tests do): `onlet agents` omits disabled
    agents and prints an `ORIGIN` column; `onlet agents --all` includes them with origin `removed`.
- [ ] **Step 2: Run and see them fail**
  Run: `mise exec -- go test -race -count=1 -run 'AgentsView|Agents' ./internal/api/ ./cmd/onlet/`
  Expected: FAIL (fields missing / compile errors).
- [ ] **Step 3: Implement.** `listAgents` builds views as specified; `PromptUpdates` is
  `a.Prompt == "builtin:"+name && a.PromptText == ""`; `BuiltinPromptText` is filled only when the
  name is in `BuiltinAgents()` and `PromptUpdates` is false. Engine already satisfies the two new
  Controller methods from Task 2; add them to the fake. Client `Agents(ctx, project string)`
  adds `?project=` when non-empty. `cmdAgents`: new `--all` flag; columns
  `ROLE, ORIGIN, HARNESS, MODEL, KINDS, DESCRIPTION`. The orchestrator reads `onlet agents`, so
  hiding disabled agents there is what keeps them out of its choices.
- [ ] **Step 4: Docs.** `docs/api.md`: the `GET /agents` row gets `?project=`, and a paragraph
  describing `AgentView` fields, including that anyone who can read sees `prompt_text`, because
  every agent run is given it verbatim, so prompts must never hold secrets. `docs/openapi.yaml`:
  same. README Commands table: `agents [--all]`.
- [ ] **Step 5: Run and see them pass**
  Run: `mise exec -- go vet ./... && mise exec -- go test -race -count=1 ./internal/api/ ./cmd/onlet/`
  Expected: PASS, including `TestOpenAPISpecMatchesRoutes`.
- [ ] **Step 6: Commit** `git commit -m "Show each agent's origin, prompt and uses in the API and CLI"`.

### Task 4: Web: Agents page with owner editing

**Files:**
- Modify: `web/src/types.ts` (`AgentView`, `AgentUse`, `AgentOrigin`)
- Create: `web/src/lib/agentChanges.ts`, `web/src/lib/agentChanges.test.ts`
- Create: `web/src/components/AgentCard.tsx`, `web/src/components/AgentEditor.tsx`
- Modify: `web/src/pages/Agents.tsx`, `web/src/components/NewTaskModal.tsx` (hide disabled roles)
- Modify: CSS next to the existing agent-card styles
- Docs: `docs/ui.md` (Agents entry)

**Interfaces:**
- Consumes: `AgentView` JSON from Task 3; `useConfigDocument`, `useSaveConfigDocument`
  (`web/src/queries.ts`), `ConfigChange`, `saveFailure` (`web/src/components/config/saveFailure.ts`),
  `useCan('administer')` (`web/src/auth`), `PageHeader` `actions` prop.
- Produces (`agentChanges.ts`):
  ```ts
  export type AgentEntry = Record<string, unknown>; // the file's agents.<name>, if present
  export interface AgentForm {
    name: string; description: string; harness: string; model: string; args: string[];
    promptMode: 'ref' | 'text'; promptRef: string; promptText: string;
  }
  export function formFromAgent(a: AgentView): AgentForm;
  export function saveChanges(form: AgentForm): ConfigChange[];
  export function removeChanges(a: AgentView, entry: AgentEntry | undefined): ConfigChange[];
  export function restoreChanges(name: string, entry: AgentEntry | undefined): ConfigChange[];
  export function resetChanges(name: string): ConfigChange[];
  ```

- [ ] **Step 1: Write the failing tests** (`agentChanges.test.ts`):
  - `removeChanges` custom → `[{op:'delete', path:['agents','docs']}]`.
  - `removeChanges` built-in, no entry → `[{op:'set', path:['agents','improver'], value:{disabled:true}}]`.
  - `removeChanges` edited (entry present) → `[{op:'set', path:['agents','reviewer','disabled'], value:true}]`.
  - `restoreChanges('improver', {disabled:true})` → `[{op:'delete', path:['agents','improver']}]`.
  - `restoreChanges('reviewer', {harness:'claude', disabled:true})` → `[{op:'delete', path:['agents','reviewer','disabled']}]`.
  - `resetChanges('reviewer')` → `[{op:'delete', path:['agents','reviewer']}]`.
  - `saveChanges` with `promptMode 'text'` → one `set` of `['agents',name]` whose value has
    `prompt_text` and no `prompt`; empty `model`, `description` and `args` are omitted.
  - `saveChanges` with `promptMode 'ref'` → value has `prompt: promptRef` and no `prompt_text`.
  - `formFromAgent` on an inline agent → `promptMode 'text'`; on `builtin:x` → `'ref'`.
- [ ] **Step 2: Run and see them fail**
  Run: `cd web && npx vitest run src/lib/agentChanges.test.ts`
  Expected: FAIL (module not found).
- [ ] **Step 3: Implement `agentChanges.ts`** to the tests, then the UI:
  - **AgentCard**: name (mono), a text badge: `built-in`, `edited`, `custom` or `removed`;
    description; harness (type) and model (`harness default` when empty); **Used for**: one line
    per use: kind tag (when set), description, `in <project>` (when set); **Prompt**: the first
    8 lines in a `<pre>` with "Show full prompt"; `prompt_error` instead, in the error style, when
    set. Edited with `prompt_updates` false: the line "Customized: no longer receives onlet's
    prompt updates" and a "Compare with built-in" toggle showing `builtin_prompt_text` beside the
    prompt (stacked under phone width).
  - Owner-only controls (left out for others): Edit, Remove (built-in/edited; sets `disabled`)
    or Delete (custom; deletes the entry, since nothing would bring it back; a refinement of the
    spec's "Remove sets disabled", Claude's call),
    Reset to built-in (edited), Restore (removed). Remove/Delete/Reset ask for confirmation, like
    Users. Each action reads the file entry from `useConfigDocument().data.document.agents?.[name]`
    and saves with that document's `version`.
  - **AgentEditor**: a sheet like Users' "Add user": name (new only; refuse empty or existing
    names client-side), description, harness (select of configured harness names from
    `GET /config`), model, args (one per line), prompt: "Use a prompt reference" (text input,
    `builtin:<name>` or a path) or "Write the prompt here" (textarea, prefilled with the agent's
    current `prompt_text`). "New agent" (PageHeader action) offers "Start from" any existing
    agent. A save failure shows `saveFailure`'s message under the form and keeps the form open;
    422 problems list their messages.
  - **Agents page**: enabled agents first; removed agents under a "Removed" heading. Empty state
    unchanged.
  - **NewTaskModal**: the role list leaves out disabled agents.
- [ ] **Step 4: Run web checks**
  Run: `cd web && npm ci && npm run typecheck && npm test && npm run build`
  Expected: all pass.
- [ ] **Step 5: Real browser check.** Build (`mise exec -- scripts/build.sh`), start a throwaway
  daemon (minimal config, spare port, temp `data_dir`), sign in as owner, and at desktop and phone
  width: edit `reviewer`'s prompt (card shows "Customized" and compare works), reset it, remove
  `improver` then restore it (the file has no `improver` entry afterwards), try removing
  `reviewer` (validation message shown, file unchanged), create a custom agent from `triager`.
  Sign in as a viewer: no controls. Screenshot each state.
- [ ] **Step 6: Docs.** `docs/ui.md` Agents entry: cards, badges, uses, prompt view, owner actions.
- [ ] **Step 7: Commit** `git commit -m "Let owners add, edit, remove and restore agents on the Agents page"`.

### Task 5: Strong built-in prompts and descriptions (main agent, not a subagent)

Judgment work per the spec: the orchestrating session (or the owner) writes it.

**Files:** `internal/engine/prompts/{orchestrator,implementer,reviewer,triager,sre,improver}.md`,
descriptions in `BuiltinAgents()` (`internal/config/defaults.go`).

- [ ] **Step 1:** Re-read each prompt against `common.md`, `tasks/lessons.md` and the runner's
  real inputs; tighten each one (what it receives, what done means, what it must never do).
  Rewrite each description to say what the agent does and when it runs.
- [ ] **Step 2:** `mise exec -- go test -race -count=1 ./internal/engine/ ./internal/config/` → PASS.
- [ ] **Step 3: Commit** `git commit -m "Tighten the built-in agent prompts and descriptions"`.

### Task 6: Final verification (orchestrator)

- [ ] Run the full CI set from `AGENTS.md` on the settled branch: `go vet`, `gofmt -l` empty,
  `go test -race -count=1 ./...`, and the web line. All pass.
- [ ] Repeat the Task 4 browser walk-through on the final build.
- [ ] Open the PR against `main`; its body lists the Claude's-call decisions from the spec.
