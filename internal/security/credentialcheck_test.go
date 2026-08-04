package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScanString_AWSKey(t *testing.T) {
	scanner := NewCredentialScanner()
	input := "aws_access_key_id = AKIAIOSFODNN7EXAMPLE"
	result := scanner.ScanString(input)
	if len(result.Leaks) == 0 {
		t.Fatal("expected leak detection for AWS key")
	}
	if result.Leaks[0].PatternName != "aws_access_key" {
		t.Errorf("expected aws_access_key pattern, got %s", result.Leaks[0].PatternName)
	}
	if strings.Contains(result.RedactedBody, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("secret should be redacted from output")
	}
}

func TestScanString_GitHubToken(t *testing.T) {
	scanner := NewCredentialScanner()
	input := `{"token": "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"}`
	result := scanner.ScanString(input)
	if len(result.Leaks) == 0 {
		t.Fatal("expected leak detection for GitHub PAT")
	}
	if result.Leaks[0].PatternName != "github_token" {
		t.Errorf("expected github_token pattern, got %s", result.Leaks[0].PatternName)
	}
}

func TestScanString_SlackToken(t *testing.T) {
	scanner := NewCredentialScanner()
	input := "SLACK_TOKEN=" + "xoxb-1234" + "56789012" + "-1234567890123-" + "AbCdEfGhIjKlMnOpQrStUvWx"
	result := scanner.ScanString(input)
	if len(result.Leaks) == 0 {
		t.Fatal("expected leak detection for Slack token")
	}
	if result.Leaks[0].PatternName != "slack_token" {
		t.Errorf("expected slack_token pattern, got %s", result.Leaks[0].PatternName)
	}
}

func TestScanString_PrivateKey(t *testing.T) {
	scanner := NewCredentialScanner()
	input := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA..."
	result := scanner.ScanString(input)
	if len(result.Leaks) == 0 {
		t.Fatal("expected leak detection for private key")
	}
	if result.Leaks[0].PatternName != "private_key_block" {
		t.Errorf("expected private_key_block, got %s", result.Leaks[0].PatternName)
	}
}

func TestScanString_StripeKey(t *testing.T) {
	scanner := NewCredentialScanner()
	input := "stripe_key = " + "sk_live_" + "abc123def456ghi" + "789jkl012mno"
	result := scanner.ScanString(input)
	if len(result.Leaks) == 0 {
		t.Fatal("expected leak detection for Stripe key")
	}
	if result.Leaks[0].PatternName != "stripe_key" {
		t.Errorf("expected stripe_key, got %s", result.Leaks[0].PatternName)
	}
}

func TestScanString_GoogleAPIKey(t *testing.T) {
	scanner := NewCredentialScanner()
	input := "api_key: " + string([]byte{'A', 'I', 'z', 'a'}) + "SAMPLEKEY000000000000000000000000"
	result := scanner.ScanString(input)
	if len(result.Leaks) == 0 {
		t.Fatal("expected leak detection for Google API key")
	}
	if result.Leaks[0].PatternName != "google_api_key" {
		t.Errorf("expected google_api_key, got %s", result.Leaks[0].PatternName)
	}
}

func TestScanString_Password(t *testing.T) {
	scanner := NewCredentialScanner()
	input := `password: "SuperSecret123!"`
	result := scanner.ScanString(input)
	found := false
	for _, l := range result.Leaks {
		if l.PatternName == "password_field" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected leak detection for password field")
	}
}

func TestScanString_JWT(t *testing.T) {
	scanner := NewCredentialScanner()
	input := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoiMTIzNCJ9.signature123"
	result := scanner.ScanString(input)
	found := false
	for _, l := range result.Leaks {
		if l.PatternName == "jwt_token" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected leak detection for JWT token")
	}
}

func TestScanString_EmailDisabled(t *testing.T) {
	scanner := NewCredentialScanner()
	input := "Contact user@example.com for more info"
	result := scanner.ScanString(input)
	for _, l := range result.Leaks {
		if l.PatternName == "email_address" {
			t.Error("email detection should be disabled by default")
		}
	}
}

func TestScanString_EmailEnabled(t *testing.T) {
	scanner := NewCredentialScanner()
	scanner.SetRedactionRule(RedactionRule{
		PatternName: "email_address",
		Replacement: "[REDACTED_EMAIL]",
		Enabled:     true,
	})
	input := "Contact user@example.com for more info"
	result := scanner.ScanString(input)
	found := false
	for _, l := range result.Leaks {
		if l.PatternName == "email_address" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected email detection when enabled")
	}
	if strings.Contains(result.RedactedBody, "user@example.com") {
		t.Error("email should be redacted")
	}
}

