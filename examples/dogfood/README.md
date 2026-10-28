# Dogfood: foundry on its own repo

foundry runs against its own repository (`bobbyrc/foundry`). Agent PRs target the `dogfood`
branch, never `main`. Each merged change is released to a local staging copy of foundry, which
runs on port 7798 while the daemon serves its UI on port 7797.

## Prerequisites

- `go` and `gofmt` on PATH (mise works: `mise exec -- go`, or put its bin directory on PATH)
- `node` and `npm` (the `web deps` setup step runs `npm ci`)
- `gh`, authenticated (`gh auth status`)
- the `claude` CLI, logged in
- `python3` for the observer (`/usr/bin/python3` is used)

## Setup

```
export DOGFOOD=~/.local/share/foundry-dogfood
mkdir -p "$DOGFOOD"
git clone https://github.com/bobbyrc/foundry "$DOGFOOD/clone" && git -C "$DOGFOOD/clone" checkout dogfood
foundry serve --config examples/dogfood/foundry.yaml
```

`DOGFOOD` must be exported in the shell that runs `foundry serve`. The deploy, logs and observer
commands inherit it from there.

## Watching

- Web UI: http://127.0.0.1:7797
- Staging copy: http://127.0.0.1:7798/api/v1/health
- Staging log: `$DOGFOOD/staging/log/staging.log`

## How work arrives

- Issues labelled `foundry` in `bobbyrc/foundry` become tasks.
- Or create one by hand: `foundry task create`.

## What needs a human

- Approvals for medium and high risk changes. Approve or reject them in the UI.
- Any change under `examples/dogfood/**` is high risk. That covers the release scripts, so a
  human approves every change to the deploy pipeline.

## Stopping

- Stop new work: `foundry pause`.
- Stop the daemon: interrupt `foundry serve`.
- Stop the staging copy: its pid is in `$DOGFOOD/staging/pid`. Check that the process is still
  foundry (`ps -p <pid> -o command=`) before you kill it.
