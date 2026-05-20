#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════
# OpenWebPanel Installer v2.0
#   Web Hosting Control Panel — One-command install on Ubuntu/Debian
#
# Usage:
#   # Via curl (standalone — no files needed):
#   curl -fsSL https://raw.githubusercontent.com/openwebcpanel/openwebcpanel/main/install.sh | sudo bash
#
#   # Via wget:
#   wget -qO- https://raw.githubusercontent.com/openwebcpanel/openwebcpanel/main/install.sh | sudo bash
#
#   # From local source:
#   sudo bash install.sh
#
#   # With options:
#   sudo OWP_DOMAIN=panel.example.com OWP_USER=myadmin bash install.sh
#
# Env overrides:
#   OWP_VERSION       Git tag/branch to clone (default: main)
#   OWP_REPO          GitHub repo (default: openwebcpanel/openwebcpanel)
#   OWP_USER          System user for the panel (default: openwebpanel)
#   OWP_HOME          Install directory (default: /opt/openwebpanel)
#   OWP_DOMAIN        Server domain or public IP (default: auto-detected)
#   OWP_JWT_SECRET    JWT signing secret (default: auto-generated)
#   OWP_PANEL_PORT    Admin panel port (default: 2086)
#   OWP_USER_PORT     User panel port (default: 2082)
#   OWP_PHPMYADMIN_VER phpMyAdmin version (default: 5.2.2)
#   OWP_SKIP_FIREWALL Set to "true" to skip UFW config
#   OWP_SKIP_SWAP     Set to "true" to skip creating swap
#   OWP_DEBUG         Set to "true" for verbose output
# ═══════════════════════════════════════════════════════════════════════════
set -euo pipefail

# ─── Config ──────────────────────────────────────────────────────────────
OWP_VERSION="${OWP_VERSION:-main}"
OWP_REPO="${OWP_REPO:-openwebcpanel/openwebcpanel}"
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

# Generated passwords
OWP_MYSQL_ROOT_PW="$(tr -dc A-Za-z0-9 </dev/urandom | head -c 20)"
OWP_MYSQL_ADMIN_PW="$(tr -dc A-Za-z0-9 </dev/urandom | head -c 20)"

# ─── Colors & Helpers ────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
err()   { echo -e "${RED}[ERR]${NC}   $*"; }
header(){ echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"; echo -e "${BLUE}  $*${NC}"; echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"; }
section(){ echo -e "\n${YELLOW}▶ $*${NC}"; }

debug() {
  if [[ "$OWP_DEBUG" == "true" ]]; then
    echo -e "${YELLOW}[DEBUG]${NC} $*"
  fi
}

cleanup() {
  local ec=$?
  if [[ $ec -ne 0 ]]; then
    echo ""
    err "Installation FAILED (exit code $ec)"
    err "Check the log above for details."
    err ""
    err "You can re-run the installer after fixing any issues."
    err "If you need help, open an issue at:"
    err "  https://github.com/openwebcpanel/openwebcpanel/issues"
  fi
  exit $ec
}
trap cleanup EXIT

