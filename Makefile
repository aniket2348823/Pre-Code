# VigilAgent Makefile

# Go parameters
GOCMD = go
GOBUILD = $(GOCMD) build
GOCLEAN = $(GOCMD) clean
GOTEST = $(GOCMD) test
GOGET = $(GOCMD) get
GOMOD = $(GOCMD) mod
GOFMT = gofmt

# Binary name
BINARY_NAME = vigilagent
BINARY_PATH = ./bin/$(BINARY_NAME)

# Main package
MAIN_PATH = ./main.go

# Docker
DOCKER_IMAGE = vigilagent
DOCKER_TAG = latest

# Migration
MIGRATIONS_PATH = ./migrations
DB_URL = postgres://vigilagent:vigilagent@localhost:5432/vigilagent?sslmode=disable

.PHONY: all build run test test-coverage test-integration lint fmt clean migrate migrate-up migrate-down migrate-create install-tools docker-build docker-run help

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	$(GOBUILD) -o $(BINARY_PATH) $(MAIN_PATH)
	@echo "Build complete: $(BINARY_PATH)"

# Run the application
run:
	@echo "Running $(BINARY_NAME)..."
	$(GOCMD) run $(MAIN_PATH)

# Run all tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v -tags=integration ./...

# Run linter
lint:
	@echo "Running linter..."
	golangci-lint run ./...

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf ./bin
	rm -f coverage.out coverage.html

# Database migrations
migrate:
	@echo "Running migrations..."
	migrate -path $(MIGRATIONS_PATH) -database "$(DB_URL)" up

migrate-up:
	@echo "Running migrations up..."
	migrate -path $(MIGRATIONS_PATH) -database "$(DB_URL)" up 1

migrate-down:
	@echo "Running migrations down..."
	migrate -path $(MIGRATIONS_PATH) -database "$(DB_URL)" down 1

migrate-create:
	@echo "Creating new migration..."
	@if [ -z "$(NAME)" ]; then echo "Usage: make migrate-create NAME=migration_name"; exit 1; fi
	migrate create -ext sql -dir $(MIGRATIONS_PATH) -seq $(NAME)

# Install development tools
install-tools:
	@echo "Installing development tools..."
	$(GOGET) -u github.com/golangci/golangci-lint/cmd/golangci-lint
	$(GOGET) -u github.com/golang-migrate/migrate/v4/cmd/migrate
	$(GOGET) -u github.com/air-verse/air

# Docker
docker-build:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-run:
	@echo "Running Docker container..."
	docker run -p 8080:8080 --env-file .env $(DOCKER_IMAGE):$(DOCKER_TAG)

# Development with hot reload
dev:
	@echo "Starting development server with hot reload..."
	air

# Help
help:
	@echo "Available commands:"
	@echo "  build          - Build the binary"
	@echo "  run            - Run the application"
	@echo "  test           - Run all tests"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  test-integration - Run integration tests"
	@echo "  lint           - Run linter"
	@echo "  fmt            - Format code"
	@echo "  clean          - Clean build artifacts"
	@echo "  migrate        - Run all pending migrations"
	@echo "  migrate-up     - Run next migration"
	@echo "  migrate-down   - Rollback last migration"
	@echo "  migrate-create - Create new migration (NAME=migration_name)"
	@echo "  install-tools  - Install development tools"
	@echo "  docker-build   - Build Docker image"
	@echo "  docker-run     - Run Docker container"
	@echo "  dev            - Start development server with hot reload"
	@echo "  help           - Show this help"
