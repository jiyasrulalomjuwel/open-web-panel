#!/bin/bash
set -e

# Resolve this script's own location so it works from any checkout path.
if [ -n "$BASH_SOURCE" ]; then
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
else
  SCRIPT_DIR="$(pwd)"
fi
cd "${SCRIPT_DIR}"
mkdir -p nginx/vhosts nginx/logs

PID_FILE="/tmp/parentd.pid"

# Clean up a stale PID file left behind by a previous crash (the guard below
# would otherwise refuse to start even though nothing is actually running).
if [ -f "$PID_FILE" ]; then
  STALE_PID="$(cat "$PID_FILE" 2>/dev/null || true)"
  if [ -z "$STALE_PID" ] || ! kill -0 "$STALE_PID" 2>/dev/null; then
    rm -f "$PID_FILE"
  fi
fi

export OWP_DB_PATH=./openwebpanel.db
export OWP_JWT_SECRET="${OWP_JWT_SECRET:-$(openssl rand -base64 32 2>/dev/null || tr -dc 'A-Za-z0-9' < /dev/urandom | head -c48)}"
export OWP_ADMIN_PASSWORD="${OWP_ADMIN_PASSWORD:-$(openssl rand -base64 12 2>/dev/null || tr -dc 'A-Za-z0-9' < /dev/urandom | head -c16)}"
echo "Admin password: $OWP_ADMIN_PASSWORD (set OWP_ADMIN_PASSWORD to override; only used on first boot)"
export OWP_HOMES_BASE=./homes/
export OWP_ADMIN_STATIC_DIR=./web/dist/admin
export OWP_CHILD_STATIC_DIR=./web/dist/child
export OWP_ADMIN_LISTEN=127.0.0.1:9000
export OWP_CHILD_LISTEN=127.0.0.1:9001
export NGINX_PREFIX=./nginx
export NGINX_LOG_DIR=./nginx/logs

# Check if already running
if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE" 2>/dev/null)" 2>/dev/null; then
    echo "Already running as PID $(cat "$PID_FILE")"
    exit 0
fi

setsid ./bin/parentd < /dev/null > /dev/null 2>&1 &
PID=$!
echo $PID > "$PID_FILE"
echo "Started parentd with PID $PID"
sleep 2
if kill -0 $PID 2>/dev/null; then
    echo "Process is running"
    ss -tlnp 2>/dev/null | grep "$PID" || echo "(no listening socket yet)"
fi