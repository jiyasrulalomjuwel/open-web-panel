#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════════
# OpenWebPanel Installer v2.1
#   One-command web hosting control panel installer for Ubuntu/Debian
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/jiyasrulalomjuwel/open-web-panel/main/install.sh | sudo bash
#   sudo bash install.sh
#   sudo OWP_DOMAIN=1.2.3.4 bash install.sh
#
# Env overrides:
#   OWP_VERSION, OWP_REPO, OWP_USER, OWP_HOME, OWP_DOMAIN,
#   OWP_JWT_SECRET, OWP_PANEL_PORT, OWP_USER_PORT,
#   OWP_SKIP_FIREWALL, OWP_SKIP_SWAP, OWP_DEBUG
# ═══════════════════════════════════════════════════════════════════════════════

# Do NOT use set -e — we handle errors manually so output is always visible
set -uo pipefail

# ─── Force noninteractive apt so install never hangs on prompts ───────────
export DEBIAN_FRONTEND=noninteractive

# ─── Config ───────────────────────────────────────────────────────────────
OWP_VERSION="${OWP_VERSION:-main}"
OWP_REPO="${OWP_REPO:-jiyasrulalomjuwel/open-web-panel}"
OWP_USER="${OWP_USER:-openwebpanel}"
OWP_HOME="${OWP_HOME:-/opt/openwebpanel}"
OWP_DATA="${OWP_HOME}/data"
OWP_HOMES_DIR="${OWP_HOME}/homes"
OWP_LOGS="${OWP_HOME}/logs"
OWP_BACKUPS="${OWP_HOME}/backups"
OWP_JWT_SECRET="${OWP_JWT_SECRET:-}"
OWP_DOMAIN="${OWP_DOMAIN:-}"
OWP_SKIP_FIREWALL="${OWP_SKIP_FIREWALL:-false}"
OWP_SKIP_SWAP="${OWP_SKIP_SWAP:-false}"
OWP_DEBUG="${OWP_DEBUG:-false}"
OWP_PANEL_PORT="${OWP_PANEL_PORT:-2086}"
OWP_USER_PORT="${OWP_USER_PORT:-2082}"
OWP_PHPMYADMIN_VER="${OWP_PHPMYADMIN_VER:-5.2.2}"
OWP_GO_VERSION="1.25.0"
OWP_NODE_VERSION="20"
PASSGEN_SOURCE="openssl"

# ─── Colors ───────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; CYAN='\033[0;36m'; NC='\033[0m'
info()   { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()     { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()   { echo -e "${YELLOW}[WARN]${NC}  $*"; }
err()    { echo -e "${RED}[ERR]${NC}   $*"; }
die()    { err "$@"; exit 1; }
header() { echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"; echo -e "${BLUE}  $*${NC}"; echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"; }
section(){ echo -e "\n${YELLOW}▶ $*${NC}"; }

debug() { [[ "$OWP_DEBUG" == "true" ]] && echo -e "${YELLOW}[DEBUG]${NC} $*"; }

# ─── Password generation (works on all Ubuntu versions) ───────────────────
gen_password() {
  local len="${1:-20}"
  if command -v openssl &>/dev/null; then
    openssl rand -base64 30 2>/dev/null | tr -dc 'A-Za-z0-9' | head -c "$len"
  elif command -v python3 &>/dev/null; then
    python3 -c "import secrets; print(secrets.token_hex($len))" 2>/dev/null | head -c "$len"
  else
    # Fallback: use /dev/urandom only with limited charset
    head -c 100 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9' | head -c "$len"
  fi
  echo
}

# ─── Pre-flight check (called BEFORE anything else) ──────────────────────
preflight_check() {
  # Immediately show the script is running — this is the first visible output
  echo ""
  echo -e "${BLUE}╔══════════════════════════════════════════════════╗${NC}"
  echo -e "${BLUE}║     OpenWebPanel Installer v2.1                 ║${NC}"
  echo -e "${BLUE}║     Web Hosting Control Panel                   ║${NC}"
  echo -e "${BLUE}╚══════════════════════════════════════════════════╝${NC}"
  echo ""
  echo "[STATUS] Pre-flight checks starting..."

  # Check root
  if [[ $EUID -ne 0 ]]; then
    echo "[ERROR] This installer must be run as root (or with sudo)"
    exit 1
  fi
  echo "[OK] Running as root"

  # Check OS
  if [[ -f /etc/os-release ]]; then
    . /etc/os-release
    echo "[OK] OS: $NAME $VERSION_ID"
  else
    echo "[WARN] Cannot detect OS, proceeding anyway"
    ID="unknown"
    VERSION_ID="unknown"
  fi

  # Check arch
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  ARCH="amd64" ;; aarch64) ARCH="arm64" ;;
    *)
      echo "[ERROR] Unsupported architecture: $ARCH"
      exit 1
      ;;
  esac
  echo "[OK] Architecture: $ARCH"

  # Check bash version
  echo "[OK] Bash: $BASH_VERSION"

  # Generate passwords
  echo "[STATUS] Generating passwords..."
  OWP_MYSQL_ROOT_PW="$(gen_password 20)"
  OWP_MYSQL_ADMIN_PW="$(gen_password 20)"
  debug "MySQL root password: $OWP_MYSQL_ROOT_PW"

  # Set JWT secret
  if [[ -z "$OWP_JWT_SECRET" ]]; then
    OWP_JWT_SECRET="$(gen_password 48)"
  fi

  echo "[OK] Pre-flight checks complete"
  echo ""
}

