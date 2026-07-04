// Package feedback implements the memory feedback loop: tracks which responses
// users accepted/rejected, measures outcomes, and feeds results back into the
// memory system to improve future responses. This is the self-improvement
// mechanism that makes VigilAgent get better over time.
package feedback

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/memory"
)

// Outcome represents the result of a user interaction.
type Outcome struct {
	ID           string    `json:"id"`
	RequestID    string    `json:"request_id"`
	UserID       string    `json:"user_id"`
	Accepted     bool      `json:"accepted"`      // did user accept the response?
	Model        string    `json:"model"`          // which model was used
	TaskType     string    `json:"task_type"`      // code_generation, review, etc.
	Score        float64   `json:"score"`          // critic score if available
	Cost         float64   `json:"cost"`           // LLM cost
	TokensUsed   int       `json:"tokens_used"`
	DurationMs   float64   `json:"duration_ms"`
	Tags         []string  `json:"tags,omitempty"`
	Feedback     string    `json:"feedback,omitempty"` // user-provided feedback text
	CreatedAt    time.Time `json:"created_at"`
}

// ModelStats tracks aggregate performance for a model.
type ModelStats struct {
	Model          string  `json:"model"`
	TotalRequests  int     `json:"total_requests"`
	AcceptedCount  int     `json:"accepted_count"`
	RejectedCount  int     `json:"rejected_count"`
	AcceptRate     float64 `json:"accept_rate"`
	AvgScore       float64 `json:"avg_score"`
	AvgCost        float64 `json:"avg_cost"`
	AvgDurationMs  float64 `json:"avg_duration_ms"`
	TotalCost      float64 `json:"total_cost"`
}

// TaskTypeStats tracks performance by task type.
type TaskTypeStats struct {
	TaskType      string  `json:"task_type"`
	TotalRequests int     `json:"total_requests"`
	AcceptedCount int     `json:"accepted_count"`
	RejectedCount int     `json:"rejected_count"`
	AcceptRate    float64 `json:"accept_rate"`
	AvgScore      float64 `json:"avg_score"`
	BestModel     string  `json:"best_model"` // model with highest accept rate
}

// Engine tracks outcomes and computes learning metrics.
type Engine struct {
	mu      sync.RWMutex
	outcomes []Outcome
	models   map[string]*ModelStats
	tasks    map[string]*TaskTypeStats
	mem      *memory.Manager
}

// NewEngine creates a feedback engine.
func NewEngine(mem *memory.Manager) *Engine {
	return &Engine{
		models: make(map[string]*ModelStats),
		tasks:  make(map[string]*TaskTypeStats),
		mem:    mem,
	}
}

// RecordOutcome records the result of a user interaction.
func (e *Engine) RecordOutcome(ctx context.Context, o Outcome) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if o.CreatedAt.IsZero() {
		o.CreatedAt = time.Now()
	}

	e.outcomes = append(e.outcomes, o)

	// Update model stats
	ms, ok := e.models[o.Model]
	if !ok {
		ms = &ModelStats{Model: o.Model}
		e.models[o.Model] = ms
	}
	ms.TotalRequests++
	ms.TotalCost += o.Cost
	if o.Accepted {
		ms.AcceptedCount++
	} else {
		ms.RejectedCount++
	}
	ms.AcceptRate = float64(ms.AcceptedCount) / float64(ms.TotalRequests)
	// Running average
	alpha := 1.0 / float64(ms.TotalRequests)
	ms.AvgScore = ms.AvgScore*(1-alpha) + o.Score*alpha
	ms.AvgCost = ms.AvgCost*(1-alpha) + o.Cost*alpha
	ms.AvgDurationMs = ms.AvgDurationMs*(1-alpha) + o.DurationMs*alpha

	// Update task type stats
	ts, ok := e.tasks[o.TaskType]
	if !ok {
		ts = &TaskTypeStats{TaskType: o.TaskType}
		e.tasks[o.TaskType] = ts
	}
	ts.TotalRequests++
	if o.Accepted {
		ts.AcceptedCount++
	} else {
		ts.RejectedCount++
	}
	ts.AcceptRate = float64(ts.AcceptedCount) / float64(ts.TotalRequests)
	ts.AvgScore = ts.AvgScore*(1-alpha) + o.Score*alpha

	// Store in memory for future recall
	if e.mem != nil {
		title := fmt.Sprintf("feedback: %s via %s", o.TaskType, o.Model)
		content := fmt.Sprintf("Model: %s, Accepted: %v, Score: %.2f, Cost: $%.4f",
			o.Model, o.Accepted, o.Score, o.Cost)
		importance := 0.4
		if o.Accepted {
			importance = 0.8
		}
		err := e.mem.StoreEpisode(ctx, o.UserID, "feedback", title, content, importance)
		if err != nil {
			slog.Warn("feedback: failed to store in memory", "error", err)
		}
	}
}

// GetModelStats returns stats for all models.
func (e *Engine) GetModelStats() map[string]*ModelStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]*ModelStats, len(e.models))
	for k, v := range e.models {
		cp := *v
		out[k] = &cp
	}
	return out
}

// GetTaskStats returns stats for all task types.
func (e *Engine) GetTaskStats() map[string]*TaskTypeStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]*TaskTypeStats, len(e.tasks))
	for k, v := range e.tasks {
		cp := *v
		out[k] = &cp
	}
	return out
}

// GetBestModel returns the model with the highest accept rate for a task type.
func (e *Engine) GetBestModel(taskType string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var bestModel string
	var bestRate float64

	for _, ms := range e.models {
		if ms.TotalRequests < 5 {
			continue // need minimum data
		}
		if ms.AcceptRate > bestRate {
			bestRate = ms.AcceptRate
			bestModel = ms.Model
		}
	}

	return bestModel
}

// GetRecentOutcomes returns the last N outcomes.
func (e *Engine) GetRecentOutcomes(n int) []Outcome {
	e.mu.RLock()
	defer e.mu.RUnlock()

	total := len(e.outcomes)
	if n > total {
		n = total
	}
	start := total - n
	out := make([]Outcome, n)
	copy(out, e.outcomes[start:])
	return out
}

// TotalOutcomes returns the total number of recorded outcomes.
func (e *Engine) TotalOutcomes() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.outcomes)
}

// AcceptRate returns the overall acceptance rate.
func (e *Engine) AcceptRate() float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.outcomes) == 0 {
		return 0
	}
	accepted := 0
	for _, o := range e.outcomes {
		if o.Accepted {
			accepted++
		}
	}
	return float64(accepted) / float64(len(e.outcomes))
}



// DecayStats applies time-based decay to stats (older outcomes matter less).
func (e *Engine) DecayStats(decayFactor float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, ms := range e.models {
		ms.AcceptRate *= decayFactor
		ms.AvgScore *= decayFactor
		// Ensure values stay in valid range
		ms.AcceptRate = math.Max(0, math.Min(1, ms.AcceptRate))
		ms.AvgScore = math.Max(0, math.Min(1, ms.AvgScore))
	}
}
