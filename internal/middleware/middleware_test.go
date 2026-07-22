package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
)

// ─── CSRF Tests ──────────────────────────────────────────────────────────

func TestCSRF_AllowsSafeMethods(t *testing.T) {
	secret := []byte("test-secret-key-for-csrf-32bytes!")
	m := NewCSRFMiddleware(secret)
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{"GET", "HEAD", "OPTIONS", "TRACE"} {
		req := httptest.NewRequest(method, "/api/v1/data", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("safe method %s should pass, got %d", method, w.Code)
		}
	}
}

func TestCSRF_RejectsPostWithoutToken(t *testing.T) {
	secret := []byte("test-secret-key-for-csrf-32bytes!")
	m := NewCSRFMiddleware(secret)
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/data", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("POST without CSRF token should return 403, got %d", w.Code)
	}
}

func TestCSRF_RejectsInvalidToken(t *testing.T) {
	secret := []byte("test-secret-key-for-csrf-32bytes!")
	m := NewCSRFMiddleware(secret)
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/data", nil)
	req.Header.Set("X-CSRF-Token", "invalid-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("invalid CSRF token should return 403, got %d", w.Code)
	}
}

func TestCSRF_AcceptsValidSignedToken(t *testing.T) {
	secret := []byte("test-secret-key-for-csrf-32bytes!")
	m := NewCSRFMiddleware(secret)

	// Generate a valid signed token
	rawToken := "test-token-value"
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(rawToken))
	sig := hex.EncodeToString(mac.Sum(nil))
	validToken := rawToken + "." + sig

	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/data", nil)
	req.Header.Set("X-CSRF-Token", validToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("valid signed CSRF token should pass, got %d", w.Code)
	}
}

func TestCSRF_VerifyTokenRejectsTampered(t *testing.T) {
	secret := []byte("test-secret-key-for-csrf-32bytes!")
	m := NewCSRFMiddleware(secret)

	wrongMac := hmac.New(sha256.New, []byte("wrong-secret"))
	wrongMac.Write([]byte("test-token"))
	wrongSig := hex.EncodeToString(wrongMac.Sum(nil))
	tamperedToken := "test-token." + wrongSig

	if m.verifyToken(tamperedToken) {
		t.Error("tampered token should be rejected")
	}
}

func TestCSRF_SkipsExcludedPaths(t *testing.T) {
	secret := []byte("test-secret-key-for-csrf-32bytes!")
	m := NewCSRFMiddleware(secret)
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/api/v1/health", "/api/v1/ready", "/api/v1/metrics"} {
		req := httptest.NewRequest("POST", path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("excluded path %s should pass without CSRF, got %d", path, w.Code)
		}
	}
}

func TestCSRF_SetsTokenOnResponse(t *testing.T) {
	secret := []byte("test-secret-key-for-csrf-32bytes!")
	m := NewCSRFMiddleware(secret)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/data", nil)
	m.SetToken(w, req)

	if w.Header().Get("X-CSRF-Token") == "" {
		t.Error("SetToken should set X-CSRF-Token header")
	}
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "_csrf" {
			found = true
			if c.SameSite != http.SameSiteStrictMode {
				t.Errorf("SameSite should be Strict, got %v", c.SameSite)
			}
		}
	}
	if !found {
		t.Error("SetToken should set _csrf cookie")
	}
}

// ─── CSRF API Key Bypass Tests ──────────────────────────────────────────

func TestCSRF_SkipsXAPIKeyHeader(t *testing.T) {
	secret := []byte("test-secret-key-for-csrf-32bytes!")
	m := NewCSRFMiddleware(secret)
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// POST with X-API-Key header should bypass CSRF
	req := httptest.NewRequest("POST", "/api/v1/data", nil)
	req.Header.Set("X-API-Key", "va_abc123def456")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST with X-API-Key should bypass CSRF, got %d", w.Code)
	}
}

func TestCSRF_SkipsBearerAPIKey(t *testing.T) {
	secret := []byte("test-secret-key-for-csrf-32bytes!")
	m := NewCSRFMiddleware(secret)
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// POST with Bearer API key (has underscore, no dots) should bypass CSRF
	req := httptest.NewRequest("POST", "/api/v1/data", nil)
	req.Header.Set("Authorization", "Bearer va_abc123_def456")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST with Bearer API key should bypass CSRF, got %d", w.Code)
	}
}

