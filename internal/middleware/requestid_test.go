package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// --- helpers ---

// uuidRegex matches the UUID v4 format: 8-4-4-4-12 lowercase hex.
var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func isUUIDv4(s string) bool {
	return uuidRegex.MatchString(s)
}

// reqID is a convenience to extract the X-Request-ID from a response recorder.
func reqID(w *httptest.ResponseRecorder) string {
	return w.Header().Get("X-Request-ID")
}

// --- Tests: NewRequestID middleware ---

func TestNewRequestID_GeneratesID(t *testing.T) {
	var captured string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("X-Request-ID")
	})

	handler := NewRequestID(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if captured == "" {
		t.Fatal("expected X-Request-ID on request")
	}
	if !isUUIDv4(captured) {
		t.Fatalf("expected UUID v4, got %q", captured)
	}
	if rec.Header().Get("X-Request-ID") != captured {
		t.Fatalf("response header %q != request header %q", rec.Header().Get("X-Request-ID"), captured)
	}
}

func TestNewRequestID_PropagatesExistingID(t *testing.T) {
	var captured string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("X-Request-ID")
	})

	handler := NewRequestID(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "my-custom-id-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if captured != "my-custom-id-123" {
		t.Fatalf("expected existing ID preserved, got %q", captured)
	}
	if rec.Header().Get("X-Request-ID") != "my-custom-id-123" {
		t.Fatalf("response header should be 'my-custom-id-123', got %q", rec.Header().Get("X-Request-ID"))
	}
}

func TestNewRequestID_UniquePerRequest(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := NewRequestID(inner)

	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		id := reqID(rec)
		if ids[id] {
			t.Fatalf("duplicate ID on iteration %d: %q", i, id)
		}
		ids[id] = true
	}
}

func TestNewRequestID_EmptyHeaderGeneratesNew(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := NewRequestID(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	id := reqID(rec)
	if id == "" {
		t.Fatal("expected generated ID when header is empty")
	}
	if !isUUIDv4(id) {
		t.Fatalf("expected UUID v4, got %q", id)
	}
}

func TestNewRequestID_PreservesExistingOnAllMethods(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
			handler := NewRequestID(inner)

			req := httptest.NewRequest(method, "/test", nil)
			req.Header.Set("X-Request-ID", "method-id-"+method)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if id := reqID(rec); id != "method-id-"+method {
				t.Errorf("expected 'method-id-%s', got %q", method, id)
			}
		})
	}
}

func TestNewRequestID_JSONResponseContainsRequestID(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rid := RequestIDFromContext(r.Context())
		json.NewEncoder(w).Encode(map[string]string{"request_id": rid})
	})
	handler := NewRequestID(inner)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if body["request_id"] != reqID(rec) {
		t.Fatalf("body request_id %q != header %q", body["request_id"], reqID(rec))
	}
}

func TestNewRequestID_PassesRequestToNext(t *testing.T) {
	var gotURL, gotMethod string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		gotMethod = r.Method
	})
	handler := NewRequestID(inner)

	req := httptest.NewRequest("POST", "/api/v1/items?limit=10", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if gotURL != "/api/v1/items?limit=10" {
		t.Errorf("expected URL '/api/v1/items?limit=10', got %q", gotURL)
	}
	if gotMethod != "POST" {
		t.Errorf("expected method POST, got %q", gotMethod)
	}
}

// --- Tests: RequestIDFromContext ---

func TestRequestIDFromContext_Present(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey, "ctx-123")
	if id := RequestIDFromContext(ctx); id != "ctx-123" {
		t.Fatalf("expected 'ctx-123', got %q", id)
	}
}

func TestRequestIDFromContext_Missing(t *testing.T) {
	if id := RequestIDFromContext(context.Background()); id != "" {
		t.Fatalf("expected empty string, got %q", id)
	}
}

func TestRequestIDFromContext_NilContext(t *testing.T) {
	if id := RequestIDFromContext(nil); id != "" {
		t.Fatalf("expected empty string for nil context, got %q", id)
	}
}

func TestRequestIDFromContext_WrongKeyType(t *testing.T) {
	type otherKey string
	ctx := context.WithValue(context.Background(), otherKey("request_id"), 42)
	if id := RequestIDFromContext(ctx); id != "" {
		t.Fatalf("expected empty string for wrong type, got %q", id)
	}
}

func TestRequestIDFromContext_ViaMiddleware(t *testing.T) {
	var ctxID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = RequestIDFromContext(r.Context())
	})
	handler := NewRequestID(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if ctxID == "" {
		t.Fatal("expected request ID in context")
	}
	if !isUUIDv4(ctxID) {
		t.Fatalf("expected UUID v4 in context, got %q", ctxID)
	}
}

// --- Tests: ContextWithRequestID ---

