# VigilAgent — Repository Architecture

> Documenting how requests flow through the system and the responsibilities of each layer.

---

## 1. Request Flow Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        HTTP Request                              │
│  POST /api/v1/projects                                          │
│  Authorization: Bearer eyJhbGci...                              │
│  Content-Type: application/json                                 │
│  X-Request-Id: abc123def456                                    │
└──────────────────────────┬──────────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────────┐
│                    Layer 0: Chi Router                           │
│  Route matching: POST /api/v1/projects                          │
│  URL params extraction: {projectID}                             │
│  Method validation: POST allowed                                │
└──────────────────────────┬──────────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────────┐
│                    Layer 1: Middleware Stack                      │
│  1. RealIP          — Extract real client IP                    │
│  2. Logger          — Log request details                       │
│  3. Recoverer       — Catch panics                              │
│  4. requestid       — Generate/propagate request ID             │
│  5. slogger         — Structured logging context                │
│  6. compression     — Gzip response                             │
│  7. CORS            — Cross-origin headers                      │
│  8. securityHeaders — X-Frame-Options, CSP, HSTS               │
│  9. Timeout         — 30s request timeout                       │
│  10. RateLimitHeaders — X-RateLimit-* headers                   │
│  11. limitBodySize  — 2 MiB body limit                         │
│  12. authRateLimit  — Auth endpoint rate limiting               │
│  13. Sanitize       — SQLi/XSS/path traversal detection         │
│  14. authMiddleware — JWT + API key authentication              │
│  15. apiKeyRateLimit — Per-key rate limiting                    │
└──────────────────────────┬──────────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────────┐
│                    Layer 2: Handler                               │
│  createProjectHandler(w, req)                                   │
│  1. Extract auth claims from context                           │
│  2. Parse request body (JSON decode)                           │
│  3. Validate input (manual checks)                             │
│  4. Check permissions (org membership)                         │
│  5. Call repository to create resource                         │
│  6. Dispatch webhook event                                     │
│  7. Return HTTP response                                       │
└──────────────────────────┬──────────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────────┐
│                    Layer 3: Repository                           │
│  ProjectRepository.Create(ctx, project)                        │
│  1. Build SQL query                                            │
│  2. Execute with parameterized query                           │
│  3. Scan results into Go struct                                │
│  4. Return error or nil                                        │
└──────────────────────────┬──────────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────────┐
│                    Layer 4: Database                             │
│  PostgreSQL 16 + pgvector                                       │
│  Connection pool via pgx/v5                                     │
│  Transactions via pgx.Tx                                       │
└─────────────────────────────────────────────────────────────────┘
```

---

## 2. Layer 0: Chi Router

### File: `internal/router/router.go`

### Responsibilities
- Route definition and matching
- URL parameter extraction (`{projectID}`, `{taskID}`)
- HTTP method validation
- Middleware chain assembly
- Handler registration

### Key Patterns
```go
func (r *Router) setupRoutes() {
    r.Route("/api/v1", func(v1 chi.Router) {
        // Public routes (no auth)
        public := v1.Group(nil)
        public.Use(limitBodySize)
        public.Use(r.authRateLimitMiddleware)
        public.Use(mw.SanitizeMiddleware)
        {
            public.Post("/auth/register", r.registerHandler)
            public.Post("/auth/login", r.loginHandler)
        }

        // Protected routes (auth required)
        protected := v1.Group(nil)
        protected.Use(limitBodySize)
        protected.Use(r.authMiddleware)
        protected.Use(r.apiKeyRateLimitMiddleware)
        {
            protected.Post("/projects", r.createProjectHandler)
            protected.Get("/projects", r.listProjectsHandler)
            // ...
        }
    })
}
```

### Router Struct Dependencies
```go
type Router struct {
    cfg           *config.Config
    db            *database.Conn
    rds           *redis.Client
    nats          *queue.NATS
    auth          *auth.JWT
    apiKeyAuth    *middleware.APIKeyAuth
    lockout       *middleware.Lockout
    email         EmailService
    users         repository.UserRepositoryInterface
    orgs          repository.OrganizationRepositoryInterface
    projects      repository.ProjectRepositoryInterface
    agents        repository.AgentRepositoryInterface
    sessions      repository.SessionRepositoryInterface
    events        repository.EventRepositoryInterface
    tasks         repository.TaskRepositoryInterface
    skills        repository.SkillRepositoryInterface
    alerts        repository.AlertRepositoryInterface
    webhookEngine *webhook.Engine
    skillRAG      *skills.RAGEngine
    skillEngine   *skillengine.Engine
    featureFlags  FeatureFlags
    authSessionMiddleware *middleware.AuthSessionMiddleware
}
```

---

## 3. Layer 1: Middleware Stack

### File: `internal/middleware/`

### Responsibilities
- Cross-cutting concerns (auth, rate limiting, security)
- Request/response transformation
- Context enrichment
- Error handling

### Middleware Execution Order
1. **RealIP** — Extract `X-Forwarded-For` / `X-Real-IP`
2. **Logger** — Log request method, path, status
3. **Recoverer** — Catch panics, return 500
4. **requestid** — Generate/propagate `X-Request-Id`
5. **slogger** — Add request ID to log context
6. **compression** — Gzip compress responses
7. **CORS** — Set `Access-Control-*` headers
8. **securityHeaders** — Set `X-Frame-Options`, `CSP`, etc.
9. **Timeout** — Cancel handler after 30s
10. **RateLimitHeaders** — Set `X-RateLimit-*` headers
11. **limitBodySize** — Limit request body to 2 MiB
12. **authRateLimit** — Redis-backed rate limit for auth endpoints
13. **Sanitize** — Detect SQLi/XSS/path traversal
14. **authMiddleware** — Validate JWT or API key
15. **apiKeyRateLimit** — Redis-backed rate limit per API key

### Key Middleware Implementations

#### Authentication Middleware
```go
func (r *Router) authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
        // 1. Try API key authentication
        if r.apiKeyAuth != nil {
            c, err := r.apiKeyAuth.Authenticate(req)
            if err != nil {
                response.JSON(w, http.StatusUnauthorized, ...)
                return
            }
            if c != nil {
                claims = c
            }
        }

        // 2. Try JWT authentication
        if claims == nil {
            authHeader := req.Header.Get("Authorization")
            // ... parse Bearer token
            // ... validate JWT
            // ... extract claims
        }

        // 3. Inject claims into context
        ctx := auth.ContextWithClaims(req.Context(), claims)
        next.ServeHTTP(w, req.WithContext(ctx))
    })
}
```

#### Rate Limiting Middleware
```go
func (rl *RateLimiter) Middleware(keyFunc func(r *http.Request) string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            key := "ratelimit:" + keyFunc(r)
            now := time.Now().Unix()

            // Execute Lua script for atomic sliding window
            result, err := rateLimitScript.Run(ctx, rl.client, []string{key},
                int64(rl.window.Seconds()), rl.limit, now,
            ).Int64Slice()

            count := result[0]
            retryAfter := result[1]

            // Set rate limit headers
            w.Header().Set("X-RateLimit-Limit", ...)
            w.Header().Set("X-RateLimit-Remaining", ...)

            if count > rl.limit {
                response.JSON(w, http.StatusTooManyRequests, ...)
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}
```

---

## 4. Layer 2: Handlers

### File: `internal/router/router.go` (and split files)

### Responsibilities
- Parse and validate request input
- Extract authentication claims from context
- Check authorization (org membership, project access)
- Call repository methods
- Dispatch webhook events
- Return HTTP response

### Handler Pattern
```go
func (r *Router) createProjectHandler(w http.ResponseWriter, req *http.Request) {
    // 1. Extract auth claims
    claims, ok := auth.ClaimsFromContext(req.Context())
    if !ok {
        response.Unauthorized(w, "missing authentication")
        return
    }

    // 2. Parse request body
    var input struct {
        OrgID       string `json:"org_id"`
        Name        string `json:"name"`
        Description string `json:"description"`
    }
    if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
        apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
        response.JSON(w, apiErr.HTTPStatus(), apiErr)
        return
    }

    // 3. Validate input
    if input.OrgID == "" || input.Name == "" {
        apiErr := apperrors.New(apperrors.ErrMissingField, "org_id and name are required")
        response.JSON(w, apiErr.HTTPStatus(), apiErr)
        return
    }

    // 4. Check permissions
    member, err := r.orgs.IsMember(req.Context(), input.OrgID, claims.UserID)
    if err != nil || !member {
        apiErr := apperrors.New(apperrors.ErrInsufficientPerms, "access denied")
        response.JSON(w, apiErr.HTTPStatus(), apiErr)
        return
    }

    // 5. Create resource
    project := &repository.Project{
        OrgID:       input.OrgID,
        Name:        input.Name,
        Description: input.Description,
        Status:      "active",
    }
    if err := r.projects.Create(req.Context(), project); err != nil {
        apiErr := apperrors.New(apperrors.ErrDBError, "failed to create project")
        response.JSON(w, apiErr.HTTPStatus(), apiErr)
        return
    }

    // 6. Dispatch webhook
    if r.webhookEngine != nil {
        r.webhookEngine.Dispatch(req.Context(), webhook.Event{
            Type:    "project.created",
            Payload: map[string]interface{}{"project_id": project.ID, "name": project.Name},
        })
    }

    // 7. Return response
    response.Created(w, project)
}
```

### Helper Methods
```go
// requireOrgMember checks if user is a member of the organization
func (r *Router) requireOrgMember(ctx context.Context, orgID, userID string) error {
    member, err := r.orgs.IsMember(ctx, orgID, userID)
    if err != nil || !member {
        return fmt.Errorf("access denied")
    }
    return nil
}

