package llm

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Provider defines the interface for all LLM providers.
type Provider interface {
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error)
	HealthCheck(ctx context.Context) error
	Name() string
}

// ChatRequest represents a request to an LLM.
type ChatRequest struct {
	Model       string
	Messages    []Message
	MaxTokens   int
	Temperature float64
	Tools       []ToolDef
	System      string
}

// ChatResponse represents a response from an LLM.
type ChatResponse struct {
	Content      string
	ToolCalls    []ToolCall
	InputTokens  int
	OutputTokens int
	Cost         float64
	Latency      time.Duration
	Model        string
	Provider     string
	StopReason   string
}

// ChatChunk represents a streaming chunk.
type ChatChunk struct {
	Content    string
	ToolCalls  []ToolCall
	StopReason string
	Finish     bool
}

// Message represents a chat message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolDef defines a tool for LLM function calling.
type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ToolCall represents an LLM's request to call a tool.
type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// Complexity represents task complexity level.
type Complexity float64

const (
	ComplexitySimple   Complexity = 0.2
	ComplexityModerate Complexity = 0.5
	ComplexityComplex  Complexity = 0.8
	ComplexityCritical Complexity = 1.0
)

// ModelInfo contains pricing and capability info for a model.
type ModelInfo struct {
	Name            string
	Provider        string
	InputCostPer1K  float64
	OutputCostPer1K float64
	MaxTokens       int
}

// PriceTable maps model names to pricing.
var PriceTable = map[string]ModelInfo{
	"claude-opus-4":            {Name: "claude-opus-4", Provider: "anthropic", InputCostPer1K: 0.015, OutputCostPer1K: 0.075, MaxTokens: 8192},
	"claude-sonnet-4-20250514": {Name: "claude-sonnet-4-20250514", Provider: "anthropic", InputCostPer1K: 0.003, OutputCostPer1K: 0.015, MaxTokens: 8192},
	"claude-haiku-3.5":         {Name: "claude-haiku-3.5", Provider: "anthropic", InputCostPer1K: 0.0008, OutputCostPer1K: 0.004, MaxTokens: 8192},
	"gpt-4.5":                  {Name: "gpt-4.5", Provider: "openai", InputCostPer1K: 0.015, OutputCostPer1K: 0.06, MaxTokens: 16384},
	"gpt-4o":                   {Name: "gpt-4o", Provider: "openai", InputCostPer1K: 0.0025, OutputCostPer1K: 0.01, MaxTokens: 16384},
	"gpt-4o-mini":              {Name: "gpt-4o-mini", Provider: "openai", InputCostPer1K: 0.00015, OutputCostPer1K: 0.0006, MaxTokens: 16384},
	"gemini-2.5-pro":           {Name: "gemini-2.5-pro", Provider: "google", InputCostPer1K: 0.00125, OutputCostPer1K: 0.01, MaxTokens: 8192},
	"gemini-2.0-flash":         {Name: "gemini-2.0-flash", Provider: "google", InputCostPer1K: 0.000075, OutputCostPer1K: 0.0003, MaxTokens: 8192},
	"deepseek-r1":              {Name: "deepseek-r1", Provider: "deepseek", InputCostPer1K: 0.00055, OutputCostPer1K: 0.00219, MaxTokens: 8192},
}

// Task represents a task for model routing (canonical definition).
type Task struct {
	ID                   string
	Type                 string
	Description          string
	FilesChanged         []string
	RequiresReasoning    bool
	IsNovel              bool
	Tags                 []string
	Messages             []Message
	Complexity           Complexity
	RequiredCapabilities []string
}

// RoutingDecision represents the result of model routing.
type RoutingDecision struct {
	Provider   string
	Model      string
	Reason     string
	EstCost    float64
	EstLatency time.Duration
	Confidence float64
	Fallbacks  []FallbackOption
}

// FallbackOption represents a fallback model choice.
type FallbackOption struct {
	Provider string
	Model    string
	EstCost  float64
}

// RoutingCandidate represents a potential model selection.
type RoutingCandidate struct {
	Provider   string
	Model      string
	Reason     string
	EstCost    float64
	EstLatency time.Duration
	Confidence float64
}

// ModelRouter selects the optimal LLM for each task.
type ModelRouter struct {
	providers     map[string]Provider
	healthMonitor *HealthMonitor
	config        *RouterConfig
	mu            sync.RWMutex
}

// RouterConfig holds routing configuration.
type RouterConfig struct {
	DefaultModel  string
	BudgetPerTask float64
}

// NewModelRouter creates a new model router.
func NewModelRouter(cfg *RouterConfig) *ModelRouter {
	r := &ModelRouter{
		providers:     make(map[string]Provider),
		healthMonitor: NewHealthMonitor(),
		config:        cfg,
	}
	if r.config == nil {
		r.config = &RouterConfig{
			DefaultModel:  "claude-sonnet-4-20250514",
			BudgetPerTask: 1.00,
		}
	}
	return r
}

// RegisterProvider adds a provider to the router.
func (r *ModelRouter) RegisterProvider(name string, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = p
	r.healthMonitor.RegisterProvider(name, p)
}