func TestCSRF_DoesNotSkipJWTBearer(t *testing.T) {
	secret := []byte("test-secret-key-for-csrf-32bytes!")
	m := NewCSRFMiddleware(secret)
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// POST with Bearer JWT (has dots, no underscore) should NOT bypass CSRF
	req := httptest.NewRequest("POST", "/api/v1/data", nil)
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.signature.payload")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("POST with JWT bearer should NOT bypass CSRF, got %d", w.Code)
	}
}

func TestIsAPIKeyRequest(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		authHdr  string
		expected bool
	}{
		{"X-API-Key present", "va_abc123", "", true},
		{"Bearer API key", "", "Bearer va_abc123_def", true},
		{"Bearer JWT", "", "Bearer eyJabc.signature", false},
		{"No auth", "", "", false},
		{"Basic auth", "", "Basic dXNlcjpwYXNz", false},
		{"Bearer with underscore but no dots", "", "Bearer my_key_123", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				req.Header.Set("X-API-Key", tt.header)
			}
			if tt.authHdr != "" {
				req.Header.Set("Authorization", tt.authHdr)
			}
			got := isAPIKeyRequest(req)
			if got != tt.expected {
				t.Errorf("isAPIKeyRequest() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ─── Idempotency Tests ───────────────────────────────────────────────────

func TestIdempotency_PassesNonPostRequests(t *testing.T) {
	m := NewIdempotencyMiddleware(time.Minute)
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/data", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET should pass through, got %d", w.Code)
	}
}

func TestIdempotency_PassesPostWithoutKey(t *testing.T) {
	m := NewIdempotencyMiddleware(time.Minute)
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest("POST", "/api/v1/data", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("POST without key should pass through, got %d", w.Code)
	}
}

func TestIdempotency_CachesResponse(t *testing.T) {
	m := NewIdempotencyMiddleware(time.Minute)
	callCount := 0
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	}))

	req1 := httptest.NewRequest("POST", "/api/v1/data", nil)
	req1.Header.Set("Idempotency-Key", "test-key-1")
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	req2 := httptest.NewRequest("POST", "/api/v1/data", nil)
	req2.Header.Set("Idempotency-Key", "test-key-1")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if callCount != 1 {
		t.Errorf("handler should be called once, got %d", callCount)
	}
	if w2.Header().Get("Idempotency-Replayed") != "true" {
		t.Error("replayed response should have Idempotency-Replayed header")
	}
}

func TestIdempotency_DifferentKeysAreIndependent(t *testing.T) {
	m := NewIdempotencyMiddleware(time.Minute)
	callCount := 0
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest("POST", "/api/v1/data", nil)
	req1.Header.Set("Idempotency-Key", "key-a")
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	req2 := httptest.NewRequest("POST", "/api/v1/data", nil)
	req2.Header.Set("Idempotency-Key", "key-b")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if callCount != 2 {
		t.Errorf("different keys should both call handler, got %d calls", callCount)
	}
}

func TestIdempotency_PassesDeleteAndPut(t *testing.T) {
	m := NewIdempotencyMiddleware(time.Minute)
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{"DELETE", "PUT", "PATCH"} {
		req := httptest.NewRequest(method, "/api/v1/data", nil)
		req.Header.Set("Idempotency-Key", "some-key")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("%s should pass through idempotency, got %d", method, w.Code)
		}
	}
}

// ─── Scope Tests ──────────────────────────────────────────────────────────

func TestHasScope_ExactMatch(t *testing.T) {
	if !hasScope([]string{"tasks:read"}, "tasks:read") {
		t.Error("should match exact scope")
	}
}

func TestHasScope_WildcardMatch(t *testing.T) {
	if !hasScope([]string{"admin:*"}, "admin:read") {
		t.Error("admin:* should match admin:read")
	}
	if !hasScope([]string{"admin:*"}, "admin:write") {
		t.Error("admin:* should match admin:write")
	}
}

func TestHasScope_GlobalWildcard(t *testing.T) {
	if !hasScope([]string{"*"}, "anything:at_all") {
		t.Error("* should match everything")
	}
}

func TestHasScope_NoMatch(t *testing.T) {
	if hasScope([]string{"tasks:read"}, "tasks:write") {
		t.Error("tasks:read should not match tasks:write")
	}
}

func TestHasScope_EmptyScopes(t *testing.T) {
	if hasScope([]string{}, "tasks:read") {
		t.Error("empty scopes should not match anything")
	}
}

