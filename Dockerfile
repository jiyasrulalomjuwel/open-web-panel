FROM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

FROM golang:1.25-alpine AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o bin/parentd ./cmd/parentd/ && \
    go build -o bin/childd ./cmd/childd/

FROM alpine:3.19
RUN apk add --no-cache nginx php83 php83-fpm php83-mysqli php83-curl php83-mbstring php83-xml \
    mariadb mariadb-client curl tar sudo bash

COPY --from=backend-builder /app/bin/ /app/bin/
COPY --from=frontend-builder /app/web/dist/ /app/web/dist/
COPY --from=backend-builder /app/cmd/parentd/ /app/cmd/parentd/
COPY --from=backend-builder /app/homes/ /app/homes/
COPY --from=backend-builder /app/openwebpanel.db /app/

# Nginx config
COPY nginx/ /etc/nginx/

# PHP config
COPY deploy/php/ /etc/php83/

EXPOSE 80 443 2082 2086 9000 2525

WORKDIR /app
CMD ["/bin/sh", "-c", "/usr/bin/mysqld_safe & sleep 3 && /app/bin/parentd"]
