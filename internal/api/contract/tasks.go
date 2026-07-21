package contract

// ---------------------------------------------------------------------------
// Task resource types — API contract §2.1
// ---------------------------------------------------------------------------

// DefaultMaxTokens per reconciliation report C5 (resolved to 8192 for Claude Sonnet).
const DefaultMaxTokens = 8192

// DefaultMaxIterations per reconciliation report C6 / Master doc (default: 20).
const DefaultMaxIterations = 20

// CreateTaskRequest is the body for POST /v1/tasks.
type CreateTaskRequest struct {
	Prompt          string     `json:"prompt"`
	ProjectID       string     `json:"project_id"`
	MaxTokens       int        `json:"max_tokens,omitempty"`
	ModelPreference string     `json:"model_preference,omitempty"`
	TimeoutSeconds  int        `json:"timeout_seconds,omitempty"`
	MaxIterations   int        `json:"max_iterations,omitempty"`
	RequireApproval bool       `json:"require_approval,omitempty"`
	IdempotencyKey  string     `json:"idempotency_key,omitempty"`
	Complexity      Complexity `json:"complexity,omitempty"`
}

// Validate checks required fields and applies defaults.
func (r *CreateTaskRequest) Validate() ValidationErrors {
	var errs ValidationErrors
	if r.Prompt == "" {
		errs.Add("prompt", "prompt is required")
	}
	if r.ProjectID == "" {
		errs.Add("project_id", "project_id is required")
	}
	if r.MaxTokens < 0 {
		errs.Add("max_tokens", "max_tokens must be non-negative")
	}
	if r.MaxIterations < 0 {
		errs.Add("max_iterations", "max_iterations must be non-negative")
	}
	if r.TimeoutSeconds < 0 {
		errs.Add("timeout_seconds", "timeout_seconds must be non-negative")
	}
	if r.Complexity != "" && !r.Complexity.Valid() {
		errs.Add("complexity", "invalid complexity value")
	}
	return errs
}

// ApplyDefaults fills zero-value fields with documented defaults.
func (r *CreateTaskRequest) ApplyDefaults() {
	if r.MaxTokens == 0 {
		r.MaxTokens = DefaultMaxTokens
	}
	if r.MaxIterations == 0 {
		r.MaxIterations = DefaultMaxIterations
	}
}

// CreateTaskResponse wraps the created task.
type CreateTaskResponse struct {
	Task Task `json:"task"`
}

// Task is the full task entity returned in API responses.
type Task struct {
	ID          string      `json:"id"`
	ProjectID   string      `json:"project_id"`
	UserID      string      `json:"user_id"`
	Prompt      string      `json:"prompt"`
	Status      TaskStatus  `json:"status"`
	Plan        *TaskPlan   `json:"plan,omitempty"`
	Result      *TaskResult `json:"result,omitempty"`
	Cost        TaskCost    `json:"cost"`
	Model       string      `json:"model,omitempty"`
	Provider    string      `json:"provider,omitempty"`
	Complexity  Complexity  `json:"complexity,omitempty"`
	MaxTokens   int         `json:"max_tokens"`
	MaxIter     int         `json:"max_iterations"`
	CreatedAt   Timestamp   `json:"created_at"`
	UpdatedAt   Timestamp   `json:"updated_at"`
	CompletedAt *Timestamp  `json:"completed_at,omitempty"`
}

// TaskPlan is the structured plan per reconciliation report C8 resolution.
type TaskPlan struct {
	Steps      []PlanStep `json:"steps"`
	TotalSteps int        `json:"total_steps"`
}

// PlanStep is one step within a task plan.
type PlanStep struct {
	Index       int        `json:"index"`
	Tool        string     `json:"tool"`
	Description string     `json:"description"`
	Status      TaskStatus `json:"status"`
	Result      string     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
	StartedAt   *Timestamp `json:"started_at,omitempty"`
	CompletedAt *Timestamp `json:"completed_at,omitempty"`
}

// TaskResult holds the outcome of a completed task.
type TaskResult struct {
	Summary      string   `json:"summary"`
	FilesChanged []string `json:"files_changed,omitempty"`
	ActionsTaken []string `json:"actions_taken,omitempty"`
}

// TaskCost holds token and dollar cost information.
type TaskCost struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	Cost         float64 `json:"cost"`
}

// ListTasksRequest holds query parameters for GET /v1/tasks.
type ListTasksRequest struct {
	ProjectID string     `json:"project_id,omitempty"`
	Status    TaskStatus `json:"status,omitempty"`
	PageRequest
}

// Validate checks filter values.
func (r *ListTasksRequest) Validate() ValidationErrors {
	var errs ValidationErrors
	if r.Status != "" && !r.Status.Valid() {
		errs.Add("status", "invalid status filter")
	}
	return errs
}

// ListTasksResponse is the response for GET /v1/tasks.
type ListTasksResponse struct {
	Tasks []Task       `json:"tasks"`
	Page  PageResponse `json:"page"`
}

// CancelTaskRequest is the body for POST /v1/tasks/{id}/cancel.
type CancelTaskRequest struct {
	Reason string `json:"reason,omitempty"`
}

// CancelTaskResponse wraps the cancelled task.
type CancelTaskResponse struct {
	Task Task `json:"task"`
}
