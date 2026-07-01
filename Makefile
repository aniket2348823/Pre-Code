# VigilAgent Makefile
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"
BINARY_DIR := ./bin

# Go parameters
GOCMD    := go
GOBUILD  := $(GOCMD) build
GOTEST   := $(GOCMD) test
GOMOD    := $(GOCMD) mod
GOFMT    := gofmt

# Migration
MIGRATIONS_PATH := ./migrations
DB_URL := postgres://vigilagent:vigilagent@localhost:5432/vigilagent?sslmode=disable

# Docker
DOCKER_IMAGE := vigilagent
DOCKER_TAG   := latest

.PHONY: all build build-api build-migrate run test test-short test-race test-cover test-integration \
	lint fmt tidy vet clean migrate migrate-up migrate-down migrate-create \
	docker-build docker-run security check help

# ── Default ──────────────────────────────────────────────
all: build

# ── Build ────────────────────────────────────────────────
build: build-api build-migrate

build-api:
	@echo "Building vigil-api..."
	@CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BINARY_DIR)/vigil-api ./cmd/api
	@echo "→ $(BINARY_DIR)/vigil-api"

build-migrate:
	@echo "Building vigil-migrate..."
	@CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BINARY_DIR)/vigil-migrate ./cmd/migrate
	@echo "→ $(BINARY_DIR)/vigil-migrate"

run:
	@$(GOCMD) run ./cmd/api

# ── Test ─────────────────────────────────────────────────
test:
	@echo "Running all tests..."
	@$(GOTEST) ./...

test-short:
	@echo "Running short tests..."
	@$(GOTEST) -short -count=1 ./...

test-race:
	@echo "Running tests with race detector..."
	@CGO_ENABLED=1 $(GOTEST) -short -race -count=1 ./...

test-cover:
	@echo "Running tests with coverage..."
	@CGO_ENABLED=1 $(GOTEST) -short -race -coverprofile=coverage.out -covermode=atomic ./...
	@$(GOCMD) tool cover -func=coverage.out
	@$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-integration:
	@echo "Running integration tests (requires Docker)..."
	@$(GOTEST) -tags=integration -v ./...

# ── Quality ──────────────────────────────────────────────
lint:
	@golangci-lint run ./...

fmt:
	@$(GOFMT) -s -w .
	@goimports -w . 2>/dev/null || true

vet:
	@$(GOCMD) vet ./...

tidy:
	@$(GOMOD) tidy
	@$(GOMOD) verify

security:
	@gosec ./... 2>/dev/null || echo "gosec not installed, skipping"
	@$(GOCMD) install golang.org/x/vuln/cmd/govulncheck@latest
	@govulncheck ./...

check: fmt vet test-short
	@echo "All checks passed!"

# ── Migrate ──────────────────────────────────────────────
migrate:
	@$(GOCMD) run ./cmd/migrate up

migrate-up:
	@$(GOCMD) run ./cmd/migrate up

migrate-version:
	@$(GOCMD) run ./cmd/migrate version

# ── Docker ───────────────────────────────────────────────
docker-build:
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-run:
	@docker run -p 8080:8080 --env-file .env $(DOCKER_IMAGE):$(DOCKER_TAG)

# ── Clean ────────────────────────────────────────────────
clean:
	@rm -rf $(BINARY_DIR)
	@rm -f coverage.out coverage.html

# ── Help ─────────────────────────────────────────────────
help:
	@echo "VigilAgent Makefile"
	@echo ""
	@echo "Build:"
	@echo "  make build          Build all binaries (api + migrate)"
	@echo "  make build-api      Build API server binary"
	@echo "  make build-migrate  Build migration tool binary"
	@echo "  make run            Run the API server"
	@echo ""
	@echo "Test:"
	@echo "  make test           Run all tests"
	@echo "  make test-short     Run tests (skip integration)"
	@echo "  make test-race      Run tests with race detector"
	@echo "  make test-cover     Run tests with coverage report"
	@echo "  make test-integration  Run integration tests (Docker required)"
	@echo ""
	@echo "Quality:"
	@echo "  make lint           Run golangci-lint"
	@echo "  make fmt            Format code"
	@echo "  make vet            Run go vet"
	@echo "  make tidy           Tidy and verify go modules"
	@echo "  make security       Run security scans"
	@echo "  make check          Run fmt + vet + test-short"
	@echo ""
	@echo "Migrate:"
	@echo "  make migrate        Apply pending migrations"
	@echo "  make migrate-version Show current migration version"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build   Build Docker image"
	@echo "  make docker-run     Run Docker container"
	@echo ""
	@echo "  make clean          Remove build artifacts"
	@echo "  make help           Show this help"
