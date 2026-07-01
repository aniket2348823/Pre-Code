package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/agent"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// createTaskHandler creates a new task and starts execution.
func (r *Router) createTaskHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		Prompt          string `json:"prompt"`
		ProjectID       string `json:"project_id"`
		MaxTokens       int    `json:"max_tokens,omitempty"`
		MaxIterations   int    `json:"max_iterations,omitempty"`
		ModelPreference string `json:"model_preference,omitempty"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Prompt == "" {
		response.BadRequest(w, "prompt is required")
		return
	}
	if input.ProjectID == "" {
		response.BadRequest(w, "project_id is required")
		return
	}
	if input.MaxTokens <= 0 {
		input.MaxTokens = 8192
	}
	if input.MaxIterations <= 0 {
		input.MaxIterations = 20
	}

	task := &repository.Task{
		ProjectID:     input.ProjectID,
		UserID:        claims.UserID,
		Prompt:        input.Prompt,
		Status:        "pending",
		MaxTokens:     input.MaxTokens,
		MaxIterations: input.MaxIterations,
	}
	if err := r.tasks.Create(req.Context(), task); err != nil {
		response.InternalError(w, "failed to create task")
		return
	}

	// Start agent execution in background using a fresh context
	go func() {
		bgCtx := context.Background()
		agentTask := &agent.Task{
			ID:            task.ID,
			UserID:        task.UserID,
			ProjectID:     task.ProjectID,
			Title:         task.Prompt,
			Description:   task.Prompt,
			MaxIterations: task.MaxIterations,
			MaxRetries:    3,
			MaxTokens:     task.MaxTokens,
			State:         agent.StatePending,
			Tags:          []string{},
		}
		if r.agentExec != nil {
			result, err := r.agentExec.ExecuteTask(bgCtx, agentTask)
			if err != nil {
				_ = r.tasks.UpdateStatus(bgCtx, task.ID, "failed")
			} else {
				_ = r.tasks.Complete(bgCtx, task.ID, result.Result, "", "",
					result.TokensUsed, 0, result.TokensUsed, result.Cost)
			}
		} else {
			// No agent configured — mark as completed with echo
			_ = r.tasks.Complete(bgCtx, task.ID, task.Prompt, "", "", 0, 0, 0, 0)
		}
	}()

	response.Created(w, map[string]interface{}{
		"task": map[string]interface{}{
			"id":             task.ID,
			"project_id":     task.ProjectID,
			"prompt":         task.Prompt,
			"status":         task.Status,
			"max_tokens":     task.MaxTokens,
			"max_iterations": task.MaxIterations,
			"created_at":     task.CreatedAt,
		},
	})
}

// getTaskHandler returns task details by ID.
func (r *Router) getTaskHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	taskID := chi.URLParam(req, "taskID")
	task, err := r.tasks.FindByID(req.Context(), taskID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	project, err := r.projects.FindByID(req.Context(), task.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	response.JSON(w, http.StatusOK, task)
}

// listTasksHandler lists tasks with pagination.
func (r *Router) listTasksHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	projectID := req.URL.Query().Get("project_id")
	if projectID == "" {
		response.BadRequest(w, "project_id query parameter is required")
		return
	}
	project, err := r.projects.FindByID(req.Context(), projectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}

	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(req.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	tasks, total, err := r.tasks.ListByProject(req.Context(), projectID, offset, pageSize)
	if err != nil {
		response.InternalError(w, "failed to list tasks")
		return
	}
	if tasks == nil {
		tasks = []repository.Task{}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"tasks": tasks,
		"page": map[string]interface{}{
			"page":        page,
			"page_size":   pageSize,
			"total":       total,
			"total_pages": (total + pageSize - 1) / pageSize,
		},
	})
}

// cancelTaskHandler cancels a running task.
func (r *Router) cancelTaskHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	taskID := chi.URLParam(req, "taskID")
	task, err := r.tasks.FindByID(req.Context(), taskID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	project, err := r.projects.FindByID(req.Context(), task.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	if task.Status == "completed" || task.Status == "cancelled" || task.Status == "failed" {
		response.BadRequest(w, fmt.Sprintf("cannot cancel task in status: %s", task.Status))
		return
	}
	if err := r.tasks.Cancel(req.Context(), taskID); err != nil {
		response.InternalError(w, "failed to cancel task")
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"message": "task cancelled",
		"task_id": taskID,
	})
}

// streamTaskHandler streams task updates via SSE.
func (r *Router) streamTaskHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	taskID := chi.URLParam(req, "taskID")
	task, err := r.tasks.FindByID(req.Context(), taskID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	project, err := r.projects.FindByID(req.Context(), task.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)
	if !canFlush {
		response.InternalError(w, "streaming not supported")
		return
	}

	// Send current task status
	data, _ := json.Marshal(map[string]interface{}{
		"task_id": task.ID,
		"status":  task.Status,
		"prompt":  task.Prompt,
	})
	fmt.Fprintf(w, "event: task_update\ndata: %s\n\n", data)
	flusher.Flush()

	// Use a fresh context for the polling goroutine
	bgCtx := context.Background()
	ctx, cancel := context.WithCancel(bgCtx)
	defer cancel()

	// Poll task status until terminal state
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t, err := r.tasks.FindByID(ctx, taskID)
				if err != nil {
					return
				}
				d, _ := json.Marshal(map[string]interface{}{
					"task_id":       t.ID,
					"status":        t.Status,
					"result":        t.Result,
					"input_tokens":  t.InputTokens,
					"output_tokens": t.OutputTokens,
					"cost":          t.Cost,
				})
				fmt.Fprintf(w, "event: task_update\ndata: %s\n\n", d)
				flusher.Flush()

				if t.Status == "completed" || t.Status == "failed" || t.Status == "cancelled" {
					fmt.Fprintf(w, "event: done\ndata: {}\n\n")
					flusher.Flush()
					return
				}
			}
		}
	}()

	// Wait for client disconnect or stream completion
	select {
	case <-req.Context().Done():
		cancel()
	case <-done:
		// Stream completed naturally
	}
}
