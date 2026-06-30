package contract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCreateTaskRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		req      CreateTaskRequest
		hasErr   bool
		errField string
	}{
		{
			name:   "valid minimal request",
			req:    CreateTaskRequest{Prompt: "fix the bug", ProjectID: "proj-1"},
			hasErr: false,
		},
		{
			name:     "missing prompt",
			req:      CreateTaskRequest{ProjectID: "proj-1"},
			hasErr:   true,
			errField: "prompt",
		},
		{
			name:     "missing project_id",
			req:      CreateTaskRequest{Prompt: "fix the bug"},
			hasErr:   true,
			errField: "project_id",
		},
		{
			name:     "negative max_tokens",
			req:      CreateTaskRequest{Prompt: "x", ProjectID: "p", MaxTokens: -1},
			hasErr:   true,
			errField: "max_tokens",
		},
		{
			name:     "negative max_iterations",
			req:      CreateTaskRequest{Prompt: "x", ProjectID: "p", MaxIterations: -1},
			hasErr:   true,
			errField: "max_iterations",
		},
		{
			name:     "invalid complexity",
			req:      CreateTaskRequest{Prompt: "x", ProjectID: "p", Complexity: "easy"},
			hasErr:   true,
			errField: "complexity",
		},
		{
			name:   "valid with complexity",
			req:    CreateTaskRequest{Prompt: "x", ProjectID: "p", Complexity: ComplexityComplex},
			hasErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.req.Validate()
			if tt.hasErr && !errs.HasErrors() {
				t.Errorf("expected validation error on field %q", tt.errField)
			}
			if !tt.hasErr && errs.HasErrors() {
				t.Errorf("unexpected validation errors: %v", errs.ToMap())
			}
			if tt.hasErr && errs.HasErrors() {
				m := errs.ToMap()
				if _, ok := m[tt.errField]; !ok {
					t.Errorf("expected error on field %q, got: %v", tt.errField, m)
				}
			}
		})
	}
}

func TestCreateTaskRequest_ApplyDefaults(t *testing.T) {
	req := CreateTaskRequest{Prompt: "test", ProjectID: "p"}
	req.ApplyDefaults()

	if req.MaxTokens != DefaultMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", req.MaxTokens, DefaultMaxTokens)
	}
	if req.MaxIterations != DefaultMaxIterations {
		t.Errorf("MaxIterations = %d, want %d", req.MaxIterations, DefaultMaxIterations)
	}
}

func TestCreateTaskRequest_ApplyDefaults_PreserveExisting(t *testing.T) {
	req := CreateTaskRequest{Prompt: "test", ProjectID: "p", MaxTokens: 4096, MaxIterations: 10}
	req.ApplyDefaults()

	if req.MaxTokens != 4096 {
		t.Errorf("MaxTokens should be preserved at 4096, got %d", req.MaxTokens)
	}
	if req.MaxIterations != 10 {
		t.Errorf("MaxIterations should be preserved at 10, got %d", req.MaxIterations)
	}
}

func TestTask_JSONRoundTrip(t *testing.T) {
	now := TimestampFromTime(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	original := Task{
		ID:        "task-123",
		ProjectID: "proj-456",
		UserID:    "user-789",
		Prompt:    "add error handling",
		Status:    TaskStatusExecuting,
		Plan: &TaskPlan{
			Steps: []PlanStep{
				{Index: 0, Tool: "read_file", Description: "read main.go", Status: TaskStatusCompleted},
				{Index: 1, Tool: "edit_file", Description: "add error handling", Status: TaskStatusPending},
			},
			TotalSteps: 2,
		},
		Cost: TaskCost{
			InputTokens:  1500,
			OutputTokens: 800,
			TotalTokens:  2300,
			Cost:         0.0345,
		},
		Model:      "claude-sonnet-4",
		Provider:   "anthropic",
		Complexity: ComplexityModerate,
		MaxTokens:  DefaultMaxTokens,
		MaxIter:    DefaultMaxIterations,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status = %q, want %q", decoded.Status, original.Status)
	}
	if decoded.Plan == nil {
		t.Fatal("Plan should not be nil")
	}
	if decoded.Plan.TotalSteps != 2 {
		t.Errorf("Plan.TotalSteps = %d, want 2", decoded.Plan.TotalSteps)
	}
	if len(decoded.Plan.Steps) != 2 {
		t.Fatalf("Plan.Steps len = %d, want 2", len(decoded.Plan.Steps))
	}
	if decoded.Plan.Steps[0].Tool != "read_file" {
		t.Errorf("Step[0].Tool = %q, want read_file", decoded.Plan.Steps[0].Tool)
	}
	if decoded.Cost.TotalTokens != 2300 {
		t.Errorf("Cost.TotalTokens = %d, want 2300", decoded.Cost.TotalTokens)
	}
	if decoded.MaxTokens != DefaultMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", decoded.MaxTokens, DefaultMaxTokens)
	}
	if decoded.MaxIter != DefaultMaxIterations {
		t.Errorf("MaxIter = %d, want %d", decoded.MaxIter, DefaultMaxIterations)
	}
}

func TestTaskPlan_Structure(t *testing.T) {
	// Validates the plan JSONB structure per reconciliation report C8 resolution.
	plan := TaskPlan{
		Steps: []PlanStep{
			{Index: 0, Tool: "read_file", Description: "inspect", Status: TaskStatusCompleted},
		},
		TotalSteps: 1,
	}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Verify the JSON structure has "steps" and "total_steps" keys.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, ok := raw["steps"]; !ok {
		t.Error("plan JSON missing 'steps' key")
	}
	if _, ok := raw["total_steps"]; !ok {
		t.Error("plan JSON missing 'total_steps' key")
	}
}

func TestListTasksRequest_Validate(t *testing.T) {
	t.Run("valid status filter", func(t *testing.T) {
		req := ListTasksRequest{Status: TaskStatusPending}
		if errs := req.Validate(); errs.HasErrors() {
			t.Errorf("unexpected errors: %v", errs.ToMap())
		}
	})

	t.Run("invalid status filter", func(t *testing.T) {
		req := ListTasksRequest{Status: TaskStatus("bogus")}
		errs := req.Validate()
		if !errs.HasErrors() {
			t.Error("expected validation error for invalid status")
		}
	})

	t.Run("empty status is valid", func(t *testing.T) {
		req := ListTasksRequest{}
		if errs := req.Validate(); errs.HasErrors() {
			t.Errorf("unexpected errors: %v", errs.ToMap())
		}
	})
}

func TestTaskCost_JSONPrecision(t *testing.T) {
	original := TaskCost{
		InputTokens:  1234,
		OutputTokens: 567,
		TotalTokens:  1801,
		Cost:         0.00234567,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded TaskCost
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Cost != original.Cost {
		t.Errorf("Cost precision lost: got %v, want %v", decoded.Cost, original.Cost)
	}
}
