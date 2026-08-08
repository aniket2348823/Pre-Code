//go:build go1.18

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var uuidFuzzRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func FuzzGenerateUUIDv4(f *testing.F) {
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, _ []byte) {
		id := generateUUIDv4()
		if len(id) != 36 {
			t.Fatalf("expected 36 chars, got %d: %q", len(id), id)
		}
		if !uuidFuzzRegex.MatchString(id) {
			t.Fatalf("not a valid UUID v4: %q", id)
		}
		parts := strings.Split(id, "-")
		if len(parts) != 5 {
			t.Fatalf("expected 5 parts, got %d", len(parts))
		}
		if !strings.HasPrefix(parts[2], "4") {
			t.Fatalf("expected version 4 nibble '4', got %q", parts[2][:1])
		}
		first := rune(parts[3][0])
		if first != '8' && first != '9' && first != 'a' && first != 'b' {
			t.Fatalf("expected variant 10xx (8/9/a/b), got %q", string(first))
		}
	})
}

func FuzzGenerateUUIDv4_Unique(f *testing.F) {
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, _ []byte) {
		seen := make(map[string]bool)
		for i := 0; i < 100; i++ {
			id := generateUUIDv4()
			if seen[id] {
				t.Fatalf("duplicate UUID at iteration %d: %q", i, id)
			}
			seen[id] = true
		}
	})
}

func FuzzRequestIDFromContext(f *testing.F) {
	f.Add("req-abc-123")
	f.Add("")
	f.Add(strings.Repeat("x", 10000))

	f.Fuzz(func(t *testing.T, id string) {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := RequestIDFromContext(r.Context())
			if got != id {
				t.Errorf("context ID mismatch: got %q, want %q", got, id)
			}
		})
		handler := NewRequestID(inner)

		req := httptest.NewRequest("GET", "/test", nil)
		if id != "" {
			req.Header.Set("X-Request-ID", id)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	})
}

func FuzzContextWithRequestID(f *testing.F) {
	f.Add("my-id", "check-id")
	f.Add("", "")
	f.Add("id-1", "id-2")

	f.Fuzz(func(t *testing.T, storeID, checkID string) {
		ctx := ContextWithRequestID(context.TODO(), storeID)
		got := RequestIDFromContext(ctx)
		if got != storeID {
			t.Fatalf("stored ID mismatch: got %q, want %q", got, storeID)
		}
		_ = checkID
	})
}

func FuzzNewRequestID_PropagateExisting(f *testing.F) {
	f.Add("my-custom-id")
	f.Add("")
	f.Add(strings.Repeat("z", 5000))

	f.Fuzz(func(t *testing.T, existingID string) {
		var captured string
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured = r.Header.Get("X-Request-ID")
		})
		handler := NewRequestID(inner)

		req := httptest.NewRequest("GET", "/test", nil)
		if existingID != "" {
			req.Header.Set("X-Request-ID", existingID)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if existingID != "" {
			if captured != existingID {
				t.Errorf("expected existing ID %q, got %q", existingID, captured)
			}
		} else {
			if captured == "" {
				t.Fatal("expected generated ID when none provided")
			}
			if !uuidFuzzRegex.MatchString(captured) {
				t.Fatalf("generated ID not valid UUID v4: %q", captured)
			}
		}
	})
}
