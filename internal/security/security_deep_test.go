package security

import (
	"strings"
	"sync"
	"testing"
)

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
