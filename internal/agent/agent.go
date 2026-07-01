package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/vigilagent/vigilagent/internal/llm"
	"github.com/vigilagent/vigilagent/internal/tools"
)

// Agent orchestrates task execution using LLM providers and tools.
type Agent struct {
	router  *llm.ModelRouter
	tools   *tools.ToolRegistry
	sm      *StateMachine
	maxIter int
}

// NewAgent creates a new agent with the given dependencies.
func NewAgent(router *llm.ModelRouter, toolRegistry *tools.ToolRegistry) *Agent {
	return &Agent{
		router:  router,
		tools:   toolRegistry,
		sm:      NewStateMachine(),
		maxIter: 20,
	}
}

// TaskResult represents the result of a completed task.
type TaskResult struct {
	TaskID     string        `json:"task_id"`
	Status     string        `json:"status"`
	Result     string        `json:"result"`
	Steps      int           `json:"steps"`
	Cost       float64       `json:"cost"`
	Duration   time.Duration `json:"duration"`
	TokensUsed int           `json:"tokens_used"`
}

// llmPlanResponse is the expected JSON structure from LLM planning.
type llmPlanResponse struct {
	Steps []llmPlanStep `json:"steps"`
}

// llmPlanStep is a single step from LLM planning output.
type llmPlanStep struct {
	Tool        string                 `json:"tool"`
	Description string                 `json:"description"`
	Params      map[string]interface{} `json:"params,omitempty"`
}

// ExecuteTask runs a task through the agent loop: think -> act -> observe -> decide.
func (a *Agent) ExecuteTask(ctx context.Context, task *Task) (*TaskResult, error) {
	slog.Info("agent: executing task", "task_id", task.ID, "title", task.Title)

	if err := a.sm.Transition(task, EventStart); err != nil {
		return nil, fmt.Errorf("failed to start task: %w", err)
	}

	start := time.Now()
	now := time.Now()
	task.StartedAt = &now

	// Phase 1: Plan the task using the LLM
	plan, err := a.planTask(ctx, task)
	if err != nil {
		a.sm.Transition(task, EventStepFailed)
		return nil, fmt.Errorf("planning failed: %w", err)
	}
	task.Plan = plan

	if err := a.sm.Transition(task, EventPlanReady); err != nil {
		return nil, fmt.Errorf("plan transition failed: %w", err)
	}

	slog.Info("agent: plan created", "task_id", task.ID, "steps", plan.TotalSteps)

	// Phase 2: Execute each step, feeding results back to LLM for decisions
	var conversationHistory []llm.Message
	conversationHistory = append(conversationHistory, llm.Message{
		Role:    "system",
		Content: a.systemPrompt(),
	})
	conversationHistory = append(conversationHistory, llm.Message{
		Role:    "user",
		Content: fmt.Sprintf("Task: %s\n\nDescription: %s", task.Title, task.Description),
	})

	for i := 0; i < a.maxIter && i < plan.TotalSteps; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		step := plan.Steps[i]
		task.CurrentStep = i

		// Check HITL requirement
		tool, ok := a.tools.Get(step.Tool)
		if ok && tool.RequiresHITL(step.Params) {
			a.sm.Transition(task, EventHITLApproved)
		}

		slog.Info("agent: executing step", "task_id", task.ID, "step", i, "tool", step.Tool)

		// Execute the step
		stepResult := a.executeStep(ctx, task, step)
		task.Steps = append(task.Steps, stepResult)

		// Track tokens and cost
		task.InputTokens += stepResult.TokensUsed
		task.Cost += stepResult.Cost

		// Check token budget
		if task.MaxTokens > 0 && task.InputTokens+task.OutputTokens >= task.MaxTokens {
			slog.Warn("agent: token budget exhausted", "task_id", task.ID, "used", task.InputTokens+task.OutputTokens, "budget", task.MaxTokens)
			break
		}

		// Feed result back into conversation for next LLM decision
		stepResultJSON, _ := json.Marshal(map[string]interface{}{
			"tool":       step.Tool,
			"status":     stepResult.Status,
			"output":     stepResult.Result,
			"error":      stepResult.Error,
			"durationMs": stepResult.DurationMs,
		})
		conversationHistory = append(conversationHistory, llm.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("I executed step %d: %s (%s). Here are the results.", i, step.Description, step.Tool),
		})
		conversationHistory = append(conversationHistory, llm.Message{
			Role:    "user",
			Content: fmt.Sprintf("Step result: %s", string(stepResultJSON)),
		})

		if stepResult.Error != "" {
			a.sm.Transition(task, EventStepFailed)

			// Ask LLM what to do about the failure
			if i+1 < plan.TotalSteps {
				adjusted, err := a.reflectOnFailure(ctx, conversationHistory, task)
				if err == nil && adjusted != nil && len(adjusted.Steps) > 0 {
					// Filter adjusted steps to only valid tools
					validSteps := a.filterValidSteps(adjusted.Steps)
					// Replace remaining steps with adjusted plan
					remaining := plan.TotalSteps - i - 1
					if len(validSteps) < remaining {
						remaining = len(validSteps)
					}
					for j := 0; j < remaining && i+1+j < plan.TotalSteps; j++ {
						plan.Steps[i+1+j] = validSteps[j]
					}
				}
			}
			continue
		}

		a.sm.Transition(task, EventStepComplete)
	}

	// Phase 3: Final review using LLM
	a.sm.Transition(task, EventReviewPassed)

	result := a.buildResult(conversationHistory)
	task.Result = result
	task.TotalTokens = task.InputTokens + task.OutputTokens
	duration := time.Since(start)

	return &TaskResult{
		TaskID:     task.ID,
		Status:     string(task.State),
		Result:     result,
		Steps:      len(task.Steps),
		Cost:       task.Cost,
		Duration:   duration,
		TokensUsed: task.TotalTokens,
	}, nil
}

