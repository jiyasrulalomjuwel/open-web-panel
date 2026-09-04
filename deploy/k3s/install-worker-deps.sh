#!/usr/bin/env bash
# OpenWebPanel worker dependency installer.
# Runs ON the worker host (chrooted to / by the panel's installer Job, or
# directly as root for manual use): nginx + PHP-FPM + MariaDB client + UFW.
# Every step prints [STEP n/7] ... OK/FAIL so the parent panel can stream it.
set -uo pipefail
export DEBIAN_FRONTEND=noninteractive

step() { echo "[STEP $1/7] $2..."; }
ok()   { echo "[STEP $1/7] $2 ... OK"; }
fail() { echo "[STEP $1/7] $2 ... FAIL: $3"; exit 1; }

[[ $EUID -eq 0 ]] || { echo "must run as root"; exit 1; }

# 1 — detect OS
step 1 "detect OS"
# shellcheck disable=SC1091
. /etc/os-release 2>/dev/null || fail 1 "detect OS" "no /etc/os-release"
echo "     ${NAME:-unknown} ${VERSION_ID:-?}"
case "${ID:-}" in ubuntu|debian) ok 1 "detect OS";; *) fail 1 "detect OS" "unsupported ${ID:-?}";; esac

# 2 — pick PHP (matches the PHP version baked into the panel image)
step 2 "pick PHP version"
PHP_VER=""
if [[ "$ID" == "ubuntu" ]]; then
  case "$VERSION_ID" in
    20.04) PHP_VER="8.0";; 22.04) PHP_VER="8.1";; 24.04) PHP_VER="8.3";; 24.10) PHP_VER="8.3";; 25.04|25.10) PHP_VER="8.4";; 26.04) PHP_VER="8.5";; *) PHP_VER="8.3";;
  esac
else
  case "$VERSION_ID" in
    11) PHP_VER="7.4";; 12) PHP_VER="8.2";; 13) PHP_VER="8.3";; *) PHP_VER="8.2";;
  esac
fi
AVAIL=$(apt-cache search "^php${PHP_VER}-fpm$" 2>/dev/null | head -1 || true)
[[ -n "$AVAIL" ]] || PHP_VER=$(apt-cache search '^php[0-9]+\.[0-9]+-fpm$' 2>/dev/null | grep -oP 'php\K[0-9]+\.[0-9]+' | sort -V | tail -1)
[[ -n "$PHP_VER" ]] || fail 2 "pick PHP version" "no php-fpm in apt"
ok 2 "pick PHP version (php${PHP_VER})"

# 3 — apt update (with DNS fixup for chrooted runs)
step 3 "apt update"
# When executed chrooted from a pod, the host's 127.0.0.x stub resolver is
# dead in the pod netns. Swap in the pod-provided resolvers (or public ones)
# for the duration of this script, then restore.
RESOLV_SWAPPED=""
if grep -qE '^nameserver 127\.' /etc/resolv.conf 2>/dev/null; then
  cp /etc/resolv.conf /tmp/owp-resolv.bak 2>/dev/null || true
  if [[ -s /tmp/owp-pod-resolv.conf ]]; then
    cp /tmp/owp-pod-resolv.conf /etc/resolv.conf
  else
    printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' > /etc/resolv.conf
  fi
  RESOLV_SWAPPED=1
  restore_resolv() {
    [[ -n "$RESOLV_SWAPPED" && -f /tmp/owp-resolv.bak ]] && mv /tmp/owp-resolv.bak /etc/resolv.conf 2>/dev/null || true
    rm -f /tmp/owp-pod-resolv.conf /tmp/owp-deps.sh
  }
  trap restore_resolv EXIT
fi
apt-get update -qq >/tmp/owp-apt.log 2>&1 || fail 3 "apt update" "$(tail -2 /tmp/owp-apt.log)"
ok 3 "apt update"

# 4 — install packages
step 4 "install nginx php mariadb-client ufw"
apt-get install -y -qq nginx "php${PHP_VER}-fpm" "php${PHP_VER}-mysql" "php${PHP_VER}-curl" \
  "php${PHP_VER}-mbstring" "php${PHP_VER}-xml" mariadb-client ufw curl >/tmp/owp-pkgs.log 2>&1 \
  || fail 4 "install packages" "$(tail -3 /tmp/owp-pkgs.log)"
ok 4 "install packages"

# 5 — nginx serves panel vhosts dir
step 5 "configure nginx"
mkdir -p /etc/nginx/vhosts /var/log/nginx
grep -q 'include /etc/nginx/vhosts/\*.conf;' /etc/nginx/nginx.conf 2>/dev/null || \
  sed -i '/include \/etc\/nginx\/sites-enabled\/\*;/a\\tinclude /etc/nginx/vhosts/*.conf;' /etc/nginx/nginx.conf
: > /etc/nginx/vhosts/zz-owp-placeholder.conf
nginx -t >/tmp/owp-nginx.log 2>&1 || fail 5 "configure nginx" "$(tail -3 /tmp/owp-nginx.log)"
systemctl enable --now nginx >/dev/null 2>&1 || service nginx start >/dev/null 2>&1 || true
ok 5 "configure nginx"

# 6 — php-fpm socket up
step 6 "start php-fpm"
systemctl enable --now "php${PHP_VER}-fpm" >/dev/null 2>&1 || service "php${PHP_VER}-fpm" start >/dev/null 2>&1 || true
ls /run/php/php"${PHP_VER}"-fpm.sock >/dev/null 2>&1 || fail 6 "start php-fpm" "socket missing"
ok 6 "start php-fpm"

# 7 — firewall
step 7 "firewall 80/443/10250/8472"
for r in "80/tcp" "443/tcp" "10250/tcp" "8472/udp"; do ufw allow "$r" >/dev/null 2>&1 || true; done
ufw allow ssh >/dev/null 2>&1 || true
ufw --force enable >/dev/null 2>&1 || true
ok 7 "firewall"

echo "WORKER-DEPS-DONE php=${PHP_VER}"
