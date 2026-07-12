package router

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// WebSocketManager is the unified manager for both SSE subscriptions and WebSocket connection limits.
// It replaces the split between SSEHub (global) and WSConnectionLimits (per-Router).
type WebSocketManager struct {
	// SSE Hub — per-task subscriber channels
	sseMu          sync.RWMutex
	sseSubscribers map[string]chan TaskSSEEvent

	// Connection limits
	connMu         sync.RWMutex
	maxPerUser     int
	maxGlobal      int
	maxMessageSize int64
	connTimeout    time.Duration
	userConns      map[string]int
	globalConns    int
}

// WebSocketManagerConfig configures the unified WebSocket manager.
type WebSocketManagerConfig struct {
	MaxPerUser     int
	MaxGlobal      int
	MaxMessageSize int64
	ConnTimeout    time.Duration
}

// DefaultWebSocketManagerConfig returns production-ready defaults.
func DefaultWebSocketManagerConfig() WebSocketManagerConfig {
	return WebSocketManagerConfig{
		MaxPerUser:     5,
		MaxGlobal:      1000,
		MaxMessageSize: 64 * 1024,
		ConnTimeout:    30 * time.Minute,
	}
}

// NewWebSocketManager creates a unified WebSocket manager.
func NewWebSocketManager(cfg WebSocketManagerConfig) *WebSocketManager {
	if cfg.MaxPerUser <= 0 {
		cfg.MaxPerUser = 5
	}
	if cfg.MaxGlobal <= 0 {
		cfg.MaxGlobal = 1000
	}
	if cfg.MaxMessageSize <= 0 {
		cfg.MaxMessageSize = 64 * 1024
	}
	if cfg.ConnTimeout <= 0 {
		cfg.ConnTimeout = 30 * time.Minute
	}

	return &WebSocketManager{
		sseSubscribers: make(map[string]chan TaskSSEEvent),
		maxPerUser:     cfg.MaxPerUser,
		maxGlobal:      cfg.MaxGlobal,
		maxMessageSize: cfg.MaxMessageSize,
		connTimeout:    cfg.ConnTimeout,
		userConns:      make(map[string]int),
	}
}

// --- SSE Hub Methods ---

// SSERegister adds a subscriber channel for a task.
func (m *WebSocketManager) SSERegister(taskID string, ch chan TaskSSEEvent) {
	m.sseMu.Lock()
	defer m.sseMu.Unlock()
	m.sseSubscribers[taskID] = ch
}

// SSEUnregister removes a subscriber channel for a task.
func (m *WebSocketManager) SSEUnregister(taskID string) {
	m.sseMu.Lock()
	defer m.sseMu.Unlock()
	if ch, ok := m.sseSubscribers[taskID]; ok {
		close(ch)
		delete(m.sseSubscribers, taskID)
	}
}

// SSEBroadcast sends an event to the subscriber channel for a task (non-blocking).
func (m *WebSocketManager) SSEBroadcast(taskID string, evt TaskSSEEvent) {
	m.sseMu.RLock()
	ch, ok := m.sseSubscribers[taskID]
	m.sseMu.RUnlock()
	if !ok {
		return
	}
	select {
	case ch <- evt:
	default:
	}
}

// --- Connection Limit Methods ---

// CanConnect checks if a new connection is allowed.
func (m *WebSocketManager) CanConnect(userID string) bool {
	m.connMu.Lock()
	defer m.connMu.Unlock()
	if m.globalConns >= m.maxGlobal {
		return false
	}
	if m.userConns[userID] >= m.maxPerUser {
		return false
	}
	return true
}

// RegisterConnection increments connection count.
func (m *WebSocketManager) RegisterConnection(userID string) {
	m.connMu.Lock()
	defer m.connMu.Unlock()
	m.userConns[userID]++
	m.globalConns++
}

// UnregisterConnection decrements connection count.
func (m *WebSocketManager) UnregisterConnection(userID string) {
	m.connMu.Lock()
	defer m.connMu.Unlock()
	if m.userConns[userID] > 0 {
		m.userConns[userID]--
	}
	if m.userConns[userID] == 0 {
		delete(m.userConns, userID)
	}
	if m.globalConns > 0 {
		m.globalConns--
	}
}

// Stats returns current connection statistics.
func (m *WebSocketManager) Stats() map[string]int {
	m.connMu.RLock()
	defer m.connMu.RUnlock()
	return map[string]int{
		"global":   m.globalConns,
		"max":      m.maxGlobal,
		"per_user": m.maxPerUser,
	}
}

// GetMaxMessageSize returns the configured max message size.
func (m *WebSocketManager) GetMaxMessageSize() int64 {
	return m.maxMessageSize
}

// GetConnTimeout returns the configured connection timeout.
func (m *WebSocketManager) GetConnTimeout() time.Duration {
	return m.connTimeout
}

// --- HTTP Middleware ---

// WSConnectionLimitsMiddleware rejects WebSocket upgrades when limits are exceeded.
func (m *WebSocketManager) WSConnectionLimitsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		userID := ""
		if claims, ok := auth.ClaimsFromContext(req.Context()); ok {
			userID = claims.UserID
		}
		if userID == "" {
			response.Unauthorized(w, "authentication required for WebSocket")
			return
		}
		if !m.CanConnect(userID) {
			slog.Warn("WebSocket connection rejected: limit exceeded", "user_id", userID)
			response.JSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "WebSocket connection limit exceeded",
			})
			return
		}
		next.ServeHTTP(w, req)
	})
}
