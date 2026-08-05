package router

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Content from websocket_test.go
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

// Content from websocket_manager_test.go
func TestNewWebSocketManager_Defaults(t *testing.T) {
	cfg := DefaultWebSocketManagerConfig()
	m := NewWebSocketManager(cfg)
	require.NotNil(t, m)
	assert.Equal(t, 5, m.maxPerUser)
	assert.Equal(t, 1000, m.maxGlobal)
	assert.Equal(t, int64(64*1024), m.maxMessageSize)
	assert.Equal(t, 30*time.Minute, m.connTimeout)
}

func TestNewWebSocketManager_ZeroValuesGetDefaults(t *testing.T) {
	cfg := WebSocketManagerConfig{}
	m := NewWebSocketManager(cfg)
	assert.Equal(t, 5, m.maxPerUser)
	assert.Equal(t, 1000, m.maxGlobal)
	assert.Equal(t, int64(64*1024), m.maxMessageSize)
	assert.Equal(t, 30*time.Minute, m.connTimeout)
}

func TestNewWebSocketManager_CustomValues(t *testing.T) {
	cfg := WebSocketManagerConfig{
		MaxPerUser:     10,
		MaxGlobal:      500,
		MaxMessageSize: 1024,
		ConnTimeout:    time.Hour,
	}
	m := NewWebSocketManager(cfg)
	assert.Equal(t, 10, m.maxPerUser)
	assert.Equal(t, 500, m.maxGlobal)
	assert.Equal(t, int64(1024), m.maxMessageSize)
	assert.Equal(t, time.Hour, m.connTimeout)
}

func TestSSEHub_RegisterUnregister(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	ch := make(chan TaskSSEEvent, 16)

	m.SSERegister("task-1", ch)
	m.SSEBroadcast("task-1", TaskSSEEvent{TaskID: "task-1", Event: "test"})
	select {
	case evt := <-ch:
		assert.Equal(t, "task-1", evt.TaskID)
		assert.Equal(t, "test", evt.Event)
	default:
		t.Fatal("expected event on channel")
	}

	m.SSEUnregister("task-1")
	// Broadcast after unregister should not panic
	m.SSEBroadcast("task-1", TaskSSEEvent{TaskID: "task-1", Event: "test2"})
}

func TestSSEHub_BroadcastNonExistent(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	// Should not panic when broadcasting to non-existent task
	m.SSEBroadcast("nonexistent", TaskSSEEvent{TaskID: "nonexistent", Event: "test"})
}

func TestSSEHub_BroadcastFullChannel(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	ch := make(chan TaskSSEEvent, 1) // buffer of 1
	m.SSERegister("task-1", ch)

	// Fill the channel
	m.SSEBroadcast("task-1", TaskSSEEvent{TaskID: "task-1", Event: "first"})
	// This should not block (non-blocking send)
	m.SSEBroadcast("task-1", TaskSSEEvent{TaskID: "task-1", Event: "second"})

	m.SSEUnregister("task-1")
}

func TestConnectionLimits_CanConnect(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	assert.True(t, m.CanConnect("user-1"))
}

func TestConnectionLimits_PerUserLimit(t *testing.T) {
	cfg := WebSocketManagerConfig{MaxPerUser: 2, MaxGlobal: 100}
	m := NewWebSocketManager(cfg)

	m.RegisterConnection("user-1")
	assert.True(t, m.CanConnect("user-1"))
	m.RegisterConnection("user-1")
	assert.False(t, m.CanConnect("user-1"))

	// Different user still allowed
	assert.True(t, m.CanConnect("user-2"))
}

func TestConnectionLimits_GlobalLimit(t *testing.T) {
	cfg := WebSocketManagerConfig{MaxPerUser: 100, MaxGlobal: 2}
	m := NewWebSocketManager(cfg)

	m.RegisterConnection("user-1")
	assert.True(t, m.CanConnect("user-2"))
	m.RegisterConnection("user-2")
	assert.False(t, m.CanConnect("user-3"))
}

func TestConnectionLimits_Unregister(t *testing.T) {
	cfg := WebSocketManagerConfig{MaxPerUser: 1, MaxGlobal: 1}
	m := NewWebSocketManager(cfg)

	m.RegisterConnection("user-1")
	assert.False(t, m.CanConnect("user-1"))

	m.UnregisterConnection("user-1")
	assert.True(t, m.CanConnect("user-1"))
}

func TestConnectionLimits_UnregisterNonExistent(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	// Should not panic
	m.UnregisterConnection("nonexistent")
}

func TestConnectionLimits_UnregisterBelowZero(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	m.UnregisterConnection("user-1") // should not go negative
	assert.Equal(t, 0, m.globalConns)
}

func TestStats(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	m.RegisterConnection("user-1")
	m.RegisterConnection("user-1")

	stats := m.Stats()
	assert.Equal(t, 2, stats["global"])
	assert.Equal(t, 1000, stats["max"])
	assert.Equal(t, 5, stats["per_user"])
}

func TestGetMaxMessageSize(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	assert.Equal(t, int64(64*1024), m.GetMaxMessageSize())
}

func TestGetConnTimeout(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	assert.Equal(t, 30*time.Minute, m.GetConnTimeout())
}

func TestWSConnectionLimitsMiddleware_NoAuth(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := m.WSConnectionLimitsMiddleware(inner)
	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestWSConnectionLimitsMiddleware_WithAuth(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := m.WSConnectionLimitsMiddleware(inner)
	req := reqWithClaims("GET", "/ws", nil, testClaims)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestWSConnectionLimitsMiddleware_ExceedsLimit(t *testing.T) {
	cfg := WebSocketManagerConfig{MaxPerUser: 1, MaxGlobal: 100}
	m := NewWebSocketManager(cfg)
	m.RegisterConnection("user-123")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := m.WSConnectionLimitsMiddleware(inner)
	req := reqWithClaims("GET", "/ws", nil, testClaims)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestWebSocketManager_ConcurrentAccess(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	var wg sync.WaitGroup

	// Concurrent RegisterConnection
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.RegisterConnection("user-1")
			m.CanConnect("user-1")
			m.UnregisterConnection("user-1")
		}(i)
	}
	wg.Wait()
}

func TestWebSocketManager_ConcurrentSSE(t *testing.T) {
	m := NewWebSocketManager(DefaultWebSocketManagerConfig())
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			taskID := "task-" + string(rune('A'+i%26)) + "-" + string(rune('0'+i/26))
			ch := make(chan TaskSSEEvent, 16)
			m.SSERegister(taskID, ch)
			m.SSEBroadcast(taskID, TaskSSEEvent{TaskID: taskID, Event: "test"})
			m.SSEUnregister(taskID)
		}(i)
	}
	wg.Wait()
}
