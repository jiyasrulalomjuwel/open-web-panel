#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════════
# OpenWebPanel Installer — Installation Stages
# ═══════════════════════════════════════════════════════════════════════════════

# ═══════════════════════════════════════════════════════════════════════════════
# STAGE 1: Checking System Requirements
# ═══════════════════════════════════════════════════════════════════════════════
stage_requirements() {
  set_status "Verifying root privileges..." "info"
  if [[ $EUID -ne 0 ]]; then
    _fatal_error "This installer must be run as root (use sudo)"
  fi

  set_status "Detecting operating system..." "info"
  OS_ID="unknown"; OS_NAME="Unknown"; OS_VERSION="0"; OS_CODENAME=""
  if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    OS_ID="${ID,,}"
    OS_NAME="$NAME"
    OS_VERSION="$VERSION_ID"
    OS_CODENAME="$VERSION_CODENAME"
  fi

  case "$OS_ID" in
    ubuntu)
      case "$OS_VERSION" in
        20.04|22.04|24.04|24.10|25.04) : ;;
        *) log_warn "Ubuntu $OS_VERSION not officially supported, proceeding anyway" ;;
      esac
      ;;
    debian)
      case "$OS_VERSION" in
        11|12|13) : ;;
        *) log_warn "Debian $OS_VERSION not officially supported, proceeding anyway" ;;
      esac
      ;;
    *)
      _fatal_error "Unsupported OS: $OS_NAME $OS_VERSION. Ubuntu 20.04+ or Debian 11+ required."
      ;;
  esac
  log_info "OS: $OS_NAME $OS_VERSION ($OS_CODENAME)"

  set_status "Checking architecture..." "info"
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *)
      _fatal_error "Unsupported architecture: $ARCH (requires x86_64 or aarch64)"
      ;;
  esac
  log_info "Architecture: $ARCH"

  set_status "Checking memory..." "info"
  local mem_kb
  mem_kb=$(grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}' || echo 0)
  MEM_MB=$((mem_kb / 1024))
  log_info "Memory: ${MEM_MB}MB"
  if [[ $MEM_MB -lt 512 ]]; then
    _fatal_error "At least 512MB RAM required (${MEM_MB}MB detected)"
  fi

  set_status "Checking disk space..." "info"
  local avail_kb
  avail_kb=$(df / --output=avail 2>/dev/null | tail -1)
  DISK_MB=$((avail_kb / 1024))
  log_info "Disk available: ${DISK_MB}MB"
  if [[ -n "$avail_kb" && "$avail_kb" -lt 2097152 ]]; then
    _fatal_error "At least 2GB free disk space required (${DISK_MB}MB detected)"
  fi

  set_status "Checking required commands..." "info"
  local required_cmds=("curl" "tar" "wget" "grep" "sed" "awk" "useradd" "chown" "gpg")
  local missing=()
  for cmd in "${required_cmds[@]}"; do
    cmd_exists "$cmd" || missing+=("$cmd")
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    _fatal_error "Missing required commands: ${missing[*]}"
  fi

  set_status "Checking system shell for nologin..." "info"
  local shell
  shell=$(find_valid_shell)
  log_info "Using shell for system users: $shell"

  set_status "Checking network connectivity..." "info"
  if ! curl -sf --connect-timeout 10 https://github.com >/dev/null 2>&1; then
    log_warn "Cannot reach GitHub — some downloads may fail"
  fi

  # ── Port Availability Check ──
  set_status "Checking required ports..." "info"
  local required_ports=("80" "${OWP_PANEL_PORT}" "${OWP_USER_PORT}" "9000" "8080" "${OWP_SMTP_PORT}" "3306")
  local conflict_ports=()
  for port in "${required_ports[@]}"; do
    if ss -tlnp "sport = :${port}" 2>/dev/null | grep -q LISTEN; then
      local proc_name
      proc_name=$(ss -tlnp "sport = :${port}" 2>/dev/null | grep -oP 'users:\(\K[^)]+' | head -1 || echo "unknown")
      log_info "Port $port is already in use by $proc_name"
      conflict_ports+=("$port($proc_name)")
    fi
  done
  if [[ ${#conflict_ports[@]} -gt 0 ]]; then
    log_warn "Ports already in use: ${conflict_ports[*]}"
  fi

  # ── UFW Firewall Check & Setup ──
  set_status "Checking firewall status..." "info"
  if cmd_exists ufw; then
    if ufw status 2>/dev/null | grep -qi "active"; then
      log_info "UFW firewall is active"
      for port in 80 443 "${OWP_PANEL_PORT}" "${OWP_USER_PORT}"; do
        if ! ufw status 2>/dev/null | grep -qP "^\s*${port}/tcp"; then
          log_warn "Port $port not open in UFW — will configure later"
        fi
      done
    else
      log_info "UFW is installed but not active — will configure later"
    fi
  else
    log_warn "UFW not installed — will install and configure during security stage"
  fi

  # Determine PHP version based on OS
  if [[ "$OS_ID" == "ubuntu" ]]; then
    case "$OS_VERSION" in
      20.04) PHP_VER="8.0" ;;
      22.04) PHP_VER="8.1" ;;
      24.04) PHP_VER="8.3" ;;
      24.10) PHP_VER="8.3" ;;
      25.04) PHP_VER="8.4" ;;
      *)     PHP_VER="8.3" ;;
    esac
  elif [[ "$OS_ID" == "debian" ]]; then
    case "$OS_VERSION" in
      11) PHP_VER="8.0" ;;
      12) PHP_VER="8.2" ;;
      13) PHP_VER="8.3" ;;
      *)  PHP_VER="8.3" ;;
    esac
  fi

  set_status "System requirements verified" "success"
}