# ─── Stage 1: Prerequisites ──────────────────────────────────────────────
stage_prereqs() {
  header "Stage 1/10 — Prerequisites"

  # Auto-detect domain
  if [[ -z "$OWP_DOMAIN" ]]; then
    echo "[STATUS] Detecting public IP..."
    OWP_DOMAIN=$(curl -4 -s --connect-timeout 5 ifconfig.me 2>/dev/null || \
                 hostname -I 2>/dev/null | awk '{print $1}' || \
                 echo "127.0.0.1")
    echo "[OK] Server IP: $OWP_DOMAIN"
  else
    info "Server: $OWP_DOMAIN"
  fi

  # Check disk space
  AVAIL_KB=$(df / --output=avail 2>/dev/null | tail -1)
  if [[ -n "$AVAIL_KB" && "$AVAIL_KB" -lt 1048576 ]]; then
    die "Less than 1GB disk space available. At least 1GB is required."
  fi
  info "Disk space: $((AVAIL_KB / 1024))MB available"

  # Memory check + swap
  local mem_kb
  mem_kb=$(grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}' || echo 0)
  local mem_mb=$((mem_kb / 1024))
  info "Memory: ${mem_mb}MB"

  if [[ "$mem_mb" -lt 1024 && "$OWP_SKIP_SWAP" != "true" ]]; then
    warn "Less than 1GB RAM. Creating 2GB swap file..."
    if [[ -f /swapfile ]]; then
      info "Swap already exists at /swapfile"
    else
      dd if=/dev/zero of=/swapfile bs=1M count=2048 status=progress 2>/dev/null
      chmod 600 /swapfile
      mkswap /swapfile >/dev/null 2>&1
      swapon /swapfile
      echo '/swapfile none swap sw 0 0' >> /etc/fstab
      ok "2GB swap created"
    fi
  fi

  ok "Stage 1 complete"
}

# ─── Stage 2: System User ────────────────────────────────────────────────
stage_user() {
  header "Stage 2/10 — System User"

  if id "$OWP_USER" &>/dev/null; then
    info "User '$OWP_USER' already exists"
  else
    useradd -m -d "$OWP_HOME" -s /usr/sbin/nologin -U "$OWP_USER" 2>&1 || die "Failed to create user '$OWP_USER'"
    info "Created system user: $OWP_USER"
  fi

  mkdir -p "$OWP_HOME" "$OWP_DATA" "$OWP_HOMES_DIR" "$OWP_LOGS" "$OWP_BACKUPS"
  usermod -aG "$OWP_USER" www-data 2>/dev/null || true
  chown -R "$OWP_USER:$OWP_USER" "$OWP_HOME"
  chmod 755 "$OWP_HOME"

  ok "Stage 2 complete — user $OWP_USER at $OWP_HOME"
}

# ─── Wait for dpkg lock (handles hung unattended-upgrades) ─────────────
wait_for_dpkg() {
  local waited=0
  local MAX_WAIT=120
  echo "[STATUS] Checking for dpkg lock..."

  while fuser /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/cache/apt/archives/lock &>/dev/null 2>&1; do
    local pid
    local pname
    pid=$(fuser /var/lib/dpkg/lock-frontend 2>/dev/null | head -1)
    if [[ -n "$pid" && "$pid" =~ ^[0-9]+$ ]]; then
      pname=$(ps -p "$pid" -o comm= 2>/dev/null || echo "unknown")
    else
      pid="?"
      pname="unknown"
    fi

    if [[ $waited -ge $MAX_WAIT ]]; then
      echo ""
      warn "dpkg lock held for ${MAX_WAIT}s by ${pname} (PID ${pid}) — force-clearing..."
      kill -9 "$pid" 2>/dev/null || true
      sleep 2
      # Kill any other apt/dpkg processes
      killall -9 unattended-upgrade apt-get apt dpkg 2>/dev/null || true
      rm -f /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/cache/apt/archives/lock \
            /var/lib/dpkg/lock-frontend /var/lib/apt/lists/lock 2>/dev/null || true
      dpkg --configure -a 2>/dev/null || true
      echo "[OK] dpkg lock force-cleared"
      return 0
    fi

    if [[ $((waited % 10)) -eq 0 ]]; then
      echo "[WAIT] ${pname} (PID ${pid}) holds dpkg lock — waiting... (${waited}s)"
    fi
    sleep 2
    waited=$((waited + 2))
  done
  echo "[OK] dpkg is free"
}

