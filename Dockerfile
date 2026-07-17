FROM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package.json ./
RUN npm install
COPY web/ .
RUN npm run build:all

FROM golang:1.25-alpine AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o bin/parentd ./cmd/parentd/ && \
    go build -o bin/childd ./cmd/childd/

FROM alpine:3.19
RUN apk add --no-cache nginx php83 php83-fpm php83-mysqli php83-curl php83-mbstring php83-xml \
    mariadb mariadb-client curl tar

COPY --from=backend-builder /app/bin/ /app/bin/
COPY --from=frontend-builder /app/web/dist/ /app/web/dist/

EXPOSE 80 443 2082 2086 9000

WORKDIR /app

COPY deploy/docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
