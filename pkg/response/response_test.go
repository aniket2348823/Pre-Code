package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/requestid"
)

// Content from response_test.go
// --- Helper ---

func reqWithID(id string) *http.Request {
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("X-Request-Id", id)
	var captured *http.Request
	rr := httptest.NewRecorder()
	mw := requestid.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		captured = req
	}))
	mw.ServeHTTP(rr, r)
	return captured
}

func decodeResp(t *testing.T, w *httptest.ResponseRecorder) APIResponse {
	t.Helper()
	var resp APIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	return resp
}

func TestJSON(t *testing.T) {
	t.Run("sets content type and status", func(t *testing.T) {
		w := httptest.NewRecorder()
		JSON(w, http.StatusOK, map[string]string{"key": "value"})

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Errorf("expected Content-Type application/json; charset=utf-8, got %s", ct)
		}
	})

	t.Run("encodes data correctly", func(t *testing.T) {
		w := httptest.NewRecorder()
		JSON(w, http.StatusCreated, map[string]int{"count": 42})

		var resp map[string]int
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["count"] != 42 {
			t.Errorf("expected count=42, got %d", resp["count"])
		}
	})
}

func TestSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	Success(w, http.StatusOK, "test-data")

	resp := decodeResp(t, w)
	if !resp.Success {
		t.Fatal("expected success=true")
	}
	if resp.Error != nil {
		t.Errorf("expected empty error, got %v", resp.Error)
	}
}

func TestErrorResponse(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, http.StatusBadRequest, "something went wrong")

	resp := decodeResp(t, w)
	if resp.Success {
		t.Fatal("expected success=false")
	}
	if resp.Error == nil || resp.Error.Message != "something went wrong" {
		t.Errorf("expected error message, got %v", resp.Error)
	}
	if resp.Error.Timestamp == "" {
		t.Error("expected timestamp to be set")
	}
}

func TestCreated(t *testing.T) {
	w := httptest.NewRecorder()
	Created(w, map[string]string{"id": "123"})

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if !resp.Success {
		t.Fatal("expected success=true")
	}
}

func TestNoContent(t *testing.T) {
	w := httptest.NewRecorder()
	NoContent(w)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

func TestNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	NotFound(w, "resource not found")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error == nil || resp.Error.Message != "resource not found" {
		t.Errorf("expected error message, got %v", resp.Error)
	}
}

func TestBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	BadRequest(w, "invalid input")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestUnauthorized(t *testing.T) {
	w := httptest.NewRecorder()
	Unauthorized(w, "unauthorized access")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestForbidden(t *testing.T) {
	w := httptest.NewRecorder()
	Forbidden(w, "access denied")

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestInternalError(t *testing.T) {
	w := httptest.NewRecorder()
	InternalError(w, "server error")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestAPIResponseStruct(t *testing.T) {
	t.Run("omits empty fields in JSON", func(t *testing.T) {
		resp := APIResponse{Success: true, Data: "hello", RequestID: "test-123"}
		b, _ := json.Marshal(resp)
		var m map[string]interface{}
		json.Unmarshal(b, &m)

		if _, ok := m["error"]; ok {
			t.Fatal("expected error field to be omitted")
		}
		if _, ok := m["meta"]; ok {
			t.Fatal("expected meta field to be omitted")
		}
	})

	t.Run("includes request_id", func(t *testing.T) {
		resp := APIResponse{Success: true, Data: "hello", RequestID: "req-abc"}
		b, _ := json.Marshal(resp)
		var m map[string]interface{}
		json.Unmarshal(b, &m)

		if m["request_id"] != "req-abc" {
			t.Fatalf("expected request_id=req-abc, got %v", m["request_id"])
		}
	})
}

func TestSuccessWithMeta(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/items?limit=10", nil)

	SuccessWithMeta(w, r, http.StatusOK, []string{"a", "b"}, &Meta{
		Total:   50,
		Limit:   10,
		Offset:  0,
		HasMore: true,
	})

	resp := decodeResp(t, w)
	if !resp.Success {
		t.Fatal("expected success=true")
	}
	if resp.Meta == nil {
		t.Fatal("expected meta to be set")
	}
	if resp.Meta.Total != 50 {
		t.Errorf("expected total=50, got %d", resp.Meta.Total)
	}
	if !resp.Meta.HasMore {
		t.Error("expected has_more=true")
	}
}

func TestErrorWithDetails(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/items", nil)

	ErrorWithDetails(w, r, http.StatusBadRequest, "VALIDATION_001", "invalid input", []ValidationErrorDetail{
		{Field: "email", Rule: "required", Message: "email is required"},
	})

	resp := decodeResp(t, w)
	if resp.Success {
		t.Fatal("expected success=false")
	}
	if resp.Error == nil {
		t.Fatal("expected error to be set")
	}
	if resp.Error.Code != "VALIDATION_001" {
		t.Errorf("expected code=VALIDATION_001, got %s", resp.Error.Code)
	}
	if resp.Error.Timestamp == "" {
		t.Error("expected timestamp to be set")
	}
}

func TestValidationErrorResponse(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/items", nil)

	ValidationErrorResponse(w, r, []ValidationErrorDetail{
		{Field: "name", Rule: "required", Message: "name is required"},
		{Field: "email", Rule: "format", Message: "invalid email format"},
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Success {
		t.Fatal("expected success=false")
	}
	if resp.Error == nil {
		t.Fatal("expected error to be set")
	}
	if resp.Error.Code != CodeResourceValidationFailed {
		t.Errorf("expected code=%s, got %s", CodeResourceValidationFailed, resp.Error.Code)
	}
	// Check details contain field-level errors
	details, ok := resp.Error.Details.([]interface{})
	if !ok || len(details) != 2 {
		t.Fatalf("expected 2 details, got %v", resp.Error.Details)
	}
}

// --- R-suffix tests ---

func TestSuccessR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-123")
	SuccessR(w, r, http.StatusOK, "data")

	resp := decodeResp(t, w)
	if !resp.Success {
		t.Fatal("expected success=true")
	}
	if resp.RequestID != "req-123" {
		t.Errorf("expected request_id=req-123, got %q", resp.RequestID)
	}
}

func TestCreatedR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-456")
	CreatedR(w, r, map[string]string{"id": "1"})

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if !resp.Success {
		t.Fatal("expected success=true")
	}
	if resp.RequestID != "req-456" {
		t.Errorf("expected request_id=req-456, got %q", resp.RequestID)
	}
}

func TestErrorR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-err")
	ErrorR(w, r, http.StatusBadRequest, "CODE_001", "bad request")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Success {
		t.Fatal("expected success=false")
	}
	if resp.Error.Code != "CODE_001" {
		t.Errorf("expected code=CODE_001, got %q", resp.Error.Code)
	}
	if resp.RequestID != "req-err" {
		t.Errorf("expected request_id=req-err, got %q", resp.RequestID)
	}
	if resp.Error.Timestamp == "" {
		t.Error("expected timestamp to be set")
	}
}

func TestNotFoundR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-nf")
	NotFoundR(w, r, "not found")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeResourceNotFound {
		t.Errorf("expected code %s, got %q", CodeResourceNotFound, resp.Error.Code)
	}
}

func TestBadRequestR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-br")
	BadRequestR(w, r, "bad request")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeBadRequest {
		t.Errorf("expected code %s, got %q", CodeBadRequest, resp.Error.Code)
	}
}

func TestUnauthorizedR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-unauth")
	UnauthorizedR(w, r, "unauthorized")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeAUTHMissingToken {
		t.Errorf("expected code %s, got %q", CodeAUTHMissingToken, resp.Error.Code)
	}
}

func TestForbiddenR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-forbid")
	ForbiddenR(w, r, "forbidden")

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeAUTHInsufficientPerms {
		t.Errorf("expected code %s, got %q", CodeAUTHInsufficientPerms, resp.Error.Code)
	}
}

func TestInternalErrorR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-ise")
	InternalErrorR(w, r, "internal error")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeServiceUnavailable {
		t.Errorf("expected code %s, got %q", CodeServiceUnavailable, resp.Error.Code)
	}
}

