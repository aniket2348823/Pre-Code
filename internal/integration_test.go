package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/agent"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/config"
	"github.com/vigilagent/vigilagent/internal/llm"
	ratelimit "github.com/vigilagent/vigilagent/internal/middleware"
	"github.com/vigilagent/vigilagent/internal/router"
	"github.com/vigilagent/vigilagent/internal/tools"
)

// mockProvider implements llm.Provider for testing.
type mockProvider struct {
	name string
	resp *llm.ChatResponse
}

func (m *mockProvider) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if m.resp == nil {
		return nil, fmt.Errorf("mock provider failure")
	}
	return m.resp, nil
}
func (m *mockProvider) Stream(ctx context.Context, req *llm.ChatRequest) (<-chan *llm.ChatChunk, error) {
	ch := make(chan *llm.ChatChunk, 1)
	ch <- &llm.ChatChunk{Content: "mock response", Finish: true}
	close(ch)
	return ch, nil
}
func (m *mockProvider) HealthCheck(ctx context.Context) error { return nil }
func (m *mockProvider) Name() string                          { return m.name }

// newTestRouter creates a router with mock dependencies for unit testing.
func newTestRouter() *router.Router {
	cfg := &config.Config{
		Server: config.ServerConfig{Env: "test"},
		Auth:   config.AuthConfig{JWTSecret: "test-secret-key-for-testing-only"},
	}

	jwtSvc := auth.NewJWT(&cfg.Auth)
	apiKeySvc := auth.NewAPIKeyService("vga_")

	llmRouter := llm.NewModelRouter(&llm.RouterConfig{
		DefaultModel:  "gpt-4o-mini",
		BudgetPerTask: 1.0,
	})
	llmRouter.RegisterProvider("openai", &mockProvider{
		name: "openai",
		resp: &llm.ChatResponse{
			Content:      `{"steps": [{"tool":"list_directory","description":"Explore project structure","params":{"path":"."}}]}`,
			InputTokens:  100,
			OutputTokens: 50,
			Cost:         0.001,
		},
	})

	toolRegistry := tools.NewToolRegistry()
	toolRegistry.Register(&tools.ListDirectoryTool{})
	toolRegistry.Register(&tools.ReadFileTool{})

	agentExec := agent.NewAgent(llmRouter, toolRegistry)

	return router.New(router.Options{
		Config:    cfg,
		JWT:       jwtSvc,
		APIKeys:   apiKeySvc,
		APIAuth:   nil,
		RateLimit: ratelimit.NewRateLimiter(nil, 100, time.Minute),
		AgentExec: agentExec,
		LLMRouter: llmRouter,
		Memory:    nil,
	})
}

func TestHealthEndpoint(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health endpoint returned status %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "healthy" {
		t.Errorf("health status = %q, want healthy", body["status"])
	}
}

