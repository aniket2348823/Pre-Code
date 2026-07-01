package agent

import (
	"encoding/json"
	"time"
)

// EventType represents the type of agent event.
type EventType string

const (
	WSPlanCreated   EventType = "plan_created"
	WSStepStarted   EventType = "step_started"
	WSStepComplete  EventType = "step_complete"
	WSStepFailed    EventType = "step_failed"
	WSToken         EventType = "token"
	WSTaskComplete  EventType = "task_complete"
	WSTaskFailed    EventType = "task_failed"
	WSCostUpdate    EventType = "cost_update"
	WSHITLRequired  EventType = "hitl_required"
	WSReflection    EventType = "reflection"
)

// AgentEvent represents a real-time event from the agent execution loop.
type AgentEvent struct {
	Type      EventType            `json:"type"`
	TaskID    string               `json:"task_id"`
	Step      int                  `json:"step,omitempty"`
	Total     int                  `json:"total,omitempty"`
	Data      interface{}          `json:"data,omitempty"`
	Timestamp time.Time            `json:"timestamp"`
	Cost      float64              `json:"cost,omitempty"`
	Tokens    int                  `json:"tokens,omitempty"`
	Model     string               `json:"model,omitempty"`
	Error     string               `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// EventPayload is the generic payload for event data.
type EventPayload struct {
	Tool        string `json:"tool,omitempty"`
	Description string `json:"description,omitempty"`
	Output      string `json:"output,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
}

// NewPlanCreatedEvent creates a plan created event.
func NewPlanCreatedEvent(taskID string, steps int) AgentEvent {
	return AgentEvent{
		Type:      WSPlanCreated,
		TaskID:    taskID,
		Total:     steps,
		Timestamp: time.Now(),
	}
}

// NewStepStartedEvent creates a step started event.
func NewStepStartedEvent(taskID string, step, total int, tool, description string) AgentEvent {
	return AgentEvent{
		Type:   WSStepStarted,
		TaskID: taskID,
		Step:   step,
		Total:  total,
		Data: EventPayload{
			Tool:        tool,
			Description: description,
		},
		Timestamp: time.Now(),
	}
}

// NewStepCompleteEvent creates a step complete event.
func NewStepCompleteEvent(taskID string, step, total int, tool, output string, durationMs int64) AgentEvent {
	return AgentEvent{
		Type:   WSStepComplete,
		TaskID: taskID,
		Step:   step,
		Total:  total,
		Data: EventPayload{
			Tool:       tool,
			Output:     output,
			DurationMs: durationMs,
		},
		Timestamp: time.Now(),
	}
}

// NewStepFailedEvent creates a step failed event.
func NewStepFailedEvent(taskID string, step int, tool, errMsg string) AgentEvent {
	return AgentEvent{
		Type:      WSStepFailed,
		TaskID:    taskID,
		Step:      step,
		Error:     errMsg,
		Timestamp: time.Now(),
		Metadata:  map[string]interface{}{"tool": tool},
	}
}

// NewTokenEvent creates a token streaming event.
func NewTokenEvent(taskID, content, model string) AgentEvent {
	return AgentEvent{
		Type:      WSToken,
		TaskID:    taskID,
		Model:     model,
		Data:      content,
		Timestamp: time.Now(),
	}
}

// NewTaskCompleteEvent creates a task complete event.
func NewTaskCompleteEvent(taskID, result string, cost float64, tokens int) AgentEvent {
	return AgentEvent{
		Type:      WSTaskComplete,
		TaskID:    taskID,
		Data:      result,
		Cost:      cost,
		Tokens:    tokens,
		Timestamp: time.Now(),
	}
}

// NewTaskFailedEvent creates a task failed event.
func NewTaskFailedEvent(taskID, errMsg string) AgentEvent {
	return AgentEvent{
		Type:      WSTaskFailed,
		TaskID:    taskID,
		Error:     errMsg,
		Timestamp: time.Now(),
	}
}

// NewCostUpdateEvent creates a cost update event.
func NewCostUpdateEvent(taskID string, cost float64, tokens int) AgentEvent {
	return AgentEvent{
		Type:      WSCostUpdate,
		TaskID:    taskID,
		Cost:      cost,
		Tokens:    tokens,
		Timestamp: time.Now(),
	}
}

// SerializeJSON serializes the event to JSON bytes.
func (e AgentEvent) SerializeJSON() []byte {
	data, _ := json.Marshal(e)
	return data
}
