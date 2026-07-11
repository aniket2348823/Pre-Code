package router

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vigilagent/vigilagent/internal/agent"
	"github.com/vigilagent/vigilagent/internal/auth"
)

// stripHostPort removes the port from a host string, handling both IPv4
// (1.2.3.4:8080) and IPv6 bracket notation ([::1]:8080) correctly.
func stripHostPort(host string) string {
	if strings.HasPrefix(host, "[") {
		// IPv6 bracket notation: [::1]:port → ::1
		if idx := strings.LastIndex(host, "]:"); idx != -1 {
			return host[1:idx]
		}
		// No port, just [::1] → ::1
		if strings.HasSuffix(host, "]") {
			return host[1 : len(host)-1]
		}
	}
	// IPv4 or bare IPv6: strip after last colon
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		return host[:idx]
	}
	return host
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return false
		}
		host := r.Host
		if host == "" {
			return false
		}
		// Extract host from origin (remove scheme)
		originHost := origin
		if idx := strings.Index(originHost, "://"); idx != -1 {
			originHost = originHost[idx+3:]
		}
		// Strip port from origin and Host header (IPv4 + IPv6 safe)
		originHost = stripHostPort(originHost)
		hostOnly := stripHostPort(host)
		return originHost == hostOnly
	},
}

// WSMessage represents a WebSocket message.
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// WSResponse represents a server-to-client WebSocket response.
type WSResponse struct {
	Type      string      `json:"type"`
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// WSConnection represents an active WebSocket connection.
type WSConnection struct {
	conn   *websocket.Conn
	userID string
	taskID string
	send   chan []byte
	mu     sync.Mutex
}

// agentWebSocket manages WebSocket connections for real-time agent interaction.
type agentWebSocket struct {
	connections map[string]*WSConnection // taskID -> connection
	mu          sync.RWMutex
}

var wsHub = &agentWebSocket{
	connections: make(map[string]*WSConnection),
}

// handleWebSocket upgrades HTTP to WebSocket for real-time agent streaming.
func (r *Router) handleWebSocket(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	wsConn := &WSConnection{
		conn:   conn,
		userID: claims.UserID,
		send:   make(chan []byte, 256),
	}

	// Start read/write pumps
	go wsConn.writePump()
	go wsConn.readPump(r)

	slog.Info("websocket client connected", "user_id", claims.UserID)
}

// BroadcastEvent sends an agent event to the WebSocket client for a task.
func BroadcastEvent(taskID string, event agent.AgentEvent) {
	wsHub.mu.RLock()
	conn, ok := wsHub.connections[taskID]
	wsHub.mu.RUnlock()

	if !ok {
		return
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	select {
	case conn.send <- data:
	default:
		// Connection buffer full, drop event
		slog.Warn("websocket buffer full, dropping event", "task_id", taskID)
	}
}

// RegisterWSConnection registers a WebSocket connection for a task.
func RegisterWSConnection(taskID string, conn *WSConnection) {
	wsHub.mu.Lock()
	defer wsHub.mu.Unlock()
	conn.taskID = taskID
	wsHub.connections[taskID] = conn
}

// UnregisterWSConnection removes a WebSocket connection for a task.
func UnregisterWSConnection(taskID string) {
	wsHub.mu.Lock()
	defer wsHub.mu.Unlock()
	delete(wsHub.connections, taskID)
}

func (c *WSConnection) readPump(r *Router) {
	defer func() {
		UnregisterWSConnection(c.taskID)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Error("websocket read error", "error", err)
			}
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		c.handleMessage(r, msg)
	}
}

func (c *WSConnection) handleMessage(r *Router, msg WSMessage) {
	switch msg.Type {
	case "hitl_approve":
		// Handle human-in-the-loop approval
		var payload struct {
			TaskID       string `json:"task_id"`
			CheckpointID string `json:"checkpoint_id"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			c.sendError("invalid HITL approval payload")
			return
		}
		slog.Info("HITL approved via websocket", "task_id", payload.TaskID, "checkpoint", payload.CheckpointID)
		c.sendResponse("hitl_approved", map[string]string{"checkpoint_id": payload.CheckpointID})

	case "hitl_reject":
		var payload struct {
			TaskID       string `json:"task_id"`
			CheckpointID string `json:"checkpoint_id"`
			Reason       string `json:"reason"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			c.sendError("invalid HITL rejection payload")
			return
		}
		slog.Info("HITL rejected via websocket", "task_id", payload.TaskID)

	case "ping":
		c.sendResponse("pong", nil)

	default:
		c.sendError("unknown message type: " + msg.Type)
	}
}

func (c *WSConnection) sendResponse(msgType string, data interface{}) {
	resp := WSResponse{
		Type:      msgType,
		Success:   true,
		Data:      data,
		Timestamp: time.Now(),
	}
	dataBytes, _ := json.Marshal(resp)
	select {
	case c.send <- dataBytes:
	default:
	}
}

func (c *WSConnection) sendError(errMsg string) {
	resp := WSResponse{
		Type:      "error",
		Success:   false,
		Error:     errMsg,
		Timestamp: time.Now(),
	}
	dataBytes, _ := json.Marshal(resp)
	select {
	case c.send <- dataBytes:
	default:
	}
}

func (c *WSConnection) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
