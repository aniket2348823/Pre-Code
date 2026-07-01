# ── Build stage ──────────────────────────────────────────
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build both binaries
ARG VERSION=dev
ARG BUILD_TIME

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags "-X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -o /app/bin/vigil-api ./cmd/api

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags "-X main.version=${VERSION}" \
    -o /app/bin/vigil-migrate ./cmd/migrate

# ── Runtime stage ────────────────────────────────────────
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata

# Non-root user
RUN addgroup -g 1001 -S vigil && \
    adduser -S vigil -u 1001 -G vigil

WORKDIR /app

COPY --from=builder /app/bin/vigil-api .
COPY --from=builder /app/bin/vigil-migrate .
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/configs ./configs

RUN chown -R vigil:vigil /app

USER vigil

EXPOSE 8080

ENTRYPOINT ["./vigil-api"]
