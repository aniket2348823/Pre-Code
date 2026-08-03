# Contributing to VigilAgent

Thank you for your interest in contributing to VigilAgent! This guide will help you get started.

## Getting Started

### Prerequisites

- Go 1.26+
- PostgreSQL 16+
- Redis 7+
- Docker (optional, for local development)

### Setup

```bash
# Clone the repository
git clone https://github.com/vigilagent/vigilagent.git
cd vigilagent

# Start infrastructure
docker-compose -f docker-compose.dev.yml up -d

# Run migrations
make migrate

# Run tests
make test-short
```

## Development Workflow

### Branch Naming

- `feature/description` — New features
- `fix/description` — Bug fixes
- `docs/description` — Documentation changes
- `refactor/description` — Code refactoring

### Commit Messages

Follow Conventional Commits:
```
feat: add OAuth2 login support
fix: resolve race condition in task execution
docs: update API documentation
refactor: extract service layer from handlers
```

### Code Style

- Follow Go conventions (`gofmt`, `goimports`)
- Use `golangci-lint` for linting
- Write meaningful comments for exported functions
- Keep functions focused and small

### Testing

```bash
# Run all tests
make test

# Run short tests (no DB required)
make test-short

# Run with race detector
make test-race

# Generate coverage report
make test-cover
```

### Pull Request Process

1. Create a feature branch from `main`
2. Make your changes
3. Add/update tests
4. Ensure all checks pass (`make check`)
5. Submit a pull request

## Adding New Features

### Adding a New LLM Provider

1. Create `internal/llm/{provider}.go`
2. Implement the `Provider` interface:
   ```go
   type Provider interface {
       Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
       Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error)
       HealthCheck(ctx context.Context) error
       Name() string
   }
   ```
3. Add provider to `internal/llm/models.go` with pricing
4. Add provider to `internal/proxy/providers.go` routing
5. Write tests in `internal/llm/{provider}_test.go`

### Adding a New Tool

1. Create `internal/tools/{tool}.go`
2. Implement the `Tool` interface:
   ```go
   type Tool interface {
       Name() string
       Description() string
       Execute(ctx context.Context, params map[string]interface{}) (interface{}, error)
   }
   ```
3. Register in `internal/tools/registry.go`
4. Write tests

### Adding a New API Endpoint

1. Create handler in appropriate `internal/router/{domain}_handlers.go`
2. Add route in `internal/router/router.go`
3. Add OpenAPI spec in `internal/router/openapi.yaml`
4. Write handler tests

## Architecture Decisions

### Why chi over gorilla/mux?

chi is lighter, more idiomatic, and supports middleware composition better.

### Why pgvector for semantic memory?

Native vector support in PostgreSQL avoids external dependencies and enables hybrid queries.

### Why NATS over RabbitMQ?

Simpler operational model, built-in JetStream for persistence, better performance.

## Getting Help

- Open an issue for bugs
- Start a discussion for questions
- Join our Discord for real-time chat

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
