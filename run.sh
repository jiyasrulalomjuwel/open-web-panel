#!/bin/bash
export OWP_DB_PATH=./openwebpanel.db
export OWP_JWT_SECRET=dev-secret
export OWP_ADMIN_PASSWORD=admin123
export OWP_HOMES_BASE=./homes/
export OWP_ADMIN_STATIC_DIR=./web/dist/admin
export OWP_CHILD_STATIC_DIR=./web/dist/child
export OWP_ADMIN_LISTEN=:9000
export OWP_CHILD_LISTEN=:9001
export OWP_PUBLIC_HOST=127.0.0.1:9000
export OWP_SHARED_IP=127.0.0.1
exec ./bin/parentd
