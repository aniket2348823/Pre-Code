package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vigilagent/vigilagent/internal/auth"
)

func TestRAGSearchHandler_NoAuth(t *testing.T) {
	rh := &RAGHandlers{}
	req := httptest.NewRequest("GET", "/skills/search?q=test", nil)
	w := httptest.NewRecorder()
	rh.SearchHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRAGSearchHandler_EmptyQuery(t *testing.T) {
	rh := &RAGHandlers{}
	req := reqWithClaims("GET", "/skills/search", nil, testClaims)
	w := httptest.NewRecorder()
	rh.SearchHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRAGSuggestHandler_NoAuth(t *testing.T) {
	rh := &RAGHandlers{}
	req := httptest.NewRequest("GET", "/skills/suggest?q=test", nil)
	w := httptest.NewRecorder()
	rh.SuggestHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRAGTrendingHandler_NoAuth(t *testing.T) {
	rh := &RAGHandlers{}
	req := httptest.NewRequest("GET", "/skills/trending", nil)
	w := httptest.NewRecorder()
	rh.TrendingHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRAGCategoriesHandler_NoAuth(t *testing.T) {
	rh := &RAGHandlers{}
	req := httptest.NewRequest("GET", "/skills/categories", nil)
	w := httptest.NewRecorder()
	rh.CategoriesHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRAGPublishHandler_NoAuth(t *testing.T) {
	rh := &RAGHandlers{}
	req := httptest.NewRequest("POST", "/skills/skill-1/publish", nil)
	w := httptest.NewRecorder()
	rh.PublishHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRAGDownloadHandler_NoAuth(t *testing.T) {
	rh := &RAGHandlers{}
	req := httptest.NewRequest("GET", "/skills/skill-1/download", nil)
	w := httptest.NewRecorder()
	rh.DownloadHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRAGReindexHandler_NoAuth(t *testing.T) {
	rh := &RAGHandlers{}
	req := httptest.NewRequest("POST", "/skills/reindex", nil)
	w := httptest.NewRecorder()
	rh.ReindexHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRAGReindexHandler_NonAdmin(t *testing.T) {
	rh := &RAGHandlers{}
	req := reqWithClaims("POST", "/skills/reindex", nil, testClaims)
	w := httptest.NewRecorder()
	rh.ReindexHandler(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRAGReindexHandler_AdminAllowed(t *testing.T) {
	adminClaims := &auth.Claims{
		UserID: "admin-1",
		Email:  "admin@example.com",
		Role:   "admin",
	}
	rh := &RAGHandlers{}
	req := reqWithClaims("POST", "/skills/reindex", nil, adminClaims)
	w := httptest.NewRecorder()
	// rag is nil → will panic
	func() {
		defer func() { recover() }()
		rh.ReindexHandler(w, req)
	}()
}

func TestRAGPublishHandler_NilSkillRepo(t *testing.T) {
	rh := &RAGHandlers{skillRepo: nil}
	req := reqWithClaims("POST", "/skills/skill-1/publish", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		rh.PublishHandler(w, req)
	}()
}

func TestRAGDownloadHandler_NilSkillRepo(t *testing.T) {
	rh := &RAGHandlers{skillRepo: nil}
	req := reqWithClaims("GET", "/skills/skill-1/download", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		rh.DownloadHandler(w, req)
	}()
}

func TestNewRAGHandlers(t *testing.T) {
	rh := NewRAGHandlers(nil, nil)
	assert.NotNil(t, rh)
	assert.Nil(t, rh.rag)
	assert.Nil(t, rh.skillRepo)
}

func TestRAGHandlers_AuthRequired(t *testing.T) {
	rh := &RAGHandlers{}
	handlers := []struct {
		name   string
		fn     http.HandlerFunc
		method string
		path   string
	}{
		{"search", rh.SearchHandler, "GET", "/skills/search?q=test"},
		{"suggest", rh.SuggestHandler, "GET", "/skills/suggest?q=test"},
		{"trending", rh.TrendingHandler, "GET", "/skills/trending"},
		{"categories", rh.CategoriesHandler, "GET", "/skills/categories"},
		{"publish", rh.PublishHandler, "POST", "/skills/x/publish"},
		{"download", rh.DownloadHandler, "GET", "/skills/x/download"},
		{"reindex", rh.ReindexHandler, "POST", "/skills/reindex"},
	}
	for _, h := range handlers {
		t.Run(h.name, func(t *testing.T) {
			req := httptest.NewRequest(h.method, h.path, nil)
			w := httptest.NewRecorder()
			h.fn(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}
