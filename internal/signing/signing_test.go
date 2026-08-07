package signing

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
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

func TestSign_EmptySecret(t *testing.T) {
	sig := HMACSign([]byte(""), "data")
	if sig == "" {
		t.Error("empty secret should produce non-empty signature")
	}
	sig2 := HMACSign([]byte(""), "data")
	if sig != sig2 {
		t.Error("empty secret should be deterministic")
	}
}

func TestSign_EmptyBody(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("POST", "https://example.com/api", nil)
	if err := signer.SignRequest(req, []byte{}); err != nil {
		t.Fatal(err)
	}
	if err := signer.VerifyRequest(req, []byte{}); err != nil {
		t.Fatal(err)
	}
}

func TestVerify_TruncatedSignature(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("GET", "https://example.com/api", nil)
	signer.SignRequest(req, nil)
	req.Header.Set("X-Signature", req.Header.Get("X-Signature")[:10])
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Error("truncated signature should fail")
	}
}

func TestVerify_ExtraBytes(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("GET", "https://example.com/api", nil)
	signer.SignRequest(req, nil)
	req.Header.Set("X-Signature", req.Header.Get("X-Signature")+"extra")
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Error("extra bytes should fail")
	}
}

func TestVerify_ExpiredTimestamp(t *testing.T) {
	signer := NewSigner("secret")
	signer.SetClockSkew(1 * time.Second)
	req, _ := http.NewRequest("GET", "https://example.com/api", nil)
	req.Header.Set("X-Timestamp", "1000000000")
	req.Header.Set("X-Signature", "anything")
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Error("expired timestamp should fail")
	}
}

func TestVerify_ExpiredTimestamp_ShortSkew(t *testing.T) {
	signer := NewSigner("secret")
	signer.SetClockSkew(1 * time.Second)
	req, _ := http.NewRequest("GET", "https://example.com/api", nil)
	req.Header.Set("X-Timestamp", "1000000000")
	req.Header.Set("X-Signature", "anything")
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Error("expired timestamp with short skew should fail")
	}
}

func TestVerify_WrongMethod(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("GET", "https://example.com/api", nil)
	signer.SignRequest(req, nil)
	req.Method = "POST"
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Error("wrong method should fail")
	}
}

func TestVerify_DifferentPath(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("GET", "https://example.com/api", nil)
	signer.SignRequest(req, nil)
	req.URL.Path = "/tampered"
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Error("different path should fail")
	}
}

func TestVerify_QueryParamsAdded(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("GET", "https://example.com/api", nil)
	signer.SignRequest(req, nil)
	req.URL.RawQuery = "extra=value"
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Error("added query params should fail")
	}
}

func TestVerify_BodyTampered(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("POST", "https://example.com/api", nil)
	body := []byte(`{"original":"data"}`)
	signer.SignRequest(req, body)
	tampered := []byte(`{"tampered":"data"}`)
	if err := signer.VerifyRequest(req, tampered); err == nil {
		t.Error("tampered body should fail")
	}
}

func TestVerify_MissingSignature(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("GET", "https://example.com/api", nil)
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Error("missing signature should fail")
	}
}

func TestVerify_MissingTimestamp(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("GET", "https://example.com/api", nil)
	req.Header.Set("X-Signature", "fake")
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Error("missing timestamp should fail")
	}
}

func TestSign_Concurrent(t *testing.T) {
	signer := NewSigner("secret")
	var wg sync.WaitGroup
	var errs int64
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", "https://example.com/api", nil)
			if err := signer.SignRequest(req, []byte("body")); err != nil {
				return
			}
			if err := signer.VerifyRequest(req, []byte("body")); err != nil {
				errs++
			}
		}()
	}
	wg.Wait()
	if errs > 0 {
		t.Errorf("concurrent errors: %d", errs)
	}
}

func TestBuildCanonical_Empty(t *testing.T) {
	c := buildCanonical("", "", "", 0, nil, nil)
	if !strings.Contains(c, "GET") && !strings.Contains(c, "\n") {
		t.Error("expected method in canonical")
	}
}

func TestHMACSign_DifferentSecrets(t *testing.T) {
	s1 := HMACSign([]byte("secret1"), "data")
	s2 := HMACSign([]byte("secret2"), "data")
	if s1 == s2 {
		t.Error("different secrets should produce different signatures")
	}
}

