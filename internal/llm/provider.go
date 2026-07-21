package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// BudgetGuard gates and records spend per org/task. It is satisfied by
// cost.BudgetManager; the router depends only on this interface so the llm
// package need not import cost.
type BudgetGuard interface {
	CheckBudget(ctx context.Context, orgID, taskID string, proposedCost float64) error
	RecordCost(orgID, taskID string, cost float64)
}

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
	// Capabilities the model supports (e.g. "tools", "vision", "reasoning").
	// Used to filter out models that cannot satisfy a task's requirements.
	Capabilities []string
}

// Supports reports whether the model advertises the given capability.
func (m ModelInfo) Supports(capability string) bool {
	for _, c := range m.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// priceTableMu guards concurrent reads/writes to PriceTable.
var priceTableMu sync.RWMutex

// PriceTable maps model names to pricing. It is the built-in default; deployments
// can override prices at runtime via ModelRouter.SetPrices (see doc 06/07 — prices
// drift monthly and should not be pinned in code for production).
var PriceTable = map[string]ModelInfo{
	"claude-opus-4":            {Name: "claude-opus-4", Provider: "anthropic", InputCostPer1K: 0.015, OutputCostPer1K: 0.075, MaxTokens: 8192, Capabilities: []string{"tools", "vision", "reasoning"}},
	"claude-sonnet-4-20250514": {Name: "claude-sonnet-4-20250514", Provider: "anthropic", InputCostPer1K: 0.003, OutputCostPer1K: 0.015, MaxTokens: 8192, Capabilities: []string{"tools", "vision"}},
	"claude-haiku-3.5":         {Name: "claude-haiku-3.5", Provider: "anthropic", InputCostPer1K: 0.0008, OutputCostPer1K: 0.004, MaxTokens: 8192, Capabilities: []string{"tools"}},
	"gpt-4.5":                  {Name: "gpt-4.5", Provider: "openai", InputCostPer1K: 0.015, OutputCostPer1K: 0.06, MaxTokens: 16384, Capabilities: []string{"tools", "vision", "reasoning"}},
	"gpt-4o":                   {Name: "gpt-4o", Provider: "openai", InputCostPer1K: 0.0025, OutputCostPer1K: 0.01, MaxTokens: 16384, Capabilities: []string{"tools", "vision"}},
	"gpt-4o-mini":              {Name: "gpt-4o-mini", Provider: "openai", InputCostPer1K: 0.00015, OutputCostPer1K: 0.0006, MaxTokens: 16384, Capabilities: []string{"tools"}},
	"deepseek-r1":              {Name: "deepseek-r1", Provider: "deepseek", InputCostPer1K: 0.00055, OutputCostPer1K: 0.00219, MaxTokens: 8192, Capabilities: []string{"reasoning"}},
	// Gemini
	"gemini-2.5-pro":           {Name: "gemini-2.5-pro", Provider: "gemini", InputCostPer1K: 0.00125, OutputCostPer1K: 0.01, MaxTokens: 8192, Capabilities: []string{"tools", "vision", "reasoning"}},
	"gemini-2.0-flash":         {Name: "gemini-2.0-flash", Provider: "gemini", InputCostPer1K: 0.000075, OutputCostPer1K: 0.0003, MaxTokens: 8192, Capabilities: []string{"tools", "vision"}},
	// Mistral
	"mistral-large-latest":     {Name: "mistral-large-latest", Provider: "mistral", InputCostPer1K: 0.002, OutputCostPer1K: 0.006, MaxTokens: 8192, Capabilities: []string{"tools"}},
	"mistral-small-latest":     {Name: "mistral-small-latest", Provider: "mistral", InputCostPer1K: 0.001, OutputCostPer1K: 0.003, MaxTokens: 8192, Capabilities: []string{"tools"}},
	// Groq
	"llama-3.1-70b-versatile":  {Name: "llama-3.1-70b-versatile", Provider: "groq", InputCostPer1K: 0.00059, OutputCostPer1K: 0.00079, MaxTokens: 8192, Capabilities: []string{"tools"}},
	"llama-3.1-8b-instant":     {Name: "llama-3.1-8b-instant", Provider: "groq", InputCostPer1K: 0.00005, OutputCostPer1K: 0.00008, MaxTokens: 8192, Capabilities: []string{"tools"}},
	// NVIDIA NIM
	"nvidia/llama-3.1-405b-instruct": {Name: "nvidia/llama-3.1-405b-instruct", Provider: "nvidia_nim", InputCostPer1K: 0.003, OutputCostPer1K: 0.009, MaxTokens: 8192, Capabilities: []string{"tools", "reasoning"}},
	"nvidia/llama-3.1-70b-instruct":  {Name: "nvidia/llama-3.1-70b-instruct", Provider: "nvidia_nim", InputCostPer1K: 0.00088, OutputCostPer1K: 0.00088, MaxTokens: 8192, Capabilities: []string{"tools"}},
	// Cohere
	"command-r-plus":           {Name: "command-r-plus", Provider: "cohere", InputCostPer1K: 0.0015, OutputCostPer1K: 0.00225, MaxTokens: 8192, Capabilities: []string{"tools"}},
	"command-r":                {Name: "command-r", Provider: "cohere", InputCostPer1K: 0.00015, OutputCostPer1K: 0.00015, MaxTokens: 8192, Capabilities: []string{"tools"}},
}

// Task represents a task for model routing (canonical definition).
type Task struct {
	ID                   string
	OrgID                string
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
	prices        map[string]ModelInfo
	cache         ResponseCache
	budget        BudgetGuard
	mu            sync.RWMutex
}

// RouterConfig holds routing configuration.
type RouterConfig struct {
	DefaultModel  string
	BudgetPerTask float64
	// DefaultOutputTokens is the assumed completion length used for cost
	// estimation when the real output size is not yet known.
	DefaultOutputTokens int
}

// NewModelRouter creates a new model router.
func NewModelRouter(cfg *RouterConfig) *ModelRouter {
	r := &ModelRouter{
		providers:     make(map[string]Provider),
		healthMonitor: NewHealthMonitor(),
		config:        cfg,
		prices:        PriceTable, // default; override with SetPrices
	}
	if r.config == nil {
		r.config = &RouterConfig{DefaultModel: "claude-sonnet-4-20250514", BudgetPerTask: 1.00}
	}
	if r.config.DefaultOutputTokens == 0 {
		r.config.DefaultOutputTokens = 500
	}
	return r
}

// SetCache attaches a response cache so identical requests skip paid calls.
func (r *ModelRouter) SetCache(c ResponseCache) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = c
}