# ═══════════════════════════════════════════════════════════════════════════════
# STAGE 2: Preparing Installation
# ═══════════════════════════════════════════════════════════════════════════════
stage_prepare() {
  set_status "Generating secure passwords..." "info"
  if [[ -z "$OWP_JWT_SECRET" ]]; then
    OWP_JWT_SECRET=$(gen_password 48)
    log_info "JWT secret generated"
  fi
  if [[ -z "$OWP_ADMIN_PASSWORD" ]]; then
    OWP_ADMIN_PASSWORD=$(gen_password 16)
    log_info "Admin password generated"
  fi
  if [[ -z "$OWP_MYSQL_ROOT_PASSWORD" ]]; then
    OWP_MYSQL_ROOT_PASSWORD=$(gen_password 24)
    log_info "MySQL root password generated"
  fi
  if [[ -z "$OWP_MYSQL_ADMIN_PASSWORD" ]]; then
    OWP_MYSQL_ADMIN_PASSWORD=$(gen_password 24)
    log_info "MySQL admin password generated"
  fi
  if [[ -z "$OWP_PMA_BLOWFISH_SECRET" ]]; then
    OWP_PMA_BLOWFISH_SECRET=$(gen_password 32)
    log_info "phpMyAdmin blowfish secret generated"
  fi

  set_status "Auto-detecting server IP..." "info"
  if [[ -z "$OWP_DOMAIN" ]]; then
    OWP_DOMAIN=$(curl -4 -s --connect-timeout 5 ifconfig.me 2>/dev/null || \
                 hostname -I 2>/dev/null | awk '{print $1}' || \
                 echo "127.0.0.1")
  fi
  log_info "Server domain/IP: $OWP_DOMAIN"

  # Find a valid shell before creating user
  local user_shell
  user_shell=$(find_valid_shell)
  log_info "Using shell for system users: $user_shell"

  set_status "Creating system user '${OWP_USER}'..." "info"
  if id "$OWP_USER" &>/dev/null; then
    log_info "User '$OWP_USER' already exists"
  else
    # Create parent directory first if needed
    local parent_dir
    parent_dir=$(dirname "$OWP_HOME" 2>/dev/null || echo "/opt")
    mkdir -p "$parent_dir" 2>/dev/null || true

    local useradd_output rc
    if getent group "$OWP_USER" &>/dev/null; then
      useradd_output=$(useradd -m -d "$OWP_HOME" -s "$user_shell" -g "$OWP_USER" "$OWP_USER" 2>&1); rc=$?
    else
      useradd_output=$(useradd -m -d "$OWP_HOME" -s "$user_shell" -U "$OWP_USER" 2>&1); rc=$?
    fi
    if [[ $rc -ne 0 ]]; then
      log_error "useradd failed: $useradd_output"
      _fatal_error "Failed to create user '$OWP_USER' (shell=$user_shell). $useradd_output"
    fi
    log_info "Created system user: $OWP_USER"
  fi
  register_rollback "userdel -f '$OWP_USER' 2>/dev/null; rm -rf '$OWP_HOME' 2>/dev/null" "Remove system user and files"

  set_status "Creating directory structure..." "info"
  mkdir -p "$OWP_HOME" "$OWP_DATA_DIR" "$OWP_HOMES_DIR" "$OWP_LOGS_DIR" \
           "$OWP_BACKUPS_DIR" "$OWP_SSL_DIR" "$OWP_TMP_DIR"
  # Validate critical paths
  local critical_dirs=("$OWP_HOME" "$OWP_DATA_DIR" "$OWP_LOGS_DIR")
  for d in "${critical_dirs[@]}"; do
    if [[ ! -d "$d" ]]; then
      _fatal_error "Failed to create directory: $d"
    fi
  done
  usermod -aG "$OWP_USER" www-data 2>/dev/null || true
  chown -R "$OWP_USER:$OWP_USER" "$OWP_HOME" 2>/dev/null || log_warn "Could not chown $OWP_HOME"
  chmod 755 "$OWP_HOME" 2>/dev/null || true
  log_info "Directory structure created at $OWP_HOME"

  # Swap if needed
  if [[ $MEM_MB -lt 1024 && "$OWP_SKIP_SWAP" != "true" ]]; then
    set_status "Low memory detected — creating 2GB swap..." "warn"
    if [[ ! -f /swapfile ]]; then
      dd if=/dev/zero of=/swapfile bs=1M count=2048 status=none 2>/dev/null || {
        log_warn "Could not create swap file, skipping"
      }
      if [[ -f /swapfile && -s /swapfile ]]; then
        chmod 600 /swapfile
        mkswap /swapfile >/dev/null 2>&1
        swapon /swapfile
        echo '/swapfile none swap sw 0 0' >> /etc/fstab
        register_rollback "swapoff /swapfile 2>/dev/null; rm -f /swapfile 2>/dev/null" "Remove swap file"
        log_info "2GB swap created"
      fi
    fi
  fi

  set_status "Installation prepared" "success"
}

