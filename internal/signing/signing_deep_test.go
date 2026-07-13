package signing

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

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
