package security

import (
	"strings"
	"testing"
)

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  hello  ", "hello"},
		{"hello\x00world", "helloworld"},
		{"test\x01\x02\x03", "test"},
		{"normal string", "normal string"},
		{"tab\there", "tab\there"},
		{"newline\nhere", "newline\nhere"},
	}
	for _, tt := range tests {
		got := SanitizeInput(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeInput(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"../../../etc/passwd", "etc_passwd"},
		{"normal-file.txt", "normal-file.txt"},
		{"file with spaces", "file_with_spaces"},
		{"", "unnamed"},
		{"..", "unnamed"},
	}
	for _, tt := range tests {
		got := SanitizeFilename(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestEscapeHTML(t *testing.T) {
	got := EscapeHTML(`<script>alert("xss")</script>`)
	if strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Errorf("EscapeHTML did not escape HTML: %s", got)
	}
	if !strings.Contains(got, "&lt;") {
		t.Error("expected &lt; in escaped output")
	}
}

func TestStripSQLInjection(t *testing.T) {
	got := StripSQLInjection("hello; DROP TABLE users; --")
	if strings.Contains(got, "DROP") {
		t.Error("SQL injection not stripped")
	}
	if strings.Contains(got, "--") {
		t.Error("-- not stripped")
	}
}

func TestMaskSecret(t *testing.T) {
	got := MaskSecret("sk-1234567890abcdef", 4)
	if got != "***************cdef" {
		t.Errorf("MaskSecret = %q, want %q", got, "***************cdef")
	}
	short := MaskSecret("abc", 4)
	if short != "***" {
		t.Errorf("MaskSecret short = %q, want ***", short)
	}
}

func TestEncryptDecryptAES(t *testing.T) {
	passphrase := "my-secret-passphrase"
	plaintext := []byte("hello, world!")

	encrypted, err := EncryptAES(passphrase, plaintext)
	if err != nil {
		t.Fatalf("EncryptAES failed: %v", err)
	}
	if string(encrypted) == string(plaintext) {
		t.Error("encrypted data should differ from plaintext")
	}

	decrypted, err := DecryptAES(passphrase, encrypted)
	if err != nil {
		t.Fatalf("DecryptAES failed: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptAESWrongKey(t *testing.T) {
	encrypted, err := EncryptAES("key1", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecryptAES("key2", encrypted)
	if err == nil {
		t.Error("expected error with wrong key")
	}
}

func TestBase64(t *testing.T) {
	data := []byte("hello, world!")
	encoded := EncodeBase64(data)
	decoded, err := DecodeBase64(encoded)
	if err != nil {
		t.Fatalf("DecodeBase64 failed: %v", err)
	}
	if string(decoded) != string(data) {
		t.Errorf("round-trip failed: got %q", decoded)
	}
}

func TestSecurityHeaders(t *testing.T) {
	headers := SecurityHeaders()
	if len(headers) < 5 {
		t.Errorf("expected at least 5 headers, got %d", len(headers))
	}
	if headers["X-Content-Type-Options"] != "nosniff" {
		t.Error("expected nosniff header")
	}
}

func TestValidateAPIKey(t *testing.T) {
	if !ValidateAPIKey("va_12345678901234567890123456789012", "va") {
		t.Error("expected valid API key")
	}
	if ValidateAPIKey("vb_12345678901234567890123456789012", "va") {
		t.Error("expected invalid prefix")
	}
	if ValidateAPIKey("va_short", "va") {
		t.Error("expected invalid short key")
	}
}

func TestEncryptAESRoundTrip(t *testing.T) {
	passphrase := "test-passphrase"
	for i := 0; i < 10; i++ {
		data := []byte(strings.Repeat("x", 100+i*100))
		encrypted, err := EncryptAES(passphrase, data)
		if err != nil {
			t.Fatalf("iteration %d: EncryptAES failed: %v", i, err)
		}
		decrypted, err := DecryptAES(passphrase, encrypted)
		if err != nil {
			t.Fatalf("iteration %d: DecryptAES failed: %v", i, err)
		}
		if string(decrypted) != string(data) {
			t.Errorf("iteration %d: round-trip failed", i)
		}
	}
}
