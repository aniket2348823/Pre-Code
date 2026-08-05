//go:build go1.18

package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func FuzzAPIResponse_MarshalJSON(f *testing.F) {
	f.Add(true, "hello", "", "req-123")
	f.Add(false, "", "error occurred", "req-456")
	f.Add(true, "", "", "")

	f.Fuzz(func(t *testing.T, success bool, data, errStr, reqID string) {
		resp := APIResponse{
			Success:   success,
			RequestID: reqID,
		}
		if data != "" {
			resp.Data = data
		}
		if errStr != "" {
			resp.Error = &ErrorBody{
				Code:      "TEST_CODE",
				Message:   errStr,
				RequestID: reqID,
			}
		}

		b, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var decoded APIResponse
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if decoded.Success != resp.Success {
			t.Errorf("success mismatch: got %v, want %v", decoded.Success, resp.Success)
		}
	})
}

func FuzzErrorBody_MarshalJSON(f *testing.F) {
	f.Add("CODE_001", "something went wrong", "req-abc")
	f.Add("", "", "")
	f.Add("AUTH_004", "invalid token", "req-xyz")

	f.Fuzz(func(t *testing.T, code, message, reqID string) {
		body := &ErrorBody{
			Code:      code,
			Message:   message,
			RequestID: reqID,
		}

		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var decoded ErrorBody
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if decoded.Code != code {
			t.Errorf("code mismatch: got %q, want %q", decoded.Code, code)
		}
	})
}

func FuzzValidationErrorDetail_MarshalJSON(f *testing.F) {
	f.Add("email", "required", "email is required")
	f.Add("name", "min_length", "name too short")
	f.Add("", "", "")

	f.Fuzz(func(t *testing.T, field, rule, msg string) {
		detail := ValidationErrorDetail{
			Field:   field,
			Rule:    rule,
			Message: msg,
		}

		b, err := json.Marshal(detail)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var decoded ValidationErrorDetail
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if decoded.Field != field {
			t.Errorf("field mismatch: got %q, want %q", decoded.Field, field)
		}
	})
}

func FuzzMeta_MarshalJSON(f *testing.F) {
	f.Add(50, 10, 0, true, "")
	f.Add(0, 0, 0, false, "")
	f.Add(100, 20, 80, true, "cursor-abc123")

	f.Fuzz(func(t *testing.T, total, limit, offset int, hasMore bool, cursor string) {
		meta := &Meta{
			Total:      total,
			Limit:      limit,
			Offset:     offset,
			HasMore:    hasMore,
			NextCursor: cursor,
		}

		b, err := json.Marshal(meta)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var decoded Meta
		if err := json.Unmarshal(b, &decoded); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if decoded.Total != total {
			t.Errorf("total mismatch: got %d, want %d", decoded.Total, total)
		}
	})
}

func FuzzWritePaginated(f *testing.F) {
	f.Add(1, 10, 50)
	f.Add(1, 20, 0)
	f.Add(5, 10, 100)
	f.Add(0, 0, 0)
	f.Add(100, 1, 1)

	f.Fuzz(func(t *testing.T, page, perPage, total int) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1/items", nil)
		w.Header().Set("X-Request-Id", "fuzz-req-id")

		WritePaginated(w, r, []string{"item"}, page, perPage, total)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp APIResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}
		if !resp.Success {
			t.Error("expected success=true")
		}
	})
}

func FuzzWriteError(f *testing.F) {
	f.Add("CODE_001", "bad request")
	f.Add("AUTH_004", "invalid token")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, code, message string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/test", nil)

		WriteError(w, r, http.StatusBadRequest, code, message, nil)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}

		var resp APIResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}
		if resp.Success {
			t.Error("expected success=false")
		}
		if resp.Error != nil && resp.Error.Code != code {
			t.Errorf("code mismatch: got %q, want %q", resp.Error.Code, code)
		}
	})
}
