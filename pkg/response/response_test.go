package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vigilagent/vigilagent/internal/requestid"
)

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

	var resp APIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
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

	var resp APIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.Success {
		t.Fatal("expected success=false")
	}
	if resp.Error == nil || resp.Error.Message != "something went wrong" {
		t.Errorf("expected error message, got %v", resp.Error)
	}
}

func TestCreated(t *testing.T) {
	w := httptest.NewRecorder()
	Created(w, map[string]string{"id": "123"})

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
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
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
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

	var resp APIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
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

	var resp APIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.Success {
		t.Fatal("expected success=false")
	}
	if resp.Error == nil {
		t.Fatal("expected error to be set")
	}
	if resp.Error.Code != "VALIDATION_001" {
		t.Errorf("expected code=VALIDATION_001, got %s", resp.Error.Code)
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
	var resp APIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.Success {
		t.Fatal("expected success=false")
	}
	if resp.Error == nil {
		t.Fatal("expected error to be set")
	}
}

// Helper to create a request with request ID in context via middleware
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

func TestSuccessR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-123")
	SuccessR(w, r, http.StatusOK, "data")

	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
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
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
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
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Success {
		t.Fatal("expected success=false")
	}
	if resp.Error.Code != "CODE_001" {
		t.Errorf("expected code=CODE_001, got %q", resp.Error.Code)
	}
	if resp.RequestID != "req-err" {
		t.Errorf("expected request_id=req-err, got %q", resp.RequestID)
	}
}

func TestNotFoundR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-nf")
	NotFoundR(w, r, "not found")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "RES_001" {
		t.Errorf("expected code RES_001, got %q", resp.Error.Code)
	}
}

func TestBadRequestR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-br")
	BadRequestR(w, r, "bad request")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "VAL_001" {
		t.Errorf("expected code VAL_001, got %q", resp.Error.Code)
	}
}

func TestUnauthorizedR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-unauth")
	UnauthorizedR(w, r, "unauthorized")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "AUTH_001" {
		t.Errorf("expected code AUTH_001, got %q", resp.Error.Code)
	}
}

func TestForbiddenR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-forbid")
	ForbiddenR(w, r, "forbidden")

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "AUTH_007" {
		t.Errorf("expected code AUTH_007, got %q", resp.Error.Code)
	}
}

func TestInternalErrorR(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-ise")
	InternalErrorR(w, r, "internal error")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "INFRA_002" {
		t.Errorf("expected code INFRA_002, got %q", resp.Error.Code)
	}
}

func TestTooManyRequests(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-429")
	TooManyRequests(w, r, "rate limited")

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "INFRA_001" {
		t.Errorf("expected code INFRA_001, got %q", resp.Error.Code)
	}
}

func TestConflict(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-conflict")
	Conflict(w, r, "conflict")

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "RES_002" {
		t.Errorf("expected code RES_002, got %q", resp.Error.Code)
	}
}

func TestServiceUnavailable(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-503")
	ServiceUnavailable(w, r, "unavailable")

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "INFRA_002" {
		t.Errorf("expected code INFRA_002, got %q", resp.Error.Code)
	}
}

func TestSuccessR_NoRequestID(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	SuccessR(w, r, http.StatusOK, "data")

	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
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

	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.RequestID != "" {
		t.Errorf("expected empty request_id, got %q", resp.RequestID)
	}
}

func TestSuccessWithMeta_NilMeta(t *testing.T) {
	w := httptest.NewRecorder()
	r := reqWithID("req-meta")
	SuccessWithMeta(w, r, http.StatusOK, "data", nil)

	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
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

	var resp APIResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Meta.NextCursor != "abc123" {
		t.Errorf("expected cursor abc123, got %q", resp.Meta.NextCursor)
	}
}