// --- 429 with Retry-After ---

func TestTooManyRequests(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-429")
	TooManyRequests(w, r, "rate limited")

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	if ra := w.Header().Get("Retry-After"); ra == "" {
		t.Error("expected Retry-After header")
	}
	if rl := w.Header().Get("X-RateLimit-Reset"); rl == "" {
		t.Error("expected X-RateLimit-Reset header")
	}
	if w.Header().Get("X-RateLimit-Limit") != "0" {
		t.Error("expected X-RateLimit-Limit: 0")
	}
	if w.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Error("expected X-RateLimit-Remaining: 0")
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeRateLimitExceeded {
		t.Errorf("expected code %s, got %q", CodeRateLimitExceeded, resp.Error.Code)
	}
}

func TestTooManyRequestsAfter(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-429-custom")
	TooManyRequestsAfter(w, r, "slow down", 30)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	if ra := w.Header().Get("Retry-After"); ra != "30" {
		t.Errorf("expected Retry-After: 30, got %s", ra)
	}
	reset := w.Header().Get("X-RateLimit-Reset")
	if reset == "" {
		t.Fatal("expected X-RateLimit-Reset header")
	}
	// Reset timestamp should be ~30 seconds in the future
	resetTime := time.Unix(parseInt64(reset), 0)
	diff := time.Until(resetTime)
	if diff < 20*time.Second || diff > 40*time.Second {
		t.Errorf("expected reset time ~30s in future, got diff %v", diff)
	}
}

func TestConflict(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-conflict")
	Conflict(w, r, "conflict")

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeResourceConflict {
		t.Errorf("expected code %s, got %q", CodeResourceConflict, resp.Error.Code)
	}
}

func TestServiceUnavailable(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-503")
	ServiceUnavailable(w, r, "unavailable")

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeServiceUnavailable {
		t.Errorf("expected code %s, got %q", CodeServiceUnavailable, resp.Error.Code)
	}
}

// --- New standardized helpers ---

func TestErrorWithCode(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-ewc")
	ErrorWithCode(w, r, http.StatusForbidden, "CUSTOM_001", "custom error")

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != "CUSTOM_001" {
		t.Errorf("expected code=CUSTOM_001, got %q", resp.Error.Code)
	}
	if resp.Error.Timestamp == "" {
		t.Error("expected timestamp")
	}
	if resp.RequestID != "req-ewc" {
		t.Errorf("expected request_id=req-ewc, got %q", resp.RequestID)
	}
}