# ═══════════════════════════════════════════════════════════════════════════════
# STAGE 3: Downloading Required Packages
# ═══════════════════════════════════════════════════════════════════════════════
stage_download() {
  # ── Update APT ──
  set_status "Updating package lists..." "info"
  wait_for_dpkg
  run_cmd "Updating APT cache" "DEBIAN_FRONTEND=noninteractive apt-get update -qq" "true"

  # ── Go ──
  if cmd_exists go; then
    local go_ver
    go_ver=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+' 2>/dev/null || echo "0")
    if awk "BEGIN {exit !($go_ver < 1.22)}" 2>/dev/null; then
      log_warn "Go $go_ver is too old, upgrading..."
      rm -f "$(which go)" 2>/dev/null || true
    else
      log_info "Go $(go version | grep -oP 'go\S+') already installed"
      OWP_SKIP_GO=true
    fi
  fi

  if [[ "${OWP_SKIP_GO:-false}" != "true" ]]; then
    set_status "Downloading Go ${OWP_GO_VERSION}..." "info"
    run_retry "Download Go ${OWP_GO_VERSION}" \
      "wget -q 'https://go.dev/dl/go${OWP_GO_VERSION}.linux-${ARCH}.tar.gz' -O /tmp/go.tar.gz" \
      3 5 || _fatal_error "Failed to download Go ${OWP_GO_VERSION}"
  fi

  # ── Node.js ──
  if cmd_exists node; then
    local node_major
    node_major=$(node -v | sed 's/v//' | cut -d. -f1)
    if [[ "$node_major" -ge 20 ]]; then
      log_info "Node.js $(node -v) already installed"
      OWP_SKIP_NODE=true
    fi
  fi

  if [[ "${OWP_SKIP_NODE:-false}" != "true" ]]; then
    set_status "Downloading Node.js ${OWP_NODE_MAJOR}.x..." "info"
    run_retry "Download NodeSource setup script" \
      "curl -fsSL 'https://deb.nodesource.com/setup_${OWP_NODE_MAJOR}.x' -o /tmp/nodesource.sh" \
      3 5 || _fatal_error "Failed to download Node.js setup script"
  fi

  # ── phpMyAdmin ──
  set_status "Downloading phpMyAdmin ${OWP_PHPMYADMIN_VER}..." "info"
  if ! run_retry "Download phpMyAdmin ${OWP_PHPMYADMIN_VER}" \
    "wget -q 'https://files.phpmyadmin.net/phpMyAdmin/${OWP_PHPMYADMIN_VER}/phpMyAdmin-${OWP_PHPMYADMIN_VER}-all-languages.zip' -O /tmp/pma.zip" \
    3 5; then
    log_warn "phpMyAdmin download failed after retries, will skip phpMyAdmin installation"
    OWP_SKIP_PMA=true
  fi

  set_status "All packages downloaded" "success"
}

# ═══════════════════════════════════════════════════════════════════════════════
# STAGE 4: Installing Dependencies
# ═══════════════════════════════════════════════════════════════════════════════
stage_dependencies() {
  wait_for_dpkg

  set_status "Installing system packages (nginx, MariaDB, PHP)..." "info"
  run_cmd "Install system packages" \
    "DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
      curl wget git tar gpg build-essential \
      nginx \
      mariadb-server mariadb-client \
      'php${PHP_VER}-fpm' 'php${PHP_VER}-mysqli' 'php${PHP_VER}-curl' \
      'php${PHP_VER}-mbstring' 'php${PHP_VER}-xml' 'php${PHP_VER}-bcmath' \
      'php${PHP_VER}-gd' 'php${PHP_VER}-zip' \
      unzip openssl sudo cron sqlite3 \
      psmisc rsync jq ufw 2>&1"

  register_rollback "DEBIAN_FRONTEND=noninteractive apt-get remove -y nginx mariadb-server php*-fpm 2>/dev/null || true" "Remove system packages"

  # ── Go ──
  if [[ "${OWP_SKIP_GO:-false}" != "true" ]]; then
    set_status "Installing Go ${OWP_GO_VERSION}..." "info"
    run_cmd "Install Go" "
      rm -rf /usr/local/go &&
      tar -C /usr/local -xzf /tmp/go.tar.gz &&
      rm -f /tmp/go.tar.gz &&
      ln -sf /usr/local/go/bin/go /usr/local/bin/go &&
      ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
    "
    register_rollback "rm -rf /usr/local/go /usr/local/bin/go /usr/local/bin/gofmt 2>/dev/null || true" "Remove Go installation"
  fi

  # ── Node.js ──
  if [[ "${OWP_SKIP_NODE:-false}" != "true" ]]; then
    set_status "Installing Node.js ${OWP_NODE_MAJOR}.x..." "info"
    run_cmd "Add NodeSource repository" "bash -c 'set +eu; source /tmp/nodesource.sh' 2>&1 | tail -5"
    wait_for_dpkg
    run_cmd "Install Node.js" "DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nodejs"
    rm -f /tmp/nodesource.sh
  fi

  set_status "Dependencies installed" "success"
}

