package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func websocketTestRouter() *Router {
	return &Router{Mux: chi.NewMux(), wsManager: NewWebSocketManager(DefaultWebSocketManagerConfig())}
}

func TestHandleWebSocket_NoAuth(t *testing.T) {
	r := websocketTestRouter()
	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()
	r.handleWebSocket(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleWebSocket_WithAuth(t *testing.T) {
	r := websocketTestRouter()
	req := reqWithClaims("GET", "/ws", nil, testClaims)
	w := httptest.NewRecorder()
	r.handleWebSocket(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	result := parseJSON(t, w)
	assert.Contains(t, result["message"], "SSE")
	assert.Contains(t, result["sse_endpoint"], "/api/v1/tasks/{taskID}/stream")
}

func TestHandleWebSocket_EventsList(t *testing.T) {
	r := websocketTestRouter()
	req := reqWithClaims("GET", "/ws", nil, testClaims)
	w := httptest.NewRecorder()
	r.handleWebSocket(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	result := parseJSON(t, w)
	events, ok := result["events"].([]interface{})
	assert.True(t, ok)
	assert.True(t, len(events) > 0)
}

func TestHandleWebSocket_Timestamp(t *testing.T) {
	r := websocketTestRouter()
	req := reqWithClaims("GET", "/ws", nil, testClaims)
	w := httptest.NewRecorder()
	r.handleWebSocket(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	result := parseJSON(t, w)
	_, hasTimestamp := result["timestamp"]
	assert.True(t, hasTimestamp)
}

func TestHandleWebSocket_Usage(t *testing.T) {
	r := websocketTestRouter()
	req := reqWithClaims("GET", "/ws", nil, testClaims)
	w := httptest.NewRecorder()
	r.handleWebSocket(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	result := parseJSON(t, w)
	usage, ok := result["usage"].(string)
	assert.True(t, ok)
	assert.Contains(t, usage, "Authorization")
}

func TestHandleWebSocket_NilManager(t *testing.T) {
	r := &Router{Mux: chi.NewMux(), wsManager: nil}
	req := reqWithClaims("GET", "/ws", nil, testClaims)
	w := httptest.NewRecorder()
	r.handleWebSocket(w, req)
	// With nil wsManager, should still work (nil check in handler)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleWebSocket_ConnectionLimit(t *testing.T) {
	r := websocketTestRouter()
	// Fill up the user's connection limit
	for i := 0; i < 5; i++ {
		r.wsManager.RegisterConnection("user-123")
	}
	req := reqWithClaims("GET", "/ws", nil, testClaims)
	w := httptest.NewRecorder()
	r.handleWebSocket(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestHandleWebSocket_AuthRequired(t *testing.T) {
	r := websocketTestRouter()
	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()
	r.handleWebSocket(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTaskSSEEvent_Fields(t *testing.T) {
	evt := TaskSSEEvent{
		TaskID:  "task-1",
		Event:   "hitl_decision",
		Payload: map[string]interface{}{"decision": "approve"},
	}
	assert.Equal(t, "task-1", evt.TaskID)
	assert.Equal(t, "hitl_decision", evt.Event)
	assert.Equal(t, "approve", evt.Payload["decision"])
}
