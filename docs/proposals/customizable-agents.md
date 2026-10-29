# Customizable agents with strong defaults

Status: proposal, for the owner's approval. Two phases; phase 2 builds on phase 1.

## Why

Today onett compiles six roles into the binary (`orchestrator`, `implementer`, `reviewer`,
`triager`, `sre`, `improver`, in `internal/config/defaults.go`) and adds back any that
`onett.yaml` leaves out on every config load. So a person can override a built-in but never
remove one, and the Agents page is read-only. The page also shows `builtin:reviewer` instead of
the instructions, and lists only the task kinds routed to an agent, not the other ways it gets
used. An agent with "none routed" can still be picked by the orchestrator, which sees every
role and may delegate to any of them by name. On the maintainer's instance that includes the
Codex implementer the config deliberately routes around.

The owner's view: most people will never think about agents, so the defaults must do good work
out of the box; power users must be able to create, change and remove anything. The owner also
wants the defaults to follow the model-economy strategy they use day to day (below), which
works well for them, while staying editable.

## Goals

- Every agent is visible, editable and removable. People can add their own.
- Each default is a strong example: its full prompt is readable, and it says plainly when it is
  used.
- Removing something another part of onett depends on fails with a message that says what to
  change first.
- Untouched defaults improve when onett is upgraded; a customized one stays the owner's.
- (Phase 2) The default set follows the model-economy strategy: a strong model plans and
  accepts, a cheaper model executes, and failures re-plan before they step up a tier.

## Non-goals

- A second place to store agents. `onett.yaml` stays the single source of truth; the UI edits it.
- Per-user agents. Agents belong to the instance, as config does.
- Changing how harnesses work, or adding new harnesses.

## Anti-goals (a failure even if it works)

- Any path that lets an agent edit agents. Agent edits are config edits: owner session only
  (invariant 12). The API token never gets them.
- A default that quietly costs a lot more than today without the owner being able to see why.
- Removing an agent silently disabling part of the pipeline (for example, PR reviews stop
  happening because `reviewer` was removed).

## Constraints

All invariants in `AGENTS.md` hold. In particular: agents judge, code acts (a planner's output
is only text in a task); risk only goes up; agents get `sysproc.AgentEnv`; a denial or stall is
terminal; evidence shown to an agent is redacted and starts with `evidenceNote`; unknown YAML
keys stay errors. Most of the files involved (`internal/engine/prompts/**`,
`internal/config/defaults.go`, `internal/engine/helpers.go`, `internal/api/routes.go`) are rated
high risk, so the owner approves each PR.

## How things work today (facts the design relies on)

- Every task gets its agent at creation through `Config.AgentForProject` (`internal/config/load.go`):
  an explicit `role`, else the project's `routing`, else the global `routing`. PR reviews
  (`loop_shepherd.go`, kind `review`), incidents from deploys and signals, follow-ups and
  retrospectives all go through this. The one exception is chat, which hard-codes
  `orchestrator` (`internal/engine/chat.go`).
- `Validate` already rejects routing that names an unknown agent.
- The visual config editor already adds and removes map entries (`ConfigSection.tsx`,
  `MapEditor`) and saves through `PATCH /config/document`, which needs an owner session.
- A retry re-runs the same agent with the failure appended to the task body
  (`helpers.go`, "## Attempt N: ..."), up to `limits.max_attempts` (default 3).

## Phase 1: customizable agents

### Config

- Built-ins stay defaults that are merged in when `onett.yaml` does not name them. That keeps the
  zero-config experience.
- New agent field `disabled: true` removes an agent, built-in or not. A disabled agent is not
  routable, not usable with `--role`, and not shown to the orchestrator. It still appears on the
  Agents page, marked removed, so it can be restored.
- New agent field `prompt_text`: the instructions inline in `onett.yaml`. An agent has exactly one
  of `prompt` (`builtin:<name>` or a file path, as today) or `prompt_text`. This is what the UI
  writes when someone edits a prompt, so editing never needs a second file. *(Builder's call on
  the exact YAML style, such as a block scalar.)*