# ─── Stage 3: System Packages ────────────────────────────────────────────
stage_system_packages() {
  header "Stage 3/10 — System Packages"

  wait_for_dpkg

  echo "[STATUS] Updating package lists..."
  apt-get update -qq 2>&1 | tail -3 || warn "apt-get update had issues, continuing..."

  # Determine PHP version
  PHP_VER="8.3"
  if [[ "$ID" == "ubuntu" ]]; then
    case "$VERSION_ID" in
      20.04) PHP_VER="8.0" ;;
      22.04) PHP_VER="8.1" ;;
    esac
  fi
  echo "$PHP_VER" > /tmp/owp_php_ver
  info "PHP version: $PHP_VER"

  info "Installing packages (this may take a few minutes)..."
  apt-get install -y -qq \
    curl wget git tar gpg build-essential \
    nginx \
    mariadb-server mariadb-client \
    "php${PHP_VER}-fpm" "php${PHP_VER}-mysqli" "php${PHP_VER}-curl" \
    "php${PHP_VER}-mbstring" "php${PHP_VER}-xml" "php${PHP_VER}-bcmath" \
    "php${PHP_VER}-gd" "php${PHP_VER}-zip" \
    unzip openssl sudo cron sqlite3 \
    rsync jq ufw 2>&1 || {
      err "Some packages failed to install."
      err "Try running: apt-get install -y nginx mariadb-server php${PHP_VER}-fpm unzip openssl"
      die "Package installation failed"
    }

  ok "Stage 3 complete — system packages installed"
}

# ─── Stage 4: Go & Node.js ───────────────────────────────────────────────
stage_languages() {
  header "Stage 4/10 — Go & Node.js"

  # ─── Go ──────────────────────────────────────────────────────────
  if command -v go &>/dev/null; then
    GO_CUR=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+' 2>/dev/null || echo "0")
    info "Go $(go version | grep -oP 'go\S+' 2>/dev/null) already installed"
    if awk "BEGIN {exit !($GO_CUR < 1.22)}" 2>/dev/null; then
      warn "Go $GO_CUR is too old (need 1.22+), upgrading..."
      rm -f "$(which go)" 2>/dev/null || true
    else
      GO_CUR=""  # flag to skip install
    fi
  fi

  if ! command -v go &>/dev/null || [[ -n "${GO_CUR:-}" ]]; then
    info "Installing Go ${OWP_GO_VERSION}..."
    wget -q "https://go.dev/dl/go${OWP_GO_VERSION}.linux-${ARCH}.tar.gz" -O /tmp/go.tar.gz || die "Failed to download Go"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz || die "Failed to extract Go"
    rm /tmp/go.tar.gz
    ln -sf /usr/local/go/bin/go /usr/local/bin/go
    ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
    ok "Go ${OWP_GO_VERSION} installed"
  fi

  # ─── Node.js ─────────────────────────────────────────────────────
  if command -v node &>/dev/null; then
    NODE_MAJOR=$(node -v | sed 's/v//' | cut -d. -f1)
    info "Node.js $(node -v) already installed"
    if [[ "$NODE_MAJOR" -lt 20 ]]; then
      warn "Node.js is too old (need 20+), upgrading..."
    else
      NODE_EXISTS=true
    fi
  fi

  if [[ "${NODE_EXISTS:-false}" != "true" ]]; then
    info "Installing Node.js ${OWP_NODE_VERSION}.x..."
    # Download the NodeSource script (don't pipe to bash to avoid set -u conflicts)
    curl -fsSL "https://deb.nodesource.com/setup_${OWP_NODE_VERSION}.x" -o /tmp/nodesource.sh 2>&1 || die "Failed to download NodeSource setup"
    # Run with set +u so NodeSource's unbound vars don't crash us
    bash -c "set +eu; source /tmp/nodesource.sh" 2>&1 | tail -5 || die "Failed to setup NodeSource repo"
    rm -f /tmp/nodesource.sh
    wait_for_dpkg
    apt-get install -y -qq nodejs 2>&1 | tail -3 || die "Failed to install Node.js"
    ok "Node.js $(node -v) installed"
  fi

  ok "Stage 4 complete — Go & Node.js ready"
}

