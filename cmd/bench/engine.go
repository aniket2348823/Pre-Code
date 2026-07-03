package main

import (
	"context"
	"fmt"

	"github.com/vigilagent/vigilagent/internal/llm"
)

// stubProvider satisfies llm.Provider so the router marks it healthy and Route
// returns candidates. Its Chat is never invoked during measurement — the engine
// reads recorded costs from the fixture, not from providers.
type stubProvider struct{ name string }

func (s stubProvider) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, fmt.Errorf("stub provider %s: Chat must not be called during replay", s.name)
}
func (s stubProvider) Stream(ctx context.Context, req *llm.ChatRequest) (<-chan *llm.ChatChunk, error) {
	return nil, fmt.Errorf("stub provider %s: Stream unsupported", s.name)
}
func (s stubProvider) HealthCheck(ctx context.Context) error { return nil }
func (s stubProvider) Name() string                          { return s.name }

// BuildRouter returns a router with the given providers registered healthy,
// so Route makes real selections without any live provider call.
func BuildRouter(providerNames []string) *llm.ModelRouter {
	r := llm.NewModelRouter(&llm.RouterConfig{
		DefaultModel:        "claude-opus-4",
		BudgetPerTask:       0,
		DefaultOutputTokens: 500,
	})
	for _, name := range providerNames {
		r.RegisterProvider(name, stubProvider{name: name})
	}
	return r
}

// taskToLLM maps a workload task to the router's Task; the router's real
// classifier derives complexity from these attributes.
func taskToLLM(wt WorkloadTask) *llm.Task {
	files := make([]string, wt.FilesChanged)
	return &llm.Task{
		ID:                wt.ID,
		Type:              wt.Type,
		Description:       wt.Prompt,
		FilesChanged:      files,
		RequiresReasoning: wt.RequiresReasoning,
		IsNovel:           wt.IsNovel,
		Tags:              wt.Tags,
		Messages:          []llm.Message{{Role: "user", Content: wt.Prompt}},
	}
}

// requestFor builds the ChatRequest for a model+task the same way the router's
// execution path would, so cache keys collide on identical repeated requests.
func requestFor(model string, wt WorkloadTask) *llm.ChatRequest {
	maxTok := 4096
	if info, ok := llm.PriceTable[model]; ok && info.MaxTokens > 0 {
		maxTok = info.MaxTokens
	}
	return &llm.ChatRequest{
		Model:     model,
		Messages:  []llm.Message{{Role: "user", Content: wt.Prompt}},
		System:    wt.System,
		MaxTokens: maxTok,
	}
}

// Measure runs the baseline and optimized passes over the workload sequence,
// tallying real recorded costs, and returns the split report.
func Measure(w *Workload, fx *Fixture, premiumModel string, router *llm.ModelRouter) (*Report, error) {
	ctx := context.Background()
	// A non-expiring cache for the duration of a single run, reusing the real
	// InMemoryCache + CacheKey logic the router ships with.
	cache := llm.NewInMemoryCache(1 << 62)
	rep := &Report{}

	for _, id := range w.Sequence {
		wt, ok := w.TaskByID(id)
		if !ok {
			return nil, fmt.Errorf("sequence references unknown task %q", id)
		}
		rep.Tasks++

		// Baseline: premium model, every occurrence pays.
		pe, ok := fx.Lookup(id, premiumModel)
		if !ok {
			return nil, fmt.Errorf("fixture missing entry for (%s, premium %s)", id, premiumModel)
		}
		rep.BaselineCost += pe.Cost

		// Optimized: route, then cache-check.
		decision, err := router.Route(ctx, taskToLLM(wt))
		if err != nil {
			return nil, fmt.Errorf("route %s: %w", id, err)
		}
		routed := decision.Model
		req := requestFor(routed, wt)
		key := llm.CacheKey(req)

		if _, hit := cache.Get(key); hit {
			rep.CacheHits++
			rep.CachePortion += pe.Cost // repeat valued at what baseline would pay
			continue
		}

		re, ok := fx.Lookup(id, routed)
		if !ok {
			return nil, fmt.Errorf("fixture missing entry for (%s, routed %s)", id, routed)
		}
		rep.OptimizedCost += re.Cost
		rep.RoutingPortion += pe.Cost - re.Cost
		cache.Set(key, &llm.ChatResponse{Content: re.Content, Cost: re.Cost, Model: routed})
	}

	rep.TotalSaved = rep.BaselineCost - rep.OptimizedCost
	if rep.BaselineCost > 0 {
		rep.PercentSaved = rep.TotalSaved / rep.BaselineCost * 100
	}
	return rep, nil
}