// requireProjectMember checks if user has access to the project
func (r *Router) requireProjectMember(ctx context.Context, projectID, userID string) (*repository.Project, error) {
    project, err := r.projects.FindByID(ctx, projectID)
    if err != nil {
        return nil, err
    }
    if err := r.requireOrgMember(ctx, project.OrgID, userID); err != nil {
        return nil, err
    }
    return project, nil
}
```

---

## 5. Layer 3: Repositories

### File: `internal/repository/`

### Responsibilities
- Database access abstraction
- SQL query construction
- Result scanning into Go structs
- Error handling and wrapping

### Repository Interface Pattern
```go
// ProjectRepositoryInterface defines the interface for project data access.
type ProjectRepositoryInterface interface {
    Create(ctx context.Context, project *Project) error
    FindByID(ctx context.Context, id string) (*Project, error)
    ListByOrg(ctx context.Context, orgID string) ([]Project, error)
    Update(ctx context.Context, id, name, description, status string) error
    Delete(ctx context.Context, id string) error
}
```

### Repository Implementation Pattern
```go
// ProjectRepository handles database operations for projects.
type ProjectRepository struct {
    pool *database.Conn
}

// NewProjectRepository creates a new project repository.
func NewProjectRepository(pool *database.Conn) *ProjectRepository {
    return &ProjectRepository{pool: pool}
}

