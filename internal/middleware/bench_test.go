package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func BenchmarkSanitizeInput(b *testing.B) {
	b.Run("normal", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			SanitizeInput("hello world")
		}
	})
	b.Run("xss", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			SanitizeInput("hello world <script>alert(1)</script>")
		}
	})
	b.Run("empty", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			SanitizeInput("")
		}
	})
	b.Run("long", func(b *testing.B) {
		long := make([]byte, 10000)
		for i := range long {
			long[i] = 'a'
		}
		s := string(long)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			SanitizeInput(s)
		}
	})
	b.Run("control_chars", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			SanitizeInput("hello\x00\x01\x02\x03world\x04\x05")
		}
	})
}

func BenchmarkDetectSQLInjection(b *testing.B) {
	b.Run("positive", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			DetectSQLInjection("1' OR '1'='1")
		}
	})
	b.Run("negative", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			DetectSQLInjection("hello world")
		}
	})
	b.Run("complex", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			DetectSQLInjection("'; DROP TABLE users; SELECT * FROM orders WHERE id=1; --")
		}
	})
	b.Run("long_input", func(b *testing.B) {
		long := "SELECT * FROM users WHERE name = 'test' AND password = 'pass' UNION SELECT * FROM secrets"
		for i := 0; i < b.N; i++ {
			DetectSQLInjection(long)
		}
	})
}

func BenchmarkDetectXSS(b *testing.B) {
	b.Run("script_tag", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			DetectXSS("<script>alert(1)</script>")
		}
	})
	b.Run("javascript_uri", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			DetectXSS("javascript:alert(1)")
		}
	})
	b.Run("safe_input", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			DetectXSS("hello world")
		}
	})
	b.Run("event_handler", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			DetectXSS("<img onerror=alert(1) src=x>")
		}
	})
}

func BenchmarkSanitizeFilename(b *testing.B) {
	b.Run("normal", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			SanitizeFilename("document.pdf")
		}
	})
	b.Run("traversal", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			SanitizeFilename("../../../etc/passwd")
		}
	})
	b.Run("encoded", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			SanitizeFilename("%2e%2e%2f%2e%2e%2fetc%2fpasswd")
		}
	})
}

func BenchmarkCSRFProtect(b *testing.B) {
	secret := []byte("test-secret-key-for-csrf-32bytes!")
	m := NewCSRFMiddleware(secret)
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.Run("GET", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})

	b.Run("POST_valid", func(b *testing.B) {
		rawToken := "test-token"
		validToken := rawToken + "." + m.signToken(rawToken)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("POST", "/", nil)
			req.Header.Set("X-CSRF-Token", validToken)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})

	b.Run("POST_invalid", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("POST", "/", nil)
			req.Header.Set("X-CSRF-Token", "invalid-token")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})
}

func BenchmarkLockoutMiddleware(b *testing.B) {
	al := NewAccountLockout(5, 1*time.Minute)
	keyFunc := func(r *http.Request) string { return "user1" }
	handler := LockoutMiddleware(al, keyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.Run("not_locked", func(b *testing.B) {
		al.RecordSuccess(b.Context(), "user1")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})

	b.Run("locked", func(b *testing.B) {
		for i := 0; i < 5; i++ {
			al.RecordFailure(b.Context(), "user1")
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})
}

func BenchmarkRateLimitHeaders(b *testing.B) {
	rlm := NewRateLimitHeadersMiddleware(10000, time.Minute)
	handler := rlm.Middleware(func(r *http.Request) string {
		return "bench-key"
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.Run("single_key", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})

	b.Run("different_keys", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				req := httptest.NewRequest("GET", "/", nil)
				req.Header.Set("X-Key", "key")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				i++
			}
		})
	})
}

func BenchmarkCompareTokens(b *testing.B) {
	token := "test-token-value"
	for i := 0; i < b.N; i++ {
		compareTokens(token, token)
	}
}
