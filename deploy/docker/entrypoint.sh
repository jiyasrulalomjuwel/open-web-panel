#!/bin/sh
# OpenWebPanel Docker Entrypoint
set -e

# Start MariaDB in background
/usr/bin/mysqld_safe &

# Wait for MariaDB to be ready
for i in $(seq 1 30); do
    if mysqladmin ping -h localhost --silent 2>/dev/null; then
        break
    fi
    sleep 1
done

# Start parent daemon
exec /app/bin/parentd