func TestConflictError(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-ce")
	ConflictError(w, r, "API key")

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeResourceConflict {
		t.Errorf("expected code=%s, got %q", CodeResourceConflict, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "API key") {
		t.Errorf("expected message to contain 'API key', got %q", resp.Error.Message)
	}
}

func TestValidationError(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-ve")
	ValidationError(w, r, []ValidationErrorDetail{
		{Field: "email", Rule: "required", Message: "email is required"},
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeResourceValidationFailed {
		t.Errorf("expected code=%s, got %q", CodeResourceValidationFailed, resp.Error.Code)
	}
}

func TestNotFoundError(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-nfe")
	NotFoundError(w, r, "Agent")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeResourceNotFound {
		t.Errorf("expected code=%s, got %q", CodeResourceNotFound, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "Agent") {
		t.Errorf("expected message to contain 'Agent', got %q", resp.Error.Message)
	}
}

func TestRateLimitError(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-rle")
	RateLimitError(w, r, 120)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") != "120" {
		t.Errorf("expected Retry-After: 120, got %s", w.Header().Get("Retry-After"))
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeRateLimitExceeded {
		t.Errorf("expected code=%s, got %q", CodeRateLimitExceeded, resp.Error.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-mna")
	MethodNotAllowed(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp.Error.Code != CodeMethodNotAllowed {
		t.Errorf("expected code=%s, got %q", CodeMethodNotAllowed, resp.Error.Code)
	}
}

// --- Error code constants correctness ---

func TestErrorCodes(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected string
	}{
		{"AUTH_INVALID_TOKEN", CodeAUTHInvalidToken, "AUTH_004"},
		{"AUTH_EXPIRED_TOKEN", CodeAUTHExpiredToken, "AUTH_003"},
		{"AUTH_MISSING_TOKEN", CodeAUTHMissingToken, "AUTH_001"},
		{"AUTH_INSUFFICIENT_PERMS", CodeAUTHInsufficientPerms, "AUTH_007"},
		{"AUTH_ACCOUNT_LOCKED", CodeAUTHAccountLocked, "AUTH_005"},
		{"RESOURCE_NOT_FOUND", CodeResourceNotFound, "RES_001"},
		{"RESOURCE_CONFLICT", CodeResourceConflict, "RES_003"},
		{"RESOURCE_VALIDATION_FAILED", CodeResourceValidationFailed, "VAL_001"},
		{"RATE_LIMIT_EXCEEDED", CodeRateLimitExceeded, "INFRA_001"},
		{"BODY_TOO_LARGE", CodeBodyTooLarge, "VAL_005"},
		{"INTERNAL_SERVER_ERROR", CodeInternalServerError, "INFRA_003"},
		{"SERVICE_UNAVAILABLE", CodeServiceUnavailable, "INFRA_002"},
		{"BAD_REQUEST", CodeBadRequest, "VAL_001"},
		{"METHOD_NOT_ALLOWED", CodeMethodNotAllowed, "METHOD_001"},
		{"RESOURCE_ALREADY_EXISTS", CodeResourceAlreadyExists, "RES_002"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.expected {
				t.Errorf("expected %s = %q, got %q", tt.name, tt.expected, tt.code)
			}
		})
	}
}

// --- Backward compat: old non-R functions still work ---

func TestSuccessR_NoRequestID(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	SuccessR(w, r, http.StatusOK, "data")

	resp := decodeResp(t, w)
	if !resp.Success {
		t.Fatal("expected success=true")
	}
	if resp.RequestID != "" {
		t.Errorf("expected empty request_id, got %q", resp.RequestID)
	}
}

func TestErrorR_NoRequestID(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	ErrorR(w, r, http.StatusBadRequest, "CODE", "msg")

	resp := decodeResp(t, w)
	if resp.RequestID != "" {
		t.Errorf("expected empty request_id, got %q", resp.RequestID)
	}
}

func TestSuccessWithMeta_NilMeta(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-meta")
	SuccessWithMeta(w, r, http.StatusOK, "data", nil)

	resp := decodeResp(t, w)
	if !resp.Success {
		t.Fatal("expected success=true")
	}
	if resp.Meta != nil {
		t.Error("expected nil meta")
	}
}

func TestSuccessWithMeta_WithCursor(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-cursor")
	SuccessWithMeta(w, r, http.StatusOK, "data", &Meta{
		NextCursor: "abc123",
		HasMore:    true,
	})

	resp := decodeResp(t, w)
	if resp.Meta.NextCursor != "abc123" {
		t.Errorf("expected cursor abc123, got %q", resp.Meta.NextCursor)
	}
}

func TestRid(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		if got := rid(nil); got != "" {
			t.Errorf("expected empty string for nil request, got %q", got)
		}
	})
	t.Run("request with id", func(t *testing.T) {
		r := reqWithID("my-req-id")
		if got := rid(r); got != "my-req-id" {
			t.Errorf("expected 'my-req-id', got %q", got)
		}
	})
	t.Run("request without id", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/test", nil)
		if got := rid(r); got != "" {
			t.Errorf("expected empty string for request without id, got %q", got)
		}
	})
}

// --- Each error code produces correct HTTP status ---

func TestErrorCodesHTTPStatus(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		status int
	}{
		{"AUTH_INVALID_TOKEN", CodeAUTHInvalidToken, http.StatusUnauthorized},
		{"AUTH_EXPIRED_TOKEN", CodeAUTHExpiredToken, http.StatusUnauthorized},
		{"AUTH_MISSING_TOKEN", CodeAUTHMissingToken, http.StatusUnauthorized},
		{"AUTH_INSUFFICIENT_PERMS", CodeAUTHInsufficientPerms, http.StatusForbidden},
		{"AUTH_ACCOUNT_LOCKED", CodeAUTHAccountLocked, http.StatusTooManyRequests},
		{"RESOURCE_NOT_FOUND", CodeResourceNotFound, http.StatusNotFound},
		{"RESOURCE_CONFLICT", CodeResourceConflict, http.StatusConflict},
		{"RESOURCE_VALIDATION_FAILED", CodeResourceValidationFailed, http.StatusBadRequest},
		{"RATE_LIMIT_EXCEEDED", CodeRateLimitExceeded, http.StatusTooManyRequests},
		{"BODY_TOO_LARGE", CodeBodyTooLarge, http.StatusRequestEntityTooLarge},
		{"INTERNAL_SERVER_ERROR", CodeInternalServerError, http.StatusInternalServerError},
		{"SERVICE_UNAVAILABLE", CodeServiceUnavailable, http.StatusServiceUnavailable},
		{"BAD_REQUEST", CodeBadRequest, http.StatusBadRequest},
		{"METHOD_NOT_ALLOWED", CodeMethodNotAllowed, http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := reqWithID("req-code-test")
			ErrorWithCode(w, r, tt.status, tt.code, "test")
			if w.Code != tt.status {
				t.Errorf("expected status %d, got %d", tt.status, w.Code)
			}
			resp := decodeResp(t, w)
			if resp.Error.Code != tt.code {
				t.Errorf("expected code=%s, got %s", tt.code, resp.Error.Code)
			}
			if resp.Error.Timestamp == "" {
				t.Error("expected timestamp to be set")
			}
			if resp.Error.RequestID == "" {
				t.Error("expected request_id in error body")
			}
		})
	}
}

