package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vigilagent/vigilagent/internal/database"
)

// Task represents a task record in the database.
type Task struct {
	ID            string                 `json:"id"`
	ProjectID     string                 `json:"project_id"`
	UserID        string                 `json:"user_id"`
	Prompt        string                 `json:"prompt"`
	Status        string                 `json:"status"`
	Result        string                 `json:"result,omitempty"`
	Model         string                 `json:"model,omitempty"`
	Provider      string                 `json:"provider,omitempty"`
	Complexity    string                 `json:"complexity,omitempty"`
	MaxTokens     int                    `json:"max_tokens"`
	MaxIterations int                    `json:"max_iterations"`
	InputTokens   int                    `json:"input_tokens"`
	OutputTokens  int                    `json:"output_tokens"`
	TotalTokens   int                    `json:"total_tokens"`
	Cost          float64                `json:"cost"`
	Error         string                 `json:"error,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	PlanJSON      []byte                 `json:"-"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	CompletedAt   *time.Time             `json:"completed_at,omitempty"`
}

// TaskRepository handles database operations for tasks.
type TaskRepository struct {
	pool *database.Conn
}

// NewTaskRepository creates a new task repository.
func NewTaskRepository(pool *database.Conn) *TaskRepository {
	return &TaskRepository{pool: pool}
}

// Create inserts a new task into the database.
func (r *TaskRepository) Create(ctx context.Context, task *Task) error {
	query := `
		INSERT INTO tasks (project_id, user_id, prompt, status, max_tokens, max_iterations)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`
	return r.pool.QueryRow(ctx, query,
		task.ProjectID, task.UserID, task.Prompt, task.Status,
		task.MaxTokens, task.MaxIterations,
	).Scan(&task.ID, &task.CreatedAt, &task.UpdatedAt)
}

// FindByID retrieves a task by ID.
func (r *TaskRepository) FindByID(ctx context.Context, id string) (*Task, error) {
	query := `
		SELECT id, project_id, user_id, prompt, status, result, model, provider,
		       complexity, max_tokens, max_iterations, input_tokens, output_tokens,
		       total_tokens, cost, error, plan_json, created_at, updated_at, completed_at
		FROM tasks WHERE id = $1
	`
	task := &Task{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&task.ID, &task.ProjectID, &task.UserID, &task.Prompt, &task.Status,
		&task.Result, &task.Model, &task.Provider, &task.Complexity,
		&task.MaxTokens, &task.MaxIterations, &task.InputTokens, &task.OutputTokens,
		&task.TotalTokens, &task.Cost, &task.Error, &task.PlanJSON,
		&task.CreatedAt, &task.UpdatedAt, &task.CompletedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("task not found")
		}
		return nil, fmt.Errorf("failed to find task: %w", err)
	}
	return task, nil
}

// CursorPage holds cursor-based pagination results.
type CursorPage struct {
	Tasks      []Task `json:"tasks"`
	NextCursor string `json:"next_cursor,omitempty"` // cursor for next page
	HasMore    bool   `json:"has_more"`
}

