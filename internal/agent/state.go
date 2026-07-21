package agent

import (
	"fmt"
	"sync"
	"time"
)

// TaskState represents the state of a task in the agent state machine.
type TaskState string

const (
	StatePending    TaskState = "pending"
	StatePlanning   TaskState = "planning"
	StateExecuting  TaskState = "executing"
	StateWaitingHITL TaskState = "waiting_hitl"
	StateReviewing  TaskState = "reviewing"
	StateCompleted  TaskState = "completed"
	StateFailed     TaskState = "failed"
	StateCancelled  TaskState = "cancelled"
)

// Event represents a state transition event.
type Event string

const (
	EventStart          Event = "start"
	EventPlanReady      Event = "plan_ready"
	EventStepComplete   Event = "step_complete"
	EventStepFailed     Event = "step_failed"
	EventHITLRequired   Event = "hitl_required"
	EventHITLApproved   Event = "hitl_approved"
	EventHITLRejected   Event = "hitl_rejected"
	EventReviewPassed   Event = "review_passed"
	EventReviewFailed   Event = "review_failed"
	EventCancel         Event = "cancel"
)

// Task represents an agent task with full state.
type Task struct {
	ID                string                 `json:"id"`
	UserID            string                 `json:"user_id"`
	ProjectID         string                 `json:"project_id"`
	Title             string                 `json:"title"`
	Description       string                 `json:"description"`
	State             TaskState              `json:"state"`
	Priority          string                 `json:"priority"`
	Plan              *Plan                  `json:"plan,omitempty"`
	Steps             []StepResult           `json:"steps"`
	CurrentStep       int                    `json:"current_step"`
	MaxIterations     int                    `json:"max_iterations"`
	Result            string                 `json:"result,omitempty"`
	ModelUsed         string                 `json:"model_used,omitempty"`
	Provider          string                 `json:"provider,omitempty"`
	Complexity        string                 `json:"complexity,omitempty"`
	InputTokens       int                    `json:"input_tokens"`
	OutputTokens      int                    `json:"output_tokens"`
	TotalTokens       int                    `json:"total_tokens"`
	Cost              float64                `json:"cost"`
	HITLRequired      bool                   `json:"hitl_required"`
	HITLCheckpoint    *HITLCheckpoint        `json:"hitl_checkpoint,omitempty"`
	Error             string                 `json:"error,omitempty"`
	RetryCount        int                    `json:"retry_count"`
	MaxRetries        int                    `json:"max_retries"`
	MaxTokens         int                    `json:"max_tokens"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
	Tags              []string               `json:"tags,omitempty"`
	FilesChanged      []string               `json:"files_changed,omitempty"`
	RequiresReasoning bool                   `json:"requires_reasoning"`
	IsNovel           bool                   `json:"is_novel"`
	Messages          []Message              `json:"-"`
	StartedAt         *time.Time             `json:"started_at,omitempty"`
	CompletedAt       *time.Time             `json:"completed_at,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`

	// OnStateChange is a per-task callback fired on every state transition.
	// Set by the caller (router) to dispatch lifecycle webhooks. Nil-safe.
	OnStateChange func(taskID, oldState, newState string) `json:"-"`
}

// Plan represents the execution plan for a task.
type Plan struct {
	Steps      []PlanStep `json:"steps"`
	TotalSteps int        `json:"total_steps"`
}

// PlanStep represents a single step in the plan.
type PlanStep struct {
	Index       int    `json:"index"`
	Tool        string `json:"tool"`
	Description string `json:"description"`
	Params      map[string]interface{} `json:"params,omitempty"`
}

// StepResult represents the result of executing a step.
type StepResult struct {
	Step        int           `json:"step"`
	Tool        string        `json:"tool"`
	Status      string        `json:"status"`
	Result      string        `json:"result,omitempty"`
	Error       string        `json:"error,omitempty"`
	DurationMs  int64         `json:"duration_ms"`
	TokensUsed  int           `json:"tokens_used"`
	Cost        float64       `json:"cost"`
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
}

// HITLCheckpoint represents a human-in-the-loop checkpoint.
type HITLCheckpoint struct {
	CheckpointID string   `json:"checkpoint_id"`
	Description  string   `json:"description"`
	Options      []string `json:"options"`
	WaitingSince time.Time `json:"waiting_since"`
}

// Message represents a conversation message.
type Message struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Tokens     int    `json:"tokens,omitempty"`
}

// StateMachine manages task state transitions.
type StateMachine struct {
	mu sync.RWMutex
}

// NewStateMachine creates a new state machine.
func NewStateMachine() *StateMachine {
	return &StateMachine{}
}

// Transition validates and applies a state transition.
func (sm *StateMachine) Transition(task *Task, event Event) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	task.UpdatedAt = now

	switch task.State {
	case StatePending:
		switch event {
		case EventStart:
			task.State = StatePlanning
			return nil
		case EventCancel:
			task.State = StateCancelled
			return nil
		}

	case StatePlanning:
		switch event {
		case EventPlanReady:
			task.State = StateExecuting
			return nil
		case EventStepFailed:
			task.State = StateFailed
			return nil
		}

	case StateExecuting:
		switch event {
		case EventStepComplete:
			if task.CurrentStep >= task.Plan.TotalSteps-1 {
				task.State = StateReviewing
			}
			// Stay in executing for next step
			return nil
		case EventStepFailed:
			task.RetryCount++
			if task.RetryCount >= task.MaxRetries {
				task.State = StateFailed
				return nil
			}
			// Stay in executing for retry
			return nil
		case EventHITLRequired:
			task.State = StateWaitingHITL
			return nil
		case EventHITLApproved:
			task.State = StateExecuting
			return nil
		case EventCancel:
			task.State = StateCancelled
			return nil
		}

	case StateWaitingHITL:
		switch event {
		case EventHITLApproved:
			task.State = StateExecuting
			return nil
		case EventHITLRejected:
			task.State = StateFailed
			task.Error = "rejected by human"
			return nil
		}

	case StateReviewing:
		switch event {
		case EventReviewPassed:
			task.State = StateCompleted
			now := time.Now()
			task.CompletedAt = &now
			return nil
		case EventReviewFailed:
			task.State = StateExecuting
			return nil
		}
	}

	return fmt.Errorf("invalid transition: state=%s event=%s", task.State, event)
}

// ValidTransitions returns valid events for the current state.
func (sm *StateMachine) ValidTransitions(state TaskState) []Event {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	switch state {
	case StatePending:
		return []Event{EventStart, EventCancel}
	case StatePlanning:
		return []Event{EventPlanReady, EventStepFailed}
	case StateExecuting:
		return []Event{EventStepComplete, EventStepFailed, EventHITLRequired, EventHITLApproved, EventCancel}
	case StateWaitingHITL:
		return []Event{EventHITLApproved, EventHITLRejected}
	case StateReviewing:
		return []Event{EventReviewPassed, EventReviewFailed}
	default:
		return nil // Terminal states
	}
}

// IsTerminal returns whether the state is a terminal state.
func IsTerminal(state TaskState) bool {
	return state == StateCompleted || state == StateFailed || state == StateCancelled
}
