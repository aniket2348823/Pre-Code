package contract

import (
	"encoding/json"
	"fmt"
)

// ---------------------------------------------------------------------------
// SSE Streaming — API contract §2.2
// ---------------------------------------------------------------------------

// StreamEventType identifies the kind of server-sent event.
type StreamEventType string

const (
	EventTaskStarted   StreamEventType = "task_started"
	EventStepStarted   StreamEventType = "step_started"
	EventStepCompleted StreamEventType = "step_completed"
	EventTokenStream   StreamEventType = "token_stream"
	EventHITLRequired  StreamEventType = "hitl_required"
	EventTaskCompleted StreamEventType = "task_completed"
	EventTaskFailed    StreamEventType = "task_failed"
	EventHeartbeat     StreamEventType = "heartbeat"
)

// AllStreamEventTypes returns every defined event type.
func AllStreamEventTypes() []StreamEventType {
	return []StreamEventType{
		EventTaskStarted, EventStepStarted, EventStepCompleted,
		EventTokenStream, EventHITLRequired, EventTaskCompleted,
		EventTaskFailed, EventHeartbeat,
	}
}

// Valid returns true when the event type is one of the known values.
func (e StreamEventType) Valid() bool {
	for _, v := range AllStreamEventTypes() {
		if e == v {
			return true
		}
	}
	return false
}

// StreamEvent is a single SSE frame.
type StreamEvent struct {
	Event StreamEventType `json:"event"`
	Data  json.RawMessage `json:"data"`
	ID    string          `json:"id,omitempty"`
}

// Format renders the event in the SSE wire format:
//
//	event: <type>\ndata: <json>\n\n
func (se StreamEvent) Format() string {
	out := fmt.Sprintf("event: %s\ndata: %s\n", se.Event, string(se.Data))
	if se.ID != "" {
		out = fmt.Sprintf("id: %s\n%s", se.ID, out)
	}
	return out + "\n"
}

// NewStreamEvent creates a StreamEvent by marshalling the payload to JSON.
func NewStreamEvent(eventType StreamEventType, payload any) (StreamEvent, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return StreamEvent{}, fmt.Errorf("marshalling stream event data: %w", err)
	}
	return StreamEvent{
		Event: eventType,
		Data:  data,
	}, nil
}

// ---------------------------------------------------------------------------
// Event-specific payloads
// ---------------------------------------------------------------------------

// TaskStartedEvent is the payload for EventTaskStarted.
type TaskStartedEvent struct {
	TaskID string    `json:"task_id"`
	Plan   *TaskPlan `json:"plan,omitempty"`
}

// StepStartedEvent is the payload for EventStepStarted.
type StepStartedEvent struct {
	TaskID      string `json:"task_id"`
	StepIndex   int    `json:"step_index"`
	Tool        string `json:"tool"`
	Description string `json:"description"`
}

// StepCompletedEvent is the payload for EventStepCompleted.
type StepCompletedEvent struct {
	TaskID     string `json:"task_id"`
	StepIndex  int    `json:"step_index"`
	Result     string `json:"result"`
	DurationMs int64  `json:"duration_ms"`
}

// TokenStreamEvent is the payload for EventTokenStream.
type TokenStreamEvent struct {
	TaskID  string `json:"task_id"`
	Content string `json:"content"`
	Model   string `json:"model,omitempty"`
}

// HITLRequiredEvent is the payload for EventHITLRequired.
type HITLRequiredEvent struct {
	TaskID      string   `json:"task_id"`
	StepIndex   int      `json:"step_index"`
	Action      string   `json:"action"`
	Description string   `json:"description"`
	Options     []string `json:"options,omitempty"`
}

// TaskCompletedEvent is the payload for EventTaskCompleted.
type TaskCompletedEvent struct {
	TaskID string      `json:"task_id"`
	Result *TaskResult `json:"result"`
	Cost   TaskCost    `json:"cost"`
}

// TaskFailedEvent is the payload for EventTaskFailed.
type TaskFailedEvent struct {
	TaskID    string `json:"task_id"`
	Error     string `json:"error"`
	StepIndex *int   `json:"step_index,omitempty"`
}

// HeartbeatEvent is the payload for EventHeartbeat.
type HeartbeatEvent struct {
	Timestamp Timestamp `json:"timestamp"`
}
