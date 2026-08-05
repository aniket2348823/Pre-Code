package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vigilagent/vigilagent/internal/requestid"
)

func BenchmarkJSON(b *testing.B) {
	b.Run("small_payload", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			JSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
	})

	b.Run("large_payload", func(b *testing.B) {
		data := make([]map[string]interface{}, 100)
		for i := range data {
			data[i] = map[string]interface{}{
				"id":     i,
				"name":   "item",
				"active": true,
			}
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			JSON(w, http.StatusOK, data)
		}
	})
}

func BenchmarkSuccess(b *testing.B) {
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		Success(w, http.StatusOK, "test-data")
	}
}

func BenchmarkError(b *testing.B) {
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		Error(w, http.StatusBadRequest, "something went wrong")
	}
}

func BenchmarkErrorR(b *testing.B) {
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("X-Request-Id", "bench-req-123")
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		ErrorR(w, r, http.StatusBadRequest, "CODE_001", "bad request")
	}
}

func BenchmarkSuccessR(b *testing.B) {
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("X-Request-Id", "bench-req-123")
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		SuccessR(w, r, http.StatusOK, "test-data")
	}
}

func BenchmarkSuccessWithMeta(b *testing.B) {
	r := httptest.NewRequest("GET", "/api/v1/items?limit=10", nil)
	meta := &Meta{
		Total:   100,
		Limit:   10,
		Offset:  0,
		HasMore: true,
	}
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		SuccessWithMeta(w, r, http.StatusOK, []string{"a", "b", "c"}, meta)
	}
}

func BenchmarkWritePaginated(b *testing.B) {
	b.Run("first_page", func(b *testing.B) {
		r := httptest.NewRequest("GET", "/api/v1/items?page=1&per_page=10", nil)
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			WritePaginated(w, r, []string{"a", "b"}, 1, 10, 50)
		}
	})

	b.Run("middle_page", func(b *testing.B) {
		r := httptest.NewRequest("GET", "/api/v1/items?page=5&per_page=10", nil)
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			WritePaginated(w, r, []string{"a", "b"}, 5, 10, 50)
		}
	})

	b.Run("last_page", func(b *testing.B) {
		r := httptest.NewRequest("GET", "/api/v1/items?page=5&per_page=10", nil)
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			WritePaginated(w, r, []string{"x"}, 5, 10, 42)
		}
	})

	b.Run("large_dataset", func(b *testing.B) {
		r := httptest.NewRequest("GET", "/api/v1/items?page=1&per_page=100", nil)
		data := make([]string, 100)
		for i := range data {
			data[i] = "item"
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			WritePaginated(w, r, data, 1, 100, 10000)
		}
	})
}

func BenchmarkWriteError(b *testing.B) {
	b.Run("without_details", func(b *testing.B) {
		r := httptest.NewRequest("GET", "/test", nil)
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			WriteError(w, r, http.StatusBadRequest, "VAL_001", "validation failed", nil)
		}
	})

	b.Run("with_details", func(b *testing.B) {
		r := httptest.NewRequest("GET", "/test", nil)
		details := []ValidationErrorDetail{
			{Field: "name", Rule: "required", Message: "name is required"},
			{Field: "email", Rule: "format", Message: "invalid email"},
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			w := httptest.NewRecorder()
			WriteError(w, r, http.StatusBadRequest, "VAL_001", "validation failed", details)
		}
	})
}

func BenchmarkErrorWithDetails(b *testing.B) {
	r := httptest.NewRequest("POST", "/api/v1/items", nil)
	details := []ValidationErrorDetail{
		{Field: "email", Rule: "required", Message: "email is required"},
		{Field: "name", Rule: "min_length", Message: "name too short"},
	}
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		ErrorWithDetails(w, r, http.StatusBadRequest, "VALIDATION_001", "invalid input", details)
	}
}

func BenchmarkValidationErrorResponse(b *testing.B) {
	r := httptest.NewRequest("POST", "/api/v1/items", nil)
	errors := []ValidationErrorDetail{
		{Field: "name", Rule: "required", Message: "name is required"},
		{Field: "email", Rule: "format", Message: "invalid email format"},
	}
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		ValidationErrorResponse(w, r, errors)
	}
}

func BenchmarkConflict(b *testing.B) {
	r := httptest.NewRequest("POST", "/api/v1/items", nil)
	r.Header.Set("X-Request-Id", "bench-conflict")
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		Conflict(w, r, "resource already exists")
	}
}

func BenchmarkTooManyRequests(b *testing.B) {
	r := httptest.NewRequest("GET", "/api/data", nil)
	r.Header.Set("X-Request-Id", "bench-429")
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		TooManyRequests(w, r, "rate limit exceeded")
	}
}

func BenchmarkAPIResponseJSONRoundtrip(b *testing.B) {
	resp := APIResponse{
		Success:   true,
		Data:      map[string]string{"key": "value"},
		RequestID: "bench-roundtrip-123",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b, _ := json.Marshal(resp)
		var decoded APIResponse
		json.Unmarshal(b, &decoded)
	}
}

func BenchmarkErrorBodyJSON(b *testing.B) {
	body := ErrorBody{
		Code:      "AUTH_004",
		Message:   "invalid or expired token",
		RequestID: "bench-err-123",
		Timestamp: "2024-01-01T00:00:00Z",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b, _ := json.Marshal(body)
		var decoded ErrorBody
		json.Unmarshal(b, &decoded)
	}
}

func BenchmarkRid(b *testing.B) {
	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("X-Request-Id", "bench-rid-123")
	_ = requestid.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
	_ = r
	for i := 0; i < b.N; i++ {
		rid(r)
	}
}
