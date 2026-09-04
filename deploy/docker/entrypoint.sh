#!/bin/sh
# OpenWebPanel Docker Entrypoint
# Self-contained container: starts MariaDB (optional), PHP-FPM and nginx,
# then launches parentd in the foreground. parentd serves the admin SPA+API
# on OWP_ADMIN_LISTEN (127.0.0.1:9000) and the child SPA+API on
# OWP_CHILD_LISTEN (127.0.0.1:9001); nginx publishes 80/443 for hosted user
# sites and proxies the panel ports 2086/2082 to parentd.
set -e

log() { echo "[OWP] $*"; }

log "container entrypoint starting"

PANEL_PORT="${OWP_PANEL_PORT:-2086}"
USER_PORT="${OWP_USER_PORT:-2082}"
DATA_DIR="${OWP_DATA_DIR:-/app/data}"
HOMES_DIR="${OWP_HOMES_BASE:-/app/homes/}"
PHP_FPM_SOCKET="${PHP_FPM_SOCKET:-/run/php/php8.3-fpm.sock}"

export OWP_ADMIN_LISTEN="${OWP_ADMIN_LISTEN:-127.0.0.1:9000}"
export OWP_CHILD_LISTEN="${OWP_CHILD_LISTEN:-127.0.0.1:9001}"
export OWP_ADMIN_STATIC_DIR="${OWP_ADMIN_STATIC_DIR:-/app/web/dist/admin}"
export OWP_CHILD_STATIC_DIR="${OWP_CHILD_STATIC_DIR:-/app/web/dist/child}"
export OWP_DB_PATH="${OWP_DB_PATH:-${DATA_DIR}/openwebpanel.db}"
export OWP_HOMES_BASE="${HOMES_DIR}"

# ── Directories ──────────────────────────────────────────────────────────────
mkdir -p "$DATA_DIR" "$HOMES_DIR" /run/php /run/mysqld /var/log/nginx \
         /etc/nginx/vhosts /etc/nginx/http.d /var/lib/mysql
chown -R mysql:mysql /var/lib/mysql /run/mysqld 2>/dev/null || true

# ── MariaDB (optional; parentd stores panel data in SQLite via OWP_DB_PATH) ──
if [ -z "${MYSQL_ROOT_PASSWORD:-}" ]; then
  MYSQL_ROOT_PASSWORD=$(openssl rand -base64 24)
  log "generated MYSQL_ROOT_PASSWORD"
fi
if [ -z "${MYSQL_ADMIN_PASSWORD:-}" ]; then
  MYSQL_ADMIN_PASSWORD=$(openssl rand -base64 24)
  log "generated MYSQL_ADMIN_PASSWORD"
fi

DB_FRESH=0
if [ ! -d /var/lib/mysql/mysql ]; then
  log "initializing MariaDB data directory"
  mariadb-install-db --user=mysql --datadir=/var/lib/mysql \
      --auth-root-authentication-method=normal >/dev/null 2>&1 \
    || mariadb-install-db --user=mysql --datadir=/var/lib/mysql >/dev/null 2>&1 \
    || log "[WARN] could not initialize MariaDB data directory"
  DB_FRESH=1
fi

mariadbd --user=mysql --datadir=/var/lib/mysql --skip-networking=0 --bind-address=127.0.0.1 >/var/log/mariadb.log 2>&1 &
MARIADB_PID=$!
if ! kill -0 "$MARIADB_PID" 2>/dev/null; then
  log "[WARN] MariaDB failed to start (log: /var/log/mariadb.log); panel uses SQLite so startup continues"
fi

DB_READY=0
for i in $(seq 1 30); do
  if mariadb-admin ping --silent >/dev/null 2>&1 || mysqladmin ping --silent >/dev/null 2>&1; then
    DB_READY=1
    break
  fi
  sleep 1
done

if [ "$DB_READY" = "1" ]; then
  if [ "$DB_FRESH" = "1" ]; then
    mariadb -u root <<SQL 2>/dev/null || mysql -u root <<SQL 2>/dev/null || true
ALTER USER 'root'@'localhost' IDENTIFIED BY '$MYSQL_ROOT_PASSWORD';
FLUSH PRIVILEGES;
SQL
    mariadb -u root -p"$MYSQL_ROOT_PASSWORD" 2>/dev/null <<SQL || true
