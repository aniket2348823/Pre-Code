# Contributing to VigilAgent

Thank you for your interest in contributing to VigilAgent. This document covers everything you need to get started.

---

## Table of Contents

- [Development Setup](#development-setup)
- [Project Structure](#project-structure)
- [Development Workflow](#development-workflow)
- [Coding Standards](#coding-standards)
- [Commit Conventions](#commit-conventions)
- [Pull Request Process](#pull-request-process)
- [Testing Requirements](#testing-requirements)
- [Code Quality Tools](#code-quality-tools)
- [Architecture Guidelines](#architecture-guidelines)
- [Adding a New LLM Provider](#adding-a-new-llm-provider)
- [Adding a New API Endpoint](#adding-a-new-api-endpoint)
- [Adding a New Middleware](#adding-a-new-middleware)

---

## Development Setup

### Prerequisites

- Go 1.26+
- Docker & Docker Compose
- Make
- Git

### Clone and Setup

```bash
git clone https://github.com/vigilagent/vigilagent.git
cd vigilagent

# Copy environment template
cp .env.example .env

# Start infrastructure
docker compose -f docker-compose.dev.yml up -d

# Run database migrations
make migrate

# Verify setup
make test
make run
```

### VS Code Configuration

The repository includes VS Code settings in `.vscode/`. Recommended extensions:
- **Go** (`golang.go`) — Go language support
- **golangci-lint** — Real-time linting feedback
- **EditorConfig** — Consistent formatting

---

## Project Structure

```
cmd/                    # Binary entry points (do not add business logic here)
internal/               # Core application packages (67 packages)
├── <domain>/           # Each package owns a single business capability
│   ├── <domain>.go     # Implementation
│   └── <domain>_test.go # Tests
pkg/                    # Reusable packages safe for external import
migrations/             # SQL migration files (sequential numbering)
configs/                # YAML configuration templates
docs/                   # Extended documentation
scripts/                # Build and utility scripts
```

### Key Rules

1. **`cmd/` is thin**: `main.go` files should only initialize dependencies and start the server. No business logic.
2. **`internal/` is private**: These packages cannot be imported by external projects. This is intentional.
3. **`pkg/` is public**: Only stable, well-documented, general-purpose code belongs here.
4. **One package, one responsibility**: Each `internal/` package owns exactly one business capability.

---

## Development Workflow

### 1. Create a Branch

```bash
git checkout -b feature/your-feature-name
# or
git checkout -b fix/issue-description
```

### 2. Make Changes

Follow the coding standards and architecture guidelines below.

### 3. Test Your Changes

```bash
# Run all tests
make test

# Run tests with race detector
make test-race

# Run linter
make lint

# Verify build
make all
```

### 4. Commit and Push

```bash
git add .
git commit -m "feat(router): add webhook retry endpoint"
git push origin feature/your-feature-name
```

### 5. Open a Pull Request

Open a PR against `main` with a clear description of your changes.

---

## Coding Standards

### Go Style

- Follow the [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) guidelines
- Use `gofmt` formatting (enforced by `make fmt`)
- Keep functions under 50 lines where possible
- Keep files under 500 lines; split if larger

### Naming Conventions

| Element          | Convention                    | Example                      |
|------------------|-------------------------------|------------------------------|
| Packages         | lowercase, single word        | `scanner`, `costintel`       |
| Exported funcs   | PascalCase, verb-first        | `CreateUser`, `ValidateToken`|
| Unexported funcs | camelCase                     | `parseConfig`, `buildQuery`  |
| Interfaces       | PascalCase, `-er` suffix      | `Scanner`, `Provider`        |
| Test functions   | `Test<Function>_<Scenario>`   | `TestLogin_InvalidPassword`  |
| Constants        | PascalCase                    | `MaxRetries`, `DefaultTimeout`|
| Files            | snake_case                    | `cost_intel.go`, `auth_handler.go` |

### Error Handling

- Always wrap errors with context: `fmt.Errorf("creating user: %w", err)`
- Use domain-specific error types from `internal/errors/`
- Never ignore errors silently. If you intentionally discard, comment why.

### Imports

Organize imports in three groups separated by blank lines:

```go
import (
    // Standard library
    "context"
    "fmt"
    "net/http"

    // Third-party packages
    "github.com/go-chi/chi/v5"
    "github.com/stretchr/testify/assert"

    // Internal packages
    "github.com/vigilagent/vigilagent/internal/auth"
    "github.com/vigilagent/vigilagent/internal/config"
)
```

### Documentation

- All exported functions, types, and constants must have doc comments
- Doc comments should start with the element name: `// CreateUser creates a new user account.`
- Package-level doc comments should describe the package's purpose

---

## Commit Conventions

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

[optional body]
```

### Types

| Type       | Use For                                           |
|------------|---------------------------------------------------|
| `feat`     | New feature                                       |
| `fix`      | Bug fix                                           |
| `docs`     | Documentation only                                |
| `test`     | Adding or fixing tests                            |
| `refactor` | Code restructuring without behavior change        |
| `perf`     | Performance improvement                           |
| `chore`    | Build, CI, dependency updates                     |
| `security` | Security fix or improvement                       |

### Scopes

Use the package name as the scope: `router`, `llm`, `scanner`, `middleware`, `config`, `auth`, `agent`, etc.

### Examples

```
feat(llm): add Groq provider driver
fix(auth): handle expired JWT refresh tokens
docs(api): document webhook replay endpoint
test(scanner): add fuzz tests for Bandit adapter
refactor(router): split scan handlers into dedicated file
perf(proxy): enable HTTP connection pooling
chore(deps): update go-openai to v1.41.2
security(middleware): add CSRF token validation
```

---

## Pull Request Process

### PR Requirements

- [ ] All tests pass (`make test`)
- [ ] No race conditions (`make test-race`)
- [ ] Linter passes (`make lint`)
- [ ] Build succeeds (`make all`)
- [ ] New code has tests (aim for >80% coverage on new code)
- [ ] Documentation updated if adding/changing public APIs
- [ ] Commit messages follow Conventional Commits

### PR Description Template

```markdown
## Summary
Brief description of what this PR does.

## Changes
- Added/modified/removed X
- Added/modified/removed Y

## Testing
- How was this tested?
- Any manual testing required?

## Breaking Changes
- List any breaking changes (or "None")
```

### Review Process

1. At least one approval required before merging
2. All CI checks must pass
3. Squash merge into `main`

---

## Testing Requirements

### New Feature Tests

Every new feature must include:

1. **Unit tests** for all exported functions using table-driven tests
2. **Error case tests** covering invalid inputs, edge cases, and failure paths
3. **Integration tests** if the feature interacts with other packages

### Test Coverage

- New code should target >80% line coverage
- Critical paths (auth, security, scanning) should target >90%
- Run `make test-cover` to generate and review coverage reports

### Testing Utilities

The `internal/router/test_helpers_test.go` file provides shared test utilities for HTTP handler tests. The `internal/e2e/` and `internal/integration/` packages provide harnesses for integration and end-to-end tests.

See [docs/TESTING.md](docs/TESTING.md) for the complete testing guide.

---

## Code Quality Tools

### Makefile Targets

```bash
make lint         # Run golangci-lint with project rules
make fmt          # Format all Go files with gofmt
make vet          # Run go vet for static analysis
make tidy         # Tidy go.mod and go.sum
make security     # Run govulncheck + gosec
make check        # Run all quality checks (lint + vet + test)
```

### golangci-lint Configuration

The `.golangci.yml` file configures project-wide linting rules. Key enabled linters:
- `errcheck` — Check for unchecked errors
- `govet` — Suspicious constructs
- `staticcheck` — Advanced static analysis
- `gosec` — Security-focused analysis
- `ineffassign` — Detect ineffectual assignments
- `unused` — Find unused code

---

## Architecture Guidelines

### Adding a New Package

1. Create the package under `internal/<packagename>/`
2. Keep the package focused on a single responsibility
3. Define interfaces for any dependencies
4. Create `<packagename>.go` and `<packagename>_test.go`
5. Do not introduce circular dependencies

### Dependency Rules

```
cmd/ → internal/ → pkg/
cmd/ → internal/ (never the reverse)
internal/router/ → internal/auth/, internal/llm/, etc.
internal/auth/ → internal/database/ (never internal/router/)
```

- Packages should depend **downward** (handler → service → repository → database)
- Never create circular imports
- Use interfaces to break dependency cycles

### File Size Limits

- Source files: **500 lines maximum**. Split into logically named files if exceeding this.
- Test files: No strict limit, but prefer splitting by test category (e.g., `_deep_test.go`, `_integration_test.go`)

---

## Adding a New LLM Provider

To add a new LLM provider (e.g., "Mistral"):

### 1. Create the Driver File

```go
// internal/llm/newprovider.go
package llm

type NewProvider struct {
    apiKey string
    client *http.Client
}

func NewNewProvider(apiKey string) *NewProvider {
    return &NewProvider{
        apiKey: apiKey,
        client: &http.Client{Timeout: 30 * time.Second},
    }
}

func (p *NewProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
    // Implementation
}

func (p *NewProvider) Name() string {
    return "newprovider"
}
```

### 2. Add Models to Price Database

In `internal/llm/models.go`, add the model pricing:

```go
"newprovider-model-large": {
    Name:       "newprovider-model-large",
    Provider:   "newprovider",
    InputPer1K:  0.003,
    OutputPer1K: 0.015,
},
```

### 3. Register in Provider Engine

In `internal/llm/provider.go`, register the new provider:

```go
if cfg.NewProviderKey != "" {
    engine.Register("newprovider", NewNewProvider(cfg.NewProviderKey))
}
```

### 4. Add Configuration

In `internal/config/`, add the new API key field:

```go
type LLMConfig struct {
    // ... existing keys
    NewProviderKey string `mapstructure:"newprovider_key"`
}
```

### 5. Write Tests

Create `internal/llm/newprovider_test.go` with:
- Table-driven tests for various completion scenarios
- Error handling tests
- Streaming tests (if supported)

### 6. Update Documentation

Update the provider table in `README.md` and `docs/ARCHITECTURE.md`.

---

## Adding a New API Endpoint

### 1. Define the Route

In `internal/router/routes.go`:

```go
protected.Get("/new-endpoint", r.handleNewEndpoint)
```

### 2. Implement the Handler

In the appropriate handler file (e.g., `internal/router/system_handlers.go`):

```go
func (rt *Router) handleNewEndpoint(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    // Implementation
    response.JSON(w, http.StatusOK, result)
}
```

### 3. Add Request/Response Types

In `internal/api/`:

```go
type NewEndpointRequest struct {
    Field string `json:"field" validate:"required"`
}

type NewEndpointResponse struct {
    Result string `json:"result"`
}
```

### 4. Write Handler Tests

In the corresponding test file:

```go
func TestHandleNewEndpoint_Success(t *testing.T) {
    // Test implementation using httptest
}
```

### 5. Update OpenAPI Spec

Add the endpoint definition to `internal/router/openapi.yaml`.

---

## Adding a New Middleware

### 1. Create the Middleware

In `internal/middleware/`:

```go
// newmiddleware.go
package middleware

func NewMiddleware(config Config) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Pre-processing
            next.ServeHTTP(w, r)
            // Post-processing
        })
    }
}
```

### 2. Wire It In

Add to the middleware chain in `internal/router/middleware_wiring.go`:

```go
router.Use(middleware.NewMiddleware(config))
```

### 3. Write Tests

```go
// newmiddleware_test.go
func TestNewMiddleware_AllowsValidRequests(t *testing.T) {
    // Test implementation
}

func TestNewMiddleware_BlocksInvalidRequests(t *testing.T) {
    // Test implementation
}
```
