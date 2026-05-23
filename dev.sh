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
echo "[1/2] Building and starting backend on :9000..."
cd "$(dirname "$0")"
go build -o bin/parentd ./cmd/parentd/
OWP_PUBLIC_HOST="${SHARED_IP}:9000" OWP_SHARED_IP="${SHARED_IP}" ./bin/parentd &
BACKEND_PID=$!
sleep 1

# Start frontend
echo "[2/2] Starting frontend on :5173..."
cd web
npx vite --host 0.0.0.0 &
FRONTEND_PID=$!

echo ""
echo "=== OpenWebPanel is running ==="
echo "  Frontend:  http://${SHARED_IP}:5173"
echo "  Backend:   http://${SHARED_IP}:9000"
echo "  Login:     admin (password set via OWP_ADMIN_PASSWORD or .env)"
echo ""
echo "Press Ctrl+C to stop both services"

trap "kill $BACKEND_PID $FRONTEND_PID 2>/dev/null; exit" INT TERM
wait
