#!/usr/bin/env bash
# Updates the long-lived dogfood instance to origin/main: builds main in a temporary worktree,
# pauses the factory and waits until no run is active and no release is in flight, swaps the
# binary ($DOGFOOD/foundry; the old one is kept as foundry.prev), restarts the daemon, checks that
# it answers with the new version, and resumes the factory if it was running before. A restart
# that fails health is undone with the previous binary.
#
# Needs DOGFOOD (see README.md) and go/node on PATH (`mise exec -- examples/dogfood/update.sh`).
# The config is FOUNDRY_CONFIG, else $DOGFOOD/foundry.yaml, else the example in the clone. When
# the launchd service is installed (`foundry service install`), launchd restarts the daemon;
# otherwise it is started with $DOGFOOD/run-daemon.sh when that file exists (a machine's own
# launcher), or with `foundry serve`. Never run this from an agent that foundry runs: restarting
# the daemon ends every run it supervises.
set -euo pipefail

DOGFOOD=${DOGFOOD:?set DOGFOOD to the dogfood directory}
export DOGFOOD
if [ -n "${FOUNDRY_OUTBOX:-}" ]; then
  echo "update.sh: refusing to run inside a foundry agent run" >&2
  exit 1
fi
if [ -z "${FOUNDRY_CONFIG:-}" ]; then
  FOUNDRY_CONFIG=$DOGFOOD/foundry.yaml
  [ -f "$FOUNDRY_CONFIG" ] || FOUNDRY_CONFIG=$DOGFOOD/clone/examples/dogfood/foundry.yaml
fi
export FOUNDRY_CONFIG

bin=$DOGFOOD/foundry
repo=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
addr=$(sed -n 's/^  addr: *//p' "$FOUNDRY_CONFIG" | head -n 1)
port=${addr##*:}
[ -n "$port" ] || { echo "update.sh: no server.addr in $FOUNDRY_CONFIG" >&2; exit 1; }

tmp=$(mktemp -d)
cleanup() {
  git -C "$repo" worktree remove --force "$tmp/src" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

# Build first, so a failed build leaves the running instance untouched.
git -C "$repo" fetch -q origin main
git -C "$repo" worktree add -q --detach "$tmp/src" origin/main
OUT=$tmp/foundry "$tmp/src/scripts/build.sh"
new=$("$tmp/foundry" version | awk '{print $2}')
running=$(curl -sf "http://$addr/api/v1/health" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p' || true)
if [ "$new" = "$running" ]; then
  echo "update.sh: already running $new"
  exit 0
fi

# Under launchd (`foundry service install`) the service restarts the binary and config its plist
# names, so they must be this script's. A service that is installed but not running cannot be
# restarted with launchctl kickstart, so stop rather than guess.
service=no
log=$DOGFOOD/serve.log
svc=$("$bin" service status 2>/dev/null || true)
case "$(head -n 1 <<<"$svc")" in
  running*)
    service=yes
    log=$(sed -n 's/^log: //p' <<<"$svc")
    plist=$HOME/Library/LaunchAgents/io.foundry.serve.plist
    if ! grep -qF "<string>$bin</string>" "$plist" || ! grep -qF "<string>$FOUNDRY_CONFIG</string>" "$plist"; then
      echo "update.sh: the launchd service does not run $bin with $FOUNDRY_CONFIG (see $plist);" >&2
      echo "  reinstall it: $bin service uninstall && $bin service install --config $FOUNDRY_CONFIG" >&2
      exit 1
    fi
    ;;
  installed*)
    echo "update.sh: the launchd service is installed but not running; start it or reinstall it first" >&2
    exit 1
    ;;
esac

# The status probe must succeed: an erroring command prints nothing, which would look idle.
status=$("$bin" status)
was_paused=no
grep -q '^Factory: PAUSED' <<<"$status" && was_paused=yes
"$bin" pause >/dev/null
echo "update.sh: paused; waiting for active runs and releases to finish (up to 30 minutes)"
for i in $(seq 1 360); do
  status=$("$bin" status)
  latest=$("$bin" deploys | sed -n 2p)
  if grep -q 'Active runs: 0 ' <<<"$status" && ! grep -qE ' (pending|deploying|verifying) ' <<<"$latest"; then
    break
  fi
  if [ "$i" = 360 ]; then
    echo "update.sh: still busy after 30 minutes; the factory is left paused" >&2
    exit 1
  fi
  sleep 5
done

stop_daemon() {
  local pid
  pid=$(lsof -tiTCP:"$port" -sTCP:LISTEN || true)
  [ -n "$pid" ] || return 0
  case "$(ps -o command= -p "$pid")" in
    *" serve"*) ;;
    *) echo "update.sh: port $port is held by pid $pid, which is not foundry serve" >&2; exit 1 ;;
  esac
  kill "$pid"
  for _ in $(seq 1 60); do kill -0 "$pid" 2>/dev/null || return 0; sleep 0.5; done
  echo "update.sh: pid $pid did not exit" >&2
  exit 1
}

# restart_daemon restarts the daemon on the binary now at $bin and waits until it answers with
# version $1. Under launchd, killing the daemon would only make launchd start it again, so the
# service is restarted through launchd (`foundry service install` on an installed service).
restart_daemon() {
  if [ "$service" = yes ]; then
    "$bin" service install >/dev/null || return 1
  else
    stop_daemon
    if [ -x "$DOGFOOD/run-daemon.sh" ]; then
      nohup "$DOGFOOD/run-daemon.sh" >>"$log" 2>&1 </dev/null &
    else
      nohup "$bin" serve --config "$FOUNDRY_CONFIG" >>"$log" 2>&1 </dev/null &
    fi
    disown
  fi
  for _ in $(seq 1 60); do
    [ "$(curl -sf "http://$addr/api/v1/health" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')" = "$1" ] && return 0
    sleep 0.5
  done
  return 1
}

old=$("$bin" version | awk '{print $2}')
cp "$bin" "$bin.prev"
cp "$tmp/foundry" "$bin.new" && mv "$bin.new" "$bin"
if ! restart_daemon "$new"; then
  echo "update.sh: $new did not come up healthy; restoring $old (see $log)" >&2
  cp "$bin.prev" "$bin.new" && mv "$bin.new" "$bin"
  restart_daemon "$old" || echo "update.sh: $old did not come up either" >&2
  exit 1
fi
[ "$was_paused" = yes ] || "$bin" resume >/dev/null
echo "update.sh: running $new (was $old); previous binary kept as $bin.prev"
