# Dogfood: onett on its own repo

onett runs against its own repository (`bobbyrc/onett`). Agent PRs target the `dogfood`
branch, never `main`. Each merged change is released to a local staging copy of onett, which
runs on port 7798 while the daemon serves its UI on port 7797.

## Prerequisites

- `go` and `gofmt` on PATH (mise works: `mise exec -- go`, or put its bin directory on PATH)
- `node` and `npm` (the `web deps` setup step runs `npm ci`)
- `gh`, authenticated (`gh auth status`)
- the `claude` CLI, logged in
- `python3` for the observer (`/usr/bin/python3` is used)

## Setup

```
export DOGFOOD=~/.local/share/onett-dogfood
mkdir -p "$DOGFOOD"
git clone https://github.com/bobbyrc/onett "$DOGFOOD/clone" && git -C "$DOGFOOD/clone" checkout dogfood
onett serve --config examples/dogfood/onett.yaml
```

`DOGFOOD` must be exported in the shell that runs `onett serve`. The deploy, logs and observer
commands inherit it from there.

To keep the instance running across logins and crashes on macOS, use the launchd service instead
of a terminal. Install it from a shell where `DOGFOOD` is exported and `go` is on PATH:

```
"$DOGFOOD/onett" service install --config "$DOGFOOD/clone/examples/dogfood/onett.yaml"
```

Install it with the binary at `$DOGFOOD/onett` and the config `update.sh` uses, so that updates
restart the right daemon. The config is `ONETT_CONFIG`, else `$DOGFOOD/onett.yaml` when it
exists, else the example. The service records `DOGFOOD` and `PATH` from that shell. Installing
again only restarts it; to change a recorded value, `service uninstall` and install again. Its log
is `$DOGFOOD/data/onett.log`.

## Watching

- Web UI: http://127.0.0.1:7797
- Staging copy: http://127.0.0.1:7798/api/v1/health
- Staging log: `$DOGFOOD/staging/log/staging.log`

## How work arrives

- Issues labelled `onett` in `bobbyrc/onett` become tasks.
- Or create one by hand:
  `onett task create --project onett --kind feature --title "..." --body "..."`.

## What needs a human

- Approvals for medium and high risk changes. Approve or reject them in the UI.
- Any change under `examples/dogfood/**` is high risk. That covers the release scripts, so a
  human approves every change to the deploy pipeline.

## Updating

`update.sh` moves the instance to the latest `main` in one step:

```
mise exec -- examples/dogfood/update.sh
```

It does these steps in order:

1. Builds `main` in a temporary worktree.
2. Pauses the factory and waits until no run is active and no release is in flight.
3. Swaps `$DOGFOOD/onett`, keeping the old binary as `onett.prev`.
4. Restarts the daemon and checks that it answers with the new version.
5. Resumes the factory, unless it was already paused before the update.

If the new version doesn't come up healthy, the script restarts the previous binary. It does nothing if
the instance already runs `main`. A launcher at `$DOGFOOD/run-daemon.sh`, if present, is used to
start the daemon. Run the script from your own shell, never from an agent that onett runs.

## Stopping

- Stop new work: `onett pause`.
- Stop the daemon: interrupt `onett serve`, or `onett service uninstall` for the service.
- Stop the staging copy: its pid is in `$DOGFOOD/staging/pid`. Check that the process is still
  onett (`ps -p <pid> -o command=`) before you kill it.
