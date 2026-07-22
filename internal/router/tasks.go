package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/agent"
	"github.com/vigilagent/vigilagent/internal/auth"
	apperrors "github.com/vigilagent/vigilagent/internal/errors"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/pagination"
	"github.com/vigilagent/vigilagent/pkg/query"
	"github.com/vigilagent/vigilagent/pkg/response"
	"github.com/vigilagent/vigilagent/pkg/validation"
)

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
	v, ok := validation.DecodeAndValidate(w, req, &input)
	if !ok {
		return
	}

	input.Prompt = strings.TrimSpace(input.Prompt)

	v.Required("prompt", input.Prompt)
	v.Required("project_id", input.ProjectID)

	if v.WriteResponse(w, req) {
		return
	}

	// Verify project membership
	if _, err := r.requireProjectMember(req.Context(), input.ProjectID, claims.UserID); err != nil {
		response.Forbidden(w, "access denied")
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
		response.JSON(w, http.StatusInternalServerError, apperrors.New(apperrors.ErrDBError, "failed to create task"))
		return
	}

	// Create a channel for real-time SSE events from the agent
	sseCh := make(chan TaskSSEEvent, 16)
	if r.wsManager != nil {
		r.wsManager.SSERegister(task.ID, sseCh)
		defer r.wsManager.SSEUnregister(task.ID)
	}

	// Start agent execution in background using a fresh context
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in task execution goroutine", "panic", rec, "task_id", task.ID)
				// Mark task as failed so it doesn't get stuck
				if err := r.tasks.UpdateStatus(context.Background(), task.ID, "failed"); err != nil {
					slog.Error("failed to mark task as failed after panic", "error", err, "task_id", task.ID)
				}
			}
		}()
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
			// Wire per-task state change callback to dispatch lifecycle webhooks
			// and push real-time SSE events for HITL and state transitions
			agentTask.OnStateChange = func(taskID, oldState, newState string) {
				// Persist intermediate states to DB so SSE polling detects transitions.
				// Terminal states (completed, failed) are written by Complete/UpdateStatus
				// after ExecuteTask returns, which also sets result/tokens/cost.
				isTerminal := newState == "completed" || newState == "failed" || newState == "cancelled"
				if !isTerminal {
					if err := r.tasks.UpdateStatus(bgCtx, taskID, newState); err != nil {
						slog.Warn("failed to update task status", "error", err, "task_id", taskID, "new_state", newState)
					}
				}

				// Push real-time SSE event for all transitions
				if r.wsManager != nil {
					r.wsManager.SSEBroadcast(taskID, TaskSSEEvent{
						TaskID: taskID,
						Event:  "task_lifecycle",
						Payload: map[string]interface{}{
							"old_state": oldState,
							"new_state": newState,
						},
					})
				}

				// Dispatch webhook for mapped events
				if r.webhookEngine != nil {
					var eventType string
					switch newState {
					case "planning":
						eventType = "task.planning"
					case "executing":
						if oldState == "waiting_hitl" {
							return // already dispatched task.executing before HITL
						}
						eventType = "task.executing"
					case "waiting_hitl":
						eventType = "hitl.required"
					case "cancelled":
						eventType = "task.cancelled"
					default:
						return
					}
					r.webhookEngine.Dispatch(bgCtx, webhook.Event{
						Type: eventType,
						Payload: map[string]interface{}{
							"task_id":    taskID,
							"user_id":    task.UserID,
							"project_id": task.ProjectID,
							"old_state":  oldState,
							"new_state":  newState,
						},
					})
				}
			}

			// Dispatch task.started lifecycle event
			if r.webhookEngine != nil {
				r.webhookEngine.Dispatch(bgCtx, webhook.Event{
					Type: "task.started",
					Payload: map[string]interface{}{
						"task_id":    task.ID,
						"user_id":    task.UserID,
						"project_id": task.ProjectID,
					},
				})
			}

			result, err := r.agentExec.ExecuteTask(bgCtx, agentTask)
			if err != nil {
				if err := r.tasks.UpdateStatus(bgCtx, task.ID, "failed"); err != nil {
					slog.Error("failed to update task status to failed", "error", err, "task_id", task.ID)
				}
				if r.webhookEngine != nil {
					r.webhookEngine.Dispatch(bgCtx, webhook.Event{
						Type: "task.failed",
						Payload: map[string]interface{}{
							"task_id":   task.ID,
							"user_id":   task.UserID,
							"project_id": task.ProjectID,
							"error":     err.Error(),
						},
					})
				}
			} else {
				if err := r.tasks.Complete(bgCtx, task.ID, result.Result, "", "",
					result.TokensUsed, 0, result.TokensUsed, result.Cost); err != nil {
					slog.Error("failed to complete task", "error", err, "task_id", task.ID)
				}
				if r.webhookEngine != nil {
					r.webhookEngine.Dispatch(bgCtx, webhook.Event{
						Type: "task.completed",
						Payload: map[string]interface{}{
							"task_id":   task.ID,
							"user_id":   task.UserID,
							"project_id": task.ProjectID,
							"cost":      result.Cost,
							"tokens":    result.TokensUsed,
						},
					})
				}
			}
		} else {
			// No agent configured — mark as completed with echo
			if err := r.tasks.Complete(bgCtx, task.ID, task.Prompt, "", "", 0, 0, 0, 0); err != nil {
				slog.Error("failed to complete task (no agent)", "error", err, "task_id", task.ID)
			}
			if r.webhookEngine != nil {
				r.webhookEngine.Dispatch(bgCtx, webhook.Event{
					Type: "task.completed",
					Payload: map[string]interface{}{
						"task_id":   task.ID,
						"user_id":   task.UserID,
						"project_id": task.ProjectID,
					},
				})
			}
		}
	}()

	// Dispatch lifecycle webhook notifications
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "task.created",
			Payload: map[string]interface{}{
				"task_id":   task.ID,
				"project_id": task.ProjectID,
				"user_id":   claims.UserID,
				"prompt":    task.Prompt,
				"status":    task.Status,
			},
		})
	}

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
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	taskID := chi.URLParam(req, "taskID")
	task, _, err := r.requireTaskMember(req.Context(), taskID, claims.UserID)
	if err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
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
	if _, err := r.requireProjectMember(req.Context(), projectID, claims.UserID); err != nil {
		response.Forbidden(w, "access denied")
		return
	}

	// Fetch all tasks for filtering/sorting/pagination
	tasks, _, err := r.tasks.ListByProject(req.Context(), projectID, 0, 100000)
	if err != nil {
		response.InternalError(w, "failed to list tasks")
		return
	}
	if tasks == nil {
		tasks = []repository.Task{}
	}

	filter, sortVal := query.Parse(req)
	
	// Support page-based query as fallback, cursor-based as primary
	cursor := req.URL.Query().Get("cursor")
	if cursor == "" && req.URL.Query().Get("page") != "" {
		// Offset-based pagination
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		pageSize, _ := strconv.Atoi(req.URL.Query().Get("page_size"))
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 100 {
			pageSize = 20
		}
		
		// First filter and sort all tasks
		allProcessed, _ := query.ProcessList(tasks, filter, sortVal, pagination.Params{Limit: 100000})
		
		total := len(allProcessed)
		offset := (page - 1) * pageSize
		end := offset + pageSize
		if offset > total {
			offset = total
		}
		if end > total {
			end = total
		}
		paginated := allProcessed[offset:end]
		
		response.SuccessWithMeta(w, req, http.StatusOK, paginated, &response.Meta{
			Total:   total,
			Limit:   pageSize,
			Offset:  offset,
			HasMore: end < total,
		})
		return
	}

	pag := pagination.ParseRequest(req)
	processed, meta := query.ProcessList(tasks, filter, sortVal, pag)
	response.SuccessWithMeta(w, req, http.StatusOK, processed, meta)
}