// Create inserts a new project.
func (r *ProjectRepository) Create(ctx context.Context, project *Project) error {
    query := `
        INSERT INTO projects (org_id, name, description, status)
        VALUES ($1, $2, $3, $4)
        RETURNING id, created_at, updated_at
    `
    return r.pool.QueryRow(ctx, query,
        project.OrgID, project.Name, project.Description, project.Status,
    ).Scan(&project.ID, &project.CreatedAt, &project.UpdatedAt)
}

// FindByID retrieves a project by ID.
func (r *ProjectRepository) FindByID(ctx context.Context, id string) (*Project, error) {
    query := `
        SELECT id, org_id, name, description, status, created_at, updated_at
        FROM projects WHERE id = $1
    `
    project := &Project{}
    err := r.pool.QueryRow(ctx, query, id).Scan(
        &project.ID, &project.OrgID, &project.Name,
        &project.Description, &project.Status,
        &project.CreatedAt, &project.UpdatedAt,
    )
    if err != nil {
        if err == pgx.ErrNoRows {
            return nil, fmt.Errorf("project not found")
        }
        return nil, fmt.Errorf("failed to find project: %w", err)
    }
    return project, nil
}

// ListByOrg returns all projects for an organization.
func (r *ProjectRepository) ListByOrg(ctx context.Context, orgID string) ([]Project, error) {
    query := `
        SELECT id, org_id, name, description, status, created_at, updated_at
        FROM projects WHERE org_id = $1
        ORDER BY created_at DESC
    `
    rows, err := r.pool.Query(ctx, query, orgID)
    if err != nil {
        return nil, fmt.Errorf("failed to list projects: %w", err)
    }
    defer rows.Close()

    var projects []Project
    for rows.Next() {
        var p Project
        if err := rows.Scan(
            &p.ID, &p.OrgID, &p.Name,
            &p.Description, &p.Status,
            &p.CreatedAt, &p.UpdatedAt,
        ); err != nil {
            return nil, fmt.Errorf("failed to scan project: %w", err)
        }
        projects = append(projects, p)
    }
    return projects, rows.Err()
}
```

### Data Model Pattern
```go
// Project represents a project record in the database.
type Project struct {
    ID          string    `json:"id"`
    OrgID       string    `json:"org_id"`
    Name        string    `json:"name"`
    Description string    `json:"description,omitempty"`
    Status      string    `json:"status"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

### Compile-Time Interface Check
```go
var (
    _ ProjectRepositoryInterface = (*ProjectRepository)(nil)
)
```

---

## 6. Layer 4: Database

### File: `internal/database/`

### Responsibilities
- Connection pooling
- Health checks
- Migration management
- Transaction support

### Connection Pool Configuration
```go
type Config struct {
    Host         string
    Port         int
    User         string
    Password     string
    Name         string
    SSLMode      string
    MaxOpenConns int    // Default: 25
    MaxIdleConns int    // Default: 10
    MaxLifetime  time.Duration  // Default: 5 minutes
}
```

### Connection Wrapper
```go
type Conn struct {
    pool *pgxpool.Pool
}

func (c *Conn) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
    return c.pool.QueryRow(ctx, sql, args...)
}