# ─── Stage 1: Prerequisites ─────────────────────────────────────────────
stage_prereqs() {
  header "Stage 1/10 — Prerequisites"

  if [[ $EUID -ne 0 ]]; then
    err "This installer must be run as root (or with sudo)"
    exit 1
  fi

  if [[ ! -f /etc/os-release ]]; then
    err "Cannot detect OS — /etc/os-release not found"
    exit 1
  fi

  . /etc/os-release
  if [[ "$ID" != "ubuntu" && "$ID" != "debian" ]]; then
    warn "Target: Ubuntu/Debian. Detected: $ID $VERSION_ID — proceeding anyway"
  fi
  info "OS: $NAME $VERSION_ID"

  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *)       err "Unsupported architecture: $ARCH"; exit 1 ;;
  esac
  info "Architecture: $ARCH"

  # Auto-detect domain / public IP
  if [[ -z "$OWP_DOMAIN" ]]; then
    OWP_DOMAIN=$(curl -4 -s --connect-timeout 5 ifconfig.me 2>/dev/null || \
                 hostname -I 2>/dev/null | awk '{print $1}' || \
                 echo "127.0.0.1")
  fi
  info "Server: $OWP_DOMAIN"

  # Check disk space
  AVAIL_KB=$(df / --output=avail 2>/dev/null | tail -1)
  if [[ -n "$AVAIL_KB" && "$AVAIL_KB" -lt 1048576 ]]; then
    err "Less than 1GB disk space available. At least 1GB is required."
    exit 1
  fi

  # Check memory and set up swap if needed
  local mem_kb
  mem_kb=$(grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}' || echo 0)
  local mem_mb=$((mem_kb / 1024))
  info "Memory: ${mem_mb}MB"

  if [[ "$mem_mb" -lt 1024 && "$OWP_SKIP_SWAP" != "true" ]]; then
    warn "Less than 1GB RAM detected. Creating 2GB swap file..."
    if [[ -f /swapfile ]]; then
      info "Swap file already exists at /swapfile"
    else
      dd if=/dev/zero of=/swapfile bs=1M count=2048 status=progress
      chmod 600 /swapfile
      mkswap /swapfile
      swapon /swapfile
      echo '/swapfile none swap sw 0 0' >> /etc/fstab
      ok "2GB swap created"
    fi
  fi

  # Set JWT secret if not provided
  if [[ -z "$OWP_JWT_SECRET" ]]; then
    OWP_JWT_SECRET="$(tr -dc A-Za-z0-9 </dev/urandom | head -c 48)"
  fi

  ok "Prerequisites passed"
}

# ─── Stage 2: System User ───────────────────────────────────────────────
stage_user() {
  header "Stage 2/10 — System User"

  if id "$OWP_USER" &>/dev/null; then
    info "User '$OWP_USER' already exists"
  else
    useradd -m -d "$OWP_HOME" -s /usr/sbin/nologin -U "$OWP_USER"
    info "Created system user: $OWP_USER"
  fi

  # Create directory structure
  mkdir -p "$OWP_HOME" "$OWP_DATA" "$OWP_HOMES_DIR" "$OWP_LOGS" "$OWP_BACKUPS"

  # Add www-data to panel user group for PHP-FPM/nginx access
  usermod -aG "$OWP_USER" www-data 2>/dev/null || true

  chown -R "$OWP_USER:$OWP_USER" "$OWP_HOME"
  chmod 755 "$OWP_HOME"

  ok "User and directory structure ready"
}

# ─── Stage 3: System Packages ────────────────────────────────────────────
stage_system_packages() {
  header "Stage 3/10 — System Packages"

  export DEBIAN_FRONTEND=noninteractive

  info "Updating package lists..."
  apt-get update -qq

  # Determine PHP version based on OS
  PHP_VER="8.3"
  if [[ "$ID" == "ubuntu" ]]; then
    case "$VERSION_ID" in
      20.04) PHP_VER="8.0" ;;
      22.04) PHP_VER="8.1" ;;
      *)     PHP_VER="8.3" ;;
    esac
  fi
  echo "$PHP_VER" > /tmp/owp_php_ver
  info "PHP version: $PHP_VER"

  info "Installing packages..."
  apt-get install -y -qq \
    curl wget git tar gpg build-essential \
    nginx \
    mariadb-server mariadb-client \
    "php${PHP_VER}-fpm" "php${PHP_VER}-mysqli" "php${PHP_VER}-curl" \
    "php${PHP_VER}-mbstring" "php${PHP_VER}-xml" "php${PHP_VER}-bcmath" \
    "php${PHP_VER}-gd" "php${PHP_VER}-zip" \
    unzip openssl sudo cron sqlite3 \
    rsync jq ufw 2>&1 | tail -3

  ok "System packages installed"
}

