.PHONY: build run-parent run-child dev-backend dev-frontend migrate install test

# Build all binaries
build:
	go build -o bin/parentd ./cmd/parentd
	go build -o bin/childd ./cmd/childd

# Run parent daemon
run-parent:
	go run ./cmd/parentd

# Run child daemon
run-child:
	go run ./cmd/childd

# Run both daemons in dev mode
dev-backend:
	go run ./cmd/parentd &
	go run ./cmd/childd &
	wait

# Install frontend dependencies
install-frontend:
	cd web && npm install

# Run frontend dev servers
dev-frontend:
	cd web && npm run dev

# Run parent panel frontend only
dev-parent:
	cd web && npm run dev -- --filter=parent

# Run child panel frontend only
dev-child:
	cd web && npm run dev -- --filter=child

# Run database migrations
migrate:
	go run ./cmd/migrate

# Run tests
test:
	go test ./... -v

# Build for production
build-prod:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/parentd ./cmd/parentd
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/childd ./cmd/childd
	cd web && npm run build

# Build frontend
build-frontend:
	cd web && npm run build

# Build backend only
build-backend:
	go build -o bin/parentd ./cmd/parentd
	go build -o bin/childd ./cmd/childd

# Docker
docker-build:
	docker-compose build

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

# Clean
clean:
	rm -rf bin/
	rm -rf web/dist/
	rm -rf web/packages/*/dist

# Install the systemd services
install-services:
	cp deploy/systemd/openwebpanel-parent.service /etc/systemd/system/
	cp deploy/systemd/openwebpanel-child@.service /etc/systemd/system/
	systemctl daemon-reload
	systemctl enable openwebpanel-parent

# Create backup
backup:
	tar --exclude='node_modules' --exclude='.git' --exclude='*.db-shm' --exclude='*.db-wal' -czf "../openwebpanel-backup-$$(date +%Y%m%d-%H%M%S).tar.gz" .

# Help
help:
	@echo "OpenWebPanel Makefile"
	@echo "  build             - Build all Go binaries"
	@echo "  build-prod        - Build production binaries + frontend"
	@echo "  build-frontend    - Build frontend only"
	@echo "  build-backend     - Build backend only"
	@echo "  dev-backend       - Run backend in dev mode"
	@echo "  dev-frontend      - Run frontend dev server"
	@echo "  run-parent        - Run parent daemon"
	@echo "  run-child         - Run child daemon"
	@echo "  docker-build      - Build Docker images"
	@echo "  docker-up         - Start Docker services"
	@echo "  install-frontend  - Install npm dependencies"
	@echo "  install-services  - Install systemd services"
	@echo "  backup            - Create project backup"
	@echo "  test              - Run Go tests"
	@echo "  clean             - Remove build artifacts"