// cancelTaskHandler cancels a running task.
func (r *Router) cancelTaskHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	taskID := chi.URLParam(req, "taskID")
	task, _, err := r.requireTaskMember(req.Context(), taskID, claims.UserID)
	if err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	if task.Status == "completed" || task.Status == "cancelled" || task.Status == "failed" {
		response.JSON(w, http.StatusConflict, apperrors.New(apperrors.ErrConflict, fmt.Sprintf("cannot cancel task in status: %s", task.Status)))
		return
	}
	if err := r.tasks.Cancel(req.Context(), taskID); err != nil {
		response.JSON(w, http.StatusInternalServerError, apperrors.New(apperrors.ErrDBError, "failed to cancel task"))
		return
	}
	// Dispatch lifecycle webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "task.cancelled",
			Payload: map[string]interface{}{
				"task_id":    taskID,
				"project_id": task.ProjectID,
				"user_id":    claims.UserID,
			},
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"message": "task cancelled",
		"task_id": taskID,
	})
}

// streamTaskHandler streams task updates via SSE with lifecycle events.
func (r *Router) streamTaskHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	taskID := chi.URLParam(req, "taskID")
	task, _, err := r.requireTaskMember(req.Context(), taskID, claims.UserID)
	if err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)
	if !canFlush {
		response.JSON(w, http.StatusNotFound, apperrors.New(apperrors.ErrNotFound, "streaming not supported"))
		return
	}

	// Send current task status as initial event
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

	// Track last state to detect transitions and emit lifecycle events
	lastStatus := task.Status

	// sendSSE writes an SSE event and flushes
	sendSSE := func(eventType, payload string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, payload)
		flusher.Flush()
	}

	// sendLifecycleEvent maps DB status to SSE lifecycle event type and sends
	sendLifecycleEvent := func(t *repository.Task) {
		if t.Status == lastStatus {
			return
		}
		lastStatus = t.Status

		var eventType string
		switch t.Status {
		case "planning":
			eventType = "task_planning"
		case "executing":
			eventType = "task_executing"
		case "completed":
			eventType = "task_completed"
		case "failed":
			eventType = "task_failed"
		case "cancelled":
			eventType = "task_cancelled"
		default:
			return
		}
		d, _ := json.Marshal(map[string]interface{}{
			"task_id": t.ID,
			"status":  t.Status,
			"result":  t.Result,
		})
		sendSSE(eventType, string(d))
	}

	// Register a channel with the SSE hub so real-time events from the agent
	// (HITL decisions, state transitions) reach this SSE subscriber.
	sseCh := make(chan TaskSSEEvent, 16)
	if r.wsManager != nil {
		r.wsManager.SSERegister(taskID, sseCh)
		defer r.wsManager.SSEUnregister(taskID)
	}

	// Poll task status until terminal state
	done := make(chan struct{})
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in SSE polling goroutine", "panic", rec, "task_id", taskID)
			}
			close(done)
		}()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-sseCh:
				// Real-time event from agent (e.g. HITL, state transition)
				if !ok {
					continue
				}
				d, _ := json.Marshal(evt.Payload)
				sendSSE(evt.Event, string(d))
			case <-heartbeat.C:
				// Send heartbeat to keep connection alive and detect disconnects
				sendSSE("heartbeat", `{"ts":"`+time.Now().UTC().Format(time.RFC3339)+`"}`)
			case <-ticker.C:
				t, err := r.tasks.FindByID(ctx, taskID)
				if err != nil {
					return
				}

				// Emit lifecycle event on state transition
				sendLifecycleEvent(t)

				// Always send the general update
				d, _ := json.Marshal(map[string]interface{}{
					"task_id":       t.ID,
					"status":        t.Status,
					"result":        t.Result,
					"input_tokens":  t.InputTokens,
					"output_tokens": t.OutputTokens,
					"cost":          t.Cost,
				})
				sendSSE("task_update", string(d))

				if t.Status == "completed" || t.Status == "failed" || t.Status == "cancelled" {
					sendSSE("done", "{}")
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
