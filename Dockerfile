# Build stage
FROM golang:1.26-alpine AS builder

# Install git (required for fetching dependencies)
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o vigilagent .

# Runtime stage
FROM alpine:latest

# Install ca-certificates for HTTPS calls
RUN apk --no-cache add ca-certificates

# Set working directory
WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/vigilagent .

# Copy config files
COPY --from=builder /app/configs ./configs

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./vigilagent"]
