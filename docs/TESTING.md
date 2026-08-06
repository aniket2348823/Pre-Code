# Testing Guide

> Complete reference for VigilAgent's testing strategy, patterns, and execution.

---

## Table of Contents

- [Overview](#overview)
- [Test Suite Statistics](#test-suite-statistics)
- [Running Tests](#running-tests)
- [Test Architecture](#test-architecture)
- [Testing Patterns](#testing-patterns)
- [Package Test Coverage](#package-test-coverage)
- [Benchmarks](#benchmarks)
- [Load Testing](#load-testing)
- [Fuzz Testing](#fuzz-testing)
- [Integration Testing](#integration-testing)
- [End-to-End Testing](#end-to-end-testing)
- [CI/CD Integration](#cicd-integration)
- [Writing New Tests](#writing-new-tests)

---

## Overview

VigilAgent maintains a comprehensive test suite using Go's standard `testing` package with [testify](https://github.com/stretchr/testify) for assertions and mocking. The test philosophy prioritizes:

1. **Table-driven tests** for exhaustive input coverage
2. **Interface-based mocking** for dependency isolation
3. **Race detection** for concurrency safety
4. **Fuzz testing** for input validation hardening

---

## Test Suite Statistics

| Metric                 | Count   |
|------------------------|---------|
| Test files             | 133     |
| Packages with tests    | 47      |
| Test patterns          | Table-driven, mocks, concurrency, fuzz |

### Top Tested Packages

| Package              | Test Files | Coverage Area                                      |
|----------------------|------------|---------------------------------------------------|
| `internal/router`    | 23         | HTTP handlers, route wiring, OpenAPI, WebSocket   |
| `internal/llm`       | 16         | Provider drivers, routing, failover, circuit breaker |
| `internal/middleware` | 15         | Auth, rate limit, CORS, compression, tracing      |
| `internal/api`       | 11         | Request/response contracts and validation         |
| `internal/repository`| 11         | Data access layer CRUD operations                 |
| `internal/scanner`   | 8          | SAST engines, findings, accuracy                  |
| `internal/tools`     | 6          | Tool registry, file ops, shell sandbox            |
| `internal/config`    | 5          | Configuration loading, validation, edge cases     |
| `internal/skills`    | 5          | Manifest parsing, installation, RAG search        |
| `internal/database`  | 5          | Pool init, migrations, Redis, observability       |

---

## Running Tests

### Quick Reference

```bash
# Run all unit tests
make test

# Run tests with -short flag (skip slow tests)
make test-short

# Run tests with Go's race detector
make test-race

# Generate HTML coverage report
make test-cover

# Run integration tests only
make test-integration

# Run benchmarks
make bench
```

### Manual Test Commands

```bash
# Run all tests with verbose output
go test -v -count=1 ./...

# Run tests for a specific package
go test -v ./internal/llm/...

# Run a specific test function
go test -v -run TestProviderEngine_Failover ./internal/llm/...

# Run tests with coverage output
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Run tests with race detector and timeout
go test -race -timeout=5m ./...

# Run benchmarks for a specific package
go test -bench=. -benchmem ./internal/proxy/...
```

---

## Test Architecture

### Directory Structure

Test files live alongside the source code they test:

```
internal/
├── llm/
│   ├── provider.go              # Source
│   ├── provider_test.go         # Unit tests
│   ├── provider_bench.go        # Benchmark utilities
│   ├── provider_bench_test.go   # Benchmark tests
│   ├── provider_failover_test.go # Specialized test suite
│   ├── openai.go                # OpenAI driver
│   └── openai_test.go           # OpenAI driver tests
├── router/
│   ├── router.go
│   ├── router_test.go
│   ├── test_helpers_test.go     # Shared test utilities
│   └── handler_integration_test.go  # Integration tests
└── auth/
    ├── jwt.go
    ├── jwt_test.go
    └── jwt_fuzz_test.go         # Fuzz tests
```

### Test Categories

| Category      | File Pattern            | Purpose                              |
|---------------|-------------------------|--------------------------------------|
| Unit          | `*_test.go`             | Isolated function/method tests       |
| Deep          | `*_deep_test.go`        | Extended coverage for critical paths |
| Integration   | `*_integration_test.go` | Cross-package interaction tests      |
| Benchmark     | `*_bench_test.go`       | Performance measurement              |
| Fuzz          | `*_fuzz_test.go`        | Randomized input testing             |
| E2E           | `internal/e2e/`         | Full system end-to-end tests         |

---

## Testing Patterns

### 1. Table-Driven Tests

The standard testing pattern throughout the codebase. Each test case is a named struct in a slice:

```go
func TestSanitizeInput(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"empty string", "", ""},
        {"removes null bytes", "hello\x00world", "helloworld"},
        {"preserves newlines", "line1\nline2", "line1\nline2"},
        {"strips control chars", "test\x01\x02data", "testdata"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := SanitizeInput(tt.input)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

### 2. HTTP Handler Testing

Uses `httptest.NewRecorder()` and `chi.NewRouter()` for testing HTTP handlers without starting a server:

```go
func TestHealthEndpoint(t *testing.T) {
    router := chi.NewRouter()
    router.Get("/health", handler.Health)

    req := httptest.NewRequest("GET", "/health", nil)
    rec := httptest.NewRecorder()
    router.ServeHTTP(rec, req)

    assert.Equal(t, http.StatusOK, rec.Code)

    var resp map[string]string
    json.Unmarshal(rec.Body.Bytes(), &resp)
    assert.Equal(t, "healthy", resp["status"])
}
```

### 3. Mock-Based Dependency Injection

Services depend on interfaces, allowing test doubles:

```go
// Production: real database pool
type UserRepository interface {
    GetByID(ctx context.Context, id string) (*User, error)
}

// Test: mock implementation
type mockUserRepo struct {
    users map[string]*User
}

func (m *mockUserRepo) GetByID(ctx context.Context, id string) (*User, error) {
    user, ok := m.users[id]
    if !ok {
        return nil, ErrNotFound
    }
    return user, nil
}
```

### 4. Concurrency Safety Testing

Critical data structures are tested for thread safety:

```go
func TestConcurrentCostTracking(t *testing.T) {
    engine := NewCostIntelEngine()
    var wg sync.WaitGroup

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            engine.RecordCost(CostRecord{
                Model:  "gpt-4o",
                Tokens: 1000,
                Cost:   0.03,
            })
        }(i)
    }

    wg.Wait()
    assert.Equal(t, 100, len(engine.History()))
}
```

### 5. Testify Assertions

The codebase uses `testify/assert` and `testify/require` for readable assertions:

```go
// assert: test continues on failure
assert.Equal(t, expected, actual)
assert.NoError(t, err)
assert.Contains(t, list, item)
assert.Len(t, slice, 5)

// require: test stops on failure
require.NoError(t, err)  // Fatal if error
result := doSomething()   // Only runs if no error above
```

---

## Package Test Coverage

### Core Domain Packages

| Package                   | Tests Cover                                         |
|---------------------------|-----------------------------------------------------|
| `internal/agent`          | State machine transitions, execution loop, HITL     |
| `internal/auth`           | JWT generation/verification, API key hashing, bcrypt|
| `internal/llm`            | All 9 provider drivers, routing, failover, circuit breaker, caching, health monitoring |
| `internal/router`         | All domain handlers, route registration, middleware wiring, OpenAPI validation |
| `internal/scanner`        | Built-in regex, Semgrep, Bandit engines, finding reconciliation, accuracy |
| `internal/middleware`     | Auth, rate limiting, CORS, compression, request ID, security headers, HITL, lockout |

### Infrastructure Packages

| Package                   | Tests Cover                                         |
|---------------------------|-----------------------------------------------------|
| `internal/config`         | Viper loading, env binding, production validation, hot reload |
| `internal/database`       | Pool initialization, migration parsing, Redis adapter, session management |
| `internal/queue`          | NATS JetStream worker, dead-letter retries          |
| `internal/cache`          | Redis caching with TTL, cache invalidation          |
| `internal/repository`     | User, Org, Project, Task, Agent, Skill, Alert, Session, API key CRUD |

### Security Packages

| Package                   | Tests Cover                                         |
|---------------------------|-----------------------------------------------------|
| `internal/security`       | Input sanitization, AES encryption, credential detection |
| `internal/ipfilter`       | CIDR filtering, IP extraction, trusted proxies      |
| `internal/cors`           | Origin matching, preflight handling, credentials    |
| `internal/signing`        | HMAC-SHA256, Ed25519 payload signing                |
| `internal/secrets`        | High-entropy secret detection                       |

---

## Benchmarks

Run benchmarks across performance-critical packages:

```bash
make bench
```

This executes benchmarks in:
- `internal/proxy/` — LLM proxying throughput
- `internal/scanner/` — Scanning engine performance
- `internal/llm/` — Provider routing overhead

### Benchmark Output Example

```
BenchmarkProxyRouting-8          50000    28450 ns/op    4096 B/op    12 allocs/op
BenchmarkScannerEngine-8         10000   142300 ns/op   16384 B/op    45 allocs/op
BenchmarkProviderSelection-8    100000    10240 ns/op    2048 B/op     8 allocs/op
```

### Custom Load Testing

The `cmd/loadtest` binary runs configurable HTTP load tests:

```bash
go run ./cmd/loadtest \
  -url http://localhost:8080/api/v1/health \
  -workers 100 \
  -requests 10000
```

Output includes:
- Total requests completed
- Throughput (requests/second)
- Latency percentiles (P50, P95, P99)
- Error rate

---

## Fuzz Testing

Fuzz tests use Go's built-in fuzzing framework (`go test -fuzz`) to discover edge cases:

### Available Fuzz Targets

| File                               | Fuzz Target              | Tests                          |
|------------------------------------|--------------------------|--------------------------------|
| `internal/auth/jwt_fuzz_test.go`   | `FuzzJWTValidation`      | Malformed JWT token handling   |
| `internal/middleware/auth_fuzz.go`  | `FuzzAuthMiddleware`     | Auth header parsing edge cases |
| `internal/middleware/requestid_fuzz.go` | `FuzzRequestID`    | Request ID generation          |
| `internal/router/openapi_fuzz.go`  | `FuzzOpenAPIValidation`  | OpenAPI spec parsing           |

### Running Fuzz Tests

```bash
# Run a specific fuzz target for 30 seconds
go test -fuzz=FuzzJWTValidation -fuzztime=30s ./internal/auth/...

# Run with race detector
go test -fuzz=FuzzJWTValidation -fuzztime=1m -race ./internal/auth/...
```

---

## Integration Testing

Integration tests verify cross-package interactions and require infrastructure services:

```bash
make test-integration
```

### Integration Test Packages

| Package                 | Tests                                              |
|-------------------------|----------------------------------------------------|
| `internal/integration`  | Full API workflow tests with test server harness   |
| `internal/e2e`          | End-to-end tests with mock environment setup       |
| `internal/router`       | Handler integration tests with real middleware     |

### Test Server Harness

The `internal/integration/` package provides a reusable test server:

```go
func TestFullWorkflow(t *testing.T) {
    // Start test server with all dependencies
    srv := integration.NewTestServer(t)
    defer srv.Close()

    // Register user
    resp := srv.POST("/api/v1/auth/register", registerPayload)
    require.Equal(t, 201, resp.StatusCode)

    // Login
    resp = srv.POST("/api/v1/auth/login", loginPayload)
    token := extractToken(resp)

    // Create project with auth
    resp = srv.WithAuth(token).POST("/api/v1/projects", projectPayload)
    require.Equal(t, 201, resp.StatusCode)
}
```

---

## CI/CD Integration

### GitHub Actions

The `.github/` directory contains CI workflows that run on every push and pull request:

```yaml
# Typical CI pipeline
steps:
  - name: Lint
    run: make lint

  - name: Test
    run: make test-race

  - name: Coverage
    run: make test-cover

  - name: Security
    run: make security

  - name: Build
    run: make all
```

### Quality Gates

| Check              | Command            | Failure Criteria              |
|--------------------|--------------------|-------------------------------|
| Linting            | `make lint`        | Any golangci-lint violation   |
| Unit Tests         | `make test`        | Any test failure              |
| Race Detection     | `make test-race`   | Any data race detected        |
| Security Scan      | `make security`    | Any known vulnerability       |
| Build              | `make all`         | Compilation failure           |

---

## Writing New Tests

### Conventions

1. **File naming**: Test files use `_test.go` suffix alongside the source file
2. **Function naming**: `TestFunctionName_Scenario` (e.g., `TestLogin_InvalidPassword`)
3. **Table-driven**: Use table-driven tests for functions with multiple input cases
4. **Assertions**: Use `testify/assert` for non-fatal checks, `testify/require` for fatal ones
5. **Cleanup**: Use `t.Cleanup()` for resource cleanup, not `defer` in subtests
6. **Parallel**: Mark independent tests with `t.Parallel()` for faster execution

### Template

```go
package mypackage

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestMyFunction_Success(t *testing.T) {
    // Arrange
    input := "test-input"
    expected := "expected-output"

    // Act
    result, err := MyFunction(input)

    // Assert
    require.NoError(t, err)
    assert.Equal(t, expected, result)
}

func TestMyFunction_EdgeCases(t *testing.T) {
    tests := []struct {
        name      string
        input     string
        expected  string
        wantError bool
    }{
        {"empty input", "", "", true},
        {"normal input", "hello", "HELLO", false},
        {"unicode input", "héllo", "HÉLLO", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := MyFunction(tt.input)
            if tt.wantError {
                assert.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```
