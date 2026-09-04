#!/bin/sh
# OpenWebPanel Per-User Container Entrypoint
set -e

OWP_ACCOUNT_ID=${OWP_ACCOUNT_ID:-0}
OWP_USERNAME=${OWP_USERNAME:-user}
OWP_HOME=${OWP_HOME:-/home/user}

# Ensure home directory exists
mkdir -p "$OWP_HOME/public_html" "$OWP_HOME/.owp"

# Generate Nginx vhost configs based on domain directories in home
/usr/local/bin/owp-nginx-config.sh

# Start PHP-FPM
php-fpm83 -R --nodaemonize &
PHP_PID=$!

# Start nginx
nginx -g "daemon off;" &
NGINX_PID=$!

# Start child daemon
export OWP_CHILD_LISTEN="127.0.0.1:9001"
export OWP_HOME_DIR="$OWP_HOME"
childd &
CHILD_PID=$!

# Trap signals for graceful shutdown
trap "kill $PHP_PID $NGINX_PID $CHILD_PID 2>/dev/null; exit 0" SIGINT SIGTERM

# Wait for any process to exit
wait