// SetBudgetGuard attaches budget enforcement to execution.
func (r *ModelRouter) SetBudgetGuard(b BudgetGuard) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.budget = b
}

// SetPrices overrides the model price table (e.g. loaded from config/DB).
func (r *ModelRouter) SetPrices(prices map[string]ModelInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(prices) > 0 {
		r.prices = prices
	}
}

// priceTable returns the router's active price table under read lock.
func (r *ModelRouter) priceTable() map[string]ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.prices
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
	candidates := r.rankCandidates(task, healthy, complexity)

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no healthy provider supports the task's requirements")
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

// estimateInputTokens approximates prompt size from the task's messages and
// system prompt using a ~4-characters-per-token heuristic, so cost estimates
// reflect the actual request rather than a fixed guess.
func estimateInputTokens(task *Task) int {
	chars := 0
	for _, m := range task.Messages {
		chars += len(m.Content)
	}
	tokens := chars / 4
	if tokens < 50 {
		tokens = 50 // floor: system prompt + framing always costs something
	}
	return tokens
}

// rankCandidates builds the ordered candidate list for a task: it considers only
// models in the complexity-appropriate tier, drops any that lack a healthy
// provider or a required capability, estimates cost from the real prompt size,
// scores confidence on provider health, and returns candidates cheapest-first.
func (r *ModelRouter) rankCandidates(task *Task, healthy []string, complexity Complexity) []RoutingCandidate {
	prices := r.priceTable()
	healthySet := make(map[string]struct{}, len(healthy))
	for _, p := range healthy {
		healthySet[p] = struct{}{}
	}

	estInput := estimateInputTokens(task)
	estOutput := r.config.DefaultOutputTokens

	var candidates []RoutingCandidate
	for _, model := range r.getModelsForComplexity(complexity) {
		info, ok := prices[model]
		if !ok {
			continue
		}
		// Provider must be healthy (registered by provider name).
		if _, healthyOK := healthySet[info.Provider]; !healthyOK {
			continue
		}
		// Model must support every capability the task requires.
		if !supportsAll(info, task.RequiredCapabilities) {
			continue
		}

		estCost := (float64(estInput)/1000.0)*info.InputCostPer1K +
			(float64(estOutput)/1000.0)*info.OutputCostPer1K

		candidates = append(candidates, RoutingCandidate{
			Provider:   info.Provider,
			Model:      model,
			Reason:     fmt.Sprintf("complexity=%.2f, est_in=%d tok, cost=$%.4f", float64(complexity), estInput, estCost),
			EstCost:    estCost,
			EstLatency: 2 * time.Second,
			Confidence: r.healthMonitor.Confidence(info.Provider),
		})
	}

	// Cheapest first; break ties by higher confidence.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].EstCost != candidates[j].EstCost {
			return candidates[i].EstCost < candidates[j].EstCost
		}
		return candidates[i].Confidence > candidates[j].Confidence
	})

	return candidates
}