// --- Timestamp format ---

func TestTimestampFormat(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-ts")
	ErrorR(w, r, http.StatusBadRequest, "CODE", "msg")

	resp := decodeResp(t, w)
	ts := resp.Error.Timestamp
	if ts == "" {
		t.Fatal("expected timestamp")
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatalf("expected RFC3339 timestamp, got %q: %v", ts, err)
	}
	if time.Since(parsed) > 5*time.Second {
		t.Errorf("timestamp too old: %v", parsed)
	}
}

// parseInt64 is a test helper.
func parseInt64(s string) int64 {
	var n int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		}
	}
	return n
}

// Content from hateoas_test.go
// --- Link struct tests ---

func TestLink_MarshalJSON(t *testing.T) {
	link := Link{Href: "/api/v1/items", Method: "GET", Type: "application/json"}
	b, err := json.Marshal(link)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if m["href"] != "/api/v1/items" {
		t.Errorf("expected href=/api/v1/items, got %v", m["href"])
	}
	if m["method"] != "GET" {
		t.Errorf("expected method=GET, got %v", m["method"])
	}
	if m["type"] != "application/json" {
		t.Errorf("expected type=application/json, got %v", m["type"])
	}
}

func TestLink_OmitsEmptyFields(t *testing.T) {
	link := Link{Href: "/foo"}
	b, err := json.Marshal(link)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if _, ok := m["method"]; ok {
		t.Error("expected method to be omitted")
	}
	if _, ok := m["type"]; ok {
		t.Error("expected type to be omitted")
	}
	if _, ok := m["title"]; ok {
		t.Error("expected title to be omitted")
	}
}

func TestNewLink(t *testing.T) {
	link := NewLink("/api/test")
	if link.Href != "/api/test" {
		t.Errorf("expected href=/api/test, got %s", link.Href)
	}
	if link.Method != "" {
		t.Error("expected empty method")
	}
}

func TestNewMethodLink(t *testing.T) {
	link := NewMethodLink("/api/test", "POST")
	if link.Href != "/api/test" || link.Method != "POST" {
		t.Errorf("expected {/api/test POST}, got {%s %s}", link.Href, link.Method)
	}
}

func TestNewTypedLink(t *testing.T) {
	link := NewTypedLink("/api/test", "PUT", "application/json")
	if link.Href != "/api/test" || link.Method != "PUT" || link.Type != "application/json" {
		t.Errorf("expected {/api/test PUT application/json}, got {%s %s %s}", link.Href, link.Method, link.Type)
	}
}