# ═══════════════════════════════════════════════════════════════════════════════
# STAGE 5: Configuring Services
# ═══════════════════════════════════════════════════════════════════════════════
stage_configure() {
  # ── Nginx ──
  set_status "Configuring Nginx..." "info"
  mkdir -p /etc/nginx/sites-enabled /etc/nginx/vhosts
  chown -R "$OWP_USER:www-data" /etc/nginx/vhosts 2>/dev/null || true
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
        proxy_pass http://127.0.0.1:9001;
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

  if ! grep -q 'include /etc/nginx/vhosts/\*\.conf;' /etc/nginx/nginx.conf; then
    sed -i '/include \/etc\/nginx\/sites-enabled\/\*;/a\\tinclude /etc/nginx/vhosts/*.conf;' /etc/nginx/nginx.conf
  fi

  run_cmd "Validate nginx configuration" "nginx -t 2>&1"
  register_rollback "rm -f /etc/nginx/sites-enabled/openwebpanel; rm -f /etc/nginx/sites-available/openwebpanel; systemctl restart nginx 2>/dev/null || true" "Restore nginx configuration"

  # ── PHP ──
  set_status "Configuring PHP ${PHP_VER}..." "info"
  local php_ini="/etc/php/${PHP_VER}/fpm/php.ini"
  if [[ -f "$php_ini" ]]; then
    sed -i 's/^upload_max_filesize = .*/upload_max_filesize = 256M/' "$php_ini"
    sed -i 's/^post_max_size = .*/post_max_size = 256M/' "$php_ini"
    sed -i 's/^max_execution_time = .*/max_execution_time = 300/' "$php_ini"
    sed -i 's/^memory_limit = .*/memory_limit = 256M/' "$php_ini"
  fi

  # ── phpMyAdmin ──
  if [[ "${OWP_SKIP_PMA:-false}" != "true" && -f /tmp/pma.zip ]]; then
    set_status "Installing phpMyAdmin ${OWP_PHPMYADMIN_VER}..." "info"
    local pma_dir="/usr/share/phpmyadmin"
    mkdir -p "$pma_dir" /tmp/pma
    rm -rf /tmp/pma 2>/dev/null || true
    mkdir -p /tmp/pma
    unzip -qo /tmp/pma.zip -d /tmp/pma/ 2>/dev/null || {
      log_warn "Failed to unzip phpMyAdmin, skipping"
      OWP_SKIP_PMA=true
      return 0
    }
    if [[ -d "/tmp/pma/phpMyAdmin-${OWP_PHPMYADMIN_VER}-all-languages" ]]; then
      cp -r "/tmp/pma/phpMyAdmin-${OWP_PHPMYADMIN_VER}-all-languages/"* "$pma_dir/"
      rm -rf /tmp/pma /tmp/pma.zip

      cat > "$pma_dir/config.inc.php" <<PMACFG
<?php
\$cfg['blowfish_secret'] = '${OWP_PMA_BLOWFISH_SECRET}';
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
      chown -R www-data:www-data "$pma_dir" 2>/dev/null || true
      register_rollback "rm -rf $pma_dir 2>/dev/null || true" "Remove phpMyAdmin"
    else
      log_warn "phpMyAdmin extract not found, skipping"
      OWP_SKIP_PMA=true
    fi
  fi

  set_status "Services configured" "success"
}

# ═══════════════════════════════════════════════════════════════════════════════
# STAGE 6: Setting Up the Database
# ═══════════════════════════════════════════════════════════════════════════════
stage_database() {
  set_status "Starting MariaDB..." "info"
  systemctl enable mariadb --now 2>/dev/null || service mariadb start 2>/dev/null || true

  local i
  for i in $(seq 1 15); do
    if mysqladmin ping --silent 2>/dev/null; then break; fi
    sleep 1
  done

  set_status "Securing MariaDB installation..." "info"
  mysqladmin ping --silent 2>/dev/null || _fatal_error "MariaDB failed to start"

  local MYSQL_AUTH="socket"
  mysql -e "SELECT 1" 2>/dev/null && MYSQL_AUTH="native"

  if [[ "$MYSQL_AUTH" == "socket" ]]; then
    run_cmd "Set MariaDB root password (auth_socket)" \
      "mysql -e \"ALTER USER 'root'@'localhost' IDENTIFIED BY '${OWP_MYSQL_ROOT_PASSWORD}';\" 2>&1" \
      "true"
    if [[ $? -ne 0 ]]; then
      run_cmd "Set MariaDB root password via socket" \
        "mysql --socket=/run/mysqld/mysqld.sock -e \"ALTER USER 'root'@'localhost' IDENTIFIED BY '${OWP_MYSQL_ROOT_PASSWORD}';\" 2>&1" \
        "true"
    fi
    register_rollback "mysql -e \"DROP USER IF EXISTS 'owp_admin'@'localhost';\" 2>/dev/null || true" "Remove OWP admin user"
  else
    run_cmd "Configure MariaDB security" \
      "mysql -e \"
        ALTER USER 'root'@'localhost' IDENTIFIED BY '${OWP_MYSQL_ROOT_PASSWORD}';
        DELETE FROM mysql.user WHERE User='';
        DELETE FROM mysql.user WHERE User='root' AND Host NOT IN ('localhost','127.0.0.1','::1');
        DROP DATABASE IF EXISTS test;
        DELETE FROM mysql.db WHERE Db='test' OR Db='test\\\\_%';
        CREATE USER IF NOT EXISTS 'owp_admin'@'localhost' IDENTIFIED BY '${OWP_MYSQL_ADMIN_PASSWORD}';
        GRANT ALL PRIVILEGES ON *.* TO 'owp_admin'@'localhost' WITH GRANT OPTION;
        FLUSH PRIVILEGES;
      \" 2>&1" "true"
  fi

  cat > /root/.my.cnf <<EOF
[client]
user=root
password=${OWP_MYSQL_ROOT_PASSWORD}
EOF
  chmod 600 /root/.my.cnf 2>/dev/null || true
  log_info "MariaDB root credentials saved to /root/.my.cnf"

  # Update debian.cnf so that Debian maintenance scripts work (mysqlcheck, etc.)
  if [[ -f /etc/mysql/debian.cnf ]]; then
    cp /etc/mysql/debian.cnf /etc/mysql/debian.cnf.bak 2>/dev/null || true
    cat > /etc/mysql/debian.cnf <<DEBIANCNF
# Automatically generated by OpenWebPanel installer
# See /etc/mysql/debian.cnf.bak for the original
[client]
host     = localhost
user     = root
password = ${OWP_MYSQL_ROOT_PASSWORD}
socket   = /var/run/mysqld/mysqld.sock
[mysql_upgrade]
host     = localhost
user     = root
password = ${OWP_MYSQL_ROOT_PASSWORD}
socket   = /var/run/mysqld/mysqld.sock
[mysqldump]
user     = root
password = ${OWP_MYSQL_ROOT_PASSWORD}
socket   = /var/run/mysqld/mysqld.sock
DEBIANCNF
    chmod 640 /etc/mysql/debian.cnf 2>/dev/null || true
    log_info "Updated /etc/mysql/debian.cnf with root credentials"
  fi

  set_status "Database configured" "success"
}

