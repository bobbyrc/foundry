#!/bin/bash
# Exec observer for the staging instance: prints error and panic lines from the staging log as
# JSON Lines (one per distinct message). FOUNDRY_SINCE filters by the line's "time" when parseable.
# STAGING_OBSERVE_LOG overrides the log path (used for testing only).
set -euo pipefail

LOG=${STAGING_OBSERVE_LOG:-${DOGFOOD:?set DOGFOOD}/staging/log/staging.log}

[ -f "$LOG" ] || exit 0

export OBS_LOG="$LOG"
/usr/bin/python3 -I - <<'PYEOF'
import datetime
import hashlib
import json
import os
import re
import sys

LOG = os.environ["OBS_LOG"]
SINCE_RAW = os.environ.get("FOUNDRY_SINCE", "")
NOW = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)


def parse_time(value):
    """Parse an RFC 3339 timestamp (Z or +-hh:mm, fractional seconds of any length) to aware UTC.
    Returns None when it does not parse."""
    if not isinstance(value, str):
        return None
    m = re.match(r"^(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}:\d{2})(\.\d+)?(Z|[+-]\d{2}:\d{2})?$", value)
    if not m:
        return None
    frac = (m.group(3) or "")[1:7].ljust(6, "0")
    tz = m.group(4) or "Z"
    if tz == "Z":
        tz = "+00:00"
    try:
        base = datetime.datetime.strptime(m.group(1) + "T" + m.group(2), "%Y-%m-%dT%H:%M:%S")
        off = datetime.datetime.strptime(tz[1:], "%H:%M")
        offset = datetime.timedelta(hours=off.hour, minutes=off.minute)
        if tz[0] == "-":
            offset = -offset
    except ValueError:
        return None
    return (base.replace(microsecond=int(frac)) - offset).replace(tzinfo=datetime.timezone.utc)


def fmt(t):
    return t.astimezone(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


since = parse_time(SINCE_RAW) if SINCE_RAW else None

groups = {}  # fingerprint -> dict, in first-seen order
order = []

with open(LOG, "r", encoding="utf-8", errors="replace") as f:
    for raw in f:
        line = raw.rstrip("\n")
        is_panic = "panic:" in line
        if not is_panic and '"level":"ERROR"' not in line:
            continue

        obj = None
        try:
            parsed = json.loads(line)
            if isinstance(parsed, dict):
                obj = parsed
        except ValueError:
            obj = None

        if is_panic:
            idx = line.index("panic:")
            message = line[idx:].strip()
            if obj is not None and isinstance(obj.get("msg"), str) and "panic:" in obj["msg"]:
                message = obj["msg"]
            level = "fatal"
        else:
            message = obj.get("msg") if obj else None
            if not isinstance(message, str) or not message.strip():
                message = line
            level = "error"

        when = parse_time(obj.get("time")) if obj else None
        if when is not None and since is not None and when < since:
            continue
        when = when or NOW

        digest = hashlib.sha256(re.sub(r"\d+", "#", message).encode("utf-8")).hexdigest()
        fp = "staging-" + digest[:16]

        g = groups.get(fp)
        if g is None:
            g = {"message": message, "level": level, "count": 0, "first": when, "last": when}
            groups[fp] = g
            order.append(fp)
        g["count"] += 1
        if level == "fatal":
            g["level"] = "fatal"
        g["first"] = min(g["first"], when)
        g["last"] = max(g["last"], when)

for fp in order:
    g = groups[fp]
    print(json.dumps({
        "fingerprint": fp,
        "title": g["message"][:120],
        "level": g["level"],
        "count": g["count"],
        "first_seen": fmt(g["first"]),
        "last_seen": fmt(g["last"]),
    }, ensure_ascii=False))
PYEOF
