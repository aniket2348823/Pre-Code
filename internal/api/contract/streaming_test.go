package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStreamEventType_AllValid(t *testing.T) {
	all := AllStreamEventTypes()
	if len(all) != 11 {
		t.Errorf("AllStreamEventTypes() has %d entries, want 11", len(all))
	}
	for _, e := range all {
		if !e.Valid() {
			t.Errorf("StreamEventType(%q).Valid() = false", e)
		}
	}
}

func TestStreamEventType_InvalidRejected(t *testing.T) {
	if StreamEventType("task_paused").Valid() {
		t.Error("task_paused should be invalid")
	}
}

func TestStreamEvent_Format(t *testing.T) {
	payload := TaskStartedEvent{TaskID: "task-1"}
	se, err := NewStreamEvent(EventTaskStarted, payload)
	if err != nil {
		t.Fatalf("NewStreamEvent: %v", err)
	}

	formatted := se.Format()

	if !strings.HasPrefix(formatted, "event: task_started\n") {
		t.Errorf("expected event line, got: %q", formatted)
	}
	if !strings.Contains(formatted, "data: ") {
		t.Error("expected data line in SSE output")
	}
	if !strings.HasSuffix(formatted, "\n\n") {
		t.Error("SSE event must end with \\n\\n")
	}
}

func TestStreamEvent_FormatWithID(t *testing.T) {
	se := StreamEvent{
		Event: EventHeartbeat,
		Data:  json.RawMessage(`{}`),
		ID:    "evt-42",
	}

	formatted := se.Format()
	if !strings.Contains(formatted, "id: evt-42\n") {
		t.Errorf("expected id line, got: %q", formatted)
	}
}

func TestNewStreamEvent_MarshalError(t *testing.T) {
	// channels cannot be marshalled
	_, err := NewStreamEvent(EventHeartbeat, make(chan int))
	if err == nil {
		t.Error("expected marshal error for channel type")
	}
}

func TestTaskStartedEvent_Fields(t *testing.T) {
	evt := TaskStartedEvent{
		TaskID: "t-1",
		Plan: &TaskPlan{
			Steps:      []PlanStep{{Index: 0, Tool: "read_file", Description: "read"}},
			TotalSteps: 1,
		},
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}

	if _, ok := raw["task_id"]; !ok {
		t.Error("missing task_id field")
	}
	if _, ok := raw["plan"]; !ok {
		t.Error("missing plan field")
	}
}

func TestStepCompletedEvent_JSON(t *testing.T) {
	evt := StepCompletedEvent{
		TaskID:     "t-1",
		StepIndex:  2,
		Result:     "file written",
		DurationMs: 1500,
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded StepCompletedEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.DurationMs != 1500 {
		t.Errorf("DurationMs = %d, want 1500", decoded.DurationMs)
	}
}

func TestHITLRequiredEvent_Fields(t *testing.T) {
	evt := HITLRequiredEvent{
		TaskID:      "t-1",
		StepIndex:   3,
		Action:      "write_file",
		Description: "Write to main.go",
		Options:     []string{"approve", "reject", "edit"},
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}

	for _, field := range []string{"task_id", "step_index", "action", "description", "options"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing field %q in HITLRequiredEvent", field)
		}
	}
}

func TestTaskFailedEvent_OptionalStepIndex(t *testing.T) {
	t.Run("with step index", func(t *testing.T) {
		idx := 5
		evt := TaskFailedEvent{TaskID: "t-1", Error: "timeout", StepIndex: &idx}
		data, _ := json.Marshal(evt)
		if !strings.Contains(string(data), `"step_index":5`) {
			t.Errorf("expected step_index in JSON, got: %s", data)
		}
	})

	t.Run("without step index", func(t *testing.T) {
		evt := TaskFailedEvent{TaskID: "t-1", Error: "budget exceeded"}
		data, _ := json.Marshal(evt)
		if strings.Contains(string(data), "step_index") {
			t.Errorf("step_index should be omitted, got: %s", data)
		}
	})
}

func TestHeartbeatEvent_Timestamp(t *testing.T) {
	evt := HeartbeatEvent{Timestamp: Now()}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded HeartbeatEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Timestamp.IsZero() {
		t.Error("heartbeat timestamp should not be zero")
	}
}