# ═══════════════════════════════════════════════════════════════════════════════
# STAGE 7: Building the Web Server
# ═══════════════════════════════════════════════════════════════════════════════
stage_webserver() {
  export PATH="/usr/local/go/bin:${PATH}"

  # Verify the app directory exists
  if [[ ! -d "$OWP_APP_DIR" ]]; then
    _fatal_error "Application directory not found: $OWP_APP_DIR"
  fi

  set_status "Writing environment configuration..." "info"
  cat > "${OWP_APP_DIR}/.env" <<EOF
OWP_JWT_SECRET=${OWP_JWT_SECRET}
OWP_DB_PATH=${OWP_DATA_DIR}/openwebpanel.db
OWP_ADMIN_STATIC_DIR=${OWP_APP_DIR}/web/dist/admin
OWP_CHILD_STATIC_DIR=${OWP_APP_DIR}/web/dist/child
OWP_ADMIN_LISTEN=:9000
OWP_CHILD_LISTEN=:9001
OWP_SHARED_IP=127.0.0.1
OWP_PUBLIC_HOST=${OWP_DOMAIN}:9000
OWP_HOMES_BASE=${OWP_HOMES_DIR}/
OWP_PHPMYADMIN_PORT=http://127.0.0.1:8080
NGINX_PREFIX=${NGINX_PREFIX}
NGINX_VHOST_DIR=${NGINX_VHOST_DIR}
NGINX_BIN=${NGINX_BIN}
NGINX_CONF=${NGINX_CONF}
NGINX_LOG_DIR=${NGINX_LOG_DIR}
PHP_FPM_SOCKET=/run/php/php${PHP_VER}-fpm.sock
MYSQL_ROOT_PASSWORD=${OWP_MYSQL_ROOT_PASSWORD}
MYSQL_ADMIN_PASSWORD=${OWP_MYSQL_ADMIN_PASSWORD}
OWP_SMTP_PORT=${OWP_SMTP_PORT}
OWP_ADMIN_PASSWORD=${OWP_ADMIN_PASSWORD}
EOF
  chmod 600 "${OWP_APP_DIR}/.env" 2>/dev/null || true
  chown "$OWP_USER:$OWP_USER" "${OWP_APP_DIR}/.env" 2>/dev/null || true
  log_info "Environment file written"

  # ── Build Backend ──
  set_status "Building Go backend..." "info"
  cd "${OWP_APP_DIR}" || _fatal_error "Cannot cd to ${OWP_APP_DIR}"

  # Pre-download Go modules so transient network errors are caught with retry
  set_status "Downloading Go module dependencies..." "info"
  run_retry "Download Go modules" "go mod download 2>&1" 3 5

  # CGO is REQUIRED by github.com/mattn/go-sqlite3 — install gcc if missing
  if ! command -v gcc &>/dev/null && ! command -v cc &>/dev/null; then
    log_warn "C compiler (gcc/cc) not found — installing build-essential..."
    wait_for_dpkg
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq build-essential 2>&1 || \
      _fatal_error "C compiler required by go-sqlite3 (CGO). Install gcc or build-essential."
  fi
  run_cmd "Build parentd binary" "CGO_ENABLED=1 go build -buildvcs=false -o bin/parentd ./cmd/parentd/ 2>&1"

  # ── Build Frontend ──
  set_status "Installing npm dependencies..." "info"
  cd "${OWP_APP_DIR}/web" || _fatal_error "Cannot cd to ${OWP_APP_DIR}/web"
  if [[ ! -d node_modules ]]; then
    run_cmd "Install npm packages" "npm ci --no-audit --no-fund 2>&1" "true"
    local npm_rc=$?
    if [[ $npm_rc -ne 0 ]]; then
      run_cmd "Install npm packages (fallback)" "npm install 2>&1"
    fi
  fi

  set_status "Building React frontend..." "info"
  run_cmd "Build admin frontend" "npm run build:admin 2>&1 | tail -20"
  run_cmd "Build child frontend" "npm run build:child 2>&1 | tail -20"

  chown -R "$OWP_USER:$OWP_USER" "${OWP_APP_DIR}" 2>/dev/null || true

  set_status "Web server built and configured" "success"
}