func TestRegisterHandler_MissingFields(t *testing.T) {
	r := newTestRouter()
	body := `{"email": ""}`
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("register missing fields returned %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestRegisterHandler_ShortPassword(t *testing.T) {
	r := newTestRouter()
	body := `{"email": "test@example.com", "password": "short"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("register short password returned %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestLoginHandler_Unauthenticated(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated access returned %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestLoginHandler_InvalidToken(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-here")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("invalid token returned %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAgentExecutionFlow(t *testing.T) {
	llmRouter := llm.NewModelRouter(&llm.RouterConfig{
		DefaultModel:  "gpt-4o-mini",
		BudgetPerTask: 1.0,
	})
	llmRouter.RegisterProvider("openai", &mockProvider{
		name: "openai",
		resp: &llm.ChatResponse{
			Content:      "Plan created",
			InputTokens:  100,
			OutputTokens: 50,
			Cost:         0.001,
		},
	})

	toolRegistry := tools.NewToolRegistry()
	toolRegistry.Register(&tools.ListDirectoryTool{})

	agentExec := agent.NewAgent(llmRouter, toolRegistry)

	task := &agent.Task{
		ID:            "test-task-1",
		Title:         "Test task",
		Description:   "A test task for integration testing",
		State:         agent.StatePending,
		MaxIterations: 5,
		MaxRetries:    2,
		Tags:          []string{},
	}

	result, err := agentExec.ExecuteTask(context.Background(), task)
	if err != nil {
		t.Fatalf("agent execution failed: %v", err)
	}

	if result == nil {
		t.Fatal("agent returned nil result")
	}
	if result.TaskID != "test-task-1" {
		t.Errorf("result.TaskID = %q, want test-task-1", result.TaskID)
	}
	if result.Steps == 0 {
		t.Error("agent executed 0 steps")
	}
	if result.Duration <= 0 {
		t.Error("agent duration should be positive")
	}
}

func TestModelRouter_Routing(t *testing.T) {
	llmRouter := llm.NewModelRouter(&llm.RouterConfig{
		DefaultModel:  "gpt-4o-mini",
		BudgetPerTask: 1.0,
	})
	llmRouter.RegisterProvider("openai", &mockProvider{name: "openai"})
	llmRouter.RegisterProvider("anthropic", &mockProvider{name: "anthropic"})

	simpleTask := &llm.Task{
		ID:          "simple",
		Type:        "formatting",
		Description: "Fix formatting",
		Messages:    []llm.Message{{Role: "user", Content: "Fix formatting"}},
	}

	decision, err := llmRouter.Route(context.Background(), simpleTask)
	if err != nil {
		t.Fatalf("routing failed: %v", err)
	}
	if decision == nil {
		t.Fatal("routing returned nil decision")
	}
	if decision.Model == "" {
		t.Error("routing returned empty model")
	}
	if decision.Provider == "" {
		t.Error("routing returned empty provider")
	}

	complexTask := &llm.Task{
		ID:              "complex",
		Type:            "architecture",
		Description:     "Refactor auth system with OAuth2",
		RequiresReasoning: true,
		Tags:            []string{"security"},
		Messages:        []llm.Message{{Role: "user", Content: "Refactor auth"}},
	}

	complexDecision, err := llmRouter.Route(context.Background(), complexTask)
	if err != nil {
		t.Fatalf("complex routing failed: %v", err)
	}
	if complexDecision.EstCost <= decision.EstCost {
		t.Errorf("complex task cost (%f) should be >= simple task cost (%f)",
			complexDecision.EstCost, decision.EstCost)
	}
}

func TestCircuitBreaker(t *testing.T) {
	cb := llm.NewCircuitBreaker(3, 1*time.Second)

	if cb.IsOpen() {
		t.Error("circuit should be closed initially")
	}

	for i := 0; i < 3; i++ {
		cb.Execute(func() error {
			return fmt.Errorf("failure %d", i)
		})
	}

	if !cb.IsOpen() {
		t.Error("circuit should be open after 3 failures")
	}

	err := cb.Execute(func() error { return nil })
	if err != llm.ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}

	time.Sleep(1100 * time.Millisecond)

	err = cb.Execute(func() error { return nil })
	if err != nil {
		t.Errorf("half-open request should succeed, got %v", err)
	}
	if cb.IsOpen() {
		t.Error("circuit should be closed after success in half-open")
	}
}

func TestModelRouter_HealthCheck(t *testing.T) {
	monitor := llm.NewHealthMonitor()
	monitor.RegisterProvider("openai", &mockProvider{name: "openai"})
	monitor.RegisterProvider("anthropic", &mockProvider{name: "anthropic"})

	healthy := monitor.GetHealthyProviders()
	if len(healthy) != 2 {
		t.Errorf("expected 2 healthy providers, got %d", len(healthy))
	}

	monitor.RecordFailure("openai")
	monitor.RecordFailure("openai")
	monitor.RecordFailure("openai")

	healthy = monitor.GetHealthyProviders()
	for _, name := range healthy {
		if name == "openai" {
			t.Error("openai should not be in healthy list after 3 failures")
		}
	}

	monitor.RecordSuccess("openai", 100*time.Millisecond)
	monitor.RecordSuccess("openai", 100*time.Millisecond)
	monitor.RecordSuccess("openai", 100*time.Millisecond)

	healthy = monitor.GetHealthyProviders()
	found := false
	for _, name := range healthy {
		if name == "openai" {
			found = true
			break
		}
	}
	if !found {
		t.Error("openai should be healthy after 3 successes")
	}
}

func TestAgentStateTransitions(t *testing.T) {
	sm := agent.NewStateMachine()
	task := &agent.Task{
		State:      agent.StatePending,
		Plan:       &agent.Plan{TotalSteps: 3},
		MaxRetries: 2,
	}

	if err := sm.Transition(task, agent.EventStart); err != nil {
		t.Fatal(err)
	}
	if task.State != agent.StatePlanning {
		t.Errorf("state = %v, want planning", task.State)
	}

	if err := sm.Transition(task, agent.EventPlanReady); err != nil {
		t.Fatal(err)
	}
	if task.State != agent.StateExecuting {
		t.Errorf("state = %v, want executing", task.State)
	}

	if err := sm.Transition(task, agent.EventStepComplete); err != nil {
		t.Fatal(err)
	}
	if task.State != agent.StateExecuting {
		t.Errorf("state after step complete = %v, want executing", task.State)
	}
}