- Layering, as today plus the new fields *(Claude's call)*: a global entry with a built-in's
  name replaces the built-in whole (`applyAgentDefaults` adds a default only when the name is
  absent), so the UI writes the full entry when it edits a built-in, copying every field it
  does not change. A project override (`projects.<p>.agents`) still merges field by field
  (`AgentsFor`); setting `prompt_text` there clears the inherited `prompt` and vice versa. A
  project may set `disabled: true` to remove an agent for that project only; it cannot re-enable
  one that is disabled globally, and it cannot change `planner` or `step_up` (phase 2).
  Validation runs on the merged result for the global view and for every project.
- Validation, with messages that name the fix:
  - `orchestrator` cannot be disabled: chat needs it. It can be edited.
  - Routing (global or per project, including the built-in default routes) that points at a
    disabled agent is an error: `agents.reviewer is disabled but routing.review (built-in
    default) still sends pull-request reviews to it; route review to another agent first`.
  - Exactly one of `prompt` / `prompt_text`.
  - (Phase 2) `planner` must name an enabled agent that has no `planner` of its own.

### Where an agent comes from

Each agent has an origin, computed rather than stored:

- **built-in**: not named in `onett.yaml`.
- **edited built-in**: a built-in name that `onett.yaml` overrides. If its prompt is still
  `builtin:<name>`, it keeps getting onett's prompt updates. If its prompt was replaced
  (`prompt_text` or a file), it no longer does, and the UI says so.
- **custom**: any other name.

### When an agent is used

Computed from config, shown on the page and returned by the API:

- each task kind routed to it, globally and per project;
- fixed uses spelled out in words: `orchestrator` is "the agent you chat with"; whatever `review`
  routes to "reviews every pull request before merge"; `incident` "handles failed deploys and
  production signals"; `improve` "fixes friction and review rejections (retrospectives)";
- "the orchestrator may also hand it work by name", for every enabled agent other than the
  orchestrator. An agent with no routes gets this line alone, so "none routed" no longer reads
  as "never used".

### API

`GET /agents` gains, per agent: `origin` (`builtin` | `edited` | `custom`), `disabled`,
`prompt_text` (the resolved instructions: the built-in's embedded text, the file's contents, or
the inline text), `prompt_updates` (true while the prompt is still `builtin:`), and `uses` (a
list of `{kind, project, description}`), and for an edited built-in `builtin_prompt_text` (the
current built-in, for the compare view). `GET /agents?project=<p>` returns that project's merged
view; without it, the global view. Anyone who can read sees prompt text, as they see the
redacted config: every agent run is given it verbatim, so it must never hold secrets, and the
docs say so. *(Claude's call.)* Writes need no new route: the UI sets and deletes
`agents.<name>` through `PATCH /config/document`, which already supports `delete` and edits
the YAML tree in place, keeping the file's comments.

Keep `docs/api.md`, `docs/openapi.yaml` and `web/src/types.ts` in step.

### Agents page

- Each card shows name, origin badge, description, harness and model, **when it is used**, and
  the prompt. The prompt shows the first lines, with a full view on request.
- Owners also get **Edit** (a form for description, harness, model, args and prompt; editing a
  built-in's prompt starts from its current text), **Remove** (sets `disabled`; the server's
  validation message shows inline when something depends on it), **Restore** for a removed
  agent, **Reset to built-in** for an edited built-in (drops its override), and **New agent**
  (blank, or "copy from" an existing agent).
- An edited built-in whose prompt was replaced says "Customized: no longer receives onett's
  prompt updates", with a link to compare against the current built-in.
- Operators and viewers see everything read-only. Controls a role cannot use are left out
  (`docs/ui.md` convention).
- `onett agents` prints origin and uses too.

### Strong defaults

Each built-in prompt is reviewed and tightened as part of this phase, and each description is
rewritten to say what the agent does *and* when it runs. Prompt wording is judgment work: it
stays with the strong-model orchestrator of the build (or the owner), not a cheap implementer.

## Phase 2: the model-economy strategy as the default

### The strategy (the owner's, as it runs in their own agent setup)

A strong model does the thinking: root cause, design, and a plan detailed enough to follow
without making decisions. A cheaper model carries out that plan. When a run fails, the strong
model first writes a sharper plan; only after that fails does the run move up a model tier, one
step at a time and with a stated reason. The strong model accepts or rejects the result.
Defect review goes to a separate fresh-context agent that never sees the plan, and automated
checks outrank any reviewer.

Onett already has some of this: the orchestrator runs on Opus and writes tasks for others, the
reviewer is not given the author's reasoning, and CI and the risk policy are deterministic gates.

### What changes

1. **A planner role.** New built-in `planner` (Claude, Opus, `builtin:planner`). New agent field
   `planner: <agent name>`: before this agent runs a task, onett runs the planner on it. The
   planner reads the code and writes a plan (goal, files, steps, edge cases, the check that must
   pass). Onett puts the plan in the task body under `## Plan`, then runs the agent. Defaults:
   `implementer`, `improver` and `sre` are planned by `planner`.

   How a planner run fits the lifecycle *(Claude's calls)*: it is an ordinary Run on the same
   task, role `planner`, in the task's worktree, while the task is `running`. It does not count
   toward `max_attempts`. Its prompt says it must not change files; onett still resets the
   worktree to the branch head afterwards (git with hooks disabled, invariant 7), so anything it
   wrote is discarded. Its report carries the plan as text; onett stores only that. A planner
   that fails, stalls or is denied blocks the task with the reason. It never falls through to an
   unplanned run, because that would hide the failure. A re-plan *replaces* the `## Plan`
   section; the existing `## Attempt N` failure notes keep accumulating as today. The plan lives
   in the task body, so no schema migration is needed. Compare-and-set status changes
   (invariant 6) cover the extra steps like any other run.
2. **Skip planning for tasks the orchestrator wrote.** Those already come from a strong model
   with a plan in the body. The CLI already sends `source` on task create
   (`cmd/onett/commands.go`); it sends `"orchestrator"` instead of `"api"` when
   `ONETT_ROLE=orchestrator`, so the API surface does not change. The source is self-reported,
   which is acceptable because it only affects cost and quality, never safety. *(Claude's call.)*
3. **Acceptance.** After an agent that has a planner finishes and its checks pass, and before
   onett commits and pushes, the planner reads the diff and report against the plan in the task
   body and accepts or rejects, with reasons. This applies to orchestrator-written tasks too:
   they skip planning, not acceptance. A rejection counts as a failed attempt; on the last
   attempt the task blocks with the reasons, as attempt exhaustion does today. The reasons are
   redacted and labelled with `evidenceNote` before the next agent sees them. Acceptance can only
   stop work, never approve a merge or lower risk. The fresh-context `reviewer` still reviews the
   PR afterwards and is never shown the plan. *(Claude's calls.)*
4. **Retry ladder.** New agent field `step_up: [<model>, ...]`. With the default of 3 attempts:
   attempt 1 runs as configured; after a failure, the planner re-plans with the failure evidence
   (redacted, labelled as evidence) and attempt 2 runs on the same model; from attempt 3, each
   attempt runs on the next model in `step_up`. An agent without a planner skips the re-plan but
   follows the same schedule. A `step_up` entry only replaces `model` for that attempt and is
   passed as-is to the same harness, so its spelling must suit that harness; `args` that
   hard-code a model win, and the docs say so. The run record shows the model and why it stepped
   up ("attempt 2 failed after a re-plan: CI failed in ..."). A denied tool call or a stall still
   blocks the task at once: neither is a failure the ladder retries. *(Claude's calls.)*
5. **The owner picks a vendor; the default set follows the strategy on it** (owner's decision).
   New top-level key `agent_defaults: claude | codex` (unset means `claude`, which keeps today's
   orchestrator). It decides the harness and models of every built-in the config does not
   override:

   | Built-in | `claude` | `codex` |
   |---|---|---|
   | `orchestrator`, `planner` (thinking) | Opus | Sol |
   | `implementer`, `improver`, `sre` (execution) | Haiku, `step_up: [sonnet, opus]` | Luna, `step_up: [sol]` |
   | `reviewer`, `triager` | Haiku | Luna |

   Execution and review run on the vendor's cheapest tier, as the owner's rule says, because a
   strong model wrote the plan and accepts the result. *(Builder: confirm the exact model
   strings the Codex CLI takes for Sol and Luna, and whether Codex has a tier between them for
   `step_up`.)* `onett init` asks which vendor to use and writes the key. Changing the key later
   switches every untouched built-in at once; overridden agents keep their own settings. Today's
   default implementer and improver run on Codex, so `claude` installs move to Claude Haiku on
   upgrade; the release notes say so.
6. **On for every install** (owner's decision). Phase 2 is the default framework, so upgraded
   installs get it, like any other default. The release notes and the Agents page say so and
   point at the cost. Turning planning off for an agent is one line: override it without a
   `planner`. On the maintainer's instance, the custom `implementer-claude` agent has no planner
   until the owner adds `planner: planner`.

### Making cost visible

Run cost is already recorded per run. Planner and acceptance runs are ordinary runs with their
own role, so they show up in Runs and per-task cost. The Agents page shows the planner and
`step_up` on each card, so the owner can see why a task cost what it did.

## Left to the builder

Field and struct names beyond those fixed above, the report field the planner uses for its plan
and verdict, UI layout details within `docs/ui.md` conventions, and test structure. Engine
changes (planner, acceptance, ladder) need the real-flow check from `tasks/lessons.md`: a
throwaway repo and a second daemon on a spare port, not only unit tests.

## Decided

- Agents are created and customized by the owner; defaults are strong performers and examples.
- The model-economy strategy is the default framework, on for every install.
- The owner picks Claude or Codex; the default set uses that vendor's strong model to think and
  its cheapest model to execute.

Everything marked *(Claude's call)* above was settled while writing this design. The owner
should check those calls when reviewing it.
