#!/usr/bin/env bash
# Updates the long-lived dogfood instance to origin/main: builds main in a temporary worktree,
# pauses the factory and waits until no run is active and no release is in flight, swaps the
# binary ($DOGFOOD/foundry; the old one is kept as foundry.prev), restarts the daemon, checks that
# it answers with the new version, and resumes the factory if it was running before. A restart
# that fails health is undone with the previous binary.
#
# Needs DOGFOOD (see README.md) and go/node on PATH (`mise exec -- examples/dogfood/update.sh`).
# The config is FOUNDRY_CONFIG, else $DOGFOOD/foundry.yaml, else the example in the clone. The
# daemon is started with $DOGFOOD/run-daemon.sh when that file exists (a machine's own launcher),
# otherwise with `foundry serve`. Never run this from an agent that foundry runs: restarting the
# daemon ends every run it supervises.
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
log=$DOGFOOD/serve.log
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

start_daemon() {
  if [ -x "$DOGFOOD/run-daemon.sh" ]; then
    nohup "$DOGFOOD/run-daemon.sh" >>"$log" 2>&1 </dev/null &
  else
    nohup "$bin" serve --config "$FOUNDRY_CONFIG" >>"$log" 2>&1 </dev/null &
  fi
  disown
  for _ in $(seq 1 60); do
    [ "$(curl -sf "http://$addr/api/v1/health" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')" = "$1" ] && return 0
    sleep 0.5
  done
  return 1
}

old=$("$bin" version | awk '{print $2}')
cp "$bin" "$bin.prev"
cp "$tmp/foundry" "$bin.new" && mv "$bin.new" "$bin"
stop_daemon
if ! start_daemon "$new"; then
  echo "update.sh: $new did not come up healthy; restoring $old (see $log)" >&2
  stop_daemon
  cp "$bin.prev" "$bin.new" && mv "$bin.new" "$bin"
  start_daemon "$old" || echo "update.sh: $old did not come up either" >&2
  exit 1
fi
[ "$was_paused" = yes ] || "$bin" resume >/dev/null
echo "update.sh: running $new (was $old); previous binary kept as $bin.prev"