# ─── Stage 5: Project Files ──────────────────────────────────────────────
stage_project() {
  header "Stage 5/10 — Project Files"

  local script_dir
  script_dir="$(cd "$(dirname "$0")" && pwd 2>/dev/null)"

  if [[ -f "$script_dir/go.mod" ]] && grep -q "openwebcpanel\|open-web-panel" "$script_dir/go.mod" 2>/dev/null; then
    info "Copying project from local source: $script_dir"
    mkdir -p "${OWP_HOME}/app"
    rsync -a --delete \
      --exclude='.git' --exclude='node_modules' --exclude='bin' \
      --exclude='homes' --exclude='*.db' --exclude='*.db-shm' --exclude='*.db-wal' \
      --exclude='__pycache__' --exclude='.env' \
      "$script_dir"/ "${OWP_HOME}/app/" 2>&1
    ok "Project files copied from local source"
  elif [[ -d "${OWP_HOME}/app" && -f "${OWP_HOME}/app/go.mod" ]]; then
    info "Project already installed at ${OWP_HOME}/app"
    if command -v git &>/dev/null; then
      cd "${OWP_HOME}/app"
      git pull --ff-only origin "$OWP_VERSION" 2>/dev/null && ok "Updated to latest code" || warn "Could not update"
    fi
    return
  else
    info "Cloning ${OWP_REPO} (branch: ${OWP_VERSION})..."
    rm -rf "${OWP_HOME}/app"
    git clone --depth 1 --branch "$OWP_VERSION" "https://github.com/${OWP_REPO}.git" "${OWP_HOME}/app" 2>&1 || {
      warn "Branch '${OWP_VERSION}' not found, trying default branch..."
      git clone --depth 1 "https://github.com/${OWP_REPO}.git" "${OWP_HOME}/app" 2>&1
    }
    ok "Project cloned from GitHub"
  fi

  chown -R "$OWP_USER:$OWP_USER" "${OWP_HOME}/app" 2>/dev/null || true
  ok "Stage 5 complete — project at ${OWP_HOME}/app"
}

