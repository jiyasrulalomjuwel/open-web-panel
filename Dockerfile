FROM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package.json ./
RUN npm install
COPY web/ .
RUN npm run build:all

FROM golang:1.25-alpine AS backend-builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o bin/parentd ./cmd/parentd/ && \
    CGO_ENABLED=1 go build -o bin/childd ./cmd/childd/

FROM alpine:3.19
RUN apk add --no-cache nginx php83 php83-fpm php83-mysqli php83-curl php83-mbstring php83-xml \
    mariadb mariadb-client curl tar openssl && \
    KARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') && \
    curl -fsSL "https://dl.k8s.io/release/v1.36.0/bin/linux/${KARCH}/kubectl" -o /usr/local/bin/kubectl && \
    chmod +x /usr/local/bin/kubectl

COPY --from=backend-builder /app/bin/ /app/bin/
COPY --from=frontend-builder /app/web/dist/ /app/web/dist/

ENV OWP_PANEL_PORT=2086 \
    OWP_USER_PORT=2082 \
    OWP_ADMIN_LISTEN=127.0.0.1:9000 \
    OWP_CHILD_LISTEN=127.0.0.1:9001 \
    OWP_ADMIN_STATIC_DIR=/app/web/dist/admin \
    OWP_CHILD_STATIC_DIR=/app/web/dist/child \
    OWP_DB_PATH=/app/data/openwebpanel.db \
    OWP_HOMES_BASE=/app/homes/ \
    PHP_FPM_SOCKET=/run/php/php8.3-fpm.sock

RUN mkdir -p /app/data /app/homes /etc/nginx/vhosts /var/log/nginx && \
    chmod 755 /app/bin/parentd /app/bin/childd

EXPOSE 21 80 443 2082 2086 2525 40000-40009

WORKDIR /app

COPY deploy/docker/entrypoint.sh /entrypoint.sh
COPY deploy/docker/child-entrypoint.sh /usr/local/bin/child-entrypoint.sh
COPY deploy/docker/owp-nginx-config.sh /usr/local/bin/owp-nginx-config.sh
COPY deploy/k3s/install-worker-deps.sh /usr/local/share/openwebpanel/install-worker-deps.sh
COPY deploy/k3s/worker-diagnose.sh /usr/local/share/openwebpanel/worker-diagnose.sh
RUN chmod +x /entrypoint.sh /usr/local/bin/child-entrypoint.sh /usr/local/bin/owp-nginx-config.sh
ENTRYPOINT ["/entrypoint.sh"]
