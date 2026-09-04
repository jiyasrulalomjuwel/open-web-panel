#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════════
# OpenWebPanel Installer (Docker-only)
# Runs the whole panel plus nginx / php-fpm / MariaDB inside one self-contained
# container image. No host apt installs of nginx/mysql/php are needed, which
# makes installs identical on every machine and avoids distro-package failures.
#
# Usage:
#   # One-liner on a fresh server:
#   curl -fsSL https://raw.githubusercontent.com/jiyasrulalomjuwel/open-web-panel/main/install.sh | sudo bash
#
#   # From a repo checkout:
#   sudo bash install.sh
#
#   # Old `--docker` flag from previous guides is accepted and ignored.
#
# Offline mode: place `docker save`d image tarballs in ./bundle then run the
# installer the same way — it loads them with `docker load` (no network build).
#
# Env overrides: OWP_ADMIN_PASSWORD, OWP_ADMIN_USERNAME, MYSQL_ROOT_*,
#                OWP_PUBLIC_HOST, OWP_SHARED_IP, OWP_PANEL_PORT, OWP_USER_PORT,
#                OWP_SKIP_FIREWALL=1, OWP_AUTO_YES=1, OWP_REPO, OWP_BRANCH,
#                OWP_BUNDLE_DIR, APP_DIR
# ═══════════════════════════════════════════════════════════════════════════════

set -uo pipefail
export DEBIAN_FRONTEND=noninteractive

# ─── Old --docker flag: harmless no-op (Docker is now the only mode) ────────
_args=()
for _arg in "$@"; do
  case "$_arg" in
    --docker|--docker-compose) echo "[OWP] Note: Docker is the only install mode now; flag ignored." ;;
    *) _args+=("$_arg") ;;
  esac
done
set -- "${_args[@]}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd)"
IS_TTY=false; [[ -t 1 && -t 0 && -t 2 ]] && IS_TTY=true

APP_DIR="${APP_DIR:-/opt/openwebpanel}"

# ─── Bootstrap for curl | bash ──────────────────────────────────────────────
# In curl | bash mode there is no repo checkout next to this script, but the
# installer needs docker-compose.prod.yml. Clone once to a temp dir and
# re-exec from there (guarded against loops).
if [[ "${OWP_BOOTSTRAPPED:-0}" != "1" && ! -f "${SCRIPT_DIR}/docker-compose.prod.yml" ]]; then
  command -v git >/dev/null 2>&1 || { apt-get update -qq 2>/dev/null; apt-get install -y -qq git 2>/dev/null || true; }
  command -v git >/dev/null 2>&1 || { echo "[FATAL] git is required for the one-liner install."; exit 1; }
  TMP_SRC="$(mktemp -d)" || exit 1
  echo "[BOOTSTRAP] Fetching installer files..."
  git clone --depth 1 --branch "${OWP_BRANCH:-main}" \
    "https://github.com/${OWP_REPO:-jiyasrulalomjuwel/open-web-panel}.git" \
    "$TMP_SRC" >/dev/null 2>&1 || { echo "[FATAL] Failed to clone repository."; exit 1; }
  export OWP_BOOTSTRAPPED=1
  exec bash "$TMP_SRC/install.sh" "$@"
fi
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"

gen_secret() { openssl rand -base64 48 2>/dev/null | tr -dc 'A-Za-z0-9' | head -c "${1:-48}"; }

log() { echo "[OWP] $*"; }
die() { echo "[FATAL] $*"; exit 1; }

# Brief system fingerprint shown before the install begins.
system_summary() {
  local mem mb cores osname osver
  mem=$(grep '^MemTotal:' /proc/meminfo 2>/dev/null | awk '{print $2}')
  mb=$(( (mem > 0 ? mem : 0) / 1024 ))
  cores=$(nproc 2>/dev/null || echo 1)
  osname=""; osver=""
  if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    osname="$NAME"; osver="$VERSION_ID"
  fi
  log "System: ${osname:-Unknown} ${osver:-} | ${cores} CPU | ${mb}MB RAM"
  if command -v systemd-detect-virt >/dev/null 2>&1; then
    local v; v=$(systemd-detect-virt 2>/dev/null || echo "none")
    log "Virt: ${v:-none}"
  fi
}

require_root() {
  if [[ $EUID -ne 0 ]]; then die "must be run as root (use: sudo bash install.sh)"; fi
}