# ═══════════════════════════════════════════════════════════════════════════════
# STAGE 8: Applying Security Settings
# ═══════════════════════════════════════════════════════════════════════════════
stage_security() {
  # ── Firewall ──
  if [[ "$OWP_SKIP_FIREWALL" != "true" ]]; then
    set_status "Configuring firewall..." "info"
    # Install UFW if not present
    if ! cmd_exists ufw; then
      log_info "UFW not found — installing..."
      wait_for_dpkg
      run_cmd "Install UFW" "DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ufw 2>&1" "true" || {
        log_warn "Failed to install UFW — skipping firewall configuration"
        OWP_SKIP_FIREWALL=true
      }
    fi
    if [[ "$OWP_SKIP_FIREWALL" != "true" ]]; then
      run_cmd "Reset UFW to defaults" "ufw --force reset 2>&1" "true"
      ufw default deny incoming 2>/dev/null || true
      ufw default allow outgoing 2>/dev/null || true
      ufw allow ssh 2>/dev/null || true
      ufw allow 80/tcp 2>/dev/null || true
      ufw allow 443/tcp 2>/dev/null || true
      ufw allow "${OWP_PANEL_PORT}/tcp" 2>/dev/null || true
      ufw allow "${OWP_USER_PORT}/tcp" 2>/dev/null || true
      run_cmd "Enable UFW" "ufw --force enable 2>&1" "true"
      register_rollback "ufw --force disable 2>/dev/null || true" "Disable UFW"
      log_info "Firewall configured"
    fi
  else
    log_info "Firewall configuration skipped (OWP_SKIP_FIREWALL=true)"
  fi

  # ── Sudoers ──
  set_status "Setting up sudo permissions..." "info"
  cat > /etc/sudoers.d/openwebpanel <<SUDOERS
# Allow OpenWebPanel to reload nginx when vhost configs change
${OWP_USER} ALL=(root) NOPASSWD: /usr/sbin/nginx
${OWP_USER} ALL=(root) NOPASSWD: /usr/bin/systemctl reload nginx
${OWP_USER} ALL=(root) NOPASSWD: /usr/bin/systemctl reload nginx.service
SUDOERS
  chmod 440 /etc/sudoers.d/openwebpanel 2>/dev/null || true
  visudo -c -f /etc/sudoers.d/openwebpanel 2>/dev/null || rm -f /etc/sudoers.d/openwebpanel
  register_rollback "rm -f /etc/sudoers.d/openwebpanel 2>/dev/null || true" "Remove sudoers config"

  # ── File permissions ──
  set_status "Hardening file permissions..." "info"
  chmod 750 "$OWP_HOME" 2>/dev/null || true
  chmod 750 "$OWP_DATA_DIR" 2>/dev/null || true
  chmod 750 "$OWP_HOMES_DIR" 2>/dev/null || true
  chmod 750 "$OWP_LOGS_DIR" 2>/dev/null || true
  chmod 700 "$OWP_SSL_DIR" 2>/dev/null || true
  chmod 700 "$OWP_BACKUPS_DIR" 2>/dev/null || true
  chmod 600 "${OWP_APP_DIR}/.env" 2>/dev/null || true
  log_info "File permissions hardened"

  # ── Kernel hardening (sysctl) ──
  set_status "Applying kernel security settings..." "info"
  cat > /etc/sysctl.d/99-openwebpanel.conf <<'SYSCTL'
# OpenWebPanel Security Hardening
net.ipv4.tcp_syncookies=1
net.ipv4.ip_forward=0
net.ipv4.conf.all.rp_filter=1
net.ipv4.conf.default.rp_filter=1
net.ipv4.conf.all.accept_source_route=0
net.ipv4.conf.default.accept_source_route=0
net.ipv4.conf.all.accept_redirects=0
net.ipv4.conf.default.accept_redirects=0
net.ipv4.conf.all.secure_redirects=0
net.ipv4.conf.default.secure_redirects=0
SYSCTL
  sysctl -p /etc/sysctl.d/99-openwebpanel.conf 2>/dev/null || true
  register_rollback "rm -f /etc/sysctl.d/99-openwebpanel.conf 2>/dev/null; sysctl --system 2>/dev/null || true" "Remove sysctl hardening"

  set_status "Security settings applied" "success"
}

