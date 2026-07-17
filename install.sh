#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════════
# OpenWebPanel Installer v2.1
#   Automated web hosting control panel installer for Ubuntu/Debian
#
# Usage:
#   # From the project directory:
#   sudo bash install.sh
#
#   # One-liner on a fresh server:
#   curl -fsSL https://raw.githubusercontent.com/jiyasrulalomjuwel/open-web-panel/main/install.sh | sudo bash
#
# Env overrides:
#   OWP_VERSION, OWP_REPO, OWP_USER, OWP_HOME, OWP_DOMAIN,
#   OWP_JWT_SECRET, OWP_PANEL_PORT, OWP_USER_PORT,
#   OWP_SKIP_FIREWALL, OWP_SKIP_SWAP, OWP_DEBUG, OWP_AUTO_YES
# ═══════════════════════════════════════════════════════════════════════════════

# ─── Safety ─────────────────────────────────────────────────────────────────
set -uo pipefail

# ─── Non-interactive APT ───────────────────────────────────────────────────
export DEBIAN_FRONTEND=noninteractive

# ═══════════════════════════════════════════════════════════════════════════════
# BOOTSTRAP
# ═══════════════════════════════════════════════════════════════════════════════

# Determine script location (fallback for curl | bash mode)
SCRIPT_SOURCE="${BASH_SOURCE[0]:-}"
if [[ -z "$SCRIPT_SOURCE" ]]; then
  # piped via stdin — no script path available
  SCRIPT_DIR=""
else
  while [[ -L "$SCRIPT_SOURCE" ]]; do
    SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" && pwd)"
    SCRIPT_SOURCE="$(readlink "$SCRIPT_SOURCE")"
    [[ "$SCRIPT_SOURCE" != /* ]] && SCRIPT_SOURCE="$SCRIPT_DIR/$SCRIPT_SOURCE"
  done
  SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" && pwd)"
fi

# Default paths (can be overridden by env)
OWP_USER="${OWP_USER:-openwebpanel}"
OWP_HOME="${OWP_HOME:-/opt/openwebpanel}"
OWP_APP_DIR="${OWP_HOME}/app"

# Check if we're running locally (from the repo) or standalone (curl | bash)
IS_STANDALONE=false
if [[ -f "${SCRIPT_DIR}/go.mod" ]] && grep -q "openwebcpanel\|open-web-panel" "${SCRIPT_DIR}/go.mod" 2>/dev/null; then
  # Running from repo directory — copy to install target
  LOCAL_SRC_DIR="$SCRIPT_DIR"
else
  IS_STANDALONE=true
fi

# ─── Deploy project files to OWP_APP_DIR ───────────────────────────────────
deploy_project_files() {
  if [[ "$IS_STANDALONE" == "true" ]]; then
    echo "[BOOTSTRAP] Standalone mode — cloning repository to ${OWP_APP_DIR}..."
    if ! command -v git &>/dev/null; then
      apt-get update -qq 2>/dev/null
      apt-get install -y -qq git 2>/dev/null || {
        echo "[FATAL] Cannot install git. Please install git and try again."
        exit 1
      }
    fi
    rm -rf "$OWP_APP_DIR" 2>/dev/null || true
    git clone --depth 1 --branch "${OWP_BRANCH:-main}" \
      "https://github.com/${OWP_REPO:-jiyasrulalomjuwel/open-web-panel}.git" \
      "$OWP_APP_DIR" 2>/dev/null || {
      echo "[FATAL] Failed to clone repository."
      exit 1
    }
  else
    echo "[BOOTSTRAP] Local mode — copying project files to ${OWP_APP_DIR}..."
    mkdir -p "$OWP_APP_DIR"
    rsync -a --delete \
      --exclude='.git' --exclude='node_modules' --exclude='bin' \
      --exclude='homes' --exclude='*.db' --exclude='*.db-shm' --exclude='*.db-wal' \
      --exclude='__pycache__' --exclude='.env' \
      "$LOCAL_SRC_DIR"/ "$OWP_APP_DIR"/ 2>/dev/null || {
      echo "[FATAL] Failed to copy project files."
      exit 1
    }
  fi
}

# ─── Source installer modules ──────────────────────────────────────────────
# In local mode, source from the repo directory. In standalone mode, we handle
# this inside main() after cloning.
if [[ "$IS_STANDALONE" == "false" ]]; then
  OWP_INSTALL_DIR="${LOCAL_SRC_DIR}/install"
  for _module in config framework stages; do
    _module_path="${OWP_INSTALL_DIR}/${_module}.sh"
    if [[ ! -f "$_module_path" ]]; then
      echo "[FATAL] Installer module not found at $_module_path"
      echo "[FATAL] Ensure the install/ directory exists alongside this script."
      exit 1
    fi
    # shellcheck disable=SC1090
    source "$_module_path"
  done
fi

# ═══════════════════════════════════════════════════════════════════════════════
# MAIN ENTRY POINT
# ═══════════════════════════════════════════════════════════════════════════════
main() {
  # Deploy project files to OWP_APP_DIR (clones or copies as needed)
  deploy_project_files

  # Source modules from deployed location (handles both local and standalone)
  OWP_INSTALL_DIR="${OWP_APP_DIR}/install"
  for _module in config framework stages; do
    _module_path="${OWP_INSTALL_DIR}/${_module}.sh"
    if [[ ! -f "$_module_path" ]]; then
      echo "[FATAL] Installer module not found at $_module_path"
      exit 1
    fi
    # shellcheck disable=SC1090
    source "$_module_path"
  done

  # Clear stale state from previous runs for a clean start
  clear_state 2>/dev/null || true

  # Initialize the installer (logging, TUI, timer)
  init_installer

  # Trap EXIT to run rollback on failure
  trap '_exit_handler' EXIT

  # ─── Run All Stages ──────────────────────────────────────────────
  run_stage "requirements" stage_requirements || exit $?
  run_stage "prepare" stage_prepare || exit $?
  run_stage "download" stage_download || exit $?
  run_stage "dependencies" stage_dependencies || exit $?
  run_stage "configure" stage_configure || exit $?
  run_stage "database" stage_database || exit $?
  run_stage "webserver" stage_webserver || exit $?
  run_stage "security" stage_security || exit $?
  run_stage "services" stage_services || exit $?
  run_stage "validate" stage_validate || exit $?

  # ─── Final Output ────────────────────────────────────────────────
  if [[ $INSTALL_EXIT_CODE -eq 0 ]]; then
    print_final
    print_summary
  else
    if $HAS_TTY; then
      echo ""
      echo -e "  ${C_RED}${G_CROSS} Installation failed${C_RESET}"
      echo -e "  ${C_DIM}Check the log for details:${C_RESET} ${INSTALL_LOG}"
      echo ""
    else
      echo ""
      echo "[ERROR] Installation failed. Check log: $INSTALL_LOG"
      echo ""
    fi
  fi

  exit $INSTALL_EXIT_CODE
}

# ─── EXIT Trap Handler ────────────────────────────────────────────
_exit_handler() {
  local rc=$?
  if [[ $rc -ne 0 ]]; then
    execute_rollback
  fi
}

main "$@"