ensure_docker() {
  command -v docker >/dev/null 2>&1 || {
    log "Docker not found. Attempting online install..."
    if curl -fsSL https://get.docker.com -o /tmp/get-docker.sh >/dev/null 2>&1; then
      sh /tmp/get-docker.sh >/tmp/docker-install.log 2>&1 || { echo "   docker install output:"; tail -10 /tmp/docker-install.log; }
    else
      die "could not download Docker installer. Install Docker yourself, or pre-load offline images and re-run."
    fi
  }
  command -v docker >/dev/null 2>&1 || die "docker still unavailable after install attempt."
  docker compose version >/dev/null 2>&1 || {
    log "Installing docker compose plugin..."
    apt-get update -qq >/dev/null 2>&1
    apt-get install -y -qq docker-compose-plugin >/dev/null 2>&1 \
      || apt-get install -y -qq docker-compose-v2 >/dev/null 2>&1 \
      || die "docker compose plugin unavailable."
  }
  systemctl enable --now docker >/dev/null 2>&1 || service docker start >/dev/null 2>&1 || true
  local i
  for i in $(seq 1 20); do docker info >/dev/null 2>&1 && return 0; sleep 1; done
  die "docker daemon did not start."
}

bundle_present() { compgen -G "$SCRIPT_DIR"/bundle/*.tar >/dev/null 2>&1; }

load_bundle() {
  log "Offline bundle detected — loading images..."
  local f failed=0
  for f in "$SCRIPT_DIR"/bundle/*.tar; do
    [[ -e "$f" ]] || continue
    log "  loading: ${f##*/}"
    if ! docker load -i "$f" >/tmp/owp-load.log 2>&1; then
      log "[WARN] failed to load ${f##*/}"; tail -5 /tmp/owp-load.log
      failed=1
    fi
  done
  return $failed
}

write_env() {
  mkdir -p "$APP_DIR"
  # Escape double quotes/backslashes so a password with " $ or \ can't corrupt
  # the EnvironmentFile consumed by systemd (parse error would stop parentd).
  local esc
  esc() { local s="$1"; s="${s//\\/\\\\}"; s="${s//\"/\\\"}"; printf '%s' "$s"; }
  cat > "$APP_DIR/.env" <<EOF
OWP_JWT_SECRET="$(esc "$OWP_JWT_SECRET")"
OWP_ADMIN_USERNAME="${OWP_ADMIN_USERNAME}"
OWP_ADMIN_PASSWORD="$(esc "$OWP_ADMIN_PASSWORD")"
OWP_ADMIN_STATIC_DIR=/app/web/dist/admin
OWP_CHILD_STATIC_DIR=/app/web/dist/child
OWP_ADMIN_LISTEN=127.0.0.1:9000
OWP_CHILD_LISTEN=127.0.0.1:9001
OWP_PUBLIC_HOST=${OWP_PUBLIC_HOST:-127.0.0.1:9000}
OWP_SHARED_IP=${OWP_SHARED_IP:-127.0.0.1}
MYSQL_ROOT_PASSWORD="$(esc "$OWP_MYSQL_ROOT_PASSWORD")"
MYSQL_ADMIN_PASSWORD="$(esc "$OWP_MYSQL_ADMIN_PASSWORD")"
OWP_PANEL_PORT=${OWP_PANEL_PORT:-2086}
OWP_USER_PORT=${OWP_USER_PORT:-2082}
EOF
  chmod 600 "$APP_DIR/.env"
}

install_compose_file() {
  local src="$SCRIPT_DIR/docker-compose.prod.yml"
  if [[ -f "$src" ]]; then
    cp -f "$src" "$APP_DIR/docker-compose.yml"
  else
    die "no docker-compose.prod.yml found in $SCRIPT_DIR"
  fi
}

# Exported variables beat --env-file/interpolation at every precedence level,
# and sudo wipes the shell environment — so load the saved .env explicitly
# before ANY compose invocation. Otherwise secrets silently diverge between
# the file and the running container (empty interpolation wins).
load_env_file() {
  if [[ -f "$APP_DIR/.env" ]]; then
    set -a
    # shellcheck disable=SC1090
    . "$APP_DIR/.env"
    set +a
  fi
}

build_image() {
  [[ "${OWP_OFFLINE:-0}" == "1" ]] && { log "Offline mode — skipping image build."; return 0; }
  log "Building image from $SCRIPT_DIR ..."
  load_env_file
  docker compose -f "$SCRIPT_DIR/docker-compose.prod.yml" --env-file "$APP_DIR/.env" build 2>&1 | tail -20
  return ${PIPESTATUS[0]}
}

start_container() {
  cd "$APP_DIR" || die "cannot cd $APP_DIR"
  log "Starting container..."
  load_env_file
  docker compose --env-file "$APP_DIR/.env" up -d 2>&1 | tail -20
  return ${PIPESTATUS[0]}
}

wait_ready() {
  log "Waiting for OpenWebPanel to become ready..."
  local ok=0 i
  for i in $(seq 1 60); do
    if curl -sf -o /dev/null "http://127.0.0.1:${OWP_PANEL_PORT:-2086}/healthz" 2>/dev/null; then
      ok=1; break
    fi
    sleep 5
  done
  if [[ $ok -eq 0 ]]; then
    die "panel did not become ready. Logs:"
    docker compose -f "$APP_DIR/docker-compose.yml" logs --tail 60 2>/dev/null | tail -60
  fi
  log "panel is ready."
}