func (c *Conn) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
    return c.pool.Query(ctx, sql, args...)
}

func (c *Conn) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
    return c.pool.Exec(ctx, sql, args...)
}

func (c *Conn) HealthCheck(ctx context.Context) error {
    return c.pool.Ping(ctx)
}
```

### Transaction Support
```go
func (c *Conn) Begin(ctx context.Context) (pgx.Tx, error) {
    return c.pool.Begin(ctx)
}
```

---

## 7. Cross-Cutting Concerns

### Error Handling
```go
// Internal error types
type AppError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}

// HTTP response helpers
func response.Created(w http.ResponseWriter, data interface{}) {
    response.JSON(w, http.StatusCreated, map[string]interface{}{
        "data": data,
    })
}

func response.Unauthorized(w http.ResponseWriter, message string) {
    response.JSON(w, http.StatusUnauthorized, map[string]interface{}{
        "error": map[string]string{
            "code":    "AUTH_001",
            "message": message,
        },
    })
}
```

### Context Enrichment
```go
// Auth claims in context
ctx := auth.ContextWithClaims(req.Context(), claims)
claims := auth.ClaimsFromContext(ctx)

// Request ID in context
ctx := context.WithValue(req.Context(), requestIDKey, id)
requestID := requestid.FromContext(ctx)
```

### Webhook Dispatch
```go
// After successful resource creation
if r.webhookEngine != nil {
    r.webhookEngine.Dispatch(req.Context(), webhook.Event{
        Type:    "project.created",
        Payload: map[string]interface{}{
            "project_id": project.ID,
            "name":       project.Name,
            "org_id":     project.OrgID,
        },
    })
}
```

---

## 8. Layered Dependency Graph

```
Router
  ├── Middleware
  │     ├── auth (JWT, API Key)
  │     ├── ratelimit (Redis)
  │     ├── security (Sanitize, CSRF)
  │     └── requestid
  ├── Handlers
  │     ├── Validation (manual)
  │     ├── Permission checks
  │     └── Response formatting
  ├── Repositories
  │     ├── SQL queries
  │     ├── Result scanning
  │     └── Error wrapping
  └── Database
        ├── Connection pool
        ├── Health checks
        └── Transactions
```

---

## 9. Current Limitations

| Limitation | Impact | Recommendation |
|-----------|--------|----------------|
| No service layer | Business logic in handlers | Extract to service layer |
| Manual validation | Inconsistent validation | Add validation middleware |
| No pagination middleware | Unbounded list queries | Add pagination middleware |
| No request logging | No audit trail | Add request/response logging |
| No RBAC middleware | Simple role checks | Add granular permission checks |

---

## 10. Future Architecture

### Recommended Service Layer
```
Router → Middleware → Handler → Service → Repository → Database
```

### Service Layer Responsibilities
- Business logic orchestration
- Transaction management
- Validation rules
- Domain events
- Caching

### Example Service
```go
type ProjectService struct {
    repos   RepositoryRegistry
    webhooks *webhook.Engine
    cache   Cache
}

func (s *ProjectService) Create(ctx context.Context, orgID, userID string, req CreateProjectRequest) (*Project, error) {
    // 1. Validate
    if err := s.validate(req); err != nil {
        return nil, err
    }

    // 2. Check permissions
    if err := s.repos.Orgs.IsMember(ctx, orgID, userID); err != nil {
        return nil, ErrUnauthorized
    }

    // 3. Create
    project := &Project{OrgID: orgID, Name: req.Name, Status: "active"}
    if err := s.repos.Projects.Create(ctx, project); err != nil {
        return nil, err
    }

    // 4. Dispatch event
    s.webhooks.Dispatch(ctx, webhook.Event{
        Type:    "project.created",
        Payload: map[string]interface{}{"project_id": project.ID},
    })

    // 5. Invalidate cache
    s.cache.Delete("org:" + orgID + ":projects")

    return project, nil
}
```
