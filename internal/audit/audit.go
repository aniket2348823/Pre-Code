// Package audit provides an immutable audit trail for all pipeline operations.
// Every significant action is recorded with timestamp, actor, and context
// for compliance and debugging.
package audit

import (
	"fmt"
	"sync"
	"time"
)

// Entry represents a single audit trail entry.
type Entry struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Actor     string                 `json:"actor"`     // user_id or "system"
	Action    string                 `json:"action"`    // request.processed, skill.extracted, etc.
	Resource  string                 `json:"resource"`  // what was affected
	Details   map[string]interface{} `json:"details,omitempty"`
	Success   bool                   `json:"success"`
	Error     string                 `json:"error,omitempty"`
}

// Trail maintains an append-only audit log.
type Trail struct {
	mu      sync.RWMutex
	entries []Entry
}

// NewMemoryStore creates a new in-memory audit trail.
func NewMemoryStore() *Trail {
	return NewTrail()
}

// NewTrail creates a new audit trail.
func NewTrail() *Trail {
	return &Trail{}
}

// Record adds an audit entry.
func (t *Trail) Record(actor, action, resource string, success bool, details map[string]interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = append(t.entries, Entry{
		ID:        fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Actor:     actor,
		Action:    action,
		Resource:  resource,
		Details:   details,
		Success:   success,
	})
}

// RecordError adds a failed audit entry.
func (t *Trail) RecordError(actor, action, resource, errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = append(t.entries, Entry{
		ID:        fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Actor:     actor,
		Action:    action,
		Resource:  resource,
		Success:   false,
		Error:     errMsg,
	})
}

// Recent returns the last N entries.
func (t *Trail) Recent(n int) []Entry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	total := len(t.entries)
	if n > total {
		n = total
	}
	start := total - n
	out := make([]Entry, n)
	copy(out, t.entries[start:])
	return out
}

// ByActor returns all entries for a specific actor.
func (t *Trail) ByActor(actor string) []Entry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var out []Entry
	for _, e := range t.entries {
		if e.Actor == actor {
			out = append(out, e)
		}
	}
	return out
}

// ByAction returns all entries for a specific action.
func (t *Trail) ByAction(action string) []Entry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var out []Entry
	for _, e := range t.entries {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

// Count returns the total number of entries.
func (t *Trail) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.entries)
}
