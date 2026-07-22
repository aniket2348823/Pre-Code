package validation

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestValidatorRequired(t *testing.T) {
	v := New()
	v.Required("name", "")
	if !v.HasErrors() {
		t.Error("expected error for empty field")
	}

	v2 := New()
	v2.Required("name", "John")
	if v2.HasErrors() {
		t.Error("expected no error for non-empty field")
	}
}

func TestValidatorEmail(t *testing.T) {
	v := New()
	v.Email("email", "invalid-email")
	if !v.HasErrors() {
		t.Error("expected error for invalid email")
	}

	v2 := New()
	v2.Email("email", "john@example.com")
	if v2.HasErrors() {
		t.Error("expected no error for valid email")
	}
}

func TestValidatorUUID(t *testing.T) {
	v := New()
	v.UUID("id", "invalid-uuid")
	if !v.HasErrors() {
		t.Error("expected error for invalid UUID")
	}

	v2 := New()
	v2.UUID("id", "8e3a43fe-9404-48c7-8658-21917207a10e")
	if v2.HasErrors() {
		t.Error("expected no error for valid UUID")
	}
}

func TestDecodeAndValidate(t *testing.T) {
	type Request struct {
		Name string `json:"name"`
	}
	body := []byte(`{"name":"John"}`)
	req := httptest.NewRequest("POST", "/test", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	var reqBody Request
	v, ok := DecodeAndValidate(w, req, &reqBody)
	if !ok {
		t.Fatalf("expected successful decode")
	}
	if reqBody.Name != "John" {
		t.Errorf("expected decoded name to be 'John', got %q", reqBody.Name)
	}
	if v.HasErrors() {
		t.Error("expected no initial validation errors")
	}
}
