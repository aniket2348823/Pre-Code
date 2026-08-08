package websocket

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Content from websocket.go
const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 64 * 1024
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Connection wraps a gorilla/websocket connection with send channel and metadata.
type Connection struct {
	conn     *websocket.Conn
	send     chan Event
	hub      *Hub
	meta     ConnectionMeta
	authDone bool
}

// Handler provides the WebSocket upgrade endpoint.
type Handler struct {
	hub          *Hub
	authenticate func(token string) (userID string, ok bool)
}

// NewHandler creates a new WebSocket handler.
func NewHandler(hub *Hub, authenticate func(string) (string, bool)) *Handler {
	return &Handler{
		hub:          hub,
		authenticate: authenticate,
	}
}

// ServeHTTP handles the WebSocket upgrade.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	c := &Connection{
		conn: conn,
		send: make(chan Event, 256),
		hub:  h.hub,
		meta: ConnectionMeta{
			Channels:  make(map[string]bool),
			CreatedAt: time.Now(),
		},
	}

	h.hub.Register(c)

	go c.writePump()
	go c.readPump(h.authenticate)
}

// readPump reads messages from the WebSocket connection.
// The first message must be an auth message with a token.
func (c *Connection) readPump(authenticate func(string) (string, bool)) {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("websocket read error", "error", err)
			}
			return
		}

		var msg ClientMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		if !c.authDone {
			if authenticate == nil {
				slog.Warn("websocket auth: no authenticator configured")
				return
			}
			userID, ok := authenticate(msg.Token)
			if !ok {
				c.conn.WriteJSON(map[string]string{"error": "authentication failed"})
				return
			}
			c.meta.UserID = userID
			c.authDone = true

			// Re-publish meta to the hub: Register() stored a value copy with an
			// empty UserID (auth happens after registration), so SendToUser would
			// otherwise never match this connection.
			c.hub.mu.Lock()
			if _, ok := c.hub.connections[c]; ok {
				c.hub.connections[c] = c.meta
			}
			c.hub.mu.Unlock()

			c.conn.WriteJSON(map[string]string{"type": "authenticated", "user_id": userID})
			continue
		}

		switch msg.Type {
		case "subscribe":
			c.hub.mu.Lock()
			for _, ch := range msg.Channels {
				c.meta.Channels[ch] = true
			}
			c.hub.mu.Unlock()
		case "unsubscribe":
			c.hub.mu.Lock()
			for _, ch := range msg.Channels {
				delete(c.meta.Channels, ch)
			}
			c.hub.mu.Unlock()
		}
	}
}

// writePump pumps messages from the send channel to the WebSocket connection.
func (c *Connection) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case event, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			data, err := MarshalEventJSON(event)
			if err != nil {
				continue
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Content from hub.go
// EventType represents a WebSocket event type.
type EventType string

const (
	EventTaskUpdated    EventType = "task.updated"
	EventTaskCompleted  EventType = "task.completed"
	EventAgentStatus    EventType = "agent.status"
	EventAlertTriggered EventType = "alert.triggered"
)

// Event is a WebSocket message sent to clients.
type Event struct {
	Type      EventType   `json:"type"`
	Payload   interface{} `json:"payload"`
	Timestamp time.Time   `json:"timestamp"`
}

// ClientMessage is a message received from a client.
type ClientMessage struct {
	Type     string   `json:"type"`
	Channels []string `json:"channels,omitempty"`
	Token    string   `json:"token,omitempty"`
}

// ConnectionMeta holds metadata for an active connection.
type ConnectionMeta struct {
	UserID    string
	Channels  map[string]bool
	CreatedAt time.Time
}

// Hub manages active WebSocket connections and broadcasts events.
type Hub struct {
	mu          sync.RWMutex
	connections map[*Connection]ConnectionMeta
	register    chan *Connection
	unregister  chan *Connection
	broadcast   chan Event
	offline     chan offlineMessage
	maxQueue    int
	stop        chan struct{}
	stopOnce    sync.Once
}

type offlineMessage struct {
	userID string
	event  Event
}

// NewHub creates a new WebSocket hub.
func NewHub(maxOfflineQueue int) *Hub {
	if maxOfflineQueue <= 0 {
		maxOfflineQueue = 100
	}
	return &Hub{
		connections: make(map[*Connection]ConnectionMeta),
		register:    make(chan *Connection, 64),
		unregister:  make(chan *Connection, 64),
		broadcast:   make(chan Event, 256),
		offline:     make(chan offlineMessage, 256),
		maxQueue:    maxOfflineQueue,
		stop:        make(chan struct{}),
	}
}

// Run starts the hub event loop. Call Stop() to shut down.
func (h *Hub) Run() {
	for {
		select {
		case <-h.stop:
			return
		case conn := <-h.register:
			h.mu.Lock()
			h.connections[conn] = conn.meta
			h.mu.Unlock()
		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.connections[conn]; ok {
				delete(h.connections, conn)
				close(conn.send)
			}
			h.mu.Unlock()
		case event := <-h.broadcast:
			h.mu.RLock()
			for conn, meta := range h.connections {
				if len(meta.Channels) == 0 || meta.Channels[string(event.Type)] {
					select {
					case conn.send <- event:
					default:
						// full — drop
					}
				}
			}
			h.mu.RUnlock()
		case msg := <-h.offline:
			h.mu.RLock()
			queued := 0
			for conn, meta := range h.connections {
				if meta.UserID == msg.userID {
					queued++
					select {
					case conn.send <- msg.event:
					default:
					}
				}
			}
			h.mu.RUnlock()
			_ = queued
		}
	}
}

// Stop shuts down the hub event loop. Safe to call multiple times.
func (h *Hub) Stop() {
	h.stopOnce.Do(func() { close(h.stop) })
}

// Register adds a connection to the hub.
func (h *Hub) Register(conn *Connection) {
	select {
	case h.register <- conn:
	case <-h.stop:
	}
}

// Unregister removes a connection from the hub.
func (h *Hub) Unregister(conn *Connection) {
	select {
	case h.unregister <- conn:
	case <-h.stop:
	}
}

// Broadcast sends an event to all eligible connections.
func (h *Hub) Broadcast(event Event) {
	select {
	case h.broadcast <- event:
	default:
	}
}

// SendToUser sends an event to all connections of a specific user.
func (h *Hub) SendToUser(userID string, event Event) {
	select {
	case h.offline <- offlineMessage{userID: userID, event: event}:
	case <-h.stop:
	}
}

// ConnectionCount returns the number of active connections.
func (h *Hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections)
}

// MarshalEventJSON serializes an event to JSON bytes.
func MarshalEventJSON(event Event) ([]byte, error) {
	return json.Marshal(event)
}