# ─── Stage 4: Go & Node.js ──────────────────────────────────────────────
stage_languages() {
  header "Stage 4/10 — Go & Node.js"

  # ─── Go ───
  if command -v go &>/dev/null; then
    GO_CUR=$(go version | grep -oP 'go\K[0-9.]+' | cut -d. -f1-2)
    info "Go $(go version | grep -oP 'go\S+') already installed"
    if (( $(echo "$GO_CUR < 1.22" | bc -l 2>/dev/null || echo 0) )); then
      warn "Go $GO_CUR is too old (need 1.22+), upgrading..."
      rm -f "$(which go)" 2>/dev/null || true
    fi
  fi

  if ! command -v go &>/dev/null; then
    info "Installing Go ${OWP_GO_VERSION}..."
    wget -q "https://go.dev/dl/go${OWP_GO_VERSION}.linux-${ARCH}.tar.gz" -O /tmp/go.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    rm /tmp/go.tar.gz
    ln -sf /usr/local/go/bin/go /usr/local/bin/go
    ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
    ok "Go ${OWP_GO_VERSION} installed"
  fi

  # ─── Node.js ───
  if command -v node &>/dev/null; then
    NODE_MAJOR=$(node -v | sed 's/v//' | cut -d. -f1)
    info "Node.js $(node -v) already installed"
    if [[ "$NODE_MAJOR" -lt 20 ]]; then
      warn "Node.js $(node -v) is too old (need 20+), upgrading..."
      rm -f "$(which node)" 2>/dev/null || true
    fi
  fi

  if ! command -v node &>/dev/null || [[ $(node -v | sed 's/v//' | cut -d. -f1) -lt 20 ]]; then
    info "Installing Node.js ${OWP_NODE_VERSION}.x..."
    curl -fsSL "https://deb.nodesource.com/setup_${OWP_NODE_VERSION}.x" | bash -
    apt-get install -y -qq nodejs
    ok "Node.js $(node -v) installed"
  fi
}

# ─── Stage 5: Project Files ─────────────────────────────────────────────
stage_project() {
  header "Stage 5/10 — Project Files"
  local src_dir
  local script_dir
  script_dir="$(cd "$(dirname "$0")" && pwd 2>/dev/null)"

  # Check if we're running from inside the project source
  if [[ -f "$script_dir/go.mod" ]] && grep -q "openwebcpanel" "$script_dir/go.mod" 2>/dev/null; then
    info "Copying project from local source: $script_dir"
    src_dir="$script_dir"
    mkdir -p "${OWP_HOME}/app"
    rsync -a --delete \
      --exclude='.git' --exclude='node_modules' --exclude='bin' \
      --exclude='homes' --exclude='*.db' --exclude='*.db-shm' --exclude='*.db-wal' \
      --exclude='__pycache__' --exclude='.env' \
      "$src_dir"/ "${OWP_HOME}/app/"
  elif [[ -d "${OWP_HOME}/app" && -f "${OWP_HOME}/app/go.mod" ]]; then
    info "Project already installed at ${OWP_HOME}/app"
    if command -v git &>/dev/null; then
      cd "${OWP_HOME}/app"
      git pull --ff-only origin "$OWP_VERSION" 2>/dev/null && ok "Updated to latest code" || warn "Could not update"
    fi
    return
  else
    if ! command -v git &>/dev/null; then
      err "git is required to download the project"
      exit 1
    fi
    local repo_url="https://github.com/${OWP_REPO}.git"
    info "Cloning ${OWP_REPO} (branch: ${OWP_VERSION})..."
    rm -rf "${OWP_HOME}/app"
    git clone --depth 1 --branch "$OWP_VERSION" "$repo_url" "${OWP_HOME}/app" 2>/dev/null || {
      warn "Branch '${OWP_VERSION}' not found, trying default branch..."
      git clone --depth 1 "$repo_url" "${OWP_HOME}/app"
    }
    src_dir="${OWP_HOME}/app"
  fi

  chown -R "$OWP_USER:$OWP_USER" "${OWP_HOME}/app" 2>/dev/null || true
  ok "Project files at ${OWP_HOME}/app"
}

