package security

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
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

func TestSanitizeInput_NullBytes(t *testing.T) {
	tests := []struct {
		name, input, expected string
	}{
		{"null bytes only", "\x00\x00\x00", ""},
		{"control chars", "\x01\x02\x03test", "test"},
		{"RTL override", "test\u202Eevil", "test\u202Eevil"},
		{"tabs preserved", "a\tb", "a\tb"},
		{"newlines preserved", "a\nb", "a\nb"},
		{"normal", "hello world", "hello world"},
		{"trim spaces", "  hello  ", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeInput(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeInput(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSanitizeFilename_TraversalAndNulls(t *testing.T) {
	tests := []struct {
		name, input, expected string
	}{
		{"dot-dot slash", "../../../etc/passwd", "etc_passwd"},
		{"backslash traversal", "..\\..\\..\\system32", "system32"},
		{"null bytes", "test\x00.go", "test_.go"},
		{"empty", "", "unnamed"},
		{"only dots", "..", "unnamed"},
		{"normal", "normal-file.txt", "normal-file.txt"},
		{"spaces", "file with spaces", "file_with_spaces"},
		{"special chars", "file@#$.txt", "file___.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeFilename(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestEscapeHTML_NestedAndNulls(t *testing.T) {
	tests := []struct {
		name, input string
	}{
		{"nested script", "<script><script>alert(1)</script></script>"},
		{"null between tags", "<script>\x00</script>"},
		{"all special", `<img src="x" onerror="alert(1)">`},
		{"ampersand", "a & b"},
		{"quotes", `"hello"`},
		{"single quote", "'test'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeHTML(tt.input)
			if strings.Contains(got, "<") || strings.Contains(got, ">") {
				t.Errorf("EscapeHTML(%q) still contains HTML: %s", tt.input, got)
			}
		})
	}
}

func TestStripSQLInjection_EncodedAndDouble(t *testing.T) {
	tests := []struct {
		name, input, check string
	}{
		{"basic", "hello; DROP TABLE users; --", "DROP"},
		{"lowercase select", "select * from users", "select"},
		{"UNION attack", "' UNION SELECT password FROM users --", "SELECT"},
		{"comment markers", "/* comment */ SELECT 1", "/*"},
		{"exec", "EXEC sp_who", "EXEC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripSQLInjection(tt.input)
			if strings.Contains(strings.ToLower(got), strings.ToLower(tt.check)) {
				t.Errorf("StripSQLInjection(%q) still contains %q: %s", tt.input, tt.check, got)
			}
		})
	}
}

func TestMaskSecret_EdgeCases(t *testing.T) {
	tests := []struct {
		secret string
		vis    int
		expect string
	}{
		{"abc", 0, "***"},
		{"abc", 5, "***"},
		{"abcdef", 3, "***def"},
		{"a", 1, "*"},
	}
	for _, tt := range tests {
		got := MaskSecret(tt.secret, tt.vis)
		if got != tt.expect {
			t.Errorf("MaskSecret(%q, %d) = %q, want %q", tt.secret, tt.vis, got, tt.expect)
		}
	}
}

func TestEncryptDecryptAES_EdgeCases(t *testing.T) {
	t.Run("empty passphrase", func(t *testing.T) {
		enc, err := EncryptAES("", []byte("test"))
		if err != nil {
			t.Fatal(err)
		}
		dec, err := DecryptAES("", enc)
		if err != nil {
			t.Fatal(err)
		}
		if string(dec) != "test" {
			t.Errorf("got %q", dec)
		}
	})
	t.Run("empty plaintext", func(t *testing.T) {
		enc, err := EncryptAES("key", []byte{})
		if err != nil {
			t.Fatal(err)
		}
		dec, err := DecryptAES("key", enc)
		if err != nil {
			t.Fatal(err)
		}
		if len(dec) != 0 {
			t.Errorf("expected empty, got len=%d", len(dec))
		}
	})
	t.Run("wrong key", func(t *testing.T) {
		enc, _ := EncryptAES("key1", []byte("secret"))
		_, err := DecryptAES("key2", enc)
		if err == nil {
			t.Error("expected error with wrong key")
		}
	})
	t.Run("tampered ciphertext", func(t *testing.T) {
		enc, _ := EncryptAES("key", []byte("data"))
		if len(enc) > 0 {
			enc[len(enc)-1] ^= 0xFF
		}
		_, err := DecryptAES("key", enc)
		if err == nil {
			t.Error("expected error for tampered ciphertext")
		}
	})
	t.Run("truncated ciphertext", func(t *testing.T) {
		enc, _ := EncryptAES("key", []byte("data"))
		_, err := DecryptAES("key", enc[:5])
		if err == nil {
			t.Error("expected error for truncated ciphertext")
		}
	})
	t.Run("nonce reuse detection", func(t *testing.T) {
		enc1, _ := EncryptAES("key", []byte("same"))
		enc2, _ := EncryptAES("key", []byte("same"))
		if string(enc1) == string(enc2) {
			t.Error("same plaintext should produce different ciphertext (random nonce)")
		}
	})
}

func TestValidateAPIKey_Boundaries(t *testing.T) {
	tests := []struct {
		key, prefix string
		valid       bool
	}{
		{"va_" + strings.Repeat("a", 32), "va", true},
		{"va_" + strings.Repeat("a", 128), "va", true},
		{"va_" + strings.Repeat("a", 31), "va", false},
		{"va_" + strings.Repeat("a", 129), "va", true},
		{"va_", "va", false},
		{"", "va", false},
		{"vb_" + strings.Repeat("a", 32), "va", false},
		{"va_short", "va", false},
	}
	for _, tt := range tests {
		got := ValidateAPIKey(tt.key, tt.prefix)
		if got != tt.valid {
			t.Errorf("ValidateAPIKey(%q, %q) = %v, want %v", tt.key, tt.prefix, got, tt.valid)
		}
	}
}

func TestSecurityHeaders_AllExpected(t *testing.T) {
	h := SecurityHeaders()
	expected := []string{
		"X-Content-Type-Options", "X-Frame-Options", "X-XSS-Protection",
		"Strict-Transport-Security", "Content-Security-Policy",
		"Referrer-Policy", "Permissions-Policy",
	}
	for _, k := range expected {
		if _, ok := h[k]; !ok {
			t.Errorf("missing header: %s", k)
		}
	}
}

func TestEncryptDecryptAES_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	var errs int64
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			enc, err := EncryptAES("key", []byte("data"))
			if err != nil {
				return
			}
			dec, err := DecryptAES("key", enc)
			if err != nil || string(dec) != "data" {
				errs++
			}
		}()
	}
	wg.Wait()
	if errs > 0 {
		t.Errorf("concurrent errors: %d", errs)
	}
}

func TestEncodeBase64_RoundTrip(t *testing.T) {
	tests := []struct{ name, input string }{
		{"empty", ""}, {"normal", "hello"}, {"binary", "\x00\x01\x02\xff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := EncodeBase64([]byte(tt.input))
			dec, err := DecodeBase64(enc)
			if err != nil {
				t.Fatal(err)
			}
			if string(dec) != tt.input {
				t.Errorf("round-trip failed")
			}
		})
	}
}

type errReader struct{}

func (e *errReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("rand read failed")
}

func TestEncryptAES_RandReaderFailure(t *testing.T) {
	orig := rand.Reader
	rand.Reader = &errReader{}
	defer func() { rand.Reader = orig }()
	_, err := EncryptAES("key", []byte("test"))
	if err == nil {
		t.Error("expected error when rand.Reader fails")
	}
}

func TestStripSQLInjection_PartialWords(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"selection", false},
		{"selected", false},
		{"SELECT * FROM users", true},
		{"select x", true},
		{"user chose", false},
	}
	for _, tt := range tests {
		got := StripSQLInjection(tt.input) != tt.input
		if got != tt.expected {
			t.Errorf("StripSQLInjection(%q) changed=%v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestDecryptAES_TooShortCiphertext(t *testing.T) {
	_, err := DecryptAES("key", []byte{1, 2, 3})
	if err == nil {
		t.Error("expected error for too-short ciphertext")
	}
}

func TestValidateAPIKey_ExactMinLength(t *testing.T) {
	if !ValidateAPIKey("va_"+strings.Repeat("a", 32), "va") {
		t.Error("32-char body should be valid")
	}
	if ValidateAPIKey("va_"+strings.Repeat("a", 31), "va") {
		t.Error("31-char body should be invalid")
	}
}

func TestValidateAPIKey_ExactMaxLength(t *testing.T) {
	if !ValidateAPIKey("va_"+strings.Repeat("a", 256), "va") {
		t.Error("256-char body should be valid")
	}
	if ValidateAPIKey("va_"+strings.Repeat("a", 257), "va") {
		t.Error("257-char body should be invalid")
	}
}

func TestMaskSecret_ZeroVisible(t *testing.T) {
	got := MaskSecret("secret", 0)
	if got != "******" {
		t.Errorf("expected all masked, got %q", got)
	}
}

func TestSanitizeFilename_LeadingDotsAndUnderscores(t *testing.T) {
	got := SanitizeFilename("..hidden")
	if got != "hidden" {
		t.Errorf("expected 'hidden', got %q", got)
	}
}

func TestStripSQLInjection_AllSymbols(t *testing.T) {
	symbols := []string{"--", ";--", "/*", "*/", "@@", "@"}
	for _, sym := range symbols {
		got := StripSQLInjection("test " + sym + " data")
		if strings.Contains(got, sym) {
			t.Errorf("symbol %q not stripped from %q", sym, got)
		}
	}
}

func TestStripSQLInjection_AllWords(t *testing.T) {
	words := []string{
		"char", "nchar", "varchar", "nvarchar",
		"alter", "begin", "cast", "create", "cursor",
		"declare", "delete", "drop", "end", "exec",
		"execute", "fetch", "insert", "kill", "select",
		"sys", "sysobjects", "syscolumns", "table", "update",
	}
	for _, w := range words {
		input := "SELECT " + w + " FROM test"
		got := StripSQLInjection(input)
		if strings.Contains(strings.ToLower(got), w) {
			t.Errorf("word %q not stripped: %s", w, got)
		}
	}
}

func TestEncryptAES_EmptyData(t *testing.T) {
	enc, err := EncryptAES("key", []byte{})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := DecryptAES("key", enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(dec) != 0 {
		t.Errorf("expected empty plaintext, got %d bytes", len(dec))
	}
}

func TestSanitizeInput_OnlyControlChars(t *testing.T) {
	got := SanitizeInput("\x01\x02\x03\x04\x05")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestSanitizeFilename_MultipleDotDot(t *testing.T) {
	got := SanitizeFilename("....//..//test")
	if strings.Contains(got, "..") {
		t.Errorf("should not contain ..: %q", got)
	}
}

func TestEscapeHTML_EmptyString(t *testing.T) {
	got := EscapeHTML("")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
