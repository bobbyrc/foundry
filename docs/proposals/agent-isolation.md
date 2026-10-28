# Proposal: isolate agents from the owner's account

Status: proposal for the owner, 2026-10-28. Nothing here is implemented.

## The problem

Agents run as your macOS user. Foundry already withholds what it controls: worker agents get
no API token, an allowlisted environment (no `GH_TOKEN` and friends), no git hooks run with
foundry's credentials, and foundry — not the agent — commits, pushes and merges. But an agent
process can still read anything your user can read, including:

- `<data_dir>/token` (the foundry API token): with it, an agent could approve its own PR or
  resume a paused project;
- `gh`'s stored login (`~/.config/gh`), cloud CLIs' credentials, SSH keys, your other repos.

Today the only barriers are each harness's own safety layer — Claude Code's auto mode reviews
risky actions, and Codex's sandbox blocks writes outside the worktree (and all network) but not
reads. Those are good, but they are judgment calls by another model, not a boundary.

## Options

| | Boundary | Cost | Breaks |
|---|---|---|---|
| **A. Separate macOS user** (`_foundry-agent`), agents launched via `sudo -u` with a dedicated HOME holding only the agent CLIs' own logins | OS file permissions | one-time setup; a sudoers rule | agents can't use your gh/cloud logins (intended); each agent CLI must be logged in once as that user |
| **B. Podman containers** per run: worktree bind-mounted read-write, agent CLI auth mounted read-only, network policy per project | namespaces + VM (Podman on macOS runs a Linux VM) | image per toolchain; slower start; Linux toolchains only | macOS-only builds (Xcode, Flutter iOS) can't run inside |
| **C. Keep as is**, document the risk, rely on harness safety layers | none (convention) | none | nothing |
| **D. Token hardening only**: API token readable only by the daemon user (requires A anyway) or replaced by a short-lived per-session token handed to the browser | partial | small | nothing |

## Recommendation

**A first, B later.** A separate user is a real boundary at low cost and works with every
toolchain you use (Go, Node, Flutter, Godot). Foundry would gain a `run_as` option per harness
(`run_as: _foundry-agent`) and `foundry doctor` would check the user, its HOME and each agent
CLI's login. The worktrees root would be group-writable by that user. Containers (B) are worth
adding as a per-project option for Linux-only projects that want network policy too.

## What I need from you

1. A, B, C or D (or a combination).
2. If A: confirm foundry may ask you to run one setup command with sudo (create the user, add a
   narrow sudoers rule `bcraig ALL=(_foundry-agent) NOPASSWD: /path/to/claude, /path/to/codex`).