// systemPrompt returns the system prompt for the agent.
func (a *Agent) systemPrompt() string {
	var toolDescriptions []string
	for _, t := range a.tools.List() {
		toolDescriptions = append(toolDescriptions, fmt.Sprintf("- %s: %s", t.Name(), t.Description()))
	}

	return fmt.Sprintf(`You are VigilAgent, an expert AI coding assistant.
You can use tools to read, write, edit files, search code, and run commands.

Available tools:
%s

When planning, respond with a JSON array of steps:
{"steps": [{"tool": "tool_name", "description": "what to do", "params": {"key": "value"}}]}

When reflecting on failures, respond with adjusted remaining steps in the same format.
Always be concise and action-oriented.`, strings.Join(toolDescriptions, "\n"))
}

// planTask uses the LLM to create an execution plan for the task.
func (a *Agent) planTask(ctx context.Context, task *Task) (*Plan, error) {
	messages := []llm.Message{
		{Role: "system", Content: a.systemPrompt()},
		{Role: "user", Content: fmt.Sprintf("Plan the execution for this task:\n\nTitle: %s\nDescription: %s\n\nRespond with JSON: {\"steps\": [{\"tool\": \"...\", \"description\": \"...\", \"params\": {...}}]}", task.Title, task.Description)},
	}

	response, err := a.router.ExecuteWithFailover(ctx, &llm.Task{
		ID:          task.ID,
		Type:        task.Complexity,
		Description: task.Description,
		Tags:        task.Tags,
		Messages:    messages,
	})
	if err != nil {
		slog.Warn("LLM planning failed, using default plan", "error", err)
		return a.buildDefaultPlan(task), nil
	}

	// Track tokens from planning
	task.InputTokens += response.InputTokens
	task.OutputTokens += response.OutputTokens
	task.Cost += response.Cost
	task.ModelUsed = response.Model
	task.Provider = response.Provider

	// Parse LLM response into plan
	plan, err := a.parsePlanFromResponse(response.Content)
	if err != nil {
		slog.Warn("failed to parse LLM plan, using default", "error", err)
		return a.buildDefaultPlan(task), nil
	}

	return plan, nil
}

// parsePlanFromResponse attempts to parse an LLM response into a Plan.
func (a *Agent) parsePlanFromResponse(content string) (*Plan, error) {
	// Try to extract JSON from the response (may be wrapped in markdown)
	content = strings.TrimSpace(content)
	if idx := strings.Index(content, "```json"); idx != -1 {
		content = content[idx+7:]
		if endIdx := strings.Index(content, "```"); endIdx != -1 {
			content = content[:endIdx]
		}
	} else if idx := strings.Index(content, "```"); idx != -1 {
		content = content[idx+3:]
		if endIdx := strings.Index(content, "```"); endIdx != -1 {
			content = content[:endIdx]
		}
	}

	// Find the first { and last } to extract JSON
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response")
	}
	content = content[start : end+1]

	var parsed llmPlanResponse
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse JSON plan: %w", err)
	}

	if len(parsed.Steps) == 0 {
		return nil, fmt.Errorf("plan has no steps")
	}

	plan := &Plan{
		TotalSteps: len(parsed.Steps),
	}
	for i, s := range parsed.Steps {
		plan.Steps = append(plan.Steps, PlanStep{
			Index:       i,
			Tool:        s.Tool,
			Description: s.Description,
			Params:      s.Params,
		})
	}

	return plan, nil
}

