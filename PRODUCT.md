# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

The primary user is a developer who runs foundry as their own software factory: they hand work
to coding agents and want it carried through PR, review, merge and deploy with as little of their
attention as the risk allows. Today that is the maintainer; next come other developers who
self-host foundry, and later customers of a paid managed version. The UI is designed for that
customer now: it should read as a product people would pay for, not a personal tool.

Two situations are equally common:

- **Check-ins.** From a phone (the installed web app) or a spare tab: does anything need me?
  Approve a merge, retry or cancel a blocked task, then leave.
- **Long sessions.** Open on a second monitor while the factory works: watching runs and their
  logs, reading task detail, chatting with the orchestrator agent to create and steer work.

## Product Purpose

Foundry is one self-hosted daemon that takes work from chat, GitHub issues, production errors
and its own friction reports, runs a coding agent on it in an isolated git worktree, and carries
the result through PR, CI, agent review, a risk policy, merge, deploy, health check and rollback.
The web UI is a control room: see what the factory is doing, answer what it needs from you, and
talk to the orchestrator. Success is a user who trusts the factory enough to look away, and who,
when they look back, understands in seconds what happened and what is waiting on them.

## Positioning

**Agents judge, code acts.** Agents write code, review diffs and triage; only deterministic code
commits, pushes, merges, deploys and rolls back. Risk is computed from the diff and can only go
up, an approval covers exactly one commit, and a denied tool call or a stalled run stops and
reports rather than retrying. Foundry is a gated pipeline the user owns end to end (one binary,
SQLite, a config file, their own agent logins), not a chat window that edits code.

## Operating Context

- The UI is a client of foundry's HTTP+SSE API; state updates arrive live over one event stream.
- Phone use is through the installed web app, often over a VPN such as Tailscale.
- The things that wait on the user: approvals (merge a medium or high risk PR) and blocked tasks
  (denied, stalled, agent needs a human, attempts exhausted). Everything else is observation.
- Core objects and words, used in the UI as-is: project, task, run, role (agent profile),
  harness, loop, approval, risk (low/medium/high), deploy, signal, friction, orchestrator.
- Task lifecycle: queued → running → review → (awaiting_approval) → merged → deploying → done,
  with blocked, failed and cancelled as exits.

## Capabilities and Constraints

- Pages: Overview, Projects, Chat, Tasks, Task detail, Runs, Run detail (live log), Approvals,
  Deploys, Signals, Friction, Loops, Agents, Config, Connect (token entry).
- Stack: React + Vite + TypeScript, react-router, TanStack Query, marked + DOMPurify. No
  component library. The build is embedded in the Go binary.
- The server's Content-Security-Policy allows only same-origin scripts, styles, fonts and
  connections and forbids inline scripts: no CDN fonts or scripts, everything ships in the
  build.
- Self-hosted and offline-capable: nothing may depend on an external service at runtime.
- Agent-written text is rendered only as sanitised Markdown, never raw HTML.
- New npm dependencies are possible but need a stated reason (they are reviewed as medium risk).
- Every status colour also carries a text label.

## Brand Commitments

The product name is **foundry**. No visual element of the current UI (wordmark style, amber
accent, dark-by-default theme) was made binding; the redesign may replace them.

## Evidence on Hand

No customers, testimonials, metrics or screenshots exist yet. Do not invent any. Real content
for designs comes from a running daemon's own data (tasks, runs, logs).

## Product Principles

1. **What needs you comes first.** Approvals and blocked tasks are never more than one glance
   away, on any screen size.
2. **Show the gate, not just the result.** Risk, its reasons, the commit an approval covers and
   the reason a task stopped are always visible where a decision is made.
3. **Calm when nothing is wrong.** A healthy factory should look quiet; attention is reserved for
   what changed or what is stuck.
4. **Same truth on phone and desk.** The phone is a full client for check-ins, not a cut-down
   view; long sessions get density, not different information.
5. **Live means live.** Running work visibly moves; stale or disconnected data says so.

## Accessibility & Inclusion

Status is never conveyed by colour alone. Both themes must meet WCAG AA contrast. Respect
reduced motion for the pulsing running indicators and streaming effects.
