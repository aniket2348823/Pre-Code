package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func skillsTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestListSkillsHandler_NoAuth(t *testing.T) {
	r := skillsTestRouter()
	req := httptest.NewRequest("GET", "/skills", nil)
	w := httptest.NewRecorder()
	r.listSkillsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestListSkillsHandler_NilRepoPanics(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("GET", "/skills", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.listSkillsHandler(w, req)
	}()
}

func TestGetSkillHandler_NoAuth(t *testing.T) {
	r := skillsTestRouter()
	req := httptest.NewRequest("GET", "/skills/skill-1", nil)
	w := httptest.NewRecorder()
	r.getSkillHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetSkillHandler_NilRepoPanics(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("GET", "/skills/skill-1", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.getSkillHandler(w, req)
	}()
}

func TestCreateSkillHandler_NoAuth(t *testing.T) {
	r := skillsTestRouter()
	req := httptest.NewRequest("POST", "/skills", nil)
	w := httptest.NewRecorder()
	r.createSkillHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateSkillHandler_EmptyBody(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("POST", "/skills", nil, testClaims)
	w := httptest.NewRecorder()
	r.createSkillHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateSkillHandler_EmptyName(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("POST", "/skills", map[string]interface{}{"name": ""}, testClaims)
	w := httptest.NewRecorder()
	r.createSkillHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateSkillHandler_WhitespaceName(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("POST", "/skills", map[string]interface{}{"name": "   "}, testClaims)
	w := httptest.NewRecorder()
	r.createSkillHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateSkillHandler_NilRepoPanics(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("POST", "/skills", map[string]interface{}{"name": "my-skill"}, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.createSkillHandler(w, req)
	}()
}

func TestUpdateSkillHandler_NoAuth(t *testing.T) {
	r := skillsTestRouter()
	req := httptest.NewRequest("PUT", "/skills/skill-1", nil)
	w := httptest.NewRecorder()
	r.updateSkillHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUpdateSkillHandler_NilRepoPanics(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("PUT", "/skills/skill-1", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.updateSkillHandler(w, req)
	}()
}

func TestDeleteSkillHandler_NoAuth(t *testing.T) {
	r := skillsTestRouter()
	req := httptest.NewRequest("DELETE", "/skills/skill-1", nil)
	w := httptest.NewRecorder()
	r.deleteSkillHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeleteSkillHandler_NilRepoPanics(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("DELETE", "/skills/skill-1", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.deleteSkillHandler(w, req)
	}()
}

func TestRateSkillHandler_NoAuth(t *testing.T) {
	r := skillsTestRouter()
	req := httptest.NewRequest("POST", "/skills/skill-1/rate", nil)
	w := httptest.NewRecorder()
	r.rateSkillHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRateSkillHandler_EmptyBody(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("POST", "/skills/skill-1/rate", nil, testClaims)
	w := httptest.NewRecorder()
	r.rateSkillHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRateSkillHandler_InvalidRating(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("POST", "/skills/skill-1/rate", map[string]interface{}{
		"rating": 0,
	}, testClaims)
	w := httptest.NewRecorder()
	r.rateSkillHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRateSkillHandler_RatingTooHigh(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("POST", "/skills/skill-1/rate", map[string]interface{}{
		"rating": 6,
	}, testClaims)
	w := httptest.NewRecorder()
	r.rateSkillHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRateSkillHandler_NilRepoPanics(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("POST", "/skills/skill-1/rate", map[string]interface{}{
		"rating": 5,
	}, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.rateSkillHandler(w, req)
	}()
}

func TestListSkillRatingsHandler_NoAuth(t *testing.T) {
	r := skillsTestRouter()
	req := httptest.NewRequest("GET", "/skills/skill-1/ratings", nil)
	w := httptest.NewRecorder()
	r.listSkillRatingsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestListSkillRatingsHandler_NilRepoPanics(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("GET", "/skills/skill-1/ratings", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.listSkillRatingsHandler(w, req)
	}()
}

func TestInstallSkillHandler_NoAuth(t *testing.T) {
	r := skillsTestRouter()
	req := httptest.NewRequest("POST", "/skills/skill-1/install", nil)
	w := httptest.NewRecorder()
	r.installSkillHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInstallSkillHandler_EmptyBody(t *testing.T) {
	defer func() { recover() }()
	r := skillsTestRouter()
	req := reqWithClaims("POST", "/skills/skill-1/install", nil, testClaims)
	w := httptest.NewRecorder()
	r.installSkillHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestInstallSkillHandler_NilRepoPanics(t *testing.T) {
	r := skillsTestRouter()
	req := reqWithClaims("POST", "/skills/skill-1/install", map[string]interface{}{
		"project_id": "proj-1",
	}, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.installSkillHandler(w, req)
	}()
}

func TestSkillsHandlers_AuthRequired(t *testing.T) {
	r := skillsTestRouter()
	handlers := []struct {
		name   string
		fn     http.HandlerFunc
		method string
		path   string
	}{
		{"list", r.listSkillsHandler, "GET", "/skills"},
		{"get", r.getSkillHandler, "GET", "/skills/x"},
		{"create", r.createSkillHandler, "POST", "/skills"},
		{"update", r.updateSkillHandler, "PUT", "/skills/x"},
		{"delete", r.deleteSkillHandler, "DELETE", "/skills/x"},
		{"rate", r.rateSkillHandler, "POST", "/skills/x/rate"},
		{"ratings", r.listSkillRatingsHandler, "GET", "/skills/x/ratings"},
		{"install", r.installSkillHandler, "POST", "/skills/x/install"},
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