func TestContextWithRequestID_StoresAndRetrieves(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "my-id")
	if id := RequestIDFromContext(ctx); id != "my-id" {
		t.Fatalf("expected 'my-id', got %q", id)
	}
}

func TestContextWithRequestID_OverwritesExisting(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "first")
	ctx = ContextWithRequestID(ctx, "second")
	if id := RequestIDFromContext(ctx); id != "second" {
		t.Fatalf("expected 'second', got %q", id)
	}
}

func TestContextWithRequestID_NilParentContext(t *testing.T) {
	ctx := ContextWithRequestID(nil, "id-on-nil")
	if id := RequestIDFromContext(ctx); id != "id-on-nil" {
		t.Fatalf("expected 'id-on-nil', got %q", id)
	}
}

func TestContextWithRequestID_EmptyString(t *testing.T) {
	ctx := ContextWithRequestID(context.Background(), "")
	if id := RequestIDFromContext(ctx); id != "" {
		t.Fatalf("expected empty string, got %q", id)
	}
}

func TestContextWithRequestID_DoesNotMutateParent(t *testing.T) {
	parent := context.Background()
	parent = ContextWithRequestID(parent, "parent-id")
	_ = ContextWithRequestID(parent, "child-id")

	if id := RequestIDFromContext(parent); id != "parent-id" {
		t.Fatalf("parent context mutated: expected 'parent-id', got %q", id)
	}
}

func TestContextWithRequestID_ViaMiddlewareDownstream(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := RequestIDFromContext(r.Context())
		w.Header().Set("X-Downstream-ID", id)
		w.WriteHeader(http.StatusOK)
	})
	handler := NewRequestID(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	middlewareID := reqID(rec)
	downstreamID := rec.Header().Get("X-Downstream-ID")

	if middlewareID != downstreamID {
		t.Fatalf("middleware ID %q != downstream ID %q", middlewareID, downstreamID)
	}
}

// --- Tests: generateUUIDv4 ---

func TestGenerateUUIDv4_Format(t *testing.T) {
	id := generateUUIDv4()
	if !isUUIDv4(id) {
		t.Fatalf("not a valid UUID v4: %q", id)
	}
}

func TestGenerateUUIDv4_VersionNibble(t *testing.T) {
	id := generateUUIDv4()
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("expected 5 parts, got %d", len(parts))
	}
	// Third part starts with '4' (version 4)
	if !strings.HasPrefix(parts[2], "4") {
		t.Fatalf("expected version 4 nibble '4', got %q", parts[2][:1])
	}
}

func TestGenerateUUIDv4_VariantBits(t *testing.T) {
	id := generateUUIDv4()
	parts := strings.Split(id, "-")
	// Fourth part starts with 8, 9, a, or b (variant 10xx)
	first := rune(parts[3][0])
	if first != '8' && first != '9' && first != 'a' && first != 'b' {
		t.Fatalf("expected variant 10xx (8/9/a/b), got %q", string(first))
	}
}

func TestGenerateUUIDv4_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := generateUUIDv4()
		if seen[id] {
			t.Fatalf("duplicate UUID at iteration %d: %q", i, id)
		}
		seen[id] = true
	}
}

func TestGenerateUUIDv4_Length(t *testing.T) {
	id := generateUUIDv4()
	// 8 + 1 + 4 + 1 + 4 + 1 + 4 + 1 + 12 = 36
	if len(id) != 36 {
		t.Fatalf("expected 36 chars, got %d: %q", len(id), id)
	}
}

func TestGenerateUUIDv4_Dashes(t *testing.T) {
	id := generateUUIDv4()
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("expected 5 dash-separated parts, got %d", len(parts))
	}
	expectedLens := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != expectedLens[i] {
			t.Errorf("part %d: expected length %d, got %d (%q)", i, expectedLens[i], len(part), part)
		}
	}
}

// --- Integration: middleware + context round-trip ---

func TestIntegration_MiddlewareToContextToResponse(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := RequestIDFromContext(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"request_id": rid,
			"path":       r.URL.Path,
		})
	})
	handler := NewRequestID(inner)

	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	headerID := reqID(rec)
	if !isUUIDv4(headerID) {
		t.Fatalf("header ID not UUID v4: %q", headerID)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if body["request_id"] != headerID {
		t.Fatalf("body ID %v != header ID %q", body["request_id"], headerID)
	}
	if body["path"] != "/api/v1/users" {
		t.Fatalf("unexpected path: %v", body["path"])
	}
}

func TestIntegration_PropagatedIDInContextMatchesHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := RequestIDFromContext(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"rid": rid})
	})
	handler := NewRequestID(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "trace-abc-123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if reqID(rec) != "trace-abc-123" {
		t.Fatalf("expected propagated header, got %q", reqID(rec))
	}

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["rid"] != "trace-abc-123" {
		t.Fatalf("context ID %q != propagated ID", body["rid"])
	}
}
