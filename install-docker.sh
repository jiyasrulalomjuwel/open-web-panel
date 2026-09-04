#!/usr/bin/env bash
# OpenWebPanel — legacy alias. Docker is now the only install mode, so this
# forwards to install.sh (same directory, or the repo copy over the network).
set -uo pipefail
_HERE="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd)"
if [[ -f "$_HERE/install.sh" ]]; then
  exec bash "$_HERE/install.sh" "$@"
fi
_repo="${OWP_REPO:-jiyasrulalomjuwel/open-web-panel}"
_branch="${OWP_BRANCH:-main}"
_tmp="$(mktemp)" || exit 1
curl -fsSL --connect-timeout 20 --retry 3 \
  "https://raw.githubusercontent.com/${_repo}/${_branch}/install.sh" -o "$_tmp" \
  || { echo "[FATAL] cannot download install.sh"; exit 1; }
exec bash "$_tmp" "$@"
