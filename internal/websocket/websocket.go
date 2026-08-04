package websocket

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

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
	hub        *Hub
	authenticate func(token string) (userID string, ok bool)
}

// NewHandler creates a new WebSocket handler.
func NewHandler(hub *Hub, authenticate func(string) (string, bool)) *Handler {
	return &Handler{
		hub:         hub,
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
