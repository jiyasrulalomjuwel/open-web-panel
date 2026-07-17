#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════════
# OpenWebPanel Installer — Configuration
# ═══════════════════════════════════════════════════════════════════════════════

# ─── Version ────────────────────────────────────────────────────────────────
OWP_VERSION="2.1.0"
OWP_CODENAME="Nova"

# ─── Repository ─────────────────────────────────────────────────────────────
OWP_REPO="${OWP_REPO:-jiyasrulalomjuwel/open-web-panel}"
OWP_BRANCH="${OWP_BRANCH:-main}"

# ─── User & Paths ──────────────────────────────────────────────────────────
OWP_USER="${OWP_USER:-openwebpanel}"
OWP_HOME="${OWP_HOME:-/opt/openwebpanel}"
OWP_APP_DIR="${OWP_HOME}/app"
OWP_DATA_DIR="${OWP_HOME}/data"
OWP_HOMES_DIR="${OWP_HOME}/homes"
OWP_LOGS_DIR="${OWP_HOME}/logs"
OWP_BACKUPS_DIR="${OWP_HOME}/backups"
OWP_SSL_DIR="${OWP_HOME}/ssl"
OWP_TMP_DIR="${OWP_HOME}/tmp"

# ─── Database ───────────────────────────────────────────────────────────────
OWP_DB_PATH="${OWP_DATA_DIR}/openwebpanel.db"
OWP_DB_BACKUP="${OWP_BACKUPS_DIR}/openwebpanel.db.backup"

# ─── Networking ────────────────────────────────────────────────────────────
OWP_DOMAIN="${OWP_DOMAIN:-}"
OWP_SHARED_IP="${OWP_SHARED_IP:-127.0.0.1}"
OWP_PANEL_PORT="${OWP_PANEL_PORT:-2086}"
OWP_USER_PORT="${OWP_USER_PORT:-2082}"
OWP_SMTP_PORT="${OWP_SMTP_PORT:-2525}"
OWP_CHILD_PORT="${OWP_CHILD_PORT:-9001}"

# ─── Versions ───────────────────────────────────────────────────────────────
OWP_GO_VERSION="${OWP_GO_VERSION:-1.25.0}"
OWP_NODE_MAJOR="${OWP_NODE_MAJOR:-20}"
OWP_PHPMYADMIN_VER="${OWP_PHPMYADMIN_VER:-5.2.2}"

# ─── Credentials (auto-generated) ──────────────────────────────────────────
OWP_JWT_SECRET="${OWP_JWT_SECRET:-}"
OWP_ADMIN_PASSWORD="${OWP_ADMIN_PASSWORD:-}"
OWP_MYSQL_ROOT_PASSWORD="${OWP_MYSQL_ROOT_PASSWORD:-}"
OWP_MYSQL_ADMIN_PASSWORD="${OWP_MYSQL_ADMIN_PASSWORD:-}"
OWP_PMA_BLOWFISH_SECRET="${OWP_PMA_BLOWFISH_SECRET:-}"

# ─── Flags ──────────────────────────────────────────────────────────────────
OWP_SKIP_FIREWALL="${OWP_SKIP_FIREWALL:-false}"
OWP_SKIP_SWAP="${OWP_SKIP_SWAP:-false}"
OWP_DEBUG="${OWP_DEBUG:-false}"
OWP_AUTO_YES="${OWP_AUTO_YES:-false}"
OWP_SKIP_SSL="${OWP_SKIP_SSL:-true}"

# ─── Paths — System ────────────────────────────────────────────────────────
NGINX_PREFIX="${NGINX_PREFIX:-/etc/nginx}"
NGINX_VHOST_DIR="${NGINX_VHOST_DIR:-/etc/nginx/vhosts}"
NGINX_BIN="${NGINX_BIN:-/usr/sbin/nginx}"
NGINX_CONF="${NGINX_CONF:-/etc/nginx/nginx.conf}"
NGINX_LOG_DIR="${NGINX_LOG_DIR:-/var/log/nginx}"
PHP_FPM_SOCKET="${PHP_FPM_SOCKET:-/run/php/php-fpm.sock}"

# ─── Installer Internals ───────────────────────────────────────────────────
INSTALL_LOG="${INSTALL_LOG:-/var/log/openwebpanel-install.log}"
INSTALL_STATE_FILE="${INSTALL_STATE_FILE:-/tmp/owp-install.state}"
INSTALL_ROLLBACK_FILE="${INSTALL_ROLLBACK_FILE:-/tmp/owp-rollback.state}"
OWP_PASSWORD_FILE="${OWP_HOME}/.admin_password"
