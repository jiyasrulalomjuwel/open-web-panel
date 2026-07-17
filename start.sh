#!/bin/bash
# OpenWebPanel — Quick production start script
# Usage: sudo bash start.sh
set -e

OWP_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== OpenWebPanel Production Server ==="

# Kill existing processes
pkill -f "bin/parentd" 2>/dev/null || true
pkill -f "bin/childd" 2>/dev/null || true
sleep 0.5

# Detect shared IP
SHARED_IP=$(curl -s --connect-timeout 3 ifconfig.me 2>/dev/null || echo "127.0.0.1")

# Ensure MariaDB is running
sudo mysqld_safe 2>/dev/null &
sleep 1

# Clean stale SQLite WAL files
rm -f "$OWP_DIR/openwebpanel.db-shm" "$OWP_DIR/openwebpanel.db-wal" 2>/dev/null || true

# Start parentd (main panel daemon — runs both admin and child HTTP servers)
OWP_ADMIN_STATIC_DIR="$OWP_DIR/web/dist/admin" \
OWP_CHILD_STATIC_DIR="$OWP_DIR/web/dist/child" \
OWP_SHARED_IP="${SHARED_IP}" \
OWP_ADMIN_LISTEN=":9000" \
OWP_CHILD_LISTEN=":9001" \
OWP_SMTP_PORT="2525" \
OWP_HOMES_BASE="$OWP_DIR/homes/" \
OWP_JWT_SECRET="${OWP_JWT_SECRET:-$(openssl rand -base64 32 2>/dev/null || tr -dc 'A-Za-z0-9' < /dev/urandom | head -c48)}" \
OWP_DB_PATH="$OWP_DIR/openwebpanel.db" \
nohup "$OWP_DIR/bin/parentd" > "$OWP_DIR/parentd.log" 2>&1 &
PARENTD_PID=$!
echo "parentd started (PID: $PARENTD_PID)"

sleep 1

echo ""
echo "=== OpenWebPanel is running ==="
echo "  Admin Panel:  http://${SHARED_IP}:9000"
echo "  Child Panel:  http://${SHARED_IP}:9001"
echo "  Login:        admin (password set via OWP_ADMIN_PASSWORD or .env)"
echo ""