// supportsAll reports whether the model satisfies every required capability.
func supportsAll(info ModelInfo, required []string) bool {
	for _, cap := range required {
		if !info.Supports(cap) {
			return false
		}
	}
	return true
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

// StartHealthChecks runs periodic health checks on all registered providers.
func (r *ModelRouter) StartHealthChecks(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.mu.RLock()
			names := make([]string, 0, len(r.providers))
			for name := range r.providers {
				names = append(names, name)
			}
			r.mu.RUnlock()
			for _, name := range names {
				go r.healthMonitor.CheckHealth(ctx, name)
			}
		}
	}
}

// GetHealthMonitor returns the health monitor for external inspection.
func (r *ModelRouter) GetHealthMonitor() *HealthMonitor {
	return r.healthMonitor
}

// ExecuteWithFailover routes a task, then attempts the chosen provider and each
// fallback in turn. For every attempt it serves from cache when possible, gates
// spend through the budget guard, caps output at the model's MaxTokens, and
// records actual cost on success.
func (r *ModelRouter) ExecuteWithFailover(ctx context.Context, task *Task) (*ChatResponse, error) {
	decision, err := r.Route(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}

	// Ordered attempts: primary then fallbacks.
	attempts := []FallbackOption{{Provider: decision.Provider, Model: decision.Model, EstCost: decision.EstCost}}
	attempts = append(attempts, decision.Fallbacks...)

	var lastErr error
	for _, a := range attempts {
		resp, err := r.attempt(ctx, task, a)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all providers failed for task %s: %w", task.ID, lastErr)
	}
	return nil, fmt.Errorf("all providers failed for task %s", task.ID)
}

// attempt runs a single (provider, model) try with cache, budget gating, and
// cost recording. A BudgetExceededError is returned without trying fallbacks-worth
// of spend, since the budget applies regardless of provider.
func (r *ModelRouter) attempt(ctx context.Context, task *Task, opt FallbackOption) (*ChatResponse, error) {
	r.mu.RLock()
	provider, ok := r.providers[opt.Provider]
	cache := r.cache
	budget := r.budget
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("provider %s not registered", opt.Provider)
	}

	req := &ChatRequest{
		Model:     opt.Model,
		Messages:  task.Messages,
		System:    systemPrompt(task),
		MaxTokens: r.maxTokensFor(opt.Model),
	}

	// Cache lookup: a hit costs nothing and skips the provider entirely.
	if cache != nil {
		key := CacheKey(req)
		if hit, found := cache.Get(key); found {
			return hit, nil
		}
	}

	// Budget gate before spending.
	if budget != nil {
		if err := budget.CheckBudget(ctx, task.OrgID, task.ID, opt.EstCost); err != nil {
			return nil, err // budget error: do not retry other providers
		}
	}

	resp, err := provider.Chat(ctx, req)
	if err != nil {
		r.healthMonitor.RecordFailure(opt.Provider)
		return nil, err
	}
	r.healthMonitor.RecordSuccess(opt.Provider, resp.Latency)

	if budget != nil {
		budget.RecordCost(task.OrgID, task.ID, resp.Cost)
	}
	if cache != nil {
		cache.Set(CacheKey(req), resp)
	}
	return resp, nil
}

// maxTokensFor returns the completion cap for a model from the price table,
// defaulting to 4096 when the model is unknown.
func (r *ModelRouter) maxTokensFor(model string) int {
	if info, ok := r.priceTable()[model]; ok && info.MaxTokens > 0 {
		return info.MaxTokens
	}
	return 4096
}

// systemPrompt returns a task's system prompt, if any is derivable. Kept as a
// hook so prompt construction lives in one place.
// LookupPrice returns pricing for a model from the global PriceTable, safe for
// concurrent use. Returns zero value and false if the model is not found.
func LookupPrice(model string) (ModelInfo, bool) {
	priceTableMu.RLock()
	defer priceTableMu.RUnlock()
	info, ok := PriceTable[model]
	return info, ok
}

