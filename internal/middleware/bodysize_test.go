package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBodySizeLimiter_AllowsGET(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 1024}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_AllowsDELETE(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 1024}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("DELETE", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_LimitsPOST(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 10}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 100)
		n, err := r.Body.Read(buf)
		if n > 0 && err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader("this body is definitely larger than 10 bytes for sure")
	req := httptest.NewRequest("POST", "/", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_LimitsPUT(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 10}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 100)
		n, err := r.Body.Read(buf)
		if n > 0 && err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader("this body is definitely larger than 10 bytes")
	req := httptest.NewRequest("PUT", "/", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_LimitsPATCH(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 10}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 100)
		n, err := r.Body.Read(buf)
		if n > 0 && err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader("this body is definitely larger than 10 bytes")
	req := httptest.NewRequest("PATCH", "/", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_SmallBodyAllowed(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 1024}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 100)
		n, err := r.Body.Read(buf)
		if err != nil && err.Error() != "EOF" {
			http.Error(w, "error", http.StatusBadRequest)
			return
		}
		_ = n
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader(`{"email":"test@example.com"}`)
	req := httptest.NewRequest("POST", "/", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_DefaultConfig(t *testing.T) {
	cfg := DefaultBodySizeConfig()
	assert.Equal(t, int64(10<<20), cfg.MaxBodySize)
}

func TestBodySizeLimiter_NegativeConfigUsesDefault(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: -1}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader("small")
	req := httptest.NewRequest("POST", "/", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_NilBody(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 1024}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleMaxBytesError_NilError(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	handled := HandleMaxBytesError(w, req, nil)
	assert.False(t, handled)
}

func TestHandleMaxBytesError_OtherError(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	handled := HandleMaxBytesError(w, req, errors.New("some other error"))
	assert.False(t, handled)
}

func TestHandleMaxBytesError_MaxBytesError(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	handled := HandleMaxBytesError(w, req, errors.New("http: request body too large"))
	assert.True(t, handled)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}
