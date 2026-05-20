#!/bin/bash
set -e

echo "=== OpenWebPanel Production Server ==="

# Kill existing processes
pkill -f "bin/parentd" 2>/dev/null || true
pkill -f "bin/childd" 2>/dev/null || true
sleep 0.5

# Detect shared IP
SHARED_IP=$(curl -s --connect-timeout 3 ifconfig.me 2>/dev/null || echo "127.0.0.1")

# Ensure MariaDB is running
sudo mysqld_safe &
sleep 1

# Clean stale SQLite WAL files
rm -f "$(dirname "$0")/openwebpanel.db-shm" "$(dirname "$0")/openwebpanel.db-wal" 2>/dev/null || true

# Start parentd (main panel daemon)
OWP_STATIC_DIR="/home/claudeuser/openwebcpanel/web/dist" \
OWP_SHARED_IP="${SHARED_IP}" \
OWP_PUBLIC_HOST="${SHARED_IP}:9000" \
OWP_SMTP_PORT="2525" \
nohup ./bin/parentd > parentd.log 2>&1 &
PARENTD_PID=$!
echo "parentd started (PID: $PARENTD_PID)"

# Start childd (per-user daemon) for the test account
OWP_HOME_DIR="/home/claudeuser/openwebcpanel/homes/password" \
OWP_CHILD_LISTEN=":9001" \
nohup ./bin/childd > /dev/null 2>&1 &
CHILDD_PID=$!
echo "childd started (PID: $CHILDD_PID)"

sleep 1

echo ""
echo "=== OpenWebPanel is running ==="
echo "  Admin Panel: http://${SHARED_IP}:2086"
echo "  Child Panel: http://${SHARED_IP}:2082"
echo "  Backend:     http://${SHARED_IP}:9000"
echo "  Login:       admin / admin123"
echo ""
