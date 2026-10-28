#!/bin/bash
# Build (once per commit) and (re)start the staging copy of onlet at REF.
# Used by onlet's deploy and rollback commands (see ../onlet.yaml). Last stdout line is the
# deploy JSON.
#
# Needs DOGFOOD (the directory holding clone/ and staging/; see ../README.md). Optional:
#   GO             go binary to build with (default: go on PATH)
#   STAGING_ADDR   address for the staging copy (default: 127.0.0.1:7798)
#   STAGING_REPO   repo written into the staging config (default: bobbyrc/onlet)
set -euo pipefail

DOGFOOD=${DOGFOOD:?set DOGFOOD to the dogfood directory}
STAGE=$DOGFOOD/staging
CLONE=$DOGFOOD/clone
GO=${GO:-go}
ADDR=${STAGING_ADDR:-127.0.0.1:7798}
HEALTH=http://$ADDR/api/v1/health
LOG=$STAGE/log/staging.log

REF_IN=${1:?usage: release.sh REF}
log() { printf 'release: %s\n' "$*" >&2; }

mkdir -p "$STAGE"

# One release at a time (deploy and rollback can race).
LOCK=$STAGE/release.lock
mkdir "$LOCK" 2>/dev/null || { log "another release is running ($LOCK exists)"; exit 1; }
trap 'rmdir "$LOCK" 2>/dev/null || true' EXIT

mkdir -p "$STAGE/bin" "$STAGE/src" "$STAGE/log" "$STAGE/data"

# (a) Resolve the ref to a full commit SHA.
git -C "$CLONE" fetch -q origin
FULL=$(git -C "$CLONE" rev-parse --verify "$REF_IN^{commit}")
REF12=${FULL:0:12}
BIN=$STAGE/bin/onlet-$REF12
log "ref $REF_IN -> $FULL"

# (b) Build the binary for this commit, unless it is already built.
if [ ! -x "$BIN" ]; then
  SRC=$STAGE/src/$REF12
  rm -rf "$SRC"
  mkdir -p "$SRC"
  git -C "$CLONE" archive "$FULL" | tar -x -C "$SRC"
  mkdir -p "$SRC/internal/api/webdist"
  [ -f "$SRC/internal/api/webdist/.keep" ] || : > "$SRC/internal/api/webdist/.keep"
  log "building $BIN"
  (
    cd "$SRC"
    GOTOOLCHAIN=local GOFLAGS=-mod=mod GOCACHE=$STAGE/gocache "$GO" build -o "$BIN.tmp" ./cmd/onlet
  )
  mv "$BIN.tmp" "$BIN"
else
  log "reusing $BIN"
fi

# (c) Stop the instance we started earlier, if its PID is alive and is still our binary.
if [ -f "$STAGE/pid" ]; then
  OLD=$(cat "$STAGE/pid" || true)
  if [ -n "$OLD" ] && kill -0 "$OLD" 2>/dev/null \
     && ps -p "$OLD" -o command= 2>/dev/null | grep -qF "$STAGE/bin/onlet-"; then
    log "stopping staging pid $OLD"
    kill -TERM "$OLD"
    for _ in $(seq 1 10); do
      kill -0 "$OLD" 2>/dev/null || break
      sleep 1
    done
    if kill -0 "$OLD" 2>/dev/null; then
      log "pid $OLD ignored SIGTERM for 10s, sending SIGKILL"
      kill -KILL "$OLD" 2>/dev/null || true
      sleep 1
    fi
  fi
fi

# (d) First use only: write the staging config and check it with doctor.
CONF=$STAGE/staging.yaml
if [ ! -f "$CONF" ]; then
  REPO=${STAGING_REPO:-bobbyrc/onlet}
  mkdir -p "$STAGE/scratch-clone"
  cat > "$CONF" <<YAML
server:
  addr: $ADDR
  data_dir: $STAGE/data
projects:
  scratch:
    repo: $REPO
    path: $STAGE/scratch-clone
    paused: true
notify:
  - type: log
YAML
  log "wrote $CONF; running doctor"
  if ! "$BIN" doctor --config "$CONF" >&2; then
    log "doctor reported errors (see above)"
    exit 1
  fi
fi

# (e) Start the instance in the background, logging to the staging log. onlet leaves processes
# started by a successful deploy running, and kills them if the deploy fails.
nohup "$BIN" serve --config "$CONF" >> "$LOG" 2>&1 < /dev/null &
NEW=$!
echo "$NEW" > "$STAGE/pid"
echo "$REF12" > "$STAGE/current"
log "started pid $NEW"

# (f) Wait for health.
for _ in $(seq 1 30); do
  if curl -sf -m 2 "$HEALTH" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$NEW" 2>/dev/null; then
    log "staging process exited during startup"
    break
  fi
  sleep 1
done
if ! curl -sf -m 2 "$HEALTH" >/dev/null 2>&1; then
  log "health check failed at $HEALTH; last 30 log lines:"
  tail -n 30 "$LOG" >&2 || true
  exit 1
fi
log "healthy"

# (g) Deploy protocol: last stdout line is the release JSON.
printf '{"id":"%s","url":"http://%s"}\n' "$FULL" "$ADDR"
