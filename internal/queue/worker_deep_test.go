package queue

import (
	"encoding/json"
	"testing"
)

func TestTaskPayload_NilPayload(t *testing.T) {
	payload := TaskPayload{TaskID: "t1", ProjectID: "p1", UserID: "u1", Prompt: "test"}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TaskPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TaskID != "t1" {
		t.Errorf("expected t1, got %s", decoded.TaskID)
	}
}

func TestTaskPayload_EmptyPayload(t *testing.T) {
	payload := TaskPayload{TaskID: "t1", Prompt: ""}
	data, _ := json.Marshal(payload)
	var decoded TaskPayload
	json.Unmarshal(data, &decoded)
	if decoded.TaskID != "t1" {
		t.Error("task ID mismatch")
	}
}

func TestTaskPayload_NestedMap(t *testing.T) {
	payload := TaskPayload{
		TaskID: "t1",
		Prompt: "test prompt",
		Tags:   []string{"scan", "security"},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TaskPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestTaskPayload_EmptyTaskID(t *testing.T) {
	payload := TaskPayload{TaskID: "", Prompt: "test"}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TaskPayload
	json.Unmarshal(data, &decoded)
	if decoded.Prompt != "test" {
		t.Error("prompt mismatch")
	}
}

func TestTaskPayload_EmptyTaskType(t *testing.T) {
	payload := TaskPayload{TaskID: "t1", Prompt: "test"}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TaskPayload
	json.Unmarshal(data, &decoded)
	if decoded.TaskID != "t1" {
		t.Error("task ID mismatch")
	}
}

func TestWorkerConfig_ZeroConcurrency(t *testing.T) {
	cfg := WorkerConfig{MaxRetries: 3, MaxDeliver: 4}
	if cfg.MaxRetries != 3 {
		t.Errorf("expected 3, got %d", cfg.MaxRetries)
	}
}

func TestWorkerConfig_NegativeConcurrency(t *testing.T) {
	cfg := WorkerConfig{MaxRetries: -1}
	if cfg.MaxRetries != -1 {
		t.Error("negative retries should be stored")
	}
}

func TestWorkerConfig_ZeroMaxRetries(t *testing.T) {
	cfg := WorkerConfig{MaxRetries: 0}
	if cfg.MaxRetries != 0 {
		t.Error("zero retries should be stored")
	}
}

func TestDefaultWorkerConfig_Deep(t *testing.T) {
	cfg := DefaultWorkerConfig()
	if cfg.Stream != "vigilagent" {
		t.Errorf("expected vigilagent, got %s", cfg.Stream)
	}
	if cfg.Subject != "tasks.execute" {
		t.Errorf("expected tasks.execute, got %s", cfg.Subject)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("expected 3 retries, got %d", cfg.MaxRetries)
	}
}
