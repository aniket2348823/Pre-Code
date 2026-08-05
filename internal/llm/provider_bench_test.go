package llm

import (
	"context"
	"testing"
	"time"
)

func BenchmarkCacheKey(b *testing.B) {
	req := &ChatRequest{
		Model:    "gpt-4o",
		System:   "you are helpful",
		Messages: []Message{{Role: "user", Content: "hello world"}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CacheKey(req)
	}
}

func BenchmarkModelRouter_Route(b *testing.B) {
	router := NewModelRouter(&RouterConfig{
		DefaultModel:        "gpt-4o-mini",
		BudgetPerTask:       10.0,
		DefaultOutputTokens: 4096,
	})
	router.RegisterProvider("mock", &benchMockProvider{})
	router.SetPrices(map[string]ModelInfo{
		"gpt-4o": {
			Name:            "gpt-4o",
			Provider:        "mock",
			InputCostPer1K:  0.005,
			OutputCostPer1K: 0.015,
			MaxTokens:       128000,
			Capabilities:    []string{"tools", "vision"},
		},
	})

	task := &Task{
		ID:          "bench-1",
		Type:        "feature",
		Description: "benchmark task",
		Messages:    []Message{{Role: "user", Content: "write a function"}},
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = router.ExecuteWithFailover(ctx, task)
	}
}

func BenchmarkInMemoryCache_SetGet(b *testing.B) {
	cache := NewInMemoryCache(time.Minute)
	resp := &ChatResponse{
		Content:      "cached response",
		InputTokens:  100,
		OutputTokens: 50,
		Cost:         0.001,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set("key", resp)
		_, _ = cache.Get("key")
	}
}

func BenchmarkComplexityClassification(b *testing.B) {
	task := &Task{
		ID:          "bench-complex",
		Type:        "security",
		Description: "analyze this code for vulnerabilities and suggest fixes",
		Messages: []Message{
			{Role: "user", Content: "review this authentication code for security issues"},
		},
		RequiredCapabilities: []string{"tools", "vision"},
	}

	router := NewModelRouter(&RouterConfig{
		DefaultModel:        "gpt-4o-mini",
		BudgetPerTask:       10.0,
		DefaultOutputTokens: 4096,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = router.classifyComplexity(task)
	}
}

// benchMockProvider is a simple mock for benchmarking.
type benchMockProvider struct{}

func (m *benchMockProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{
		Content:      "mock response",
		Model:        req.Model,
		Provider:     "mock",
		InputTokens:  10,
		OutputTokens: 20,
		Cost:         0.001,
	}, nil
}

func (m *benchMockProvider) Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error) {
	ch := make(chan *ChatChunk, 1)
	ch <- &ChatChunk{Content: "mock", StopReason: "stop", Finish: true}
	close(ch)
	return ch, nil
}

func (m *benchMockProvider) HealthCheck(ctx context.Context) error { return nil }
func (m *benchMockProvider) Name() string                          { return "mock" }
