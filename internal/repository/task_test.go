package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vigilagent/vigilagent/internal/database"
)

func TestNewTaskRepository(t *testing.T) {
	t.Run("creates repo with pool", func(t *testing.T) {
		conn := database.NewConn(nil)
		r := NewTaskRepository(conn)
		require.NotNil(t, r)
		assert.Equal(t, conn, r.pool)
	})

	t.Run("creates repo with nil conn", func(t *testing.T) {
		r := NewTaskRepository(nil)
		require.NotNil(t, r)
		assert.Nil(t, r.pool)
	})
}

func TestTask_Struct(t *testing.T) {
	now := time.Now()
	completed := now.Add(5 * time.Minute)
	task := Task{
		ID:            "task-1",
		ProjectID:     "proj-1",
		UserID:        "user-1",
		Prompt:        "Fix the bug",
		Status:        "completed",
		Result:        "Bug fixed in auth.go",
		Model:         "gpt-4",
		Provider:      "openai",
		Complexity:    "medium",
		MaxTokens:     4096,
		MaxIterations: 20,
		InputTokens:   1000,
		OutputTokens:  500,
		TotalTokens:   1500,
		Cost:          0.05,
		Error:         "",
		Metadata:      map[string]interface{}{"retry": true},
		PlanJSON:      []byte(`{"steps":["analyze","fix"]}`),
		CreatedAt:     now,
		UpdatedAt:     now,
		CompletedAt:   &completed,
	}

	assert.Equal(t, "task-1", task.ID)
	assert.Equal(t, "proj-1", task.ProjectID)
	assert.Equal(t, "user-1", task.UserID)
	assert.Equal(t, "Fix the bug", task.Prompt)
	assert.Equal(t, "completed", task.Status)
	assert.Equal(t, "Bug fixed in auth.go", task.Result)
	assert.Equal(t, "gpt-4", task.Model)
	assert.Equal(t, "openai", task.Provider)
	assert.Equal(t, "medium", task.Complexity)
	assert.Equal(t, 4096, task.MaxTokens)
	assert.Equal(t, 20, task.MaxIterations)
	assert.Equal(t, 1000, task.InputTokens)
	assert.Equal(t, 500, task.OutputTokens)
	assert.Equal(t, 1500, task.TotalTokens)
	assert.Equal(t, 0.05, task.Cost)
	assert.Equal(t, true, task.Metadata["retry"])
	assert.Equal(t, []byte(`{"steps":["analyze","fix"]}`), task.PlanJSON)
	assert.NotNil(t, task.CompletedAt)
}

func TestTask_Struct_ZeroValues(t *testing.T) {
	task := Task{}
	assert.Empty(t, task.ID)
	assert.Empty(t, task.ProjectID)
	assert.Empty(t, task.UserID)
	assert.Empty(t, task.Prompt)
	assert.Empty(t, task.Status)
	assert.Empty(t, task.Result)
	assert.Empty(t, task.Model)
	assert.Empty(t, task.Provider)
	assert.Empty(t, task.Complexity)
	assert.Equal(t, 0, task.MaxTokens)
	assert.Equal(t, 0, task.MaxIterations)
	assert.Equal(t, 0, task.InputTokens)
	assert.Equal(t, 0, task.OutputTokens)
	assert.Equal(t, 0, task.TotalTokens)
	assert.Equal(t, 0.0, task.Cost)
	assert.Empty(t, task.Error)
	assert.Nil(t, task.Metadata)
	assert.Nil(t, task.PlanJSON)
	assert.True(t, task.CreatedAt.IsZero())
	assert.True(t, task.UpdatedAt.IsZero())
	assert.Nil(t, task.CompletedAt)
}

func TestTaskRepository_Create_NilPool(t *testing.T) {
	r := NewTaskRepository(nil)
	assert.Panics(t, func() {
		_ = r.Create(nil, &Task{})
	})
}

func TestTaskRepository_FindByID_NilPool(t *testing.T) {
	r := NewTaskRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.FindByID(nil, "nonexistent")
	})
}

func TestTaskRepository_ListByProject_NilPool(t *testing.T) {
	r := NewTaskRepository(nil)
	assert.Panics(t, func() {
		_, _, _ = r.ListByProject(nil, "proj-1", 0, 10)
	})
}

func TestTaskRepository_ListByProjectCursor_NilPool(t *testing.T) {
	r := NewTaskRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.ListByProjectCursor(nil, "proj-1", "", 10)
	})
}

func TestTaskRepository_UpdateStatus_NilPool(t *testing.T) {
	r := NewTaskRepository(nil)
	assert.Panics(t, func() {
		_ = r.UpdateStatus(nil, "id", "completed")
	})
}

func TestTaskRepository_Complete_NilPool(t *testing.T) {
	r := NewTaskRepository(nil)
	assert.Panics(t, func() {
		_ = r.Complete(nil, "id", "done", "gpt-4", "openai", 100, 50, 150, 0.01)
	})
}

func TestTaskRepository_Cancel_NilPool(t *testing.T) {
	r := NewTaskRepository(nil)
	assert.Panics(t, func() {
		_ = r.Cancel(nil, "id")
	})
}

func TestTaskRepository_Delete_NilPool(t *testing.T) {
	r := NewTaskRepository(nil)
	assert.Panics(t, func() {
		_ = r.Delete(nil, "id")
	})
}

func TestCursorPage_Struct(t *testing.T) {
	now := time.Now()
	page := CursorPage{
		Tasks: []Task{
			{ID: "t1", Prompt: "task 1"},
			{ID: "t2", Prompt: "task 2"},
		},
		NextCursor: now.Format(time.RFC3339Nano),
		HasMore:    true,
	}

	assert.Len(t, page.Tasks, 2)
	assert.Equal(t, "t1", page.Tasks[0].ID)
	assert.Equal(t, "t2", page.Tasks[1].ID)
	assert.NotEmpty(t, page.NextCursor)
	assert.True(t, page.HasMore)
}

func TestCursorPage_EmptyTasks(t *testing.T) {
	page := CursorPage{}
	assert.Nil(t, page.Tasks)
	assert.Empty(t, page.NextCursor)
	assert.False(t, page.HasMore)
}

func TestCursorPage_NoMorePages(t *testing.T) {
	page := CursorPage{
		Tasks:   []Task{{ID: "t1"}},
		HasMore: false,
	}
	assert.Len(t, page.Tasks, 1)
	assert.False(t, page.HasMore)
	assert.Empty(t, page.NextCursor)
}

func TestTask_Struct_NilCompletedAt(t *testing.T) {
	task := Task{CompletedAt: nil}
	assert.Nil(t, task.CompletedAt)
}

func TestTask_Struct_NilMetadata(t *testing.T) {
	task := Task{Metadata: nil}
	assert.Nil(t, task.Metadata)
}

func TestTask_Struct_NilPlanJSON(t *testing.T) {
	task := Task{PlanJSON: nil}
	assert.Nil(t, task.PlanJSON)
}

func TestTask_Struct_EmptyError(t *testing.T) {
	task := Task{Error: ""}
	assert.Empty(t, task.Error)
}

func TestTask_Struct_NonEmptyError(t *testing.T) {
	task := Task{Error: "rate limit exceeded"}
	assert.Equal(t, "rate limit exceeded", task.Error)
}