// Route selects the optimal model for a task.
func (r *ModelRouter) Route(ctx context.Context, task *Task) (*RoutingDecision, error) {
	complexity := r.classifyComplexity(task)
	healthy := r.healthMonitor.GetHealthyProviders()
	capable := r.filterByCapabilities(healthy, task.RequiredCapabilities)
	candidates := r.rankCandidates(capable, complexity)

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no healthy providers available")
	}

	primary := candidates[0]
	var fallbacks []FallbackOption
	if len(candidates) > 1 {
		end := 4
		if len(candidates) < end {
			end = len(candidates)
		}
		for _, c := range candidates[1:end] {
			fallbacks = append(fallbacks, FallbackOption{
				Provider: c.Provider,
				Model:    c.Model,
				EstCost:  c.EstCost,
			})
		}
	}

	return &RoutingDecision{
		Provider:   primary.Provider,
		Model:      primary.Model,
		Reason:     primary.Reason,
		EstCost:    primary.EstCost,
		EstLatency: primary.EstLatency,
		Confidence: primary.Confidence,
		Fallbacks:  fallbacks,
	}, nil
}

// classifyComplexity implements the 5-factor scoring formula from doc 06.
func (r *ModelRouter) classifyComplexity(task *Task) Complexity {
	score := 0.0

	switch task.Type {
	case "formatting", "rename", "documentation":
		score += 0.1
	case "bug_fix", "small_feature":
		score += 0.3
	case "refactoring", "feature":
		score += 0.5
	case "architecture", "security":
		score += 0.7
	}

	fileCount := len(task.FilesChanged)
	if fileCount > 10 {
		score += 0.3
	} else if fileCount > 5 {
		score += 0.2
	} else if fileCount > 1 {
		score += 0.1
	}

	if task.RequiresReasoning {
		score += 0.2
	}

	if task.IsNovel {
		score += 0.15
	}

	for _, tag := range task.Tags {
		if tag == "security" || tag == "production" {
			score += 0.3
			break
		}
	}

	return Complexity(minf(1.0, score))
}

func (r *ModelRouter) filterByCapabilities(healthy []string, capabilities []string) []string {
	if len(capabilities) == 0 {
		return healthy
	}
	return healthy
}

func (r *ModelRouter) rankCandidates(capable []string, complexity Complexity) []RoutingCandidate {
	var candidates []RoutingCandidate
	models := r.getModelsForComplexity(complexity)

	for _, model := range models {
		info, ok := PriceTable[model]
		if !ok {
			continue
		}

		estInput := 1500
		estOutput := 500
		estCost := (float64(estInput) / 1000.0) * info.InputCostPer1K + (float64(estOutput) / 1000.0) * info.OutputCostPer1K

		providerHealthy := false
		for _, p := range capable {
			if p == info.Provider || p == model {
				providerHealthy = true
				break
			}
		}
		if !providerHealthy {
			continue
		}

		confidence := 1.0 - estCost*10

		candidates = append(candidates, RoutingCandidate{
			Provider:   info.Provider,
			Model:      model,
			Reason:     fmt.Sprintf("complexity=%.2f, cost=$%.4f", float64(complexity), estCost),
			EstCost:    estCost,
			EstLatency: 2 * time.Second,
			Confidence: maxf(0.1, minf(1.0, confidence)),
		})
	}

	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].EstCost < candidates[i].EstCost {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	return candidates
}

func (r *ModelRouter) getModelsForComplexity(c Complexity) []string {
	switch {
	case c <= 0.3:
		return []string{"gpt-4o-mini", "claude-haiku-3.5", "gemini-2.0-flash"}
	case c <= 0.6:
		return []string{"claude-sonnet-4-20250514", "gpt-4o", "gemini-2.5-pro"}
	case c <= 0.85:
		return []string{"claude-opus-4", "gpt-4.5", "deepseek-r1"}
	default:
		return []string{"claude-opus-4", "gpt-4.5"}
	}
}

// ExecuteWithFailover tries the primary provider then falls back.
func (r *ModelRouter) ExecuteWithFailover(ctx context.Context, task *Task) (*ChatResponse, error) {
	decision, err := r.Route(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}

	r.mu.RLock()
	provider, ok := r.providers[decision.Provider]
	r.mu.RUnlock()

	if ok {
		req := &ChatRequest{
			Model:     decision.Model,
			Messages:  task.Messages,
			MaxTokens: 4096,
		}
		resp, err := provider.Chat(ctx, req)
		if err == nil {
			return resp, nil
		}
		r.healthMonitor.RecordFailure(decision.Provider)
	}

	for _, fb := range decision.Fallbacks {
		r.mu.RLock()
		provider, ok = r.providers[fb.Provider]
		r.mu.RUnlock()

		if ok {
			req := &ChatRequest{
				Model:     fb.Model,
				Messages:  task.Messages,
				MaxTokens: 4096,
			}
			resp, err := provider.Chat(ctx, req)
			if err == nil {
				return resp, nil
			}
			r.healthMonitor.RecordFailure(fb.Provider)
		}
	}

	return nil, fmt.Errorf("all providers failed for task %s", task.ID)
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
