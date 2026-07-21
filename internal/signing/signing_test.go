package signing

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHMACSign(t *testing.T) {
	secret := []byte("test-secret-key")
	sig := HMACSign(secret, "hello world")
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}
	// Same input should produce same output
	sig2 := HMACSign(secret, "hello world")
	if sig != sig2 {
		t.Fatalf("HMAC not deterministic: %s vs %s", sig, sig2)
	}
	// Different input should produce different output
	sig3 := HMACSign(secret, "different input")
	if sig == sig3 {
		t.Fatal("different inputs should produce different signatures")
	}
}

func TestSignAndVerify(t *testing.T) {
	signer := NewSigner("my-secret")
	req, _ := http.NewRequest("GET", "https://api.example.com/users?page=1", nil)
	req.Header.Set("Content-Type", "application/json")

	body := []byte(`{"name":"test"}`)
	if err := signer.SignRequest(req, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	if req.Header.Get("X-Signature") == "" {
		t.Fatal("expected X-Signature header to be set")
	}
	if req.Header.Get("X-Timestamp") == "" {
		t.Fatal("expected X-Timestamp header to be set")
	}

	// Verify should succeed with same body
	if err := signer.VerifyRequest(req, body); err != nil {
		t.Fatalf("VerifyRequest: %v", err)
	}
}

func TestVerifyRejectsTamperedRequest(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	if err := signer.SignRequest(req, nil); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Tamper with the URL
	req.URL.Path = "/tampered"
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Fatal("expected verification failure for tampered request")
	}
}

func TestVerifyRejectsMissingSignature(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Fatal("expected error for missing signature")
	}
}

func TestVerifyRejectsMissingTimestamp(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	req.Header.Set("X-Signature", "fake-sig")
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Fatal("expected error for missing timestamp")
	}
}

func TestVerifyRejectsExpiredTimestamp(t *testing.T) {
	signer := NewSigner("secret")
	signer.SetClockSkew(1 * time.Second)

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	// Set timestamp far in the past
	req.Header.Set("X-Timestamp", "1000000000")
	req.Header.Set("X-Signature", "anything")

	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Fatal("expected error for expired timestamp")
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	signer1 := NewSigner("secret-1")
	signer2 := NewSigner("secret-2")

	req, _ := http.NewRequest("GET", "https://api.example.com/data", nil)
	body := []byte("hello")
	if err := signer1.SignRequest(req, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	if err := signer2.VerifyRequest(req, body); err == nil {
		t.Fatal("expected verification failure with wrong secret")
	}
}

func TestVerifyRejectsTamperedBody(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("POST", "https://api.example.com/data", nil)
	body := []byte(`{"original":"data"}`)
	if err := signer.SignRequest(req, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Verify with different body
	tamperedBody := []byte(`{"tampered":"data"}`)
	if err := signer.VerifyRequest(req, tamperedBody); err == nil {
		t.Fatal("expected verification failure for tampered body")
	}
}

func TestVerifyWithQueryParams(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("GET", "https://api.example.com/search?q=test&page=2", nil)
	if err := signer.SignRequest(req, nil); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Verify with original query
	if err := signer.VerifyRequest(req, nil); err != nil {
		t.Fatalf("VerifyRequest: %v", err)
	}

	// Tamper with query
	req.URL.RawQuery = "q=tampered&page=2"
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Fatal("expected verification failure for tampered query")
	}
}

func TestSignWithDifferentMethods(t *testing.T) {
	signer := NewSigner("secret")
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		req, _ := http.NewRequest(method, "https://api.example.com/resource", nil)
		if err := signer.SignRequest(req, nil); err != nil {
			t.Fatalf("SignRequest for %s: %v", method, err)
		}
		if err := signer.VerifyRequest(req, nil); err != nil {
			t.Fatalf("VerifyRequest for %s: %v", method, err)
		}
	}
}

func TestBuildCanonical(t *testing.T) {
	u, _ := url.Parse("https://api.example.com/users?page=1&limit=10")
	req, _ := http.NewRequest("GET", u.String(), nil)
	req.Header.Set("X-Request-Id", "abc-123")
	req.Header.Set("Authorization", "Bearer token") // should be excluded

	c := buildCanonical(req.Method, req.URL.Path, req.URL.RawQuery, 12345, req.Header, nil)
	if c == "" {
		t.Fatal("expected non-empty canonical string")
	}
	// Should start with method
	if !strings.HasPrefix(c, "GET\n") {
		t.Fatalf("expected canonical to start with GET\\n, got: %q", c)
	}
	// Should contain empty body hash
	if !strings.Contains(c, "empty") {
		t.Fatal("expected 'empty' body marker in canonical string")
	}
}
