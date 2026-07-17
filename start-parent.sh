#!/bin/bash
set -e
cd /home/ubuntu/open-web-panel
mkdir -p nginx/vhosts nginx/logs

export OWP_DB_PATH=./openwebpanel.db
export OWP_JWT_SECRET="${OWP_JWT_SECRET:-$(openssl rand -base64 32 2>/dev/null || tr -dc 'A-Za-z0-9' < /dev/urandom | head -c48)}"
export OWP_ADMIN_PASSWORD=admin123
export OWP_HOMES_BASE=./homes/
export OWP_ADMIN_STATIC_DIR=./web/dist/admin
export OWP_CHILD_STATIC_DIR=./web/dist/child
export OWP_ADMIN_LISTEN=:9000
export OWP_CHILD_LISTEN=:9001
export NGINX_PREFIX=./nginx
export NGINX_LOG_DIR=./nginx/logs

# Check if already running
if [ -f /tmp/parentd.pid ] && kill -0 $(cat /tmp/parentd.pid) 2>/dev/null; then
    echo "Already running as PID $(cat /tmp/parentd.pid)"
    exit 0
fi

setsid ./bin/parentd < /dev/null > /dev/null 2>&1 &
PID=$!
echo $PID > /tmp/parentd.pid
echo "Started parentd with PID $PID"
sleep 2
if kill -0 $PID 2>/dev/null; then
    echo "Process is running"
    ss -tlnp 2>/dev/null | grep "$PID" || echo "(no listening socket yet)"
fi