# ─── Stage 6: Services Configuration ─────────────────────────────────────
stage_services() {
  header "Stage 6/10 — Services Configuration"
  PHP_VER=$(cat /tmp/owp_php_ver 2>/dev/null || echo "8.3")

  # ─── nginx ───────────────────────────────────────────────────────
  section "Configuring nginx"
  mkdir -p /etc/nginx/sites-enabled /etc/nginx/vhosts
  # Give openwebpanel user write access to vhosts directory for domain management
  chown -R "$OWP_USER:www-data" /etc/nginx/vhosts
  chmod 750 /etc/nginx/vhosts
  rm -f /etc/nginx/sites-enabled/default

  cat > /etc/nginx/sites-available/openwebpanel << 'NGINX_CONF'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    client_max_body_size 2048M;
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;

    location / {
        proxy_pass http://127.0.0.1:9000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:9000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }

    location /pma/ {
        proxy_pass http://127.0.0.1:8080/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location ^~ /.well-known/acme-challenge/ {
        proxy_pass http://127.0.0.1:9000;
        proxy_set_header Host $host;
    }
}

server {
    listen GUI_PORT;
    listen [::]:GUI_PORT;
    server_name _;
    client_max_body_size 0;
    location / {
        proxy_pass http://127.0.0.1:9000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}

server {
    listen USER_PORT;
    listen [::]:USER_PORT;
    server_name _;
    client_max_body_size 0;
    location / {
        proxy_pass http://127.0.0.1:9000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
NGINX_CONF

  sed -i "s/GUI_PORT/${OWP_PANEL_PORT}/g" /etc/nginx/sites-available/openwebpanel
  sed -i "s/USER_PORT/${OWP_USER_PORT}/g" /etc/nginx/sites-available/openwebpanel

  ln -sf /etc/nginx/sites-available/openwebpanel /etc/nginx/sites-enabled/openwebpanel
  sed -i 's/^user .*/user www-data;/' /etc/nginx/nginx.conf 2>/dev/null || true
  mkdir -p /var/log/nginx

  nginx -t 2>&1 && ok "nginx config valid" || warn "nginx config has issues"
  ok "nginx configured"

  # ─── MariaDB ─────────────────────────────────────────────────────
  section "Configuring MariaDB"
  systemctl enable mariadb --now 2>/dev/null || service mariadb start 2>/dev/null || true

  # Wait for MariaDB
  echo "[STATUS] Waiting for MariaDB to start..."
  for i in $(seq 1 15); do
    if mysqladmin ping --silent 2>/dev/null; then break; fi
    sleep 1
  done

  mysqladmin ping --silent 2>/dev/null || warn "MariaDB didn't respond, continuing anyway..."

  # Try to configure MariaDB (handle case where auth_socket is used)
  mysql -e "SELECT 1" 2>/dev/null && MYSQL_AUTH="native" || MYSQL_AUTH="socket"

  if [[ "$MYSQL_AUTH" == "socket" ]]; then
    # Ubuntu 24.04+ uses auth_socket by default
    mysql -e "
      ALTER USER 'root'@'localhost' IDENTIFIED BY '${OWP_MYSQL_ROOT_PW}';
    " 2>&1 || {
      mysql --socket=/run/mysqld/mysqld.sock -e "
        ALTER USER 'root'@'localhost' IDENTIFIED BY '${OWP_MYSQL_ROOT_PW}';
      " 2>&1 || warn "Could not set MariaDB root password (may need manual config)"
    }
  else
    mysql -e "
      ALTER USER 'root'@'localhost' IDENTIFIED BY '${OWP_MYSQL_ROOT_PW}';
      DELETE FROM mysql.user WHERE User='';
      DELETE FROM mysql.user WHERE User='root' AND Host NOT IN ('localhost','127.0.0.1','::1');
      DROP DATABASE IF EXISTS test;
      DELETE FROM mysql.db WHERE Db='test' OR Db='test\\_%';
      CREATE USER IF NOT EXISTS 'owp_admin'@'localhost' IDENTIFIED BY '${OWP_MYSQL_ADMIN_PW}';
      GRANT ALL PRIVILEGES ON *.* TO 'owp_admin'@'localhost' WITH GRANT OPTION;
      FLUSH PRIVILEGES;
    " 2>&1 || warn "MariaDB secure config had issues"
  fi

  # Save root credentials
  cat > /root/.my.cnf <<EOF
[client]
user=root
password=${OWP_MYSQL_ROOT_PW}
EOF
  chmod 600 /root/.my.cnf

  ok "MariaDB configured"

  # ─── PHP ─────────────────────────────────────────────────────────
  section "Configuring PHP"
  PHP_INI="/etc/php/${PHP_VER}/fpm/php.ini"
  if [[ -f "$PHP_INI" ]]; then
    sed -i 's/^upload_max_filesize = .*/upload_max_filesize = 256M/' "$PHP_INI"
    sed -i 's/^post_max_size = .*/post_max_size = 256M/' "$PHP_INI"
    sed -i 's/^max_execution_time = .*/max_execution_time = 300/' "$PHP_INI"
    sed -i 's/^memory_limit = .*/memory_limit = 256M/' "$PHP_INI"
  fi

  systemctl enable "php${PHP_VER}-fpm" --now 2>/dev/null || true
  ok "PHP ${PHP_VER} configured"

  # ─── phpMyAdmin ─────────────────────────────────────────────────
  section "Installing phpMyAdmin"
  PMA_DIR="/usr/share/phpmyadmin"
  if [[ -d "$PMA_DIR" && -f "$PMA_DIR/index.php" ]]; then
    info "phpMyAdmin already installed at $PMA_DIR"
  else
    mkdir -p "$PMA_DIR"
    info "Downloading phpMyAdmin ${OWP_PHPMYADMIN_VER}..."
    wget -q "https://files.phpmyadmin.net/phpMyAdmin/${OWP_PHPMYADMIN_VER}/phpMyAdmin-${OWP_PHPMYADMIN_VER}-all-languages.zip" -O /tmp/pma.zip || die "Failed to download phpMyAdmin"
    unzip -qo /tmp/pma.zip -d /tmp/pma/ || die "Failed to unzip phpMyAdmin"
    cp -r /tmp/pma/phpMyAdmin-${OWP_PHPMYADMIN_VER}-all-languages/* "$PMA_DIR/"
    rm -rf /tmp/pma /tmp/pma.zip
    ok "phpMyAdmin ${OWP_PHPMYADMIN_VER} installed"
  fi

  BLOWFISH_SECRET="$(gen_password 32)"
  cat > "$PMA_DIR/config.inc.php" <<PMACFG
<?php
\$cfg['blowfish_secret'] = '${BLOWFISH_SECRET}';
\$i = 1;
\$cfg['Servers'][\$i]['auth_type'] = 'cookie';
\$cfg['Servers'][\$i]['host'] = 'localhost';
\$cfg['Servers'][\$i]['port'] = '3306';
\$cfg['Servers'][\$i]['compress'] = false;
\$cfg['Servers'][\$i]['AllowNoPassword'] = false;
\$cfg['Servers'][\$i]['hide_db'] = 'information_schema|performance_schema|mysql|sys';
\$cfg['ShowChgPassword'] = false;
\$cfg['ShowDbSpecificCreation'] = true;
PMACFG
  chown -R www-data:www-data "$PMA_DIR" 2>/dev/null || true
  ok "phpMyAdmin configured"

  ok "Stage 6 complete — all services configured"
}

# ─── Stage 7: Build ───────────────────────────────────────────────────────
stage_build() {
  header "Stage 7/10 — Building"
  export PATH="/usr/local/go/bin:$PATH"
  PHP_VER=$(cat /tmp/owp_php_ver 2>/dev/null || echo "8.3")

  # ─── Environment file ────────────────────────────────────────────
  section "Writing .env configuration"
  cat > "${OWP_HOME}/app/.env" <<EOF
OWP_JWT_SECRET=${OWP_JWT_SECRET}
OWP_DB_PATH=${OWP_DATA}/openwebpanel.db
OWP_STATIC_DIR=${OWP_HOME}/app/web/dist
OWP_LISTEN=:9000
OWP_SHARED_IP=127.0.0.1
OWP_PUBLIC_HOST=${OWP_DOMAIN}:9000
OWP_HOMES_BASE=${OWP_HOMES_DIR}/
OWP_PHPMYADMIN_PORT=http://127.0.0.1:8080
NGINX_PREFIX=/etc/nginx
NGINX_VHOST_DIR=/etc/nginx/vhosts
NGINX_BIN=/usr/sbin/nginx
NGINX_CONF=/etc/nginx/nginx.conf
NGINX_LOG_DIR=/var/log/nginx
PHP_FPM_SOCKET=/run/php/php${PHP_VER}-fpm.sock
MYSQL_ROOT_PASSWORD=${OWP_MYSQL_ROOT_PW}
MYSQL_ADMIN_PASSWORD=${OWP_MYSQL_ADMIN_PW}
OWP_SMTP_PORT=2525
EOF
  chmod 600 "${OWP_HOME}/app/.env"
  chown "$OWP_USER:$OWP_USER" "${OWP_HOME}/app/.env"
  ok "Environment configured"

  # ─── Backend ─────────────────────────────────────────────────────
  section "Building backend (Go)"
  cd "${OWP_HOME}/app"
  info "Running go build for parentd..."
  go version 2>&1 | head -1
  CGO_ENABLED=1 go build -o bin/parentd ./cmd/parentd/ 2>&1 || die "Go build failed for parentd"
  ok "Backend built — bin/parentd"

  # ─── Frontend ────────────────────────────────────────────────────
  section "Building frontend (React)"
  cd "${OWP_HOME}/app/web"
  if [[ ! -d node_modules ]]; then
    info "Installing npm dependencies..."
    npm ci --no-audit --no-fund 2>&1 | tail -5 || {
      warn "npm ci failed, trying npm install..."
      npm install 2>&1 | tail -5 || die "npm install failed"
    }
  fi
  npm run build 2>&1 | tail -10 || die "Frontend build failed"
  ok "Frontend built"

  chown -R "$OWP_USER:$OWP_USER" "${OWP_HOME}/app"
  ok "Stage 7 complete — backend + frontend built"
}

# ─── Stage 8: Systemd Services ───────────────────────────────────────────
stage_systemd() {
  header "Stage 8/10 — Systemd Services"

  cat > /etc/systemd/system/openwebpanel.service <<UNIT
[Unit]
Description=OpenWebPanel - Web Hosting Control Panel
Documentation=https://github.com/${OWP_REPO}
After=network.target mariadb.service
Wants=mariadb.service nginx.service

[Service]
Type=simple
User=${OWP_USER}
Group=${OWP_USER}
WorkingDirectory=${OWP_HOME}/app
EnvironmentFile=${OWP_HOME}/app/.env
ExecStart=${OWP_HOME}/app/bin/parentd
Restart=always
RestartSec=10
StartLimitInterval=300
StartLimitBurst=5
LimitNOFILE=65536
LimitNPROC=4096
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
UNIT

  cat > /etc/systemd/system/phpmyadmin.service <<UNIT
[Unit]
Description=phpMyAdmin PHP Built-in Server
After=network.target

[Service]
Type=simple
User=www-data
ExecStart=/usr/bin/php -S 127.0.0.1:8080 -t /usr/share/phpmyadmin
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

  systemctl daemon-reload
  systemctl enable openwebpanel 2>&1 || warn "Failed to enable openwebpanel"
  systemctl enable phpmyadmin 2>&1 || warn "Failed to enable phpmyadmin"

  # ─── Sudoers: allow openwebpanel user to reload nginx ─────────────
  cat > /etc/sudoers.d/openwebpanel <<SUDOERS
# Allow OpenWebPanel to reload nginx when vhost configs change
${OWP_USER} ALL=(root) NOPASSWD: /usr/sbin/nginx
${OWP_USER} ALL=(root) NOPASSWD: /usr/bin/systemctl reload nginx
${OWP_USER} ALL=(root) NOPASSWD: /usr/bin/systemctl reload nginx.service
SUDOERS
  chmod 440 /etc/sudoers.d/openwebpanel
  visudo -c -f /etc/sudoers.d/openwebpanel 2>/dev/null || rm -f /etc/sudoers.d/openwebpanel

  ok "Stage 8 complete — systemd services + sudoers installed"
}

# ─── Stage 9: Monitoring ─────────────────────────────────────────────────
stage_monitoring() {
  header "Stage 9/10 — Monitoring & Maintenance"

  # ─── Watchdog script ─────────────────────────────────────────────
  mkdir -p "${OWP_HOME}/app/bin"
  cat > "${OWP_HOME}/app/bin/owp-watchdog.sh" <<'WATCHDOG'
#!/usr/bin/env bash
set -uo pipefail
OWP_HOME="${OWP_HOME:-/opt/openwebpanel}"
LOG="${OWP_HOME}/logs/watchdog.log"
HEALTH_URL="http://127.0.0.1:9000/healthz"

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" >> "$LOG"; }

check_health() {
  for i in 1 2 3; do
    if curl -sf --max-time 5 "$HEALTH_URL" >/dev/null 2>&1; then return 0; fi
    sleep 2
  done
  return 1
}

if ! check_health; then
  log "WARN: Panel not responding. Attempting restart..."
  systemctl restart openwebpanel 2>/dev/null || {
    if [[ -f "${OWP_HOME}/app/bin/parentd" ]]; then
      cd "${OWP_HOME}/app"
      nohup ./bin/parentd > "${OWP_HOME}/logs/parentd.log" 2>&1 &
    fi
  }
  sleep 5
  check_health && log "OK: Restarted successfully" || log "CRITICAL: Still not responding"
else
  log "OK: Healthy"
fi
WATCHDOG
  chmod +x "${OWP_HOME}/app/bin/owp-watchdog.sh"
  chown "$OWP_USER:$OWP_USER" "${OWP_HOME}/app/bin/owp-watchdog.sh"

  # ─── Cron jobs ───────────────────────────────────────────────────
  cat > /etc/cron.d/openwebpanel-watchdog <<CRON
*/5 * * * * root ${OWP_HOME}/app/bin/owp-watchdog.sh >/dev/null 2>&1
CRON
  chmod 644 /etc/cron.d/openwebpanel-watchdog || true

  # ─── Log rotation ────────────────────────────────────────────────
  cat > /etc/logrotate.d/openwebpanel <<LOGROTATE
${OWP_HOME}/logs/*.log {
    daily; rotate 30; compress; delaycompress; missingok; notifempty; copytruncate
}
${OWP_HOME}/app/parentd.log {
    daily; rotate 14; compress; delaycompress; missingok; notifempty; copytruncate
}
LOGROTATE

  # ─── Backup cron ─────────────────────────────────────────────────
  cat > /etc/cron.d/openwebpanel-backup <<CRON2
0 3 * * * root tar --exclude='node_modules' -czf ${OWP_BACKUPS}/backup-\$(date +\%Y\%m\%d).tar.gz -C ${OWP_HOME}/app . 2>/dev/null && find ${OWP_BACKUPS} -name 'backup-*.tar.gz' -mtime +7 -delete
CRON2
  chmod 644 /etc/cron.d/openwebpanel-backup || true

  ok "Stage 9 complete — watchdog, logrotate, backup configured"
}

# ─── Stage 10: Start & Verify ────────────────────────────────────────────
stage_start() {
  header "Stage 10/10 — Starting Services"

  # ─── Firewall ────────────────────────────────────────────────────
  if [[ "$OWP_SKIP_FIREWALL" != "true" ]] && command -v ufw &>/dev/null; then
    section "Configuring firewall"
    ufw --force reset 2>/dev/null || true
    ufw default deny incoming 2>/dev/null || true
    ufw default allow outgoing 2>/dev/null || true
    ufw allow ssh 2>/dev/null || true
    ufw allow 80/tcp 2>/dev/null || true
    ufw allow 443/tcp 2>/dev/null || true
    ufw allow "${OWP_PANEL_PORT}/tcp" 2>/dev/null || true
    ufw allow "${OWP_USER_PORT}/tcp" 2>/dev/null || true
    ufw --force enable 2>/dev/null || true
    ok "Firewall configured"
  fi

  PHP_VER=$(cat /tmp/owp_php_ver 2>/dev/null || echo "8.3")

  # ─── Start services in order ─────────────────────────────────────
  section "Starting MariaDB..."
  systemctl restart mariadb 2>/dev/null || service mariadb restart 2>/dev/null || true
  for i in $(seq 1 10); do
    if mysqladmin ping --silent 2>/dev/null; then break; fi
    sleep 1
  done

  section "Starting PHP-FPM..."
  systemctl restart "php${PHP_VER}-fpm" 2>/dev/null || true

  section "Starting phpMyAdmin..."
  systemctl restart phpmyadmin 2>/dev/null || true

  section "Starting nginx..."
  nginx -t 2>/dev/null && systemctl restart nginx 2>/dev/null || true

  section "Starting OpenWebPanel..."
  # Show any startup errors instead of hiding them
  systemctl start openwebpanel 2>&1 || {
    warn "systemd start failed, showing journal:"
    journalctl -u openwebpanel -n 15 --no-pager 2>/dev/null || true
  }

  echo ""
  info "Waiting for panel to respond..."
  local started=false
  for i in $(seq 1 30); do
    if curl -sf -o /dev/null http://127.0.0.1:9000/healthz 2>/dev/null; then
      started=true
      break
    fi
    sleep 1
  done

  if [[ "$started" == "true" ]]; then
    ok "OpenWebPanel is running on port 9000"
  else
    warn "Panel did not respond within 30s. Checking journal..."
    journalctl -u openwebpanel -n 30 --no-pager 2>/dev/null || true
    warn "Trying direct start as fallback (all env vars)..."
    sudo -u "$OWP_USER" \
      OWP_JWT_SECRET="${OWP_JWT_SECRET}" \
      OWP_DB_PATH="${OWP_DATA}/openwebpanel.db" \
      OWP_STATIC_DIR="${OWP_HOME}/app/web/dist" \
      OWP_LISTEN=":9000" \
      OWP_HOMES_BASE="${OWP_HOMES_DIR}/" \
      OWP_PUBLIC_HOST="${OWP_DOMAIN}:9000" \
      OWP_SHARED_IP="127.0.0.1" \
      OWP_SMTP_PORT="2525" \
      NGINX_PREFIX="/etc/nginx" \
      NGINX_VHOST_DIR="/etc/nginx/vhosts" \
      NGINX_BIN="/usr/sbin/nginx" \
      NGINX_CONF="/etc/nginx/nginx.conf" \
      NGINX_LOG_DIR="/var/log/nginx" \
      PHP_FPM_SOCKET="/run/php/php${PHP_VER}-fpm.sock" \
      MYSQL_ROOT_PASSWORD="${OWP_MYSQL_ROOT_PW}" \
      MYSQL_ADMIN_PASSWORD="${OWP_MYSQL_ADMIN_PW}" \
      "${OWP_HOME}/app/bin/parentd" &>/tmp/owp-fallback.log &
    sleep 5
    if curl -sf -o /dev/null http://127.0.0.1:9000/healthz 2>/dev/null; then
      ok "OpenWebPanel started in fallback mode"
      warn "Systemd service may still have issues — check: journalctl -u openwebpanel"
    else
      err "Failed to start panel. Last 30 lines of log:"
      tail -30 /tmp/owp-fallback.log 2>/dev/null || true
      err "Journal for openwebpanel:"
      journalctl -u openwebpanel -n 20 --no-pager 2>/dev/null || true
      err "Binary exists: $([ -f "${OWP_HOME}/app/bin/parentd" ] && echo 'YES' || echo 'MISSING!')"
      err "Env file: $([ -f "${OWP_HOME}/app/.env" ] && echo 'EXISTS' || echo 'MISSING!')"
    fi
  fi

  # Verify services
  for svc in openwebpanel phpmyadmin nginx mariadb; do
    if systemctl is-active --quiet "$svc" 2>/dev/null; then
      ok "Service '$svc' is running"
    else
      warn "Service '$svc' is not active"
    fi
  done

  # Verify auto-start on boot
  for svc in openwebpanel phpmyadmin nginx mariadb; do
    if systemctl is-enabled --quiet "$svc" 2>/dev/null; then
      ok "Service '$svc' auto-start enabled"
    else
      warn "Service '$svc' is NOT enabled for auto-start"
    fi
  done

  ok "Stage 10 complete"
}

# ─── Summary ──────────────────────────────────────────────────────────────
print_summary() {
  header "Installation Complete"
  echo ""
  echo -e "  ${GREEN}OpenWebPanel is installed and running!${NC}"
  echo ""
  echo -e "  ${CYAN}━━ Panel URLs ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "  Admin Panel:   ${BLUE}http://${OWP_DOMAIN}:${OWP_PANEL_PORT}${NC}"
  echo -e "  User Panel:    ${BLUE}http://${OWP_DOMAIN}:${OWP_USER_PORT}${NC}"
  echo -e "  Website:       ${BLUE}http://${OWP_DOMAIN}${NC}"
  echo ""
  echo -e "  ${CYAN}━━ Login ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "  Username:      ${YELLOW}admin${NC}"
  echo -e "  Password:      ${YELLOW}admin123${NC}"
  echo -e "  ${RED}⚠  CHANGE PASSWORD AFTER FIRST LOGIN${NC}"
  echo ""
  echo -e "  ${CYAN}━━ Commands ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "  Status:        systemctl status openwebpanel"
  echo -e "  Logs:          journalctl -u openwebpanel -f"
  echo -e "  Restart:       systemctl restart openwebpanel"
  echo ""
}

# ═══════════════════════════════════════════════════════════════════════════
# MAIN
# ═══════════════════════════════════════════════════════════════════════════

# Run pre-flight FIRST — this has no function dependencies and
# produces immediate visible output
preflight_check

# Now run all stages sequentially
stage_prereqs
stage_user
stage_system_packages
stage_languages
stage_project
stage_services
stage_build
stage_systemd
stage_monitoring
stage_start
print_summary