// reflectOnFailure asks the LLM to adjust the remaining plan after a failure.
func (a *Agent) reflectOnFailure(ctx context.Context, history []llm.Message, task *Task) (*Plan, error) {
	// Add timeout to prevent indefinite blocking
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Build a list of available tools for the failure message
	var toolNames []string
	for _, t := range a.tools.List() {
		toolNames = append(toolNames, t.Name())
	}

	// Copy history to avoid mutating the original slice
	reflectMessages := make([]llm.Message, len(history))
	copy(reflectMessages, history)
	reflectMessages = append(reflectMessages, llm.Message{
		Role: "user",
		Content: fmt.Sprintf(
			"A step just failed. Please provide adjusted remaining steps to recover. "+
				"Current step: %d/%d. Available tools: %s. "+
				"Respond with JSON: {\"steps\": [...]}",
			task.CurrentStep+1, task.Plan.TotalSteps, strings.Join(toolNames, ", "),
		),
	})

	response, err := a.router.ExecuteWithFailover(ctx, &llm.Task{
		ID:       task.ID,
		Messages: reflectMessages,
	})
	if err != nil {
		return nil, err
	}

	return a.parsePlanFromResponse(response.Content)
}

// filterValidSteps filters plan steps to only include valid tool names.
func (a *Agent) filterValidSteps(steps []PlanStep) []PlanStep {
	validToolNames := make(map[string]bool)
	for _, t := range a.tools.List() {
		validToolNames[t.Name()] = true
	}

	var filtered []PlanStep
	for _, s := range steps {
		if validToolNames[s.Tool] {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// buildResult uses the LLM to synthesize a final result from all step outputs.
func (a *Agent) buildResult(history []llm.Message) string {
	// Collect all step outputs
	var outputs []string
	var lastOutput string
	for _, msg := range history {
		if msg.Role == "user" && strings.Contains(msg.Content, "Step result:") {
			outputs = append(outputs, msg.Content)
			lastOutput = msg.Content
		}
	}

	if len(outputs) == 0 {
		return "Task completed with no observable output."
	}

	// Ask the LLM to synthesize a final summary with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	summaryMsgs := []llm.Message{
		{Role: "system", Content: "You are summarizing the results of a completed task. Be concise and specific about what was done."},
		{Role: "user", Content: fmt.Sprintf("Task step outputs:\n%s\n\nProvide a brief summary of what was accomplished.", strings.Join(outputs, "\n"))},
	}

	resp, err := a.router.ExecuteWithFailover(ctx, &llm.Task{
		Messages: summaryMsgs,
	})
	if err != nil || resp == nil {
		// Fallback: include last step output for useful feedback
		if lastOutput != "" {
			return fmt.Sprintf("Task completed in %d steps. Last output: %s", len(outputs), lastOutput)
		}
		return fmt.Sprintf("Task completed. %d steps executed successfully.", len(outputs))
	}

	return resp.Content
}

// buildDefaultPlan creates a fallback plan when LLM planning fails.
func (a *Agent) buildDefaultPlan(task *Task) *Plan {
	plan := &Plan{
		Steps: []PlanStep{
			{Index: 0, Tool: "list_directory", Description: "Explore project structure", Params: map[string]interface{}{"path": "."}},
			{Index: 1, Tool: "search_code", Description: "Search for relevant code", Params: map[string]interface{}{"pattern": task.Title}},
			{Index: 2, Tool: "read_file", Description: "Read relevant files"},
			{Index: 3, Tool: "edit_file", Description: "Make changes"},
			{Index: 4, Tool: "run_command", Description: "Run tests to verify"},
		},
	}
	plan.TotalSteps = len(plan.Steps)
	return plan
}

func (a *Agent) executeStep(ctx context.Context, task *Task, step PlanStep) StepResult {
	start := time.Now()

	tool, ok := a.tools.Get(step.Tool)
	if !ok {
		return StepResult{
			Step:       step.Index,
			Tool:       step.Tool,
			Status:     "failed",
			Error:      fmt.Sprintf("tool %s not found", step.Tool),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}

	result, err := tool.Execute(ctx, step.Params)
	if err != nil {
		return StepResult{
			Step:       step.Index,
			Tool:       step.Tool,
			Status:     "failed",
			Error:      err.Error(),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}

	completedAt := time.Now()
	return StepResult{
		Step:        step.Index,
		Tool:        step.Tool,
		Status:      "completed",
		Result:      result.Output,
		DurationMs:  time.Since(start).Milliseconds(),
		Cost:        result.Cost,
		StartedAt:   start,
		CompletedAt: &completedAt,
	}
}