// --- AddLink tests ---

func TestAddLink_MapData(t *testing.T) {
	data := map[string]interface{}{
		"name": "test",
	}
	result := AddLink(data, "self", "/api/items/1", "GET")

	links, ok := result.(map[string]interface{})["_links"].(Links)
	if !ok {
		t.Fatal("expected _links to be Links type")
	}
	if links["self"].Href != "/api/items/1" {
		t.Errorf("expected href=/api/items/1, got %s", links["self"].Href)
	}
	if links["self"].Method != "GET" {
		t.Errorf("expected method=GET, got %s", links["self"].Method)
	}
}

func TestAddLink_PtrMapData(t *testing.T) {
	data := &map[string]interface{}{
		"name": "test",
	}
	result := AddLink(data, "next", "/api/items?page=2", "GET")
	_ = result

	links, ok := (*data)["_links"].(Links)
	if !ok {
		t.Fatal("expected _links to be Links type on pointer map")
	}
	if links["next"].Href != "/api/items?page=2" {
		t.Errorf("expected href=/api/items?page=2, got %s", links["next"].Href)
	}
}

func TestAddLink_MergesExisting(t *testing.T) {
	data := map[string]interface{}{
		"_links": Links{
			"self": NewLink("/api/items"),
		},
	}
	AddLink(data, "next", "/api/items?page=2", "GET")

	links := data["_links"].(Links)
	if len(links) != 2 {
		t.Errorf("expected 2 links, got %d", len(links))
	}
	if links["self"].Href != "/api/items" {
		t.Error("existing self link was overwritten")
	}
	if links["next"].Href != "/api/items?page=2" {
		t.Error("next link not added correctly")
	}
}

func TestAddLink_NilMap(t *testing.T) {
	// Initialize the pointer so we can test nil inner map
	data := new(map[string]interface{})
	AddLink(data, "self", "/api/test", "GET")
	if *data == nil {
		t.Fatal("expected inner map to be initialized")
	}
}

func TestAddLink_UnsupportedType(t *testing.T) {
	data := "not a map"
	result := AddLink(data, "self", "/test", "GET")
	if result != "not a map" {
		t.Error("unsupported type should return original data")
	}
}

// --- AddSelfLink tests ---

func TestAddSelfLink(t *testing.T) {
	data := map[string]interface{}{"id": 1}
	AddSelfLink(data, "/api/items/1")

	links := data["_links"].(Links)
	if links["self"].Href != "/api/items/1" {
		t.Errorf("expected self href=/api/items/1, got %s", links["self"].Href)
	}
	if links["self"].Method != "GET" {
		t.Errorf("expected method=GET, got %s", links["self"].Method)
	}
}

// --- AddCollectionLinks tests ---

func TestAddCollectionLinks_MiddlePage(t *testing.T) {
	data := map[string]interface{}{}
	AddCollectionLinks(data, "/api/items", 2, 10, 50)

	links := data["_links"].(Links)

	assertLink(t, links, "self", "/api/items?page=2&per_page=10")
	assertLink(t, links, "first", "/api/items?page=1&per_page=10")
	assertLink(t, links, "last", "/api/items?page=5&per_page=10")
	assertLink(t, links, "next", "/api/items?page=3&per_page=10")
	assertLink(t, links, "prev", "/api/items?page=1&per_page=10")
}

func TestAddCollectionLinks_FirstPage(t *testing.T) {
	data := map[string]interface{}{}
	AddCollectionLinks(data, "/api/items", 1, 10, 50)

	links := data["_links"].(Links)

	assertLink(t, links, "self", "/api/items?page=1&per_page=10")
	assertLink(t, links, "first", "/api/items?page=1&per_page=10")
	assertLink(t, links, "last", "/api/items?page=5&per_page=10")
	assertLink(t, links, "next", "/api/items?page=2&per_page=10")

	if _, ok := links["prev"]; ok {
		t.Error("first page should not have prev link")
	}
}

