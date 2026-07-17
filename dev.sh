#!/bin/bash
set -e

echo "=== OpenWebPanel Dev Server ==="
echo ""

# Kill existing processes
pkill -f "bin/parentd" 2>/dev/null || true
sleep 0.5

# Detect shared IP
SHARED_IP=$(curl -s --connect-timeout 3 ifconfig.me 2>/dev/null || echo "127.0.0.1")

# Clean stale SQLite WAL files
rm -f "$(dirname "$0")/openwebpanel.db-shm" "$(dirname "$0")/openwebpanel.db-wal" 2>/dev/null || true

# Build and start backend
echo "[1/2] Building and starting backend (admin :9000, child :9001)..."
cd "$(dirname "$0")"
go build -o bin/parentd ./cmd/parentd/
OWP_ADMIN_STATIC_DIR=./web/dist/admin OWP_CHILD_STATIC_DIR=./web/dist/child \
OWP_ADMIN_LISTEN=:9000 OWP_CHILD_LISTEN=:9001 \
OWP_PUBLIC_HOST="${SHARED_IP}:9000" OWP_SHARED_IP="${SHARED_IP}" ./bin/parentd &
BACKEND_PID=$!
sleep 1

# Start frontend dev servers
echo "[2/2] Starting admin frontend on :5173 and child frontend on :5174..."
cd web
npx vite --config vite.admin.config.ts --host 0.0.0.0 &
ADMIN_FE_PID=$!
npx vite --config vite.child.config.ts --host 0.0.0.0 &
CHILD_FE_PID=$!

echo ""
echo "=== OpenWebPanel is running ==="
echo "  Admin Frontend:  http://${SHARED_IP}:5173 (→ backend :9000)"
echo "  Child Frontend:  http://${SHARED_IP}:5174 (→ backend :9001)"
echo "  Backend Admin:   http://${SHARED_IP}:9000"
echo "  Backend Child:   http://${SHARED_IP}:9001"
echo "  Login:           admin (password set via OWP_ADMIN_PASSWORD or .env)"
echo ""
echo "Press Ctrl+C to stop all services"

trap "kill $BACKEND_PID $ADMIN_FE_PID $CHILD_FE_PID 2>/dev/null; exit" INT TERM
wait
