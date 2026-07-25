package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/vigilagent/vigilagent/internal/telemetry"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// telemetryCounter increments the HITL checkpoints Prometheus counter for the given status.
func telemetryCounter(status string) {
	if telemetry.HITLCheckpointsTotal != nil {
		telemetry.HITLCheckpointsTotal.WithLabelValues(status).Inc()
	}
}


// HITLDecision represents a human decision on a pending checkpoint.
type HITLDecision string

const (
	HITLApprove HITLDecision = "approve"
	HITLReject  HITLDecision = "reject"
	HITLModify  HITLDecision = "modify"
)

// HITLCheckpoint represents a pending human-in-the-loop checkpoint.
type HITLCheckpointEntry struct {
	ID           string                 `json:"id"`
	TaskID       string                 `json:"task_id"`
	UserID       string                 `json:"user_id"`
	OrgID        string                 `json:"org_id"`
	StepIndex    int                    `json:"step_index"`
	Description  string                 `json:"description"`
	Tool         string                 `json:"tool"`
	Params       map[string]interface{} `json:"params,omitempty"`
	Options      []string               `json:"options"`
	Status       string                 `json:"status"` // pending, approved, rejected, timed_out
	Decision     HITLDecision           `json:"decision,omitempty"`
	ModifiedData string                 `json:"modified_data,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	DecidedAt    *time.Time             `json:"decided_at,omitempty"`
	ExpiresAt    time.Time              `json:"expires_at"`
	decisionCh   chan struct{}           `json:"-"` // closed by Decide() to wake Submit()
}

// HITLQueue manages pending human-in-the-loop checkpoints.
// NOTE: Submit() blocks until decision or timeout. It should only be called
// from background agent goroutines (e.g., agent.Execute), NEVER from HTTP
// handlers — those should use the agent state machine's HITL flow instead.
type HITLQueue struct {
	client      *redis.Client
	pending     map[string]*HITLCheckpointEntry
	mu          sync.RWMutex
	timeout     time.Duration
	callback    func(checkpoint *HITLCheckpointEntry) // called on decision
	cancel      context.CancelFunc                   // stops expiryChecker
}

// NewHITLQueue creates a new HITL queue with Redis-backed persistence.
func NewHITLQueue(client *redis.Client, timeout time.Duration) *HITLQueue {
	ctx, cancel := context.WithCancel(context.Background())
	q := &HITLQueue{
		client:  client,
		pending: make(map[string]*HITLCheckpointEntry),
		timeout: timeout,
		cancel:  cancel,
	}
	// Start background expiry checker with cancellable context
	go q.expiryChecker(ctx)
	return q
}

// Close stops the background expiry checker goroutine.
func (q *HITLQueue) Close() {
	if q.cancel != nil {
		q.cancel()
	}
}

// SetCallback sets the callback function invoked when a decision is made.
func (q *HITLQueue) SetCallback(fn func(checkpoint *HITLCheckpointEntry)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.callback = fn
}

// Submit adds a new checkpoint to the queue and blocks until a decision is made,
// the context is cancelled, or the timeout expires. Returns the decided entry.
// Must be called from background goroutines only (NEVER from HTTP handlers).
func (q *HITLQueue) Submit(ctx context.Context, entry *HITLCheckpointEntry) (*HITLCheckpointEntry, error) {
	entry.Status = "pending"
	entry.CreatedAt = time.Now()
	entry.ExpiresAt = time.Now().Add(q.timeout)
	entry.decisionCh = make(chan struct{}, 1)

	// Store in memory
	q.mu.Lock()
	q.pending[entry.ID] = entry
	q.mu.Unlock()

	// Persist to Redis for distributed queue support
	if q.client != nil {
		data, _ := json.Marshal(entry)
		key := fmt.Sprintf("hitl:%s", entry.ID)
		q.client.Set(ctx, key, data, q.timeout+30*time.Second)
		q.client.SAdd(ctx, "hitl:pending", entry.ID)
	}

	telemetryCounter("pending")
	slog.Info("HITL checkpoint submitted",
		"id", entry.ID,
		"task_id", entry.TaskID,
		"description", entry.Description,
		"expires_at", entry.ExpiresAt,
	)

	// Wait for decision: either Decide() closes the channel, context cancel, or timeout.
	timer := time.NewTimer(q.timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		q.markTimeout(entry.ID)
		return nil, ctx.Err()
	case <-entry.decisionCh:
		// Decide() was called — return the entry with updated status.
		return entry, nil
	case <-timer.C:
		q.markTimeout(entry.ID)
		return nil, fmt.Errorf("HITL checkpoint %s timed out after %s", entry.ID, q.timeout)
	}
}

// Decide processes a human decision on a pending checkpoint.
func (q *HITLQueue) Decide(ctx context.Context, checkpointID string, decision HITLDecision, modifiedData string) error {
	q.mu.Lock()
	entry, exists := q.pending[checkpointID]
	if !exists {
		q.mu.Unlock()
		return fmt.Errorf("checkpoint %s not found or already decided", checkpointID)
	}

	// Guard against double-decide race condition
	if entry.Status != "pending" {
		q.mu.Unlock()
		return fmt.Errorf("checkpoint %s already decided with status: %s", checkpointID, entry.Status)
	}

	now := time.Now()
	entry.Decision = decision
	entry.ModifiedData = modifiedData
	entry.DecidedAt = &now
	entry.Status = string(decision)
	delete(q.pending, checkpointID)
	q.mu.Unlock()

	// Update Redis
	if q.client != nil {
		data, _ := json.Marshal(entry)
		key := fmt.Sprintf("hitl:%s", checkpointID)
		q.client.Set(ctx, key, data, 1*time.Hour)
		q.client.SRem(ctx, "hitl:pending", checkpointID)
	}

	slog.Info("HITL checkpoint decided",
		"id", checkpointID,
		"decision", decision,
		"task_id", entry.TaskID,
	)

	telemetryCounter(string(decision))

	// Fire callback
	q.mu.RLock()
	cb := q.callback
	q.mu.RUnlock()
	if cb != nil {
		go cb(entry)
	}

	// Signal the waiting Submit() goroutine that a decision was made.
	if entry.decisionCh != nil {
		close(entry.decisionCh)
	}

	return nil
}

// GetPending returns all pending checkpoints for a user.
func (q *HITLQueue) GetPending(userID string) []*HITLCheckpointEntry {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var result []*HITLCheckpointEntry
	for _, entry := range q.pending {
		if entry.UserID == userID && entry.Status == "pending" {
			result = append(result, entry)
		}
	}
	return result
}

// GetPendingByTask returns all pending checkpoints for a specific task.
func (q *HITLQueue) GetPendingByTask(taskID string) []*HITLCheckpointEntry {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var result []*HITLCheckpointEntry
	for _, entry := range q.pending {
		if entry.TaskID == taskID && entry.Status == "pending" {
			result = append(result, entry)
		}
	}
	return result
}

// markTimeout marks a checkpoint as timed out.
func (q *HITLQueue) markTimeout(id string) {
	q.mu.Lock()
	entry, exists := q.pending[id]
	if exists {
		entry.Status = "timed_out"
		delete(q.pending, id)
	}
	q.mu.Unlock()

	telemetryCounter("timed_out")

	if exists && q.client != nil {
		ctx := context.Background()
		data, _ := json.Marshal(entry)
		key := fmt.Sprintf("hitl:%s", id)
		q.client.Set(ctx, key, data, 1*time.Hour)
		q.client.SRem(ctx, "hitl:pending", id)
	}

	// Signal waiting Submit() goroutine so it doesn't block forever.
	if exists && entry.decisionCh != nil {
		close(entry.decisionCh)
	}
}

// expiryChecker periodically cleans up expired checkpoints.
// Runs until the provided context is cancelled.
func (q *HITLQueue) expiryChecker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("HITL expiry checker stopped")
			return
		case <-ticker.C:
			q.mu.Lock()
			now := time.Now()
			for id, entry := range q.pending {
				if now.After(entry.ExpiresAt) {
					entry.Status = "timed_out"
					delete(q.pending, id)
					slog.Warn("HITL checkpoint expired", "id", id, "task_id", entry.TaskID)
					telemetryCounter("timed_out")
					// Signal waiting Submit() goroutine so it doesn't block until timer fires.
					if entry.decisionCh != nil {
						close(entry.decisionCh)
					}
				}
			}
			q.mu.Unlock()
		}
	}
}

// --- HITL HTTP Handlers ---

// HITLHandler provides HTTP endpoints for the HITL queue.
type HITLHandler struct {
	queue *HITLQueue
}

// NewHITLHandler creates a new HITL HTTP handler.
func NewHITLHandler(queue *HITLQueue) *HITLHandler {
	return &HITLHandler{queue: queue}
}

// ListPendingHandler returns all pending HITL checkpoints for the authenticated user.
func (h *HITLHandler) ListPendingHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.Unauthorized(w, "missing user context")
		return
	}

	checkpoints := h.queue.GetPending(userID)
	if checkpoints == nil {
		checkpoints = []*HITLCheckpointEntry{}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"checkpoints": checkpoints,
		"count":       len(checkpoints),
	})
}

// DecideHandler processes a human decision on a checkpoint.
func (h *HITLHandler) DecideHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.Unauthorized(w, "missing user context")
		return
	}

	var input struct {
		CheckpointID string       `json:"checkpoint_id"`
		Decision     HITLDecision `json:"decision"`
		ModifiedData string       `json:"modified_data,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	if input.CheckpointID == "" {
		response.BadRequest(w, "checkpoint_id is required")
		return
	}
	if input.Decision != HITLApprove && input.Decision != HITLReject && input.Decision != HITLModify {
		response.BadRequest(w, "decision must be approve, reject, or modify")
		return
	}

	if err := h.queue.Decide(r.Context(), input.CheckpointID, input.Decision, input.ModifiedData); err != nil {
		response.NotFound(w, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"message":      "decision recorded",
		"checkpoint_id": input.CheckpointID,
		"decision":     input.Decision,
	})
}

// StatusHandler returns the status of a specific checkpoint.
func (h *HITLHandler) StatusHandler(w http.ResponseWriter, r *http.Request) {
	checkpointID := r.URL.Query().Get("id")
	if checkpointID == "" {
		response.BadRequest(w, "id query parameter is required")
		return
	}

	h.queue.mu.RLock()
	entry, exists := h.queue.pending[checkpointID]
	h.queue.mu.RUnlock()

	if !exists {
		response.NotFound(w, "checkpoint not found or already decided")
		return
	}

	response.JSON(w, http.StatusOK, entry)
}
