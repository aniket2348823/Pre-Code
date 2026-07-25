package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// WorkingMemory provides in-memory session-scoped context.
// When a Redis client is provided, messages are persisted to survive restarts.
type WorkingMemory struct {
	messages []Message
	mu       sync.RWMutex
	sessionID string
	rds       *redis.Client
	ttl       time.Duration
}

// Message represents a conversation message in working memory.
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Tokens    int       `json:"tokens"`
	Timestamp time.Time `json:"timestamp"`
}

// NewWorkingMemory creates a new working memory instance.
func NewWorkingMemory(_ time.Duration) *WorkingMemory {
	return &WorkingMemory{
		messages: make([]Message, 0),
	}
}

// NewRedisBackedWorkingMemory creates a working memory instance that persists
// messages to Redis so they survive server restarts.
func NewRedisBackedWorkingMemory(rds *redis.Client, sessionID string, ttl time.Duration) *WorkingMemory {
	wm := &WorkingMemory{
		messages:  make([]Message, 0),
		sessionID: sessionID,
		rds:       rds,
		ttl:       ttl,
	}
	// Restore messages from Redis on creation.
	if rds != nil && sessionID != "" {
		wm.loadFromRedis()
	}
	return wm
}

func workingMemoryKey(sessionID string) string {
	return fmt.Sprintf("vigilagent:working_memory:%s", sessionID)
}

// loadFromRedis restores messages from Redis.
func (wm *WorkingMemory) loadFromRedis() {
	if wm.rds == nil || wm.sessionID == "" {
		return
	}
	ctx := context.Background()
	data, err := wm.rds.Get(ctx, workingMemoryKey(wm.sessionID)).Bytes()
	if err != nil {
		return // key doesn't exist or Redis error — start empty
	}
	var msgs []Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		slog.Warn("failed to unmarshal working memory from Redis", "error", err)
		return
	}
	wm.messages = msgs
}

// persistToRedis saves current messages to Redis.
// Takes a snapshot under read lock to avoid data race with concurrent Add() calls.
func (wm *WorkingMemory) persistToRedis() {
	if wm.rds == nil || wm.sessionID == "" {
		return
	}
	// Snapshot under read lock to avoid race with concurrent writers.
	wm.mu.RLock()
	snapshot := make([]Message, len(wm.messages))
	copy(snapshot, wm.messages)
	wm.mu.RUnlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		slog.Warn("failed to marshal working memory", "error", err)
		return
	}
	ctx := context.Background()
	key := workingMemoryKey(wm.sessionID)
	ttl := wm.ttl
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if err := wm.rds.Set(ctx, key, data, ttl).Err(); err != nil {
		slog.Warn("failed to persist working memory to Redis", "error", err)
	}
}

// Add appends a message to working memory and persists to Redis if configured.
func (wm *WorkingMemory) Add(msg Message) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	msg.Timestamp = time.Now()
	wm.messages = append(wm.messages, msg)
	// Persist to Redis in background (non-blocking).
	if wm.rds != nil {
		go wm.persistToRedis()
	}
}

// Get returns all messages in working memory.
func (wm *WorkingMemory) Get() []Message {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	result := make([]Message, len(wm.messages))
	copy(result, wm.messages)
	return result
}

// Search performs a simple text search in working memory.
func (wm *WorkingMemory) Search(query string, limit int) []Message {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	queryLower := strings.ToLower(query)
	var results []Message
	for _, msg := range wm.messages {
		if len(results) >= limit {
			break
		}
		if strings.Contains(strings.ToLower(msg.Content), queryLower) {
			results = append(results, msg)
		}
	}
	return results
}

// Clear removes all messages from working memory.
func (wm *WorkingMemory) Clear() {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.messages = wm.messages[:0]
}

// Count returns the number of messages.
func (wm *WorkingMemory) Count() int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	return len(wm.messages)
}

// TokenCount returns estimated token count.
func (wm *WorkingMemory) TokenCount() int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	total := 0
	for _, msg := range wm.messages {
		total += msg.Tokens
	}
	return total
}

// ProceduralStore manages learned workflows.
type ProceduralStore struct {
	workflows map[string]*Workflow
	mu        sync.RWMutex
}

// Workflow represents a learned workflow pattern.
type Workflow struct {
	ID          string                 `json:"id"`
	UserID      string                 `json:"user_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Steps       []WorkflowStep        `json:"steps"`
	SuccessRate float64               `json:"success_rate"`
	UsageCount  int                    `json:"usage_count"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

// WorkflowStep represents a step in a workflow.
type WorkflowStep struct {
	Action      string                 `json:"action"`
	Description string                 `json:"description"`
	Tool        string                 `json:"tool,omitempty"`
	Params      map[string]interface{} `json:"params,omitempty"`
}

// NewProceduralStore creates a new procedural memory store.
func NewProceduralStore() *ProceduralStore {
	return &ProceduralStore{
		workflows: make(map[string]*Workflow),
	}
}

// Store saves a workflow.
func (s *ProceduralStore) Store(_ context.Context, wf *Workflow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	wf.CreatedAt = time.Now()
	s.workflows[wf.ID] = wf
	return nil
}

// Get retrieves a workflow by ID.
func (s *ProceduralStore) Get(_ context.Context, id string) (*Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	wf, ok := s.workflows[id]
	if !ok {
		return nil, fmt.Errorf("workflow not found: %s", id)
	}
	return wf, nil
}

// Search finds workflows by name.
func (s *ProceduralStore) Search(_ context.Context, query string, limit int) ([]Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	queryLower := strings.ToLower(query)
	var results []Workflow
	for _, wf := range s.workflows {
		if len(results) >= limit {
			break
		}
		if strings.Contains(strings.ToLower(wf.Name), queryLower) || strings.Contains(strings.ToLower(wf.Description), queryLower) {
			results = append(results, *wf)
		}
	}
	return results, nil
}

// ListByUser returns all workflows for a user.
func (s *ProceduralStore) ListByUser(_ context.Context, userID string, limit int) ([]Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []Workflow
	for _, wf := range s.workflows {
		if len(results) >= limit {
			break
		}
		if wf.UserID == userID {
			results = append(results, *wf)
		}
	}
	return results, nil
}
