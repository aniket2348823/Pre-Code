package queue

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDefaultWorkerConfig(t *testing.T) {
	cfg := DefaultWorkerConfig()
	if cfg.Stream != "vigilagent" {
		t.Fatalf("expected stream 'vigilagent', got %q", cfg.Stream)
	}
	if cfg.Subject != "tasks.execute" {
		t.Fatalf("expected subject 'tasks.execute', got %q", cfg.Subject)
	}
	if cfg.MaxRetries != 3 {
		t.Fatalf("expected max retries 3, got %d", cfg.MaxRetries)
	}
	if cfg.AckWait != 60*time.Second {
		t.Fatalf("expected ack wait 60s, got %v", cfg.AckWait)
	}
	if cfg.MaxDeliver != 4 {
		t.Fatalf("expected max deliver 4, got %d", cfg.MaxDeliver)
	}
}

func TestTaskPayloadSerialization(t *testing.T) {
	payload := TaskPayload{
		TaskID:        "task-123",
		ProjectID:     "proj-456",
		UserID:        "user-789",
		Prompt:        "Fix the auth bug",
		MaxTokens:     4096,
		MaxIterations: 20,
		Tags:          []string{"bugfix", "auth"},
		Priority:      1,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded TaskPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.TaskID != payload.TaskID {
		t.Fatalf("task ID mismatch: %q vs %q", decoded.TaskID, payload.TaskID)
	}
	if decoded.Prompt != payload.Prompt {
		t.Fatalf("prompt mismatch: %q vs %q", decoded.Prompt, payload.Prompt)
	}
	if decoded.MaxTokens != payload.MaxTokens {
		t.Fatalf("max tokens mismatch: %d vs %d", decoded.MaxTokens, payload.MaxTokens)
	}
	if len(decoded.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(decoded.Tags))
	}
}

func TestTaskPayloadMinimal(t *testing.T) {
	payload := TaskPayload{
		TaskID:    "task-min",
		ProjectID: "proj-min",
		UserID:    "user-min",
		Prompt:    "hello",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify omitempty fields are omitted
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := m["tags"]; ok {
		t.Fatal("tags should be omitted when nil")
	}
}

func TestWorkerConfigCustom(t *testing.T) {
	cfg := WorkerConfig{
		Stream:     "custom-stream",
		Subject:    "custom.subject",
		MaxRetries: 5,
		AckWait:    30 * time.Second,
		MaxDeliver: 10,
	}

	if cfg.Stream != "custom-stream" {
		t.Fatalf("expected custom stream, got %q", cfg.Stream)
	}
	if cfg.MaxRetries != 5 {
		t.Fatalf("expected 5 retries, got %d", cfg.MaxRetries)
	}
}
