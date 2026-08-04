package websocket

import (
	"encoding/json"
	"sync"
	"time"
)

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
	Type      string   `json:"type"`
	Channels  []string `json:"channels,omitempty"`
	Token     string   `json:"token,omitempty"`
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
				if meta.Channels == nil || len(meta.Channels) == 0 || meta.Channels[string(event.Type)] {
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

// Stop shuts down the hub event loop.
func (h *Hub) Stop() {
	close(h.stop)
}

// Register adds a connection to the hub.
func (h *Hub) Register(conn *Connection) {
	h.register <- conn
}

// Unregister removes a connection from the hub.
func (h *Hub) Unregister(conn *Connection) {
	h.unregister <- conn
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
	h.offline <- offlineMessage{userID: userID, event: event}
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