func TestAddCollectionLinks_LastPage(t *testing.T) {
	data := map[string]interface{}{}
	AddCollectionLinks(data, "/api/items", 5, 10, 50)

	links := data["_links"].(Links)

	assertLink(t, links, "self", "/api/items?page=5&per_page=10")
	assertLink(t, links, "first", "/api/items?page=1&per_page=10")
	assertLink(t, links, "last", "/api/items?page=5&per_page=10")
	assertLink(t, links, "prev", "/api/items?page=4&per_page=10")

	if _, ok := links["next"]; ok {
		t.Error("last page should not have next link")
	}
}

func TestAddCollectionLinks_SinglePage(t *testing.T) {
	data := map[string]interface{}{}
	AddCollectionLinks(data, "/api/items", 1, 20, 5)

	links := data["_links"].(Links)

	assertLink(t, links, "self", "/api/items?page=1&per_page=20")
	assertLink(t, links, "first", "/api/items?page=1&per_page=20")
	assertLink(t, links, "last", "/api/items?page=1&per_page=20")

	if _, ok := links["next"]; ok {
		t.Error("single page should not have next link")
	}
	if _, ok := links["prev"]; ok {
		t.Error("single page should not have prev link")
	}
}

func TestAddCollectionLinks_EmptyResults(t *testing.T) {
	data := map[string]interface{}{}
	AddCollectionLinks(data, "/api/items", 1, 10, 0)

	links := data["_links"].(Links)
	// total=0, perPage=10, ceil(0/10)=0 → clamped to 1
	assertLink(t, links, "self", "/api/items?page=1&per_page=10")
	assertLink(t, links, "first", "/api/items?page=1&per_page=10")
	assertLink(t, links, "last", "/api/items?page=1&per_page=10")
}

func TestAddCollectionLinks_DefaultsZeroValues(t *testing.T) {
	data := map[string]interface{}{}
	AddCollectionLinks(data, "/api/items", 0, 0, 10)

	links := data["_links"].(Links)
	// page 0 → clamped to 1, perPage 0 → clamped to 20
	assertLink(t, links, "self", "/api/items?page=1&per_page=20")
}

func TestAddCollectionLinks_StripsQueryFromBasePath(t *testing.T) {
	data := map[string]interface{}{}
	AddCollectionLinks(data, "/api/items?filter=active&page=99", 1, 10, 30)

	links := data["_links"].(Links)
	// Should strip the existing query params
	for _, link := range links {
		if contains(link.Href, "filter=active") {
			t.Errorf("query params from basePath should be stripped, got %s", link.Href)
		}
	}
}

func TestAddCollectionLinks_PreservesExistingData(t *testing.T) {
	data := map[string]interface{}{
		"name": "test-collection",
	}
	AddCollectionLinks(data, "/api/items", 1, 10, 20)

	if data["name"] != "test-collection" {
		t.Error("existing data was modified")
	}
}

// --- AddEmbedded tests ---

func TestAddEmbedded(t *testing.T) {
	data := map[string]interface{}{}
	resources := []map[string]string{{"id": "1"}, {"id": "2"}}
	AddEmbedded(data, "items", resources)

	embedded, ok := data["_embedded"].(map[string]interface{})
	if !ok {
		t.Fatal("expected _embedded to be map")
	}
	items, ok := embedded["items"]
	if !ok {
		t.Fatal("expected items in _embedded")
	}
	itemsList, ok := items.([]map[string]string)
	if !ok || len(itemsList) != 2 {
		t.Errorf("expected 2 items, got %v", items)
	}
}

func TestAddEmbedded_MergesExisting(t *testing.T) {
	data := map[string]interface{}{
		"_embedded": map[string]interface{}{
			"first": "value1",
		},
	}
	AddEmbedded(data, "second", "value2")

	embedded := data["_embedded"].(map[string]interface{})
	if embedded["first"] != "value1" {
		t.Error("existing embedded resource was overwritten")
	}
	if embedded["second"] != "value2" {
		t.Error("second embedded resource not added")
	}
}

