# Multi-stage Dockerfile for VigilAgent API Gateway
# Target 'dev' for local development, target 'prod' for production image

FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/vigil-api ./cmd/api

# Production Stage
FROM alpine:3.21 AS prod
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/vigil-api /app/vigil-api
EXPOSE 8080
USER nobody
ENTRYPOINT ["/app/vigil-api"]

# Development Stage
FROM golang:1.26-alpine AS dev
WORKDIR /app
COPY . .
EXPOSE 8080
CMD ["go", "run", "./cmd/api"]