func TestHasScope_TrimmedWhitespace(t *testing.T) {
	if !hasScope([]string{"  tasks:read  "}, "tasks:read") {
		t.Error("should trim whitespace")
	}
}

func TestRequireScope_AllowsJWTWithoutScopes(t *testing.T) {
	// JWT-authenticated requests (non-API-key) bypass scope checks
	handler := RequireScope("admin:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	claims := &auth.Claims{UserID: "user1", Role: "admin", IsAPIKey: false}
	ctx := auth.ContextWithClaims(context.Background(), claims)
	req := httptest.NewRequest("GET", "/api/v1/admin", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("JWT should bypass scope check, got %d", w.Code)
	}
}

func TestRequireScope_AllowsAPIKeyWithMatchingScope(t *testing.T) {
	handler := RequireScope("tasks:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	claims := &auth.Claims{UserID: "user1", Role: "user", IsAPIKey: true, Scopes: []string{"tasks:read", "tasks:write"}}
	ctx := auth.ContextWithClaims(context.Background(), claims)
	req := httptest.NewRequest("GET", "/api/v1/tasks", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("API key with tasks:read scope should pass, got %d", w.Code)
	}
}

func TestRequireScope_RejectsAPIKeyWithoutScope(t *testing.T) {
	handler := RequireScope("admin:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	claims := &auth.Claims{UserID: "user1", Role: "user", IsAPIKey: true, Scopes: []string{"tasks:read"}}
	ctx := auth.ContextWithClaims(context.Background(), claims)
	req := httptest.NewRequest("GET", "/api/v1/admin", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("API key without admin:read should be rejected, got %d", w.Code)
	}
}

func TestRequireScope_AllowsAPIKeyWithWildcardScope(t *testing.T) {
	handler := RequireScope("admin:write")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	claims := &auth.Claims{UserID: "user1", Role: "user", IsAPIKey: true, Scopes: []string{"admin:*"}}
	ctx := auth.ContextWithClaims(context.Background(), claims)
	req := httptest.NewRequest("POST", "/api/v1/admin/users", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("API key with admin:* scope should pass admin:write, got %d", w.Code)
	}
}

func TestRequireScope_PassesWithoutClaims(t *testing.T) {
	// No claims in context (unauthenticated) — should pass through
	handler := RequireScope("admin:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/admin", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("no claims should pass through, got %d", w.Code)
	}
}

// ─── JWT Blacklist Tests ──────────────────────────────────────────────────

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{"valid bearer", "Bearer abc123", "abc123"},
		{"empty header", "", ""},
		{"no bearer prefix", "Token abc123", ""},
		{"api key with underscore", "Bearer va_abc_def", ""},
		{"jwt with dots", "Bearer eyJhbGciOiJIUzI1NiJ9.signature", "eyJhbGciOiJIUzI1NiJ9.signature"},
		{"wrong case", "bearer abc123", "abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			got := ExtractBearerToken(req)
			if got != tt.expected {
				t.Errorf("ExtractBearerToken() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// ─── Audit Logger Tests ───────────────────────────────────────────────────

func TestExtractResource(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/api/v1/users", "users"},
		{"/api/v1/projects/123/agents", "agents"},
		{"/api/v1/tasks", "tasks"},
		{"/api/v1/skills/abc/rate", "rate"},
		{"/api/v1", "unknown"},
		{"/", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractResource(tt.path)
			if got != tt.expected {
				t.Errorf("extractResource(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestAuditMiddleware_OnlyAuditsStateChangingMethods(t *testing.T) {
	var logged bool
	logger := &mockAuditLogger{onLog: func(event AuditEvent) { logged = true }}
	handler := AuditMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	logged = false
	req := httptest.NewRequest("GET", "/api/v1/data", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if logged {
		t.Error("GET should not be audited")
	}

	logged = false
	req = httptest.NewRequest("POST", "/api/v1/data", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if !logged {
		t.Error("POST should be audited")
	}
}

func TestAuditMiddleware_AuditsDeleteAndPut(t *testing.T) {
	var events []AuditEvent
	logger := &mockAuditLogger{onLog: func(event AuditEvent) {
		events = append(events, event)
	}}
	handler := AuditMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{"PUT", "DELETE", "PATCH"} {
		events = nil
		req := httptest.NewRequest(method, "/api/v1/resource/123", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if len(events) != 1 {
			t.Errorf("%s should be audited, got %d events", method, len(events))
			continue
		}
		if events[0].Method != method {
			t.Errorf("expected method %s, got %s", method, events[0].Method)
		}
		if events[0].Resource != "resource" {
			t.Errorf("expected resource 'resource', got %q", events[0].Resource)
		}
		if events[0].ResourceID != "123" {
			t.Errorf("expected resource_id '123', got %q", events[0].ResourceID)
		}
	}
}

func TestAuditMiddleware_CapturesStatus(t *testing.T) {
	var event AuditEvent
	logger := &mockAuditLogger{onLog: func(e AuditEvent) { event = e }}
	handler := AuditMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest("POST", "/api/v1/data", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if event.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", event.StatusCode)
	}
}

// ─── Tiered Rate Limiter Tests ────────────────────────────────────────────

func TestGetTier(t *testing.T) {
	tests := []struct {
		plan     string
		expected int
	}{
		{"free", 30},
		{"pro", 120},
		{"team", 600},
		{"", 30},
		{"unknown", 30},
	}
	for _, tt := range tests {
		t.Run(tt.plan, func(t *testing.T) {
			tier := GetTier(tt.plan)
			if tier.RequestsPerMinute != tt.expected {
				t.Errorf("GetTier(%q).RequestsPerMinute = %d, want %d", tt.plan, tier.RequestsPerMinute, tt.expected)
			}
		})
	}
}

func TestTieredRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := NewTieredRateLimiter()
	called := false
	handler := rl.Middleware(
		func(r *http.Request) string { return "user1" },
		func(r *http.Request) Tier { return FreeTier },
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("handler should be called when under limit")
	}
	if w.Header().Get("X-RateLimit-Tier") != "free" {
		t.Errorf("tier header should be 'free', got %q", w.Header().Get("X-RateLimit-Tier"))
	}
}

func TestTieredRateLimiter_RejectsOverLimit(t *testing.T) {
	rl := NewTieredRateLimiter()
	handler := rl.Middleware(
		func(r *http.Request) string { return "user1" },
		func(r *http.Request) Tier { return Tier{RequestsPerMinute: 1, RequestsPerHour: 1, RequestsPerDay: 1} },
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code == http.StatusTooManyRequests {
		t.Error("first request should pass")
	}

	req2 := httptest.NewRequest("GET", "/", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second request should be rejected, got %d", w2.Code)
	}
}

// ─── Production Validation Tests ──────────────────────────────────────────

func TestValidateProductionEnv_NonProduction(t *testing.T) {
	t.Setenv("VIGILAGENT_ENV", "development")
	if err := ValidateProductionEnv(); err != nil {
		t.Errorf("non-production should pass: %v", err)
	}
}

func TestValidateProductionEnv_MissingSecret(t *testing.T) {
	t.Setenv("VIGILAGENT_ENV", "production")
	t.Setenv("VIGILAGENT_JWT_SECRET", "")
	if err := ValidateProductionEnv(); err == nil {
		t.Error("production without secret should fail")
	}
}

func TestValidateProductionEnv_DefaultSecret(t *testing.T) {
	t.Setenv("VIGILAGENT_ENV", "production")
	t.Setenv("VIGILAGENT_JWT_SECRET", "default")
	if err := ValidateProductionEnv(); err == nil {
		t.Error("production with default secret should fail")
	}
}

func TestValidateProductionEnv_ShortSecret(t *testing.T) {
	t.Setenv("VIGILAGENT_ENV", "production")
	t.Setenv("VIGILAGENT_JWT_SECRET", "short")
	if err := ValidateProductionEnv(); err == nil {
		t.Error("production with short secret should fail")
	}
}

func TestValidateProductionEnv_ValidSecret(t *testing.T) {
	t.Setenv("VIGILAGENT_ENV", "production")
	t.Setenv("VIGILAGENT_JWT_SECRET", "a-very-long-and-secure-jwt-secret-key-32chars")
	if err := ValidateProductionEnv(); err != nil {
		t.Errorf("valid secret should pass: %v", err)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────

type mockAuditLogger struct {
	mu     sync.Mutex
	onLog  func(AuditEvent)
	events []AuditEvent
}

func (m *mockAuditLogger) Log(ctx context.Context, event AuditEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	if m.onLog != nil {
		m.onLog(event)
	}
}