func TestAddEmbedded_NilEmbedded(t *testing.T) {
	data := map[string]interface{}{
		"_embedded": nil,
	}
	AddEmbedded(data, "items", []string{"a"})

	embedded, ok := data["_embedded"].(map[string]interface{})
	if !ok {
		t.Fatal("expected _embedded to be map after nil")
	}
	if embedded["items"] == nil {
		t.Error("expected items to be set")
	}
}

func TestAddEmbedded_PtrMap(t *testing.T) {
	data := &map[string]interface{}{}
	AddEmbedded(data, "tags", []string{"go", "api"})

	embedded, ok := (*data)["_embedded"].(map[string]interface{})
	if !ok {
		t.Fatal("expected _embedded on pointer map")
	}
	if embedded["tags"] == nil {
		t.Error("expected tags in _embedded")
	}
}

// --- HALDocument tests ---

func TestHALDocument_New(t *testing.T) {
	doc := NewHALDocument("/api/items")
	if doc.Links["self"].Href != "/api/items" {
		t.Errorf("expected self=/api/items, got %s", doc.Links["self"].Href)
	}
}

func TestHALDocument_Chaining(t *testing.T) {
	doc := NewHALDocument("/api/items/1").
		WithLink("list", "/api/items").
		WithMethodLink("update", "/api/items/1", "PUT").
		WithData(map[string]string{"id": "1"}).
		WithEmbedded("comments", []string{"c1", "c2"})

	if doc.Links["list"].Href != "/api/items" {
		t.Error("list link not set")
	}
	if doc.Links["update"].Method != "PUT" {
		t.Error("update link not set")
	}
	if doc.Data == nil {
		t.Error("data not set")
	}
	if doc.Embedded["comments"] == nil {
		t.Error("embedded not set")
	}
}

func TestHALDocument_MarshalJSON(t *testing.T) {
	doc := NewHALDocument("/api/items").
		WithLink("next", "/api/items?page=2").
		WithData([]string{"a", "b"})

	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	links, ok := m["_links"].(map[string]interface{})
	if !ok {
		t.Fatal("expected _links in JSON")
	}
	if links["self"] == nil {
		t.Error("expected self link")
	}
	if links["next"] == nil {
		t.Error("expected next link")
	}
	if m["data"] == nil {
		t.Error("expected data")
	}
}

func TestHALDocument_OmitsEmptyFields(t *testing.T) {
	doc := &HALDocument{}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := m["_links"]; ok {
		t.Error("expected _links to be omitted when empty")
	}
	if _, ok := m["_embedded"]; ok {
		t.Error("expected _embedded to be omitted when empty")
	}
	if _, ok := m["data"]; ok {
		t.Error("expected data to be omitted when nil")
	}
}

// --- Integration with APIResponse ---

func TestAPIResponse_WithHALDocument(t *testing.T) {
	doc := NewHALDocument("/api/items/1").
		WithLink("collection", "/api/items").
		WithData(map[string]string{"id": "1", "name": "test"})

	resp := APIResponse{
		Success: true,
		Data:    doc,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	data, ok := m["data"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be object")
	}
	links, ok := data["_links"].(map[string]interface{})
	if !ok {
		t.Fatal("expected _links in data")
	}
	if links["self"] == nil {
		t.Error("expected self link in nested data")
	}
}

func TestAPIResponse_WithLinksInData(t *testing.T) {
	data := map[string]interface{}{
		"id":   1,
		"name": "item",
	}
	AddSelfLink(data, "/api/items/1")
	AddLink(data, "delete", "/api/items/1", "DELETE")

	resp := APIResponse{
		Success: true,
		Data:    data,
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	d := m["data"].(map[string]interface{})
	links := d["_links"].(map[string]interface{})
	if links["self"] == nil {
		t.Error("expected self link")
	}
	if links["delete"] == nil {
		t.Error("expected delete link")
	}
}

// --- Helpers ---

func assertLink(t *testing.T, links Links, rel, expectedHref string) {
	t.Helper()
	link, ok := links[rel]
	if !ok {
		t.Errorf("expected link rel=%s, not found", rel)
		return
	}
	if link.Href != expectedHref {
		t.Errorf("expected href=%s for rel=%s, got %s", expectedHref, rel, link.Href)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
