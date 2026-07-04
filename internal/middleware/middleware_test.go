package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/cache"
	"github.com/vigilagent/vigilagent/internal/contextbuilder"
	"github.com/vigilagent/vigilagent/internal/extraction"
	"github.com/vigilagent/vigilagent/internal/feedback"
	"github.com/vigilagent/vigilagent/internal/ratelimit"
	"github.com/vigilagent/vigilagent/internal/retry"
)

func TestDefaultPipelineConfig(t *testing.T) {
	cfg := DefaultPipelineConfig()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if !cfg.EnableCritique {
		t.Error("expected critique enabled by default")
	}
	if !cfg.EnableCache {
		t.Error("expected cache enabled by default")
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("expected 3 max retries, got %d", cfg.MaxRetries)
	}
}

func TestNewPipelineNilConfig(t *testing.T) {
	p := NewPipeline(nil, nil, nil, nil, nil, nil, nil, nil)
	if p == nil {
		t.Fatal("expected non-nil pipeline with nil config")
	}
	if p.config == nil {
		t.Fatal("expected default config to be applied")
	}
}

func TestPipelineWithFeedback(t *testing.T) {
	fb := feedback.NewEngine(nil)
	p := NewPipeline(nil, nil, nil, nil, nil, nil, fb, nil)
	if p.GetFeedback() == nil {
		t.Fatal("expected feedback engine to be set")
	}
	if p.GetFeedback() != fb {
		t.Error("expected same feedback engine instance")
	}
}

func TestPipelineMetricsNilDeps(t *testing.T) {
	p := NewPipeline(nil, nil, nil, nil, nil, nil, nil, nil)
	metrics := p.GetMetrics()
	if metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
	if metrics["total_requests"] != 0 {
		t.Error("expected 0 total requests")
	}
}

func TestPipelineString(t *testing.T) {
	p := NewPipeline(nil, nil, nil, nil, nil, nil, nil, nil)
	s := p.String()
	if s == "" {
		t.Error("expected non-empty string")
	}
}

func TestPipelineRateLimit(t *testing.T) {
	p := NewPipeline(nil, nil, nil, nil, nil, nil, nil, nil)
	// Override with a tight rate limit
	p.rateLimiter = ratelimit.NewLimiter(ratelimit.SlidingWindow, 1, time.Minute)

	// First request should pass (rate limit = 1)
	// But it will fail at generation since router is nil
	// We test the rate limit check specifically
	if !p.rateLimiter.AllowKey("user-1") {
		t.Error("expected first request to be allowed")
	}
	if p.rateLimiter.AllowKey("user-1") {
		t.Error("expected second request to be rate limited")
	}
}

func TestPipelineCacheMiss(t *testing.T) {
	cfg := &PipelineConfig{
		CacheConfig: &cache.Config{
			MaxSize:    100,
			DefaultTTL: time.Hour,
			KeyPrefix:  "test:",
		},
		RetryPolicy:      retryPolicy(),
		EnableCritique:   false,
		EnableExtraction: false,
		EnableMemory:     false,
		EnableCache:      true,
		EnableRateLimit:  false,
		MaxRetries:       1,
	}
	p := NewPipeline(nil, nil, nil, nil, nil, nil, nil, cfg)

	// Cache should be empty
	key := cache.HashPrompt("test", "hello")
	if p.GetCache().Contains(key) {
		t.Error("expected cache miss for fresh pipeline")
	}
}

func TestPipelineGetters(t *testing.T) {
	extractEngine := extraction.NewEngine()
	p := NewPipeline(nil, nil, extractEngine, nil, nil, nil, nil, nil)

	if p.GetCostIntel() == nil {
		t.Error("expected cost intel engine")
	}
	if p.GetTracer() == nil {
		t.Error("expected tracer")
	}
	if p.GetMetricsCollector() == nil {
		t.Error("expected metrics collector")
	}
	if p.GetHealth() == nil {
		t.Error("expected health checker")
	}
}

func TestPipelineContextBuilderNil(t *testing.T) {
	p := NewPipeline(nil, nil, nil, nil, nil, nil, nil, nil)
	// When context builder is nil, enhanced prompt should be the raw description
	// Just verifying construction doesn't panic with nil dependencies
	_ = p
}

func TestPipelineExtractionMatch(t *testing.T) {
	extractEngine := extraction.NewEngine()
	_ = NewPipeline(nil, nil, extractEngine, nil, nil, nil, nil, nil)
	// With no patterns, match should return empty
	matches := extractEngine.MatchPattern("SELECT * FROM users", "sql")
	if len(matches) != 0 {
		t.Errorf("expected no matches, got %d", len(matches))
	}
}

func TestPipelineMemoryStoreSkipped(t *testing.T) {
	p := NewPipeline(nil, nil, nil, nil, nil, nil, nil, nil)
	// storeMemory with nil memory should be a no-op
	p.storeMemory(context.Background(), &Request{}, &Response{})
}

func TestCritiqueScoreNil(t *testing.T) {
	p := NewPipeline(nil, nil, nil, nil, nil, nil, nil, nil)
	score := p.critiqueScore(nil)
	if score != 0 {
		t.Errorf("expected 0 for nil critique, got %f", score)
	}
}

func TestPipelineDefaultRetryPolicy(t *testing.T) {
	p := NewPipeline(nil, nil, nil, nil, nil, nil, nil, nil)
	if p.retryPolicy.MaxRetries == 0 {
		t.Error("expected default retry policy to have max retries > 0")
	}
}

func TestPipelineCustomRetryPolicy(t *testing.T) {
	policy := retry.DefaultPolicy()
	policy.MaxRetries = 7
	cfg := &PipelineConfig{
		RetryPolicy: &policy,
		EnableCache: false,
	}
	p := NewPipeline(nil, nil, nil, nil, nil, nil, nil, cfg)
	if p.retryPolicy.MaxRetries != 7 {
		t.Errorf("expected custom max retries 7, got %d", p.retryPolicy.MaxRetries)
	}
}

func TestPipelineScanExtractNilDeps(t *testing.T) {
	p := NewPipeline(nil, nil, nil, nil, nil, nil, nil, nil)
	// Should not panic with nil scanner and extraction
	p.scanAndExtract(context.Background(), &Request{Code: "test"}, &Response{})
}

func TestFeedbackEngineIntegration(t *testing.T) {
	fb := feedback.NewEngine(nil)
	_ = NewPipeline(nil, nil, nil, nil, nil, nil, fb, nil)

	// Simulate recording an outcome
	fb.RecordOutcome(context.Background(), feedback.Outcome{
		ID:         "fb-test-1",
		RequestID:  "req-1",
		UserID:     "user-1",
		Accepted:   true,
		Model:      "gpt-4o",
		TaskType:   "code_generation",
		Score:      0.85,
		Cost:       0.02,
		TokensUsed: 1500,
	})

	if fb.TotalOutcomes() != 1 {
		t.Errorf("expected 1 outcome, got %d", fb.TotalOutcomes())
	}
	if fb.AcceptRate() != 1.0 {
		t.Errorf("expected 100%% accept rate, got %f", fb.AcceptRate())
	}
}

func TestContextBuilderDefaultConfig(t *testing.T) {
	cfg := contextbuilder.DefaultConfig()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}