// SetPrice updates or inserts a model's pricing in the global PriceTable, safe
// for concurrent use. Callers should also call ModelRouter.SetPrices to update
// the router's internal copy.
func SetPrice(model string, info ModelInfo) {
	priceTableMu.Lock()
	defer priceTableMu.Unlock()
	PriceTable[model] = info
}

// AllPrices returns a snapshot of the entire global PriceTable, safe for
// concurrent use. The returned map is a copy — mutations do not affect the
// original table.
func AllPrices() map[string]ModelInfo {
	priceTableMu.RLock()
	defer priceTableMu.RUnlock()
	copy := make(map[string]ModelInfo, len(PriceTable))
	for k, v := range PriceTable {
		copy[k] = v
	}
	return copy
}

// StreamResult holds the streaming channel plus metadata about which model
// was selected, enabling callers to track token usage and cost attribution.
type StreamResult struct {
	Ch       <-chan *ChatChunk
	Model    string
	Provider string
	EstInput int // estimated input tokens from routing
}

// StreamWithFailover routes a task, then streams tokens from the chosen provider
// (or fallbacks) via a channel. Returns a StreamResult containing the channel
// and routing metadata so callers can track token usage and cost.
func (r *ModelRouter) StreamWithFailover(ctx context.Context, task *Task) (*StreamResult, error) {
	decision, err := r.Route(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("routing failed: %w", err)
	}

	attempts := []FallbackOption{{Provider: decision.Provider, Model: decision.Model, EstCost: decision.EstCost}}
	attempts = append(attempts, decision.Fallbacks...)

	estInput := estimateInputTokens(task)
	for _, a := range attempts {
		ch, err := r.streamAttempt(ctx, task, a)
		if err == nil {
			return &StreamResult{
				Ch:       ch,
				Model:    a.Model,
				Provider: a.Provider,
				EstInput: estInput,
			}, nil
		}
	}

	return nil, fmt.Errorf("all providers failed to stream for task %s", task.ID)
}

// streamAttempt runs a single streaming try with budget gating and cost recording.
func (r *ModelRouter) streamAttempt(ctx context.Context, task *Task, opt FallbackOption) (<-chan *ChatChunk, error) {
	r.mu.RLock()
	provider, ok := r.providers[opt.Provider]
	budget := r.budget
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("provider %s not registered", opt.Provider)
	}

	req := &ChatRequest{
		Model:     opt.Model,
		Messages:  task.Messages,
		System:    systemPrompt(task),
		MaxTokens: r.maxTokensFor(opt.Model),
	}

	// Budget gate before spending.
	if budget != nil {
		if err := budget.CheckBudget(ctx, task.OrgID, task.ID, opt.EstCost); err != nil {
			return nil, err
		}
	}

	start := time.Now()
	rawCh, err := provider.Stream(ctx, req)
	if err != nil {
		r.healthMonitor.RecordFailure(opt.Provider)
		return nil, err
	}

	// Wrap the raw channel to collect content and record cost on finish.
	wrappedCh := make(chan *ChatChunk, 32)
	go func() {
		defer close(wrappedCh)
		var content strings.Builder
	finished := false
	drainLoop:
		for {
			select {
			case <-ctx.Done():
				break drainLoop
			case chunk, ok := <-rawCh:
				if !ok {
					break drainLoop
				}
				content.WriteString(chunk.Content)
				select {
				case wrappedCh <- chunk:
				default:
				}
				if chunk.Finish {
					finished = true
					break drainLoop
				}
			}
		}
		latency := time.Since(start)
		// Only record success if stream completed normally (not cancelled).
		if finished {
			r.healthMonitor.RecordSuccess(opt.Provider, latency)

			// Estimate cost from accumulated content (output tokens ~ chars/4).
			outputTokens := content.Len() / 4
			info, _ := LookupPrice(opt.Model)
			cost := (float64(opt.EstCost) / 2.0) + // rough input cost half of estimate
				(float64(outputTokens) / 1000.0 * info.OutputCostPer1K)
			if budget != nil {
				budget.RecordCost(task.OrgID, task.ID, cost)
			}
		} else {
			r.healthMonitor.RecordFailure(opt.Provider)
		}
	}()

	return wrappedCh, nil
}

func systemPrompt(task *Task) string {
	if task == nil {
		return ""
	}
	// Messages already carry the conversation; no separate system prompt yet.
	return ""
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