# ─── Stage 6: Services Configuration ────────────────────────────────────
stage_services() {
  header "Stage 6/10 — Services Configuration"
  PHP_VER=$(cat /tmp/owp_php_ver 2>/dev/null || echo "8.3")

  # ─── nginx ──────────────────────────────────────────────────────
  section "Configuring nginx"
  mkdir -p /etc/nginx/sites-enabled /etc/nginx/vhosts
  rm -f /etc/nginx/sites-enabled/default

  cat > /etc/nginx/sites-available/openwebpanel <<NGINX_CONF
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    client_max_body_size 2048M;

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;

    location / {
        proxy_pass http://127.0.0.1:9000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_buffering off;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:9000;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    location /pma/ {
        proxy_pass http://127.0.0.1:8080/;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    }

    location ^~ /.well-known/acme-challenge/ {
        proxy_pass http://127.0.0.1:9000;
        proxy_set_header Host \$host;
    }
}

server {
    listen ${OWP_PANEL_PORT};
    listen [::]:${OWP_PANEL_PORT};
    server_name _;
    client_max_body_size 0;

    location / {
        proxy_pass http://127.0.0.1:9000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}

server {
    listen ${OWP_USER_PORT};
    listen [::]:${OWP_USER_PORT};
    server_name _;
    client_max_body_size 0;

    location / {
        proxy_pass http://127.0.0.1:9000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
NGINX_CONF

  ln -sf /etc/nginx/sites-available/openwebpanel /etc/nginx/sites-enabled/openwebpanel
  sed -i 's/^user .*/user www-data;/' /etc/nginx/nginx.conf
  mkdir -p /var/log/nginx

  # Test nginx config
  nginx -t 2>/dev/null || warn "nginx config test had issues (will fix later)"
  ok "nginx configured"

  # ─── MariaDB ────────────────────────────────────────────────────
  section "Configuring MariaDB"
  systemctl enable mariadb --now 2>/dev/null || service mariadb start 2>/dev/null || true

  for i in $(seq 1 15); do
    if mysql -e "SELECT 1" &>/dev/null; then break; fi
    sleep 1
  done

  # Secure MariaDB and create admin user
  mysql <<SQL
ALTER USER 'root'@'localhost' IDENTIFIED BY '${OWP_MYSQL_ROOT_PW}';
DELETE FROM mysql.user WHERE User='';
DELETE FROM mysql.user WHERE User='root' AND Host NOT IN ('localhost','127.0.0.1','::1');
DROP DATABASE IF EXISTS test;
DELETE FROM mysql.db WHERE Db='test' OR Db='test\\_%';
CREATE USER IF NOT EXISTS 'owp_admin'@'localhost' IDENTIFIED BY '${OWP_MYSQL_ADMIN_PW}';
GRANT ALL PRIVILEGES ON *.* TO 'owp_admin'@'localhost' WITH GRANT OPTION;
FLUSH PRIVILEGES;
SQL

  # Save root credentials
  cat > /root/.my.cnf <<EOF
[client]
user=root
password=${OWP_MYSQL_ROOT_PW}
EOF
  chmod 600 /root/.my.cnf

  # Restrict owp_admin privileges
  mysql -u root -p"${OWP_MYSQL_ROOT_PW}" -e "
    REVOKE SUPER, SHUTDOWN, CREATE USER, FILE, PROCESS, RELOAD,
      REPLICATION SLAVE, REPLICATION CLIENT
    ON *.* FROM 'owp_admin'@'localhost';
    FLUSH PRIVILEGES;
  " 2>/dev/null || true

  ok "MariaDB configured"

  # ─── PHP ────────────────────────────────────────────────────────
  section "Configuring PHP"

  PHP_INI="/etc/php/${PHP_VER}/fpm/php.ini"
  if [[ -f "$PHP_INI" ]]; then
    sed -i 's/^upload_max_filesize = .*/upload_max_filesize = 256M/' "$PHP_INI"
    sed -i 's/^post_max_size = .*/post_max_size = 256M/' "$PHP_INI"
    sed -i 's/^max_execution_time = .*/max_execution_time = 300/' "$PHP_INI"
    sed -i 's/^max_input_time = .*/max_input_time = 120/' "$PHP_INI"
    sed -i 's/^memory_limit = .*/memory_limit = 256M/' "$PHP_INI"
  fi

  POOL_CONF="/etc/php/${PHP_VER}/fpm/pool.d/www.conf"
  if [[ -f "$POOL_CONF" ]]; then
    sed -i "s|^listen = .*|listen = /run/php/php${PHP_VER}-fpm.sock|" "$POOL_CONF"
    sed -i 's/^;listen.owner = .*/listen.owner = www-data/' "$POOL_CONF"
    sed -i 's/^;listen.group = .*/listen.group = www-data/' "$POOL_CONF"
    sed -i 's/^;listen.mode = .*/listen.mode = 0660/' "$POOL_CONF"
    # Increase worker processes for hosting
    sed -i 's/^pm.max_children = .*/pm.max_children = 50/' "$POOL_CONF"
  fi

  systemctl enable "php${PHP_VER}-fpm" --now 2>/dev/null || true
  ok "PHP ${PHP_VER} configured"

  # ─── phpMyAdmin ────────────────────────────────────────────────
  section "Installing phpMyAdmin"
  PMA_DIR="/usr/share/phpmyadmin"
  if [[ -d "$PMA_DIR" && -f "$PMA_DIR/index.php" ]]; then
    info "phpMyAdmin already installed at $PMA_DIR"
  else
    mkdir -p "$PMA_DIR"
    PMA_URL="https://files.phpmyadmin.net/phpMyAdmin/${OWP_PHPMYADMIN_VER}/phpMyAdmin-${OWP_PHPMYADMIN_VER}-all-languages.zip"
    info "Downloading phpMyAdmin ${OWP_PHPMYADMIN_VER}..."
    wget -q "$PMA_URL" -O /tmp/pma.zip
    unzip -qo /tmp/pma.zip -d /tmp/pma/
    cp -r /tmp/pma/phpMyAdmin-${OWP_PHPMYADMIN_VER}-all-languages/* "$PMA_DIR/"
    rm -rf /tmp/pma /tmp/pma.zip
    ok "phpMyAdmin ${OWP_PHPMYADMIN_VER} installed"
  fi

  BLOWFISH_SECRET=$(openssl rand -base64 32 2>/dev/null || tr -dc A-Za-z0-9 </dev/urandom | head -c 32)
  if [[ ! -f "$PMA_DIR/config.inc.php" ]]; then
    cp "$PMA_DIR/config.sample.inc.php" "$PMA_DIR/config.inc.php" 2>/dev/null || true
  fi
  cat > "$PMA_DIR/config.inc.php" <<PMAEOF
<?php
declare(strict_types=1);
\$cfg['blowfish_secret'] = '${BLOWFISH_SECRET}';
\$i = 1;
\$cfg['Servers'][\$i]['auth_type'] = 'cookie';
\$cfg['Servers'][\$i]['host'] = 'localhost';
\$cfg['Servers'][\$i]['port'] = '3306';
\$cfg['Servers'][\$i]['compress'] = false;
\$cfg['Servers'][\$i]['AllowNoPassword'] = false;
\$cfg['Servers'][\$i]['hide_db'] = 'information_schema|performance_schema|mysql|sys';
\$cfg['UploadDir'] = '';
\$cfg['SaveDir'] = '';
\$cfg['ShowChgPassword'] = false;
\$cfg['ShowDbSpecificCreation'] = true;
PMAEOF
  chown -R www-data:www-data "$PMA_DIR" 2>/dev/null || true
  ok "phpMyAdmin configured"
}

# ─── Stage 7: Build ──────────────────────────────────────────────────────
stage_build() {
  header "Stage 7/10 — Building"
  export PATH="/usr/local/go/bin:$PATH"
  PHP_VER=$(cat /tmp/owp_php_ver 2>/dev/null || echo "8.3")

  cd "${OWP_HOME}/app"

  # ─── Environment file ───────────────────────────────────────────
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

  # ─── Backend ────────────────────────────────────────────────────
  section "Building backend (parentd)"
  cd "${OWP_HOME}/app"
  CGO_ENABLED=1 go build -o bin/parentd ./cmd/parentd/ 2>&1 | tail -5
  ok "Backend built (bin/parentd)"

  # ─── Frontend ───────────────────────────────────────────────────
  section "Building frontend"
  cd "${OWP_HOME}/app/web"
  if [[ ! -d node_modules ]]; then
    info "Installing npm dependencies..."
    npm ci --no-audit --no-fund 2>&1 | tail -5
  fi
  npm run build 2>&1 | tail -10
  ok "Frontend built"

  chown -R "$OWP_USER:$OWP_USER" "${OWP_HOME}/app"
}

# ─── Stage 8: Systemd Services ──────────────────────────────────────────
stage_systemd() {
  header "Stage 8/10 — Systemd Services"

  # ─── Main panel service ─────────────────────────────────────────
  cat > /etc/systemd/system/openwebpanel.service <<UNIT
[Unit]
Description=OpenWebPanel - Web Hosting Control Panel
Documentation=https://github.com/openwebcpanel/openwebcpanel
After=network.target mariadb.service nginx.service
Wants=mariadb.service
Requires=mariadb.service

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

  # ─── phpMyAdmin service ─────────────────────────────────────────
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
LimitNOFILE=4096

[Install]
WantedBy=multi-user.target
UNIT

  # ─── Monitoring watchdog service ────────────────────────────────
  cat > /etc/systemd/system/openwebpanel-watchdog.service <<UNIT
[Unit]
Description=OpenWebPanel Health Watchdog
After=openwebpanel.service

[Service]
Type=oneshot
ExecStart=${OWP_HOME}/app/bin/owp-watchdog.sh

[Install]
WantedBy=multi-user.target
UNIT

  # Reload systemd
  systemctl daemon-reload
  systemctl enable openwebpanel
  systemctl enable phpmyadmin

  ok "Systemd services installed"
}

# ─── Stage 9: Watchdog & Log Rotation ───────────────────────────────────
stage_monitoring() {
  header "Stage 9/10 — Monitoring & Maintenance"

  # ─── Watchdog script ────────────────────────────────────────────
  cat > "${OWP_HOME}/app/bin/owp-watchdog.sh" <<'WATCHDOG'
#!/usr/bin/env bash
# OpenWebPanel Watchdog — checks if the panel is healthy and restarts if needed
set -euo pipefail

OWP_HOME="${OWP_HOME:-/opt/openwebpanel}"
LOG="${OWP_HOME}/logs/watchdog.log"
HEALTH_URL="http://127.0.0.1:9000/healthz"
TIMEOUT=5
MAX_RETRIES=3

log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" >> "$LOG"
}

check_health() {
  for i in $(seq 1 "$MAX_RETRIES"); do
    if curl -sf --max-time "$TIMEOUT" "$HEALTH_URL" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

if ! check_health; then
  log "WARN: Panel not responding. Attempting restart..."
  systemctl restart openwebpanel 2>/dev/null || {
    log "ERR: systemctl restart failed, trying direct start..."
    if [[ -f "${OWP_HOME}/app/bin/parentd" ]]; then
      cd "${OWP_HOME}/app"
      nohup ./bin/parentd > "${OWP_HOME}/logs/parentd.log" 2>&1 &
    fi
  }
  sleep 5
  if check_health; then
    log "OK: Panel restarted successfully"
  else
    log "CRITICAL: Panel still not responding after restart"
  fi
else
  log "OK: Panel is healthy"
fi
WATCHDOG
  chmod +x "${OWP_HOME}/app/bin/owp-watchdog.sh"
  chown "$OWP_USER:$OWP_USER" "${OWP_HOME}/app/bin/owp-watchdog.sh"

  # ─── Cron job for watchdog (every 5 minutes) ───────────────────
  cat > /etc/cron.d/openwebpanel-watchdog <<CRON
*/5 * * * * root ${OWP_HOME}/app/bin/owp-watchdog.sh >/dev/null 2>&1
CRON
  chmod 644 /etc/cron.d/openwebpanel-watchdog

  # ─── Log rotation ───────────────────────────────────────────────
  cat > /etc/logrotate.d/openwebpanel <<LOGROTATE
${OWP_HOME}/logs/*.log {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
}

${OWP_HOME}/app/parentd.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
}
LOGROTATE

  # ─── Backup cron (daily at 3am) ─────────────────────────────────
  cat > /etc/cron.d/openwebpanel-backup <<CRON2
0 3 * * * root tar --exclude='node_modules' --exclude='homes/*/public_html/wp-content/cache' -czf ${OWP_BACKUPS}/backup-\$(date +\\%Y\\%m\\%d).tar.gz -C ${OWP_HOME}/app . 2>/dev/null && find ${OWP_BACKUPS} -name 'backup-*.tar.gz' -mtime +7 -delete
CRON2
  chmod 644 /etc/cron.d/openwebpanel-backup

  ok "Monitoring, log rotation, and backup cron configured"
}

# ─── Stage 10: Start & Verify ───────────────────────────────────────────
stage_start() {
  header "Stage 10/10 — Starting Services"

  # ─── Firewall ───────────────────────────────────────────────────
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

  # ─── Start services in order ────────────────────────────────────
  section "Starting MariaDB..."
  systemctl restart mariadb 2>/dev/null || service mariadb restart 2>/dev/null || true
  for i in $(seq 1 10); do
    if mysql -e "SELECT 1" &>/dev/null; then break; fi
    sleep 1
  done

  section "Starting PHP-FPM..."
  systemctl restart "php${PHP_VER}-fpm" 2>/dev/null || true

  section "Starting phpMyAdmin..."
  systemctl restart phpmyadmin 2>/dev/null || true

  section "Starting nginx..."
  nginx -t 2>/dev/null
  systemctl restart nginx 2>/dev/null || true

  section "Starting OpenWebPanel..."
  systemctl restart openwebpanel 2>/dev/null || true

  # Wait for panel to be ready
  echo ""
  info "Waiting for OpenWebPanel to start..."
  local started=false
  for i in $(seq 1 30); do
    if curl -sf -o /dev/null http://127.0.0.1:9000/healthz 2>/dev/null; then
      started=true
      break
    fi
    sleep 1
  done

  if [[ "$started" == "true" ]]; then
    ok "OpenWebPanel API is responding on port 9000"
  else
    warn "Panel did not respond within 30 seconds. Checking logs..."
    journalctl -u openwebpanel -n 20 --no-pager 2>/dev/null || true
    warn "Attempting direct start as fallback..."
    sudo -u "$OWP_USER" OWP_DB_PATH="${OWP_DATA}/openwebpanel.db" \
      OWP_STATIC_DIR="${OWP_HOME}/app/web/dist" \
      OWP_LISTEN=":9000" \
      "${OWP_HOME}/app/bin/parentd" &>/tmp/parentd-fallback.log &
    sleep 5
    if curl -sf -o /dev/null http://127.0.0.1:9000/healthz 2>/dev/null; then
      ok "OpenWebPanel started in fallback mode"
    else
      err "Failed to start OpenWebPanel. Check: /tmp/parentd-fallback.log"
      cat /tmp/parentd-fallback.log 2>/dev/null || true
    fi
  fi

  # Verify nginx is serving
  if curl -sf -o /dev/null http://127.0.0.1:80/healthz 2>/dev/null; then
    ok "Nginx is serving on port 80"
  fi

  # Verify systemd services
  for svc in openwebpanel phpmyadmin; do
    if systemctl is-active --quiet "$svc" 2>/dev/null; then
      ok "Service '$svc' is running"
    else
      warn "Service '$svc' is not active"
    fi
  done
}

# ─── Summary ─────────────────────────────────────────────────────────────
print_summary() {
  header "Installation Complete"

  local ip="$OWP_DOMAIN"

  echo ""
  echo -e "  ${GREEN}OpenWebPanel is installed and running!${NC}"
  echo ""
  echo -e "  ${CYAN}━━ Panel URLs ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "  Admin Panel:   ${BLUE}http://${ip}:${OWP_PANEL_PORT}${NC}"
  echo -e "  User Panel:    ${BLUE}http://${ip}:${OWP_USER_PORT}${NC}"
  echo -e "  Main Site:     ${BLUE}http://${ip}${NC}"
  echo ""
  echo -e "  ${CYAN}━━ Default Credentials ━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "  Admin login:   ${YELLOW}admin / admin123${NC}"
  echo -e "  ${RED}⚠  CHANGE THE ADMIN PASSWORD AFTER FIRST LOGIN${NC}"
  echo ""
  echo -e "  ${CYAN}━━ Paths ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "  Project:       ${YELLOW}${OWP_HOME}/app${NC}"
  echo -e "  Data:          ${YELLOW}${OWP_DATA}${NC}"
  echo -e "  User homes:    ${YELLOW}${OWP_HOMES_DIR}${NC}"
  echo -e "  Logs:          ${YELLOW}${OWP_LOGS}${NC}"
  echo -e "  Backups:       ${YELLOW}${OWP_BACKUPS}${NC}"
  echo -e "  Config:        ${YELLOW}${OWP_HOME}/app/.env${NC}"
  echo ""
  echo -e "  ${CYAN}━━ Database ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "  MariaDB root:  ${YELLOW}saved in /root/.my.cnf${NC}"
  echo -e "  Admin user:    ${YELLOW}owp_admin${NC} (restricted privileges)"
  echo ""
  echo -e "  ${CYAN}━━ Management Commands ━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "  Status:        ${YELLOW}systemctl status openwebpanel${NC}"
  echo -e "  Logs:          ${YELLOW}journalctl -u openwebpanel -f${NC}"
  echo -e "  Restart:       ${YELLOW}systemctl restart openwebpanel${NC}"
  echo -e "  Watchdog log:  ${YELLOW}tail -f ${OWP_LOGS}/watchdog.log${NC}"
  echo ""
  echo -e "  ${CYAN}━━ Next Steps ───────────────────────────────${NC}"
  echo -e "  1. Point a domain to ${ip} and run:"
  echo -e "     ${BLUE}certbot --nginx -d your-domain.com${NC}"
  echo -e "  2. Login at ${BLUE}http://${ip}:${OWP_PANEL_PORT}${NC} with ${YELLOW}admin / admin123${NC}"
  echo -e "  3. Change the admin password immediately"
  echo -e "  4. Create hosting packages and accounts"
  echo -e "  5. Configure your domain in Settings"
  echo ""
}

# ─── Main ────────────────────────────────────────────────────────────────
main() {
  echo ""
  echo -e "${BLUE}╔══════════════════════════════════════════════════╗${NC}"
  echo -e "${BLUE}║        OpenWebPanel Installer v2.0              ║${NC}"
  echo -e "${BLUE}║        Web Hosting Control Panel                ║${NC}"
  echo -e "${BLUE}╚══════════════════════════════════════════════════╝${NC}"
  echo ""

  START_TIME=$(date +%s)

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

  END_TIME=$(date +%s)
  DURATION=$((END_TIME - START_TIME))
  MINUTES=$((DURATION / 60))
  SECONDS=$((DURATION % 60))

  echo ""
  ok "Installation completed in ${MINUTES}m ${SECONDS}s"

  print_summary
}

main "$@"