// ListByProjectCursor lists tasks for a project using O(1) cursor-based pagination.
// cursor is the created_at timestamp of the last item from the previous page (RFC3339).
// Pass empty cursor to start from the beginning.
func (r *TaskRepository) ListByProjectCursor(ctx context.Context, projectID string, cursor string, limit int) (*CursorPage, error) {
	var query string
	var rows pgx.Rows
	var err error

	if cursor != "" {
		parts := strings.SplitN(cursor, "|", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid cursor")
		}
		cursorTime, parseErr := time.Parse(time.RFC3339Nano, parts[0])
		if parseErr != nil {
			return nil, fmt.Errorf("invalid cursor: %w", parseErr)
		}
		cursorID := parts[1]
		query = `
			SELECT id, project_id, user_id, prompt, status, result, model, provider,
			       complexity, max_tokens, max_iterations, input_tokens, output_tokens,
			       total_tokens, cost, error, plan_json, created_at, updated_at, completed_at
			FROM tasks WHERE project_id = $1 AND (created_at, id) < ($2, $3)
			ORDER BY created_at DESC, id DESC
			LIMIT $4
		`
		rows, err = r.pool.Query(ctx, query, projectID, cursorTime, cursorID, limit+1)
	} else {
		query = `
			SELECT id, project_id, user_id, prompt, status, result, model, provider,
			       complexity, max_tokens, max_iterations, input_tokens, output_tokens,
			       total_tokens, cost, error, plan_json, created_at, updated_at, completed_at
			FROM tasks WHERE project_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT $2
		`
		rows, err = r.pool.Query(ctx, query, projectID, limit+1)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(
			&t.ID, &t.ProjectID, &t.UserID, &t.Prompt, &t.Status,
			&t.Result, &t.Model, &t.Provider, &t.Complexity,
			&t.MaxTokens, &t.MaxIterations, &t.InputTokens, &t.OutputTokens,
			&t.TotalTokens, &t.Cost, &t.Error, &t.PlanJSON,
			&t.CreatedAt, &t.UpdatedAt, &t.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, t)
	}

	hasMore := len(tasks) > limit
	if hasMore {
		tasks = tasks[:limit]
	}

	page := &CursorPage{
		Tasks:   tasks,
		HasMore: hasMore,
	}
	if hasMore && len(tasks) > 0 {
		last := tasks[len(tasks)-1]
		page.NextCursor = last.CreatedAt.Format(time.RFC3339Nano) + "|" + last.ID
	}

	return page, nil
}

// ListByProject lists tasks for a project with OFFSET pagination.
// Deprecated: Use ListByProjectCursor for O(1) deep-page performance.
func (r *TaskRepository) ListByProject(ctx context.Context, projectID string, offset, limit int) ([]Task, int, error) {
	countQuery := `SELECT COUNT(*) FROM tasks WHERE project_id = $1`
	var total int
	if err := r.pool.QueryRow(ctx, countQuery, projectID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count tasks: %w", err)
	}

	query := `
		SELECT id, project_id, user_id, prompt, status, result, model, provider,
		       complexity, max_tokens, max_iterations, input_tokens, output_tokens,
		       total_tokens, cost, error, created_at, updated_at, completed_at
		FROM tasks WHERE project_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, projectID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(
			&t.ID, &t.ProjectID, &t.UserID, &t.Prompt, &t.Status,
			&t.Result, &t.Model, &t.Provider, &t.Complexity,
			&t.MaxTokens, &t.MaxIterations, &t.InputTokens, &t.OutputTokens,
			&t.TotalTokens, &t.Cost, &t.Error,
			&t.CreatedAt, &t.UpdatedAt, &t.CompletedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, total, nil
}

// UpdateStatus updates the status and related fields of a task.
func (r *TaskRepository) UpdateStatus(ctx context.Context, id, status string) error {
	query := `UPDATE tasks SET status = $2, updated_at = NOW() WHERE id = $1`
	tag, err := r.pool.Exec(ctx, query, id, status)
	if err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task not found")
	}
	return nil
}

// Complete marks a task as completed with result data.
func (r *TaskRepository) Complete(ctx context.Context, id, result, model, provider string, inputTokens, outputTokens, totalTokens int, cost float64) error {
	query := `
		UPDATE tasks SET status = 'completed', result = $2, model = $3, provider = $4,
		       input_tokens = $5, output_tokens = $6, total_tokens = $7, cost = $8,
		       completed_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`
	tag, err := r.pool.Exec(ctx, query, id, result, model, provider, inputTokens, outputTokens, totalTokens, cost)
	if err != nil {
		return fmt.Errorf("failed to complete task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task not found")
	}
	return nil
}

// Cancel marks a task as cancelled.
func (r *TaskRepository) Cancel(ctx context.Context, id string) error {
	query := `UPDATE tasks SET status = 'cancelled', updated_at = NOW() WHERE id = $1`
	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to cancel task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task not found")
	}
	return nil
}

// Delete removes a task by ID.
func (r *TaskRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM tasks WHERE id = $1`
	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task not found")
	}
	return nil
}
