package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJSON(t *testing.T) {
	t.Run("sets content type and status", func(t *testing.T) {
		w := httptest.NewRecorder()
		JSON(w, http.StatusOK, map[string]string{"key": "value"})

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
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

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if !resp.Success {
		t.Fatal("expected success=true")
	}
	if resp.Error != "" {
		t.Errorf("expected empty error, got %s", resp.Error)
	}
}

func TestError(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, http.StatusBadRequest, "something went wrong")

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.Success {
		t.Fatal("expected success=false")
	}
	if resp.Error != "something went wrong" {
		t.Errorf("expected error message, got %s", resp.Error)
	}
}

func TestCreated(t *testing.T) {
	w := httptest.NewRecorder()
	Created(w, map[string]string{"id": "123"})

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}
	var resp Response
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
	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "resource not found" {
		t.Errorf("expected error message, got %s", resp.Error)
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

func TestResponseStruct(t *testing.T) {
	t.Run("omits empty fields in JSON", func(t *testing.T) {
		resp := Response{Success: true, Data: "hello"}
		b, _ := json.Marshal(resp)
		var m map[string]interface{}
		json.Unmarshal(b, &m)

		if _, ok := m["error"]; ok {
			t.Fatal("expected error field to be omitted")
		}
		if _, ok := m["message"]; ok {
			t.Fatal("expected message field to be omitted")
		}
	})
}