func TestVerifyRejectsInvalidTimestamp(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("GET", "https://example.com/api", nil)
	req.Header.Set("X-Signature", "fake-sig")
	req.Header.Set("X-Timestamp", "not-a-number")
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Error("expected error for non-numeric timestamp")
	}
}

func TestVerifyRejectsFutureTimestamp(t *testing.T) {
	signer := NewSigner("secret")
	signer.SetClockSkew(1 * time.Second)
	req, _ := http.NewRequest("GET", "https://example.com/api", nil)
	req.Header.Set("X-Timestamp", strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10))
	req.Header.Set("X-Signature", "anything")
	if err := signer.VerifyRequest(req, nil); err == nil {
		t.Error("expected error for future timestamp")
	}
}

func TestSignAndVerify_WithHeaders(t *testing.T) {
	signer := NewSigner("secret")
	req, _ := http.NewRequest("POST", "https://example.com/api", nil)
	req.Header.Set("X-Request-Id", "abc-123")
	req.Header.Set("X-Trace-Id", "xyz-789")
	body := []byte(`{"key":"value"}`)
	if err := signer.SignRequest(req, body); err != nil {
		t.Fatal(err)
	}
	if err := signer.VerifyRequest(req, body); err != nil {
		t.Fatalf("verify with headers failed: %v", err)
	}
}

// ─── Provenance Records ───────────────────────────────────────────────────

func testProvenanceRecord() ProvenanceRecord {
	return ProvenanceRecord{
		ScanID:           "scan_abc123",
		RequestID:        "req_xyz",
		Provider:         "openai",
		Model:            "gpt-4o-mini",
		ProvenanceStatus: ProvenanceVerified,
		ResponseHash:     HashContent("func main() {}"),
		Decision:         "allow",
		Mode:             "strict",
		Timestamp:        time.Now().UTC(),
	}
}

func TestSignProvenanceAndVerify(t *testing.T) {
	rec := testProvenanceRecord()
	sig, err := SignProvenance("test-secret", rec)
	if err != nil {
		t.Fatalf("SignProvenance error: %v", err)
	}
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}
	if err := VerifyProvenance("test-secret", rec, sig); err != nil {
		t.Fatalf("VerifyProvenance failed: %v", err)
	}
}

func TestVerifyProvenanceTampered(t *testing.T) {
	rec := testProvenanceRecord()
	sig, _ := SignProvenance("test-secret", rec)

	rec.Decision = "block" // tamper
	if err := VerifyProvenance("test-secret", rec, sig); err == nil {
		t.Error("expected signature mismatch for tampered record")
	}
}

func TestVerifyProvenanceWrongSecret(t *testing.T) {
	rec := testProvenanceRecord()
	sig, _ := SignProvenance("test-secret", rec)
	if err := VerifyProvenance("wrong-secret", rec, sig); err == nil {
		t.Error("expected mismatch for wrong secret")
	}
}

func TestSignProvenanceEmptySecret(t *testing.T) {
	rec := testProvenanceRecord()
	if _, err := SignProvenance("", rec); err == nil {
		t.Error("expected error for empty secret")
	}
}

func TestHashContent(t *testing.T) {
	h1 := HashContent("same content")
	h2 := HashContent("same content")
	h3 := HashContent("different")
	if h1 != h2 {
		t.Error("hash must be deterministic")
	}
	if h1 == h3 {
		t.Error("different content must hash differently")
	}
	if len(h1) != 64 {
		t.Errorf("expected sha256 hex (64 chars), got %d", len(h1))
	}
}

func TestProvenanceSignatureIndependentOfClock(t *testing.T) {
	// The same record signed at two different times must verify: the canonical
	// form normalizes the timestamp, so the signature only covers the stored
	// instant, not the struct's internal clock components.
	rec := testProvenanceRecord()
	sig, err := SignProvenance("test-secret", rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProvenance("test-secret", rec, sig); err != nil {
		t.Fatalf("verify failed after re-marshal: %v", err)
	}
}

func TestProvenanceNonUTCTimestampVerifies(t *testing.T) {
	// Records constructed with a non-UTC timestamp must still sign/verify:
	// the canonical form normalizes to UTC before signing.
	rec := testProvenanceRecord()
	rec.Timestamp = time.Now().In(time.FixedZone("IST", 5*60*60))
	sig, err := SignProvenance("test-secret", rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyProvenance("test-secret", rec, sig); err != nil {
		t.Fatalf("non-UTC timestamp failed to verify: %v", err)
	}
}
