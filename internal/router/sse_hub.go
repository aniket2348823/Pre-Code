package router

import "sync"

// SSEHub manages per-task subscriber channels for real-time SSE lifecycle events.
// When the agent transitions state (especially HITL), the OnStateChange callback
// pushes events here, and any connected streamTaskHandler receives them instantly
// without waiting for the next DB poll.
type SSEHub struct {
	mu          sync.RWMutex
	subscribers map[string]chan TaskSSEEvent // taskID → channel
}

// NewSSEHub creates a new SSE hub.
func NewSSEHub() *SSEHub {
	return &SSEHub{
		subscribers: make(map[string]chan TaskSSEEvent),
	}
}

// Register adds a subscriber channel for a task.
func (h *SSEHub) Register(taskID string, ch chan TaskSSEEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscribers[taskID] = ch
}

// Unregister removes a subscriber channel for a task.
func (h *SSEHub) Unregister(taskID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.subscribers[taskID]; ok {
		close(ch)
		delete(h.subscribers, taskID)
	}
}

// Broadcast sends an event to the subscriber channel for a task (non-blocking).
func (h *SSEHub) Broadcast(taskID string, evt TaskSSEEvent) {
	h.mu.RLock()
	ch, ok := h.subscribers[taskID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	// Non-blocking send to avoid deadlocks if the subscriber is slow
	select {
	case ch <- evt:
	default:
	}
}