# ═══════════════════════════════════════════════════════════════════════════════
# STAGE 9: Starting Services
# ═══════════════════════════════════════════════════════════════════════════════
stage_services() {
  # ── systemd: OpenWebPanel ──
  set_status "Installing systemd services..." "info"
  cat > /etc/systemd/system/openwebpanel.service <<UNIT
[Unit]
Description=OpenWebPanel - Web Hosting Control Panel
Documentation=https://github.com/${OWP_REPO}
After=network.target mariadb.service nginx.service
Wants=mariadb.service nginx.service

[Service]
Type=simple
User=${OWP_USER}
Group=${OWP_USER}
WorkingDirectory=${OWP_APP_DIR}
EnvironmentFile=${OWP_APP_DIR}/.env
ExecStart=${OWP_APP_DIR}/bin/parentd
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

  systemctl daemon-reload 2>/dev/null || true
  register_rollback "systemctl stop openwebpanel phpmyadmin 2>/dev/null; systemctl disable openwebpanel phpmyadmin 2>/dev/null; rm -f /etc/systemd/system/openwebpanel.service /etc/systemd/system/phpmyadmin.service; systemctl daemon-reload" "Remove systemd services"

  # ── Watchdog ──
  set_status "Installing watchdog and monitoring..." "info"
  mkdir -p "${OWP_APP_DIR}/bin"
  cat > "${OWP_APP_DIR}/bin/owp-watchdog.sh" <<'WATCHDOG'
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
  chmod +x "${OWP_APP_DIR}/bin/owp-watchdog.sh"
  chown "$OWP_USER:$OWP_USER" "${OWP_APP_DIR}/bin/owp-watchdog.sh" 2>/dev/null || true

  # ── Cron ──
  cat > /etc/cron.d/openwebpanel-watchdog <<CRON
*/5 * * * * root ${OWP_APP_DIR}/bin/owp-watchdog.sh >/dev/null 2>&1
CRON
  chmod 644 /etc/cron.d/openwebpanel-watchdog || true

  # ── Logrotate ──
  cat > /etc/logrotate.d/openwebpanel <<LOGROTATE
${OWP_LOGS_DIR}/*.log {
    daily; rotate 30; compress; delaycompress; missingok; notifempty; copytruncate
}
${OWP_APP_DIR}/parentd.log {
    daily; rotate 14; compress; delaycompress; missingok; notifempty; copytruncate
}
LOGROTATE

  # ── Backup cron ──
  cat > /etc/cron.d/openwebpanel-backup <<CRON2
0 3 * * * root tar --exclude='node_modules' -czf ${OWP_BACKUPS_DIR}/backup-\$(date +\%Y\%m\%d).tar.gz -C ${OWP_APP_DIR} . 2>/dev/null && find ${OWP_BACKUPS_DIR} -name 'backup-*.tar.gz' -mtime +7 -delete
CRON2
  chmod 644 /etc/cron.d/openwebpanel-backup || true

  # ── Start Everything ──
  set_status "Starting MariaDB..." "info"
  systemctl restart mariadb 2>/dev/null || service mariadb restart 2>/dev/null || true
  for i in $(seq 1 10); do
    if mysqladmin ping --silent 2>/dev/null; then break; fi
    sleep 1
  done

  set_status "Starting PHP-FPM..." "info"
  systemctl enable "php${PHP_VER}-fpm" --now 2>/dev/null || true

  set_status "Starting phpMyAdmin..." "info"
  systemctl enable phpmyadmin --now 2>/dev/null || true

  set_status "Starting Nginx..." "info"
  if nginx -t 2>&1; then
    systemctl enable nginx --now 2>&1 || log_warn "systemctl enable nginx failed"
  else
    log_error "Nginx configuration is invalid - cannot start nginx"
  fi

  set_status "Starting OpenWebPanel..." "info"
  systemctl enable openwebpanel --now 2>&1 || {
    log_warn "systemd start failed, trying direct execution..."
    if [[ -f "${OWP_APP_DIR}/bin/parentd" ]]; then
      sudo -u "$OWP_USER" \
        OWP_JWT_SECRET="${OWP_JWT_SECRET}" \
        OWP_DB_PATH="${OWP_DATA_DIR}/openwebpanel.db" \
        OWP_ADMIN_STATIC_DIR="${OWP_APP_DIR}/web/dist/admin" \
        OWP_CHILD_STATIC_DIR="${OWP_APP_DIR}/web/dist/child" \
        OWP_ADMIN_LISTEN=":9000" \
        OWP_CHILD_LISTEN=":9001" \
        OWP_HOMES_BASE="${OWP_HOMES_DIR}/" \
        OWP_PUBLIC_HOST="${OWP_DOMAIN}:9000" \
        OWP_SHARED_IP="127.0.0.1" \
        OWP_SMTP_PORT="${OWP_SMTP_PORT}" \
        NGINX_PREFIX="${NGINX_PREFIX}" \
        NGINX_VHOST_DIR="${NGINX_VHOST_DIR}" \
        NGINX_BIN="${NGINX_BIN}" \
        NGINX_CONF="${NGINX_CONF}" \
        NGINX_LOG_DIR="${NGINX_LOG_DIR}" \
        PHP_FPM_SOCKET="/run/php/php${PHP_VER}-fpm.sock" \
        MYSQL_ROOT_PASSWORD="${OWP_MYSQL_ROOT_PASSWORD}" \
        MYSQL_ADMIN_PASSWORD="${OWP_MYSQL_ADMIN_PASSWORD}" \
        "${OWP_APP_DIR}/bin/parentd" &>/tmp/owp-fallback.log &
    else
      log_warn "Binary not found at ${OWP_APP_DIR}/bin/parentd"
    fi
  }

  set_status "Services started" "success"
}

# ═══════════════════════════════════════════════════════════════════════════════
# STAGE 10: Running Final Validation
# ═══════════════════════════════════════════════════════════════════════════════
stage_validate() {
  set_status "Waiting for panel to respond..." "info"

  local started=false i
  for i in $(seq 1 30); do
    if curl -sf -o /dev/null http://127.0.0.1:9000/healthz 2>/dev/null; then
      started=true
      break
    fi
    sleep 1
  done

  if [[ "$started" == "true" ]]; then
    set_status "${G_CHECK} Panel is running on port 9000" "success"
  else
    log_warn "Panel did not respond within 30s"
    if [[ -f /tmp/owp-fallback.log ]]; then
      log_warn "Fallback log contents:"
      tail -20 /tmp/owp-fallback.log >> "$INSTALL_LOG" 2>/dev/null || true
    fi
  fi

  # ── Validate Services ──
  set_status "Validating all services..." "info"
  local all_ok=true
  for svc in nginx mariadb "php${PHP_VER}-fpm"; do
    if systemctl is-active --quiet "$svc" 2>/dev/null; then
      log_ok "Service '$svc' is running"
    else
      log_warn "Service '$svc' is NOT running"
      all_ok=false
    fi
  done

  if systemctl is-active --quiet openwebpanel 2>/dev/null; then
    log_ok "Service 'openwebpanel' is running"
  else
    log_warn "Service 'openwebpanel' is NOT running"
    all_ok=false
  fi

  # ── Validate Ports ──
  set_status "Checking required ports..." "info"
  local critical_ports=(80 "${OWP_PANEL_PORT}" "${OWP_USER_PORT}" 9000 9001)
  for port in "${critical_ports[@]}"; do
    if ss -tlnp "sport = :${port}" 2>/dev/null | grep -q LISTEN; then
      log_ok "Port $port is listening"
    else
      log_error "Required port $port is not listening"
      all_ok=false
    fi
  done

  # ── Validate Binary ──
  set_status "Checking installation integrity..." "info"
  if [[ -f "${OWP_APP_DIR}/bin/parentd" ]]; then
    log_ok "Binary exists: ${OWP_APP_DIR}/bin/parentd"
  else
    log_error "Binary missing: ${OWP_APP_DIR}/bin/parentd"
    all_ok=false
  fi

  if [[ -f "${OWP_APP_DIR}/.env" ]]; then
    log_ok "Environment file exists"
  else
    log_warn "Environment file missing"
  fi

  if [[ -d "${OWP_APP_DIR}/web/dist" ]]; then
    log_ok "Frontend build exists"
  else
    log_warn "Frontend build missing"
  fi

  # ── Write password file ──
  set_status "Saving admin credentials..." "info"
  mkdir -p "$(dirname "$OWP_PASSWORD_FILE")" 2>/dev/null || {
    log_warn "Cannot create directory for password file at $(dirname "$OWP_PASSWORD_FILE")"
  }
  cat > "$OWP_PASSWORD_FILE" <<EOF
╔══════════════════════════════════════════╗
║   OpenWebPanel Admin Credentials         ║
║   Generated: $(date)           ║
╚══════════════════════════════════════════╝

Admin Panel:  http://${OWP_DOMAIN}:${OWP_PANEL_PORT}
User Panel:   http://${OWP_DOMAIN}:${OWP_USER_PORT}
Website:      http://${OWP_DOMAIN}

Username:     admin
Password:     ${OWP_ADMIN_PASSWORD}

MySQL Root:   ${OWP_MYSQL_ROOT_PASSWORD}
MySQL Admin:  ${OWP_MYSQL_ADMIN_PASSWORD}
JWT Secret:   ${OWP_JWT_SECRET}

⚠  SAVE THESE CREDENTIALS — they will not be shown again.
EOF
  chmod 600 "$OWP_PASSWORD_FILE" 2>/dev/null || true
  chown "$OWP_USER:$OWP_USER" "$OWP_PASSWORD_FILE" 2>/dev/null || true

  if [[ "$started" == "true" && "$all_ok" == "true" ]]; then
    set_status "${G_CHECK} All validations passed" "success"
  else
    set_status "${G_CHECK} Installation completed with warnings" "warn"
  fi
}

# ═══════════════════════════════════════════════════════════════════════════════
# Summary Output
# ═══════════════════════════════════════════════════════════════════════════════
print_summary() {
  local elapsed
  elapsed=$(get_elapsed)

  if $HAS_TTY; then
    echo ""
    echo -e "  ${C_GREEN}${G_BLOCK}${G_BLOCK}${G_BLOCK}  INSTALLATION COMPLETE  ${G_BLOCK}${G_BLOCK}${G_BLOCK}${C_RESET}"
    echo ""
    echo -e "  ${C_BOLD}OpenWebPanel v${OWP_VERSION}${C_RESET} ${C_DIM}(${OWP_CODENAME})${C_RESET}"
    echo ""
    echo -e "  ${C_CYAN}━━ Access ─────────────────────────────────────────${C_RESET}"
    echo -e "  ${C_DIM}Admin Panel:${C_RESET}  ${C_BOLD}http://${OWP_DOMAIN}:${OWP_PANEL_PORT}${C_RESET}"
    echo -e "  ${C_DIM}User Panel:${C_RESET}   ${C_BOLD}http://${OWP_DOMAIN}:${OWP_USER_PORT}${C_RESET}"
    echo -e "  ${C_DIM}Website:${C_RESET}      ${C_BOLD}http://${OWP_DOMAIN}${C_RESET}"
    echo ""
    echo -e "  ${C_YELLOW}━━ Login ─────────────────────────────────────────${C_RESET}"
    echo -e "  ${C_DIM}Username:${C_RESET}     ${C_BOLD}admin${C_RESET}"
    echo -e "  ${C_DIM}Password:${C_RESET}     ${C_BOLD}${OWP_ADMIN_PASSWORD}${C_RESET}"
    echo -e "  ${C_RED}⚠  Saved to:${C_RESET} ${C_DIM}${OWP_PASSWORD_FILE}${C_RESET}"
    echo ""
    echo -e "  ${C_CYAN}━━ Commands ───────────────────────────────────────${C_RESET}"
    echo -e "  ${C_DIM}Status:${C_RESET}       systemctl status openwebpanel"
    echo -e "  ${C_DIM}Logs:${C_RESET}         journalctl -u openwebpanel -f"
    echo -e "  ${C_DIM}Restart:${C_RESET}      systemctl restart openwebpanel"
    echo ""
    echo -e "  ${C_DIM}Elapsed:${C_RESET} ${elapsed}"
    echo -e "  ${C_DIM}Log:${C_RESET} ${INSTALL_LOG}"
    echo ""
  else
    echo ""
    echo "══════════════════════════════════════════════════"
    echo "  OpenWebPanel v${OWP_VERSION} — Installation Complete"
    echo "══════════════════════════════════════════════════"
    echo ""
    echo "  Admin Panel:  http://${OWP_DOMAIN}:${OWP_PANEL_PORT}"
    echo "  User Panel:   http://${OWP_DOMAIN}:${OWP_USER_PORT}"
    echo "  Website:      http://${OWP_DOMAIN}"
    echo ""
    echo "  Username: admin"
    echo "  Password: ${OWP_ADMIN_PASSWORD}"
    echo "  Saved to: ${OWP_PASSWORD_FILE}"
    echo ""
    echo "  Elapsed: ${elapsed}"
    echo "  Log: ${INSTALL_LOG}"
    echo ""
  fi
}