configure_firewall() {
  [[ "${OWP_SKIP_FIREWALL:-false}" == "true" ]] && return 0
  command -v ufw >/dev/null 2>&1 || return 0
  log "Configuring firewall..."
  ufw allow 80/tcp >/dev/null 2>&1 || true
  ufw allow 443/tcp >/dev/null 2>&1 || true
  ufw allow "${OWP_PANEL_PORT:-2086}/tcp" >/dev/null 2>&1 || true
  ufw allow "${OWP_USER_PORT:-2082}/tcp" >/dev/null 2>&1 || true
  ufw allow 2525/tcp >/dev/null 2>&1 || true
  ufw allow 21/tcp >/dev/null 2>&1 || true
  ufw allow 40000:40009/tcp >/dev/null 2>&1 || true
  ufw allow ssh >/dev/null 2>&1 || true
  ufw --force enable >/dev/null 2>&1 || true
}

interactive_creds() {
  printf "  Admin username [admin]: "; read -r OWP_ADMIN_USERNAME
  OWP_ADMIN_USERNAME="${OWP_ADMIN_USERNAME:-admin}"
  printf "  Admin password (min 8 chars): "; read -rs OWP_ADMIN_PASSWORD; echo ""
  [[ ${#OWP_ADMIN_PASSWORD} -lt 8 ]] && OWP_ADMIN_PASSWORD=""
}

verify_containers() {
  log "Verifying container..."
  local failed=0
  if docker ps --format '{{.Names}}' 2>/dev/null | grep -qx openwebpanel; then
    log "container OK: openwebpanel"
  else
    log "[ERROR] container not running: openwebpanel"
    failed=1
  fi
  # Panel health endpoint (mirror of wait_ready probe)
  if curl -sf -o /dev/null "http://127.0.0.1:${OWP_PANEL_PORT:-2086}/healthz" 2>/dev/null; then
    log "healthz OK"
  else
    log "[WARN] healthz not reachable yet (may still be starting)"
  fi
  return $failed
}

print_summary() {
  echo ""
  echo "════════════════════════════════════════════════════════"
  echo "  OpenWebPanel installed via Docker Compose"
  echo "════════════════════════════════════════════════════════"
  echo "  Container service : openwebpanel"
  echo "  Admin panel       : http://<server>:${OWP_PANEL_PORT:-2086}"
  echo "  User panel        : http://<server>:${OWP_USER_PORT:-2082}"
  echo "  Admin username    : ${OWP_ADMIN_USERNAME:-admin}"
  echo "  Admin password    : ${OWP_ADMIN_PASSWORD:-<see .env>}"
  echo ""
  echo "  Manage:"
  echo "    docker compose -f $APP_DIR/docker-compose.yml logs -f openwebpanel"
  echo "    docker compose -f $APP_DIR/docker-compose.yml restart openwebpanel"
  echo "    docker compose -f $APP_DIR/docker-compose.yml down"
  echo "════════════════════════════════════════════════════════"
}

main() {
  require_root
  system_summary
  ensure_docker

  # admin credentials
  if $IS_TTY && [[ "${OWP_AUTO_YES}" != "1" && -z "${OWP_ADMIN_PASSWORD:-}" ]]; then
    interactive_creds
  fi
  OWP_ADMIN_USERNAME="${OWP_ADMIN_USERNAME:-admin}"
  OWP_ADMIN_PASSWORD="${OWP_ADMIN_PASSWORD:-}"
  if [[ -z "$OWP_ADMIN_PASSWORD" ]]; then
    OWP_ADMIN_PASSWORD="$(gen_secret 16)"
    log "auto-generated admin password: $OWP_ADMIN_PASSWORD"
  fi
  OWP_JWT_SECRET="${OWP_JWT_SECRET:-$(gen_secret 48)}"
  OWP_MYSQL_ROOT_PASSWORD="${OWP_MYSQL_ROOT_PASSWORD:-$(gen_secret 24)}"
  OWP_MYSQL_ADMIN_PASSWORD="${OWP_MYSQL_ADMIN_PASSWORD:-$(gen_secret 24)}"

  if bundle_present; then
    OWP_OFFLINE=1
    load_bundle || log "[WARN] some bundle images failed to load."
  else
    OWP_OFFLINE=0
    log "No offline bundle found — building image locally (network required)."
  fi

  write_env
  install_compose_file
  build_image || die "image build failed"
  start_container || die "compose up failed"
  wait_ready
  verify_containers || log "[WARN] container failed verification"
  configure_firewall
  print_summary
}

main "$@"
