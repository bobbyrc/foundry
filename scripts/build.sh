#!/usr/bin/env bash
# Build onlet with the web UI embedded. OUT overrides the output path (default ./onlet).
set -euo pipefail

cd "$(dirname "$0")/.."

# Install web dependencies only when missing or the lockfile changed since the last install.
if [ ! -d web/node_modules ] || [ web/package-lock.json -nt web/node_modules ]; then
  (cd web && npm ci)
fi
(cd web && npm run build)

version=$(git describe --tags --always --dirty 2>/dev/null || true)
version=${version:-dev}
out=${OUT:-./onlet}

go build -trimpath -ldflags "-X main.version=${version}" -o "$out" ./cmd/onlet

echo "built $out (version $version)"
