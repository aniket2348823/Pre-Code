package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func tasksTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestCreateTaskHandler_NoAuth(t *testing.T) {
	r := tasksTestRouter()
	req := httptest.NewRequest("POST", "/tasks", nil)
	w := httptest.NewRecorder()
	r.createTaskHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateTaskHandler_EmptyBody(t *testing.T) {
	r := tasksTestRouter()
	req := reqWithClaims("POST", "/tasks", nil, testClaims)
	w := httptest.NewRecorder()
	r.createTaskHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetTaskHandler_NoAuth(t *testing.T) {
	r := tasksTestRouter()
	req := httptest.NewRequest("GET", "/tasks/task-1", nil)
	w := httptest.NewRecorder()
	r.getTaskHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetTaskHandler_NilRepo(t *testing.T) {
	r := tasksTestRouter()
	req := reqWithClaims("GET", "/tasks/task-1", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.getTaskHandler(w, req)
	}()
}

func TestListTasksHandler_NoAuth(t *testing.T) {
	r := tasksTestRouter()
	req := httptest.NewRequest("GET", "/tasks", nil)
	w := httptest.NewRecorder()
	r.listTasksHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestListTasksHandler_MissingProjectID(t *testing.T) {
	r := tasksTestRouter()
	req := reqWithClaims("GET", "/tasks", nil, testClaims)
	w := httptest.NewRecorder()
	r.listTasksHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListTasksHandler_NonMemberProject(t *testing.T) {
	defer func() { recover() }()
	r := tasksTestRouter()
	req := reqWithClaims("GET", "/tasks?project_id=proj-1", nil, testClaims)
	w := httptest.NewRecorder()
	r.listTasksHandler(w, req)
}

func TestCancelTaskHandler_NoAuth(t *testing.T) {
	r := tasksTestRouter()
	req := httptest.NewRequest("POST", "/tasks/task-1/cancel", nil)
	w := httptest.NewRecorder()
	r.cancelTaskHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCancelTaskHandler_NilRepo(t *testing.T) {
	r := tasksTestRouter()
	req := reqWithClaims("POST", "/tasks/task-1/cancel", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.cancelTaskHandler(w, req)
	}()
}

func TestStreamTaskHandler_NoAuth(t *testing.T) {
	r := tasksTestRouter()
	req := httptest.NewRequest("GET", "/tasks/task-1/stream", nil)
	w := httptest.NewRecorder()
	r.streamTaskHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestStreamTaskHandler_NilRepo(t *testing.T) {
	r := tasksTestRouter()
	req := reqWithClaims("GET", "/tasks/task-1/stream", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.streamTaskHandler(w, req)
	}()
}

func TestTasksHandlers_AuthRequired(t *testing.T) {
	r := tasksTestRouter()
	handlers := []struct {
		name   string
		fn     http.HandlerFunc
		method string
		path   string
	}{
		{"create", r.createTaskHandler, "POST", "/tasks"},
		{"get", r.getTaskHandler, "GET", "/tasks/x"},
		{"list", r.listTasksHandler, "GET", "/tasks"},
		{"cancel", r.cancelTaskHandler, "POST", "/tasks/x/cancel"},
		{"stream", r.streamTaskHandler, "GET", "/tasks/x/stream"},
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

func TestCreateTaskHandler_Validation(t *testing.T) {
	r := tasksTestRouter()
	tests := []struct {
		name    string
		body    interface{}
		want400 bool
	}{
		{"nil body", nil, true},
		{"empty object", map[string]interface{}{}, true},
		{"missing project_id", map[string]interface{}{"prompt": "test"}, true},
		{"missing prompt", map[string]interface{}{"project_id": "proj-1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithClaims("POST", "/tasks", tt.body, testClaims)
			w := httptest.NewRecorder()
			r.createTaskHandler(w, req)
			if tt.want400 {
				assert.Equal(t, http.StatusBadRequest, w.Code)
			}
		})
	}
}
