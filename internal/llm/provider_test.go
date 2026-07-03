package llm

import (
	"context"
	"testing"
	"time"
)

func TestPriceTable(t *testing.T) {
	if len(PriceTable) == 0 {
		t.Fatal("price table is empty")
	}

	knownModels := []string{"gpt-4o", "gpt-4o-mini", "claude-sonnet-4-20250514", "claude-haiku-3.5"}
	for _, model := range knownModels {
		info, ok := PriceTable[model]
		if !ok {
			t.Errorf("model %s not found in price table", model)
			continue
		}
		if info.InputCostPer1K <= 0 || info.OutputCostPer1K <= 0 {
			t.Errorf("model %s has invalid pricing: input=%f output=%f", model, info.InputCostPer1K, info.OutputCostPer1K)
		}
	}
}

func TestClassifyComplexity(t *testing.T) {
	router := NewModelRouter(nil)

	tests := []struct {
		name      string
		task      *Task
		minScore  float64
		maxScore  float64
	}{
		{
			name:     "formatting is very low complexity",
			task:     &Task{ID: "t1", Type: "formatting", Tags: []string{}},
			minScore: 0.0,
			maxScore: 0.3,
		},
		{
			name:     "bug_fix is moderate",
			task:     &Task{ID: "t2", Type: "bug_fix", Tags: []string{}},
			minScore: 0.2,
			maxScore: 0.5,
		},
		{
			name:     "architecture with security tag and reasoning is high",
			task:     &Task{ID: "t3", Type: "architecture", Tags: []string{"security"}, RequiresReasoning: true},
			minScore: 0.7,
			maxScore: 1.0,
		},
		{
			name:     "rename is very low complexity",
			task:     &Task{ID: "t4", Type: "rename", Tags: []string{}},
			minScore: 0.0,
			maxScore: 0.3,
		},
		{
			name:     "feature with many files is moderate-high",
			task:     &Task{ID: "t5", Type: "feature", FilesChanged: make([]string, 8), Tags: []string{}},
			minScore: 0.4,
			maxScore: 0.9,
		},
		{
			name:     "unknown type is low",
			task:     &Task{ID: "t6", Type: "unknown", Tags: []string{}},
			minScore: 0.0,
			maxScore: 0.2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := router.classifyComplexity(tt.task)
			score := float64(result)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("classifyComplexity() = %f, want between %f and %f", score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestRankCandidates(t *testing.T) {
	healthMonitor := NewHealthMonitor()
	healthMonitor.RecordSuccess("openai", 100*time.Millisecond)
	healthMonitor.RecordSuccess("anthropic", 100*time.Millisecond)
	healthMonitor.RecordSuccess("google", 100*time.Millisecond)

	router := &ModelRouter{
		providers:     make(map[string]Provider),
		healthMonitor: healthMonitor,
		config:        &RouterConfig{DefaultModel: "gpt-4o", BudgetPerTask: 1.0, DefaultOutputTokens: 500},
		prices:        PriceTable,
	}

	task := &Task{Messages: []Message{{Role: "user", Content: "rename a variable"}}}
	candidates := router.rankCandidates(task, []string{"openai", "anthropic", "google"}, ComplexitySimple)
	if len(candidates) == 0 {
		t.Fatal("rankCandidates returned no candidates for simple complexity")
	}

	// Verify candidates are sorted by cost ascending
	for i := 1; i < len(candidates); i++ {
		if candidates[i].EstCost < candidates[i-1].EstCost {
			t.Errorf("candidates not sorted: [%d]=%f > [%d]=%f",
				i-1, candidates[i-1].EstCost, i, candidates[i].EstCost)
		}
	}

	complexCandidates := router.rankCandidates(task, []string{"openai", "anthropic"}, ComplexityComplex)
	if len(complexCandidates) == 0 {
		t.Fatal("rankCandidates returned no candidates for complex complexity")
	}

	for _, c := range complexCandidates {
		if c.EstCost <= 0 {
			t.Errorf("candidate %s has non-positive cost: %f", c.Model, c.EstCost)
		}
		if c.Provider == "" {
			t.Errorf("candidate %s has empty provider", c.Model)
		}
	}
}

func TestModelRouter_New(t *testing.T) {
	router := NewModelRouter(nil)
	if router == nil {
		t.Fatal("NewModelRouter returned nil")
	}
	if router.config == nil {
		t.Fatal("config is nil")
	}
	if router.config.DefaultModel != "claude-sonnet-4-20250514" {
		t.Errorf("default model = %q", router.config.DefaultModel)
	}
}

func TestModelRouter_RegisterProvider(t *testing.T) {
	router := NewModelRouter(nil)
	mock := &mockProvider{name: "test-provider"}
	router.RegisterProvider("test-provider", mock)

	provider, ok := router.providers["test-provider"]
	if !ok {
		t.Fatal("provider not registered")
	}
	if provider.Name() != "test-provider" {
		t.Errorf("provider.Name() = %q", provider.Name())
	}
}

func TestGetModelsForComplexity(t *testing.T) {
	router := NewModelRouter(nil)

	simpleModels := router.getModelsForComplexity(ComplexitySimple)
	if len(simpleModels) == 0 {
		t.Error("no models for simple complexity")
	}

	complexModels := router.getModelsForComplexity(ComplexityComplex)
	if len(complexModels) == 0 {
		t.Error("no models for complex complexity")
	}

	// Simple and complex should return different models
	for _, sm := range simpleModels {
		for _, cm := range complexModels {
			if sm == cm {
				t.Errorf("model %s in both simple and complex", sm)
			}
		}
	}
}

type mockProvider struct {
	name string
}

func (m *mockProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{Content: "mock", Model: req.Model, Provider: m.name}, nil
}
func (m *mockProvider) Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error) {
	ch := make(chan *ChatChunk, 1)
	ch <- &ChatChunk{Content: "mock", Finish: true}
	close(ch)
	return ch, nil
}
func (m *mockProvider) HealthCheck(ctx context.Context) error { return nil }
func (m *mockProvider) Name() string                          { return m.name }
