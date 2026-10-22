# syntax=docker/dockerfile:1

# --------------------------------------------------------------------------
# 1. Build the React bundle
# --------------------------------------------------------------------------
FROM node:22-alpine AS web
WORKDIR /src/web

COPY web/package.json web/package-lock.json* ./
RUN npm ci --include=dev || npm install --include=dev

COPY web/ ./
RUN npm run build

# --------------------------------------------------------------------------
# 2. Build the Go server
# --------------------------------------------------------------------------
FROM golang:1.27-alpine AS api
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/server ./cmd/api

# --------------------------------------------------------------------------
# 3. Runtime: static binary + built assets + migrations
# --------------------------------------------------------------------------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget \
 && adduser -D -u 10001 -h /app app

WORKDIR /app

COPY --from=api /out/server /app/server
COPY --from=web /src/web/dist /app/web/dist
COPY migrations /app/migrations

USER app

ENV ADDR=":8080" \
    WEB_DIR="/app/web/dist" \
    MIGRATIONS_DIR="/app/migrations" \
    AUTO_MIGRATE="true"

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/health || exit 1

ENTRYPOINT ["/app/server"]