CREATE USER IF NOT EXISTS 'owp_admin'@'%' IDENTIFIED BY '$MYSQL_ADMIN_PASSWORD';
GRANT ALL PRIVILEGES ON *.* TO 'owp_admin'@'%';
FLUSH PRIVILEGES;
SQL
  fi
  log "MariaDB ready (root password set)"
else
  log "[WARN] MariaDB did not become ready; panel uses SQLite so startup continues"
fi

# ── PHP-FPM ──────────────────────────────────────────────────────────────────
log "configuring PHP-FPM on $PHP_FPM_SOCKET"
# Alpine's default php-fpm "www" pool binds 127.0.0.1:9000, which collides
# with parentd (OWP_ADMIN_LISTEN=127.0.0.1:9000). Disable it so the tcp port
# stays free for parentd; the custom unix-socket pool (zz-owp.conf) below is
# unaffected.
rm -f /etc/php83/php-fpm.d/www.conf
mkdir -p "$(dirname "$PHP_FPM_SOCKET")"
cat > /etc/php83/php-fpm.d/zz-owp.conf <<EOF
[owp]
user = nobody
group = nobody
listen = $PHP_FPM_SOCKET
listen.owner = nginx
listen.group = nginx
listen.mode = 0660
pm = dynamic
pm.max_children = 20
pm.start_servers = 4
pm.min_spare_servers = 2
pm.max_spare_servers = 6
pm.max_requests = 500
EOF
php-fpm83 -R >/dev/null 2>&1 &
PHP_PID=$!
log "php-fpm started (pid $PHP_PID)"

# ── Nginx ────────────────────────────────────────────────────────────────────
log "configuring nginx"
rm -f /etc/nginx/http.d/default.conf

cat > /etc/nginx/http.d/zz-owp-vhosts.conf <<'EOF'
# User-site vhosts written by parentd
include /etc/nginx/vhosts/*.conf;
EOF

# nginx errors when an include glob matches nothing, and on a fresh install
# /etc/nginx/vhosts has no vhosts yet. Keep one placeholder so `nginx -t` and
# the initial start succeed. parentd may remove it once it syncs real vhosts;
# a later reload failure is non-fatal and gets logged by parentd.
: > /etc/nginx/vhosts/zz-owp-placeholder.conf

cat > /etc/nginx/http.d/zz-owp-panel.conf <<EOF
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
    }

    location ^~ /.well-known/acme-challenge/ {
        proxy_pass http://127.0.0.1:9000;
        proxy_set_header Host \$host;
    }
}

server {
    listen $PANEL_PORT;
    listen [::]:$PANEL_PORT;
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
    }
}

server {
    listen $USER_PORT;
    listen [::]:$USER_PORT;
    server_name _;
    client_max_body_size 0;
    location / {
        proxy_pass http://127.0.0.1:9001;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    }
}
EOF

if ! nginx -t >/tmp/nginx-test.log 2>&1; then
  log "[FATAL] nginx configuration invalid:"
  cat /tmp/nginx-test.log
  exit 1
fi
nginx
log "nginx started"

# ── Cleanup on shutdown ──────────────────────────────────────────────────────
CLEANUP_DONE=0
PARENT_PID=""
cleanup() {
  [ "$CLEANUP_DONE" = "1" ] && return 0
  CLEANUP_DONE=1
  log "shutting down"
  if [ -n "$PARENT_PID" ]; then
    kill -TERM "$PARENT_PID" 2>/dev/null || true
    wait "$PARENT_PID" 2>/dev/null || true
  fi
  nginx -s quit 2>/dev/null || true
  [ -n "$PHP_PID" ] && kill "$PHP_PID" 2>/dev/null || true
  [ -n "$MARIADB_PID" ] && kill "$MARIADB_PID" 2>/dev/null || true
}
trap 'cleanup' TERM INT

# ── Parent daemon (supervised, PID 1 is this shell) ──────────────────────────
log "starting parentd (admin on $OWP_ADMIN_LISTEN, child on $OWP_CHILD_LISTEN)"
/app/bin/parentd &
PARENT_PID=$!

set +e
wait "$PARENT_PID"
RC=$?
set -e

cleanup
exit "$RC"
