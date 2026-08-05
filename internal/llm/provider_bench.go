package llm

import (
	"fmt"
	"testing"
)

func BenchmarkComplexityClassification_Tasks(b *testing.B) {
	router := NewModelRouter(&RouterConfig{
		DefaultModel:        "gpt-4o-mini",
		BudgetPerTask:       10.0,
		DefaultOutputTokens: 4096,
	})

	tasks := []struct {
		name string
		task *Task
	}{
		{
			name: "simple_formatting",
			task: &Task{
				ID:           "bench-1",
				Type:         "formatting",
				Description:  "reformat code",
				FilesChanged: []string{"main.go"},
			},
		},
		{
			name: "bug_fix",
			task: &Task{
				ID:           "bench-2",
				Type:         "bug_fix",
				Description:  "fix null pointer in auth handler",
				FilesChanged: []string{"auth.go", "auth_test.go"},
			},
		},
		{
			name: "complex_security",
			task: &Task{
				ID:                "bench-3",
				Type:              "security",
				Description:       "audit and fix SQL injection vulnerabilities",
				FilesChanged:      []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go", "h.go", "i.go", "j.go", "k.go"},
				RequiresReasoning: true,
				IsNovel:           true,
				Tags:              []string{"security", "production"},
			},
		},
		{
			name: "architecture",
			task: &Task{
				ID:                "bench-4",
				Type:              "architecture",
				Description:       "design microservice decomposition",
				FilesChanged:      []string{"a.go", "b.go", "c.go"},
				RequiresReasoning: true,
			},
		},
		{
			name: "rename",
			task: &Task{
				ID:           "bench-5",
				Type:         "rename",
				Description:  "rename variable x to count",
				FilesChanged: []string{"main.go"},
			},
		},
	}

	for _, tt := range tasks {
		b.Run(tt.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = router.classifyComplexity(tt.task)
			}
		})
	}
}

func BenchmarkEstimateInputTokens(b *testing.B) {
	task := &Task{
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant with extensive knowledge of Go programming."},
			{Role: "user", Content: "Please review this code for potential issues and suggest improvements."},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		estimateInputTokens(task)
	}
}

func BenchmarkEstimateInputTokens_Large(b *testing.B) {
	msgs := make([]Message, 50)
	for i := range msgs {
		msgs[i] = Message{
			Role:    "user",
			Content: fmt.Sprintf("This is message number %d with some content that simulates a real conversation turn in a coding assistant.", i),
		}
	}
	task := &Task{Messages: msgs}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		estimateInputTokens(task)
	}
}

func BenchmarkCacheKey_Variants(b *testing.B) {
	b.Run("minimal", func(b *testing.B) {
		req := &ChatRequest{
			Model:    "gpt-4o",
			Messages: []Message{{Role: "user", Content: "hi"}},
		}
		for i := 0; i < b.N; i++ {
			_ = CacheKey(req)
		}
	})

	b.Run("with_tools", func(b *testing.B) {
		req := &ChatRequest{
			Model:    "gpt-4o",
			System:   "you are helpful",
			Messages: []Message{{Role: "user", Content: "hello world"}},
			Tools: []ToolDef{
				{Name: "search", Description: "Search the web"},
			},
		}
		for i := 0; i < b.N; i++ {
			_ = CacheKey(req)
		}
	})

	b.Run("large_messages", func(b *testing.B) {
		msgs := make([]Message, 20)
		for i := range msgs {
			msgs[i] = Message{
				Role:    "user",
				Content: fmt.Sprintf("Message %d with substantial content for cache key computation", i),
			}
		}
		req := &ChatRequest{
			Model:       "claude-opus-4",
			System:      "You are a coding assistant with deep expertise in distributed systems.",
			Messages:    msgs,
			MaxTokens:   8192,
			Temperature: 0.7,
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = CacheKey(req)
		}
	})
}

func BenchmarkInMemoryCache_Miss(b *testing.B) {
	cache := NewInMemoryCache(0) // zero TTL = always expired
	for i := 0; i < b.N; i++ {
		_, _ = cache.Get("nonexistent-key")
	}
}

func BenchmarkInMemoryCache_Concurrent(b *testing.B) {
	cache := NewInMemoryCache(0)
	resp := &ChatResponse{Content: "cached", Cost: 0.001}
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i%100)
			if i%3 == 0 {
				cache.Set(key, resp)
			} else {
				_, _ = cache.Get(key)
			}
			i++
		}
	})
}

func BenchmarkInMemoryCache_Stats(b *testing.B) {
	cache := NewInMemoryCache(0)
	resp := &ChatResponse{Content: "cached", Cost: 0.001}
	for i := 0; i < 100; i++ {
		cache.Set(fmt.Sprintf("key-%d", i), resp)
	}
	for i := 0; i < 50; i++ {
		_, _ = cache.Get(fmt.Sprintf("key-%d", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.Stats()
	}
}

func BenchmarkLookupPrice_Variants(b *testing.B) {
	models := []string{"gpt-4o", "claude-opus-4", "gpt-4o-mini", "unknown-model"}
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = LookupPrice(models[i%len(models)])
			i++
		}
	})
}

func BenchmarkAllPrices_Variant(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = AllPrices()
	}
}

func BenchmarkSystemPrompt_Variants(b *testing.B) {
	tasks := []struct {
		name string
		task *Task
	}{
		{"bug_fix", &Task{Type: "bug_fix", Description: "fix null pointer", FilesChanged: []string{"a.go"}}},
		{"feature", &Task{Type: "feature", Description: "add new endpoint", FilesChanged: []string{"a.go", "b.go"}, Tags: []string{"api"}}},
		{"security", &Task{Type: "security", Description: "fix SQL injection", Tags: []string{"security", "production"}}},
		{"unknown", &Task{Type: "unknown_type", Description: "do something"}},
	}

	for _, tt := range tasks {
		b.Run(tt.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = systemPrompt(tt.task)
			}
		})
	}
}

func BenchmarkSupportsAll_Variants(b *testing.B) {
	tests := []struct {
		name     string
		info     ModelInfo
		required []string
	}{
		{
			name:     "all_supported",
			info:     ModelInfo{Capabilities: []string{"tools", "vision", "reasoning"}},
			required: []string{"tools", "vision"},
		},
		{
			name:     "missing_cap",
			info:     ModelInfo{Capabilities: []string{"tools"}},
			required: []string{"tools", "vision"},
		},
		{
			name:     "empty_required",
			info:     ModelInfo{Capabilities: []string{"tools"}},
			required: []string{},
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = supportsAll(tt.info, tt.required)
			}
		})
	}
}

func BenchmarkGetModelsForComplexity_Variants(b *testing.B) {
	router := NewModelRouter(nil)
	complexities := []Complexity{
		ComplexitySimple,
		ComplexityModerate,
		ComplexityComplex,
		ComplexityCritical,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = router.getModelsForComplexity(complexities[i%4])
	}
}
