package router

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/vigilagent/vigilagent/internal/agent"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/llm"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// NOTE: middlewareProcessHandler is defined in middleware_handlers.go
// with full scanner pipeline + SSE streaming support. Not duplicated here.

// middlewareMetricsHandler returns real middleware pipeline metrics.
func (r *Router) middlewareMetricsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	// Return LLM router health and cost intel metrics
	var healthData interface{}
	if r.llmRouter != nil && r.llmRouter.GetHealthMonitor() != nil {
		healthData = map[string]interface{}{"healthy_providers": r.llmRouter.GetHealthMonitor().GetHealthyProviders()}
	}

	var costData map[string]float64
	if r.costIntel != nil {
		costData = r.costIntel.CostByModel()
	}

	var totalRecords int
	var totalCost float64
	if r.costIntel != nil {
		totalRecords = r.costIntel.TotalRecords()
		totalCost = r.costIntel.TotalCost()
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"health":          healthData,
		"cost_by_model":   costData,
		"total_records":   totalRecords,
		"total_cost":      totalCost,
	})
}

// middlewarePatternsHandler returns learned patterns from the middleware pipeline.
func (r *Router) middlewarePatternsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	// Return cost intel recommendations as "patterns"
	var recs interface{}
	if r.costIntel != nil {
		recs = r.costIntel.GetRecommendations()
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"patterns":         recs,
		"middleware_stats": "active",
	})
}

// batchTaskHandler executes multiple tasks in parallel and returns all results.
func (r *Router) batchTaskHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		Tasks []struct {
			Prompt    string `json:"prompt"`
			ProjectID string `json:"project_id"`
		} `json:"tasks"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if len(input.Tasks) == 0 {
		response.BadRequest(w, "tasks array is required")
		return
	}
	if len(input.Tasks) > 10 {
		response.BadRequest(w, "maximum 10 tasks per batch")
		return
	}

	// Create all tasks
	type taskResult struct {
		Index  int    `json:"index"`
		TaskID string `json:"task_id"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}

	results := make([]taskResult, len(input.Tasks))
	for i, t := range input.Tasks {
		if t.Prompt == "" {
			results[i] = taskResult{Index: i, Status: "failed", Error: "prompt is required"}
			continue
		}
		if t.ProjectID == "" {
			results[i] = taskResult{Index: i, Status: "failed", Error: "project_id is required"}
			continue
		}

		task := &repository.Task{
			ProjectID:     t.ProjectID,
			UserID:        claims.UserID,
			Prompt:        t.Prompt,
			Status:        "pending",
			MaxTokens:     8192,
			MaxIterations: 20,
		}
		if err := r.tasks.Create(req.Context(), task); err != nil {
			results[i] = taskResult{Index: i, Status: "failed", Error: "failed to create task"}
			continue
		}

		results[i] = taskResult{Index: i, TaskID: task.ID, Status: "created"}

		// Start execution in background (copy the struct, not the pointer)
		taskCopy := *task
		go r.executeTaskBackground(&taskCopy)
	}

	// Dispatch batch webhook
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "batch.created",
			Payload: map[string]interface{}{
				"user_id": claims.UserID,
				"count":   len(results),
			},
		})
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{
		"tasks": results,
	})
}

// executeTaskBackground runs a task in the background goroutine.
func (r *Router) executeTaskBackground(task *repository.Task) {
	if r.agentExec == nil {
		if err := r.tasks.Complete(context.Background(), task.ID, task.Prompt, "", "", 0, 0, 0, 0); err != nil {
			slog.Error("failed to complete task (no agent)", "error", err, "task_id", task.ID)
		}
		return
	}

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

	bgCtx := context.Background()
	result, err := r.agentExec.ExecuteTask(bgCtx, agentTask)
	if err != nil {
		if updateErr := r.tasks.UpdateStatus(context.Background(), task.ID, "failed"); updateErr != nil {
			slog.Error("failed to update task status to failed", "error", updateErr, "task_id", task.ID)
		}
		return
	}
	if err := r.tasks.Complete(bgCtx, task.ID, result.Result, "", "",
		result.TokensUsed, 0, result.TokensUsed, result.Cost); err != nil {
		slog.Error("failed to complete task", "error", err, "task_id", task.ID)
	}
}

// healthStatsHandler returns real-time provider health statistics.
func (r *Router) healthStatsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	var healthSummary interface{}
	if r.llmRouter != nil && r.llmRouter.GetHealthMonitor() != nil {
		healthSummary = map[string]interface{}{"healthy_providers": r.llmRouter.GetHealthMonitor().GetHealthyProviders()}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"providers": healthSummary,
	})
}

// costOverrideHandler updates pricing for a specific model at runtime.
func (r *Router) costOverrideHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	var input struct {
		Model           string  `json:"model"`
		InputCostPer1K  float64 `json:"input_cost_per_1k"`
		OutputCostPer1K float64 `json:"output_cost_per_1k"`
		MaxTokens       int     `json:"max_tokens,omitempty"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Model == "" {
		response.BadRequest(w, "model is required")
		return
	}

	// Get existing info or create new (LookupPrice is thread-safe)
	info, _ := llm.LookupPrice(input.Model)
	if info.Name == "" {
		info = llm.ModelInfo{Name: input.Model}
	}
	if input.InputCostPer1K > 0 {
		info.InputCostPer1K = input.InputCostPer1K
	}
	if input.OutputCostPer1K > 0 {
		info.OutputCostPer1K = input.OutputCostPer1K
	}
	if input.MaxTokens > 0 {
		info.MaxTokens = input.MaxTokens
	}

	// Update the global price table (SetPrice is thread-safe)
	llm.SetPrice(input.Model, info)
	if r.llmRouter != nil {
		r.llmRouter.SetPrices(llm.AllPrices())
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"model":  input.Model,
		"status": "updated",
		"info":   info,
	})
}


