#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════════
# OpenWebPanel — Offline Bundle Builder (Docker-only)
# Produces the portable docker image bundle used by install.sh (offline mode).
#
# Outputs into bundle/
#   openwebpanel.tar      docker save of the built openwebpanel image
#   checksums.sha256     integrity file for every artifact
#
# Requires: docker, network. Run ONCE on a machine with Docker, then distribute
# the bundle/ folder (e.g. as a GitHub Release asset) to offline targets.
# ═══════════════════════════════════════════════════════════════════════════════

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
BUNDLE=bundle

command -v docker >/dev/null 2>&1 || { echo "docker required"; exit 1; }

echo ">> Building openwebpanel image (this may take a few minutes)..."
docker build -t openwebpanel:latest . >/tmp/owp-build.log 2>&1 || {
  echo "Build failed; tail of log:"; tail -30 /tmp/owp-build.log; exit 1
}

echo ">> Saving image to ${BUNDLE}/openwebpanel.tar ..."
mkdir -p "$BUNDLE"
docker save openwebpanel:latest -o "$BUNDLE/openwebpanel.tar"

echo ">> Writing checksums..."
(cd "$BUNDLE" && sha256sum *.tar > checksums.sha256)
cat "$BUNDLE/checksums.sha256"

echo ">> Bundle complete. Distribute bundle/ (openwebpanel.tar + checksums)."
echo ">> Offline target: place in ./bundle then: sudo bash install.sh"