func TestScanString_GitHubFineGrainedPAT(t *testing.T) {
	scanner := NewCredentialScanner()
	// github_pat_ + 82+ alphanumeric/underscore chars
	pat := "github_pat_" + strings.Repeat("A", 82)
	result := scanner.ScanString(pat)
	if len(result.Leaks) == 0 {
		t.Fatal("expected leak for GitHub fine-grained PAT")
	}
	if result.Leaks[0].PatternName != "github_fine_grained_pat" {
		t.Errorf("expected github_fine_grained_pat, got %s", result.Leaks[0].PatternName)
	}
}

func TestScanString_GenericSecret(t *testing.T) {
	scanner := NewCredentialScanner()
	input := `api_key = "abcdefghij1234567890abcdefghij"`
	result := scanner.ScanString(input)
	found := false
	for _, l := range result.Leaks {
		if l.PatternName == "generic_secret" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected generic_secret detection")
	}
}

func TestScanString_NoFalsePositive(t *testing.T) {
	scanner := NewCredentialScanner()
	input := "The quick brown fox jumps over the lazy dog. Nothing to see here."
	result := scanner.ScanString(input)
	if len(result.Leaks) != 0 {
		t.Errorf("expected no leaks, got %d", len(result.Leaks))
	}
	if result.RedactedBody != input {
		t.Error("clean text should not be modified")
	}
}

func TestScanBytes_Multiline(t *testing.T) {
	scanner := NewCredentialScanner()
	input := []byte("line one\naws_access_key_id = AKIAIOSFODNN7EXAMPLE\nline three")
	result := scanner.ScanBytes(input)
	if len(result.Leaks) == 0 {
		t.Fatal("expected leak in multiline scan")
	}
	if result.Leaks[0].LineNumber != 2 {
		t.Errorf("expected leak on line 2, got line %d", result.Leaks[0].LineNumber)
	}
}

func TestCredentialLeakMiddleware(t *testing.T) {
	scanner := NewCredentialScanner()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("aws_access_key_id = AKIAIOSFODNN7EXAMPLE"))
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	mw := CredentialLeakMiddleware(scanner)
	mw(handler).ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("middleware should redact AWS key from response")
	}
}

func TestCredentialLeakMiddleware_NoLeak(t *testing.T) {
	scanner := NewCredentialScanner()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world"))
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	mw := CredentialLeakMiddleware(scanner)
	mw(handler).ServeHTTP(rec, req)

	if rec.Body.String() != "hello world" {
		t.Errorf("clean response should pass through unchanged: got %q", rec.Body.String())
	}
}

func TestSetRedactionRule_Disable(t *testing.T) {
	scanner := NewCredentialScanner()
	scanner.SetRedactionRule(RedactionRule{
		PatternName: "aws_access_key",
		Replacement: "[REDACTED]",
		Enabled:     false,
	})

	input := "key = AKIAIOSFODNN7EXAMPLE"
	result := scanner.ScanString(input)
	for _, l := range result.Leaks {
		if l.PatternName == "aws_access_key" {
			t.Error("disabled pattern should not be detected")
		}
	}
}

func TestScanString_LineNumberTracking(t *testing.T) {
	scanner := NewCredentialScanner()
	input := "line 1\nline 2\nghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij\nline 4"
	result := scanner.ScanBytes([]byte(input))
	if len(result.Leaks) == 0 {
		t.Fatal("expected leak")
	}
	if result.Leaks[0].LineNumber != 3 {
		t.Errorf("expected line 3, got %d", result.Leaks[0].LineNumber)
	}
}

func TestScanString_MultipleLeaks(t *testing.T) {
	scanner := NewCredentialScanner()
	input := "aws_access_key_id = AKIAIOSFODNN7EXAMPLE\ntoken = ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	result := scanner.ScanString(input)
	if len(result.Leaks) < 2 {
		t.Errorf("expected at least 2 leaks, got %d", len(result.Leaks))
	}
}

func TestScanString_ContextTruncation(t *testing.T) {
	scanner := NewCredentialScanner()
	longLine := strings.Repeat("x", 200) + " AKIAIOSFODNN7EXAMPLE"
	result := scanner.ScanString(longLine)
	if len(result.Leaks) == 0 {
		t.Fatal("expected leak")
	}
	if len(result.Leaks[0].Context) > 130 {
		t.Errorf("context should be truncated, got length %d", len(result.Leaks[0].Context))
	}
}
