// Package sse provides Server-Sent Events streaming helpers for real-time
// response delivery to the VS Code extension and other clients.
package sse

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Event represents a single SSE event.
type Event struct {
	ID    string      `json:"id,omitempty"`
	Event string      `json:"event,omitempty"` // e.g. "token", "done", "error", "critique"
	Data  interface{} `json:"data"`
}

// Streamer manages an SSE connection to a client.
type Streamer struct {
	w       io.Writer
	flusher http.Flusher
	mu      sync.Mutex
	closed  bool
	eventID int
}

// NewStreamer creates a new SSE streamer from an HTTP response writer.
// Returns nil if the response writer doesn't support flushing.
func NewStreamer(w http.ResponseWriter) *Streamer {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	return &Streamer{w: w, flusher: flusher}
}

// Send writes an SSE event to the client.
func (s *Streamer) Send(evt Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("stream closed")
	}

	s.eventID++
	if evt.ID == "" {
		evt.ID = fmt.Sprintf("%d", s.eventID)
	}

	// Write event fields directly — the mutex serializes all access.
	if evt.Event != "" {
		fmt.Fprintf(s.w, "event: %s\n", evt.Event)
	}
	fmt.Fprintf(s.w, "id: %s\n", evt.ID)

	// Marshal data as JSON; fall back to a safe string representation.
	var dataBytes []byte
	if evt.Data != nil {
		if d, err := json.Marshal(evt.Data); err != nil {
			dataBytes = safeStringify(evt.Data)
		} else {
			dataBytes = d
		}
	}
	fmt.Fprintf(s.w, "data: %s\n\n", string(dataBytes))

	s.flusher.Flush()
	return nil
}

// SendToken sends a token chunk during streaming generation.
func (s *Streamer) SendToken(token string) error {
	return s.Send(Event{
		Event: "token",
		Data:  map[string]string{"token": token},
	})
}

// SendCritique sends the critique result after evaluation.
func (s *Streamer) SendCritique(critique interface{}) error {
	return s.Send(Event{
		Event: "critique",
		Data:  critique,
	})
}

// SendDone signals the end of the stream.
func (s *Streamer) SendDone(result interface{}) error {
	return s.Send(Event{
		Event: "done",
		Data:  result,
	})
}

// SendError sends an error event.
func (s *Streamer) SendError(msg string) error {
	return s.Send(Event{
		Event: "error",
		Data:  map[string]string{"error": msg},
	})
}

// SendStatus sends a status update.
func (s *Streamer) SendStatus(status string, detail interface{}) error {
	return s.Send(Event{
		Event: "status",
		Data:  map[string]interface{}{"status": status, "detail": detail},
	})
}

// Close marks the stream as closed.
func (s *Streamer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

// safeStringify safely converts a value to a byte slice without triggering
// a stack overflow on circular references. fmt.Sprintf("%v", ...) recurses
// infinitely on self-referencing maps, so we use a type switch to avoid
// calling fmt.Sprintf on collection types. json.Marshal already detected
// the failure, so we return a safe placeholder for those types.
func safeStringify(v interface{}) []byte {
	switch v.(type) {
	case map[string]interface{}, map[interface{}]interface{},
		[]interface{}:
		return []byte("null")
	default:
		return []byte(fmt.Sprintf("%v", v))
	}
}
