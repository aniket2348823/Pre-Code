// Package middleware implements the unified VigilAgent middleware pipeline
// that orchestrates all components: context building → caching → rate limiting →
// LLM routing → response generation → critique → skill extraction → memory update.
// This is the core orchestration layer that makes every LLM call better than
// raw API usage through the self-improving feedback loop.
package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/cache"
	"github.com/vigilagent/vigilagent/internal/contextbuilder"
	"github.com/vigilagent/vigilagent/internal/costintel"
	"github.com/vigilagent/vigilagent/internal/critic"
	"github.com/vigilagent/vigilagent/internal/extraction"
	"github.com/vigilagent/vigilagent/internal/feedback"
	"github.com/vigilagent/vigilagent/internal/health"
	"github.com/vigilagent/vigilagent/internal/llm"
	"github.com/vigilagent/vigilagent/internal/memory"
	"github.com/vigilagent/vigilagent/internal/observability"
	"github.com/vigilagent/vigilagent/internal/ratelimit"
	"github.com/vigilagent/vigilagent/internal/retry"
	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/security"
	"github.com/vigilagent/vigilagent/internal/util"
)

// Request is the input to the middleware pipeline.
type Request struct {
	ID          string                      `json:"id"`
	UserID      string                      `json:"user_id"`
	TaskType    string                      `json:"task_type"`
	Description string                      `json:"description"`
	Code        string                      `json:"code,omitempty"`
	Language    string                      `json:"language,omitempty"`
	Filename    string                      `json:"filename,omitempty"`
	Context     *contextbuilder.BuildRequest `json:"context,omitempty"`
	Budget      float64                     `json:"budget,omitempty"`
}

// Response is the output from the middleware pipeline.
type Response struct {
	ID            string                  `json:"id"`
	Content       string                  `json:"content"`
	Model         string                  `json:"model"`
	Provider      string                  `json:"provider"`
	Cost          float64                 `json:"cost"`
	TokensUsed    int                     `json:"tokens_used"`
	LatencyMs     float64                 `json:"latency_ms"`
	Critique      *critic.CritiqueResult  `json:"critique,omitempty"`
	SkillsMatched int                     `json:"skills_matched"`
	Retries       int                     `json:"retries"`
	Accepted      bool                    `json:"accepted"`
	Grade         string                  `json:"grade"`
	Cached        bool                    `json:"cached"`
	RateLimited   bool                    `json:"rate_limited"`
}

// PipelineConfig holds middleware pipeline configuration.
type PipelineConfig struct {
	CriticConfig     *critic.Config              `json:"critic_config"`
	ContextConfig    *contextbuilder.Config      `json:"context_config"`
	CacheConfig      *cache.Config               `json:"cache_config"`
	RetryPolicy      *retry.Policy               `json:"retry_policy"`
	EnableCritique   bool                        `json:"enable_critique"`
	EnableExtraction bool                        `json:"enable_extraction"`
	EnableMemory     bool                        `json:"enable_memory"`
	EnableCache      bool                        `json:"enable_cache"`
	EnableRateLimit  bool                        `json:"enable_rate_limit"`
	MaxRetries       int                         `json:"max_retries"`
}

// DefaultPipelineConfig returns a production-ready middleware configuration.
func DefaultPipelineConfig() *PipelineConfig {
	return &PipelineConfig{
		CriticConfig:     critic.DefaultConfig(),
		ContextConfig:    contextbuilder.DefaultConfig(),
		CacheConfig: &cache.Config{
			MaxSize:    10000,
			DefaultTTL: 24 * time.Hour,
			KeyPrefix:  "llm:",
		},
		RetryPolicy:      retryPolicy(),
		EnableCritique:   true,
		EnableExtraction: true,
		EnableMemory:     true,
		EnableCache:      true,
		EnableRateLimit:  true,
		MaxRetries:       3,
	}
}

func retryPolicy() *retry.Policy {
	p := retry.DefaultPolicy()
	return &p
}

// Pipeline is the unified VigilAgent middleware integrating all components.
type Pipeline struct {
	router        *llm.ModelRouter
	critic        *critic.Pipeline
	extraction    *extraction.Engine
	memory        *memory.Manager
	context       *contextbuilder.Builder
	scanner       *scanner.Engine
	feedback      *feedback.Engine
	responseCache *cache.ResponseCache
	rateLimiter   *ratelimit.Limiter
	costIntel     *costintel.Engine
	healthChecker *health.Health
	tracer        *observability.Tracer
	metrics       *observability.PerformanceMetrics
	config        *PipelineConfig
	retryPolicy   retry.Policy
	mu            sync.RWMutex
	// Metrics
	totalRequests    int
	totalRetries     int
	totalCost        float64
	skillsExtracted  int
	cacheHits        int
	rateLimitRejects int
}

// NewPipeline creates a new middleware pipeline integrating all components.
func NewPipeline(
	router *llm.ModelRouter,
	criticPipeline *critic.Pipeline,
	extractionEngine *extraction.Engine,
	memoryManager *memory.Manager,
	contextBuilder *contextbuilder.Builder,
	scannerEngine *scanner.Engine,
	feedbackEngine *feedback.Engine,
	cfg *PipelineConfig,
) *Pipeline {
	if cfg == nil {
		cfg = DefaultPipelineConfig()
	}
	if cfg.CacheConfig == nil {
		cfg.CacheConfig = &cache.Config{
			MaxSize:    10000,
			DefaultTTL: 24 * time.Hour,
			KeyPrefix:  "llm:",
		}
	}
	if cfg.RetryPolicy == nil {
		p := retry.DefaultPolicy()
		cfg.RetryPolicy = &p
	}
	p := &Pipeline{
		router:        router,
		critic:        criticPipeline,
		extraction:    extractionEngine,
		memory:        memoryManager,
		context:       contextBuilder,
		scanner:       scannerEngine,
		feedback:      feedbackEngine,
		responseCache: cache.NewResponseCache(*cfg.CacheConfig),
		rateLimiter:   ratelimit.NewLimiter(ratelimit.SlidingWindow, 100, time.Minute),
		costIntel:     costintel.NewEngine(),
		healthChecker: health.New(30 * time.Second),
		tracer:        observability.NewTracer(),
		metrics:       observability.NewPerformanceMetrics(),
		config:        cfg,
		retryPolicy:   *retryPolicy(),
	}
	p.retryPolicy = *cfg.RetryPolicy
	return p
}

// Process runs the full middleware pipeline.
func (p *Pipeline) Process(ctx context.Context, req *Request) (*Response, error) {
	// Trace the entire pipeline
	ctx, span := p.tracer.StartSpan(ctx, "middleware.process")
	defer p.tracer.EndSpan(span)
	observability.SetSpanAttr(span, "task_type", req.TaskType)
	observability.SetSpanAttr(span, "user_id", req.UserID)

	start := time.Now()
	slog.Info("middleware: processing request", "id", req.ID, "type", req.TaskType)

	// Step 0: Sanitize input
	req.Description = security.SanitizeInput(req.Description)
	if req.Filename != "" {
		req.Filename = security.SanitizeFilename(req.Filename)
	}

	// Step 1: Rate limiting check
	if p.config.EnableRateLimit {
		if !p.rateLimiter.AllowKey(req.UserID) {
			p.mu.Lock()
			p.rateLimitRejects++
			p.mu.Unlock()
			return &Response{
				ID:          req.ID,
				RateLimited: true,
				Accepted:    false,
				Grade:       "rate_limited",
			}, fmt.Errorf("rate limit exceeded for user %s", req.UserID)
		}
	}

	// Step 2: Check cache for identical request
	if p.config.EnableCache && req.Code != "" {
		cacheKey := cache.HashPrompt(req.TaskType, req.Description+req.Code)
		if cached := p.responseCache.GetByPrompt(req.TaskType, req.Description+req.Code); cached != nil {
			p.mu.Lock()
			p.cacheHits++
			p.mu.Unlock()
			slog.Info("middleware: cache hit", "key", cacheKey)
			return &Response{
				ID:         req.ID,
				Content:    cached.Response,
				Model:      cached.Model,
				Cost:       0, // cached = free
				Cached:     true,
				Accepted:   true,
				Grade:      "cached",
				LatencyMs:  float64(time.Since(start).Milliseconds()),
			}, nil
		}
	}

	// Step 3: Build enhanced context
	var enhancedPrompt string
	if p.context != nil && req.Context != nil {
		pc, err := p.context.BuildContext(ctx, req.Context)
		if err != nil {
			slog.Warn("middleware: context build failed, using raw prompt", "error", err)
			enhancedPrompt = req.Description
		} else {
			enhancedPrompt = p.context.BuildPrompt(pc, req.Description)
		}
	} else {
		enhancedPrompt = req.Description
	}

	// Step 4: Check skill patterns for instant matches
	if p.extraction != nil && req.Code != "" {
		matches := p.extraction.MatchPattern(req.Code, req.Language)
		if len(matches) > 0 && matches[0].Confidence > 0.8 {
			slog.Info("middleware: skill pattern matched",
				"pattern", matches[0].Pattern.Name,
				"confidence", matches[0].Confidence)
			// Record outcome for learning
			p.extraction.RecordOutcome(matches[0].Pattern.ID, true)
			return &Response{
				ID:            req.ID,
				Content:       matches[0].Pattern.Fix,
				SkillsMatched: len(matches),
				Accepted:      true,
				Grade:         "skill-match",
				LatencyMs:     float64(time.Since(start).Milliseconds()),
			}, nil
		}
	}

	// Step 5: Recall relevant memory
	if p.memory != nil && p.config.EnableMemory {
		memoryResults, err := p.memory.Recall(ctx, req.Description, 5)
		if err == nil && len(memoryResults) > 0 {
			slog.Info("middleware: recalled memory", "count", len(memoryResults))
			var memoryContext []contextbuilder.MemorySnippet
			for _, r := range memoryResults {
				memoryContext = append(memoryContext, contextbuilder.MemorySnippet{
					Type:    r.Type,
					Content: r.Content,
					Score:   r.Score,
				})
			}
			if req.Context == nil {
				req.Context = &contextbuilder.BuildRequest{}
			}
			req.Context.MemoryContext = memoryContext
			if p.context != nil {
				pc, _ := p.context.BuildContext(ctx, req.Context)
				enhancedPrompt = p.context.BuildPrompt(pc, req.Description)
			}
		}
	}

	// Step 6: Route to optimal model and generate response with retry
	response, err := p.generateWithRetry(ctx, req, enhancedPrompt)
	if err != nil {
		return nil, fmt.Errorf("middleware: generation failed: %w", err)
	}

	// Step 7: Critique the response (if enabled)
	if p.critic != nil && p.config.EnableCritique {
		p.critiqueAndRetry(ctx, req, response, enhancedPrompt)
	}

	// Step 7b: Record feedback outcome for learning loop
	if p.feedback != nil {			p.feedback.RecordOutcome(ctx, feedback.Outcome{
				ID:         fmt.Sprintf("fb-%s", req.ID),
				RequestID:  req.ID,
				UserID:     req.UserID,
			Accepted:   response.Accepted,
			Model:      response.Model,
			TaskType:   req.TaskType,
			Score:      p.critiqueScore(response.Critique),
			Cost:       response.Cost,
			TokensUsed: response.TokensUsed,
			DurationMs: response.LatencyMs,
		})
	}

	// Step 8: Cache the response
	if p.config.EnableCache && req.Code != "" {
		p.responseCache.Put(
			cache.HashPrompt(req.TaskType, req.Description+req.Code),
			response.Model,
			req.Description+req.Code,
			response.Content,
			response.TokensUsed,
			response.Cost,
			[]string{req.TaskType, req.Language},
		)
	}

	// Step 9: Extract vulnerability patterns (if code was scanned)
	if p.extraction != nil && p.config.EnableExtraction && req.Code != "" {
		p.scanAndExtract(ctx, req, response)
	}

	// Step 10: Record cost intelligence
	if p.costIntel != nil {
		p.costIntel.RecordCost(costintel.CostRecord{
			ID:           req.ID,
			Model:        response.Model,
			Provider:     response.Provider,
			CostUSD:      response.Cost,
			TaskType:     req.TaskType,
			Success:      response.Accepted,
			DurationMs:   response.LatencyMs,
		})
	}

	// Step 11: Store in memory for future recall
	if p.memory != nil && p.config.EnableMemory {
		p.storeMemory(ctx, req, response)
	}

	// Step 12: Record performance metrics
	response.LatencyMs = float64(time.Since(start).Milliseconds())
	p.metrics.RecordRequest(int64(response.LatencyMs), !response.Accepted)

	// Update pipeline metrics
	p.mu.Lock()
	p.totalRequests++
	p.totalRetries += response.Retries
	p.totalCost += response.Cost
	p.mu.Unlock()

	slog.Info("middleware: request complete",
		"id", req.ID,
		"model", response.Model,
		"cost", response.Cost,
		"grade", response.Grade,
		"cached", response.Cached,
		"latency_ms", response.LatencyMs,
	)

	return response, nil
}

// generateWithRetry handles LLM generation using the retry package.
// Uses retry.Execute for exponential backoff, jitter, and context cancellation.
func (p *Pipeline) generateWithRetry(ctx context.Context, req *Request, prompt string) (*Response, error) {
	var result *Response
	var lastErr error

	err := retry.Execute(ctx, p.retryPolicy, func(retryCtx context.Context) error {
		messages := []llm.Message{
			{Role: "user", Content: prompt},
		}
		task := &llm.Task{
			ID:           req.ID,
			Type:         req.TaskType,
			Description:  req.Description,
			FilesChanged: []string{req.Filename},
			Tags:         []string{req.Language},
			Messages:     messages,
		}
		resp, err := p.router.ExecuteWithFailover(retryCtx, task)
		if err != nil {
			lastErr = err
			slog.Warn("middleware: LLM call failed, retrying", "error", err)
			return err
		}
		result = &Response{
			ID:         req.ID,
			Content:    resp.Content,
			Model:      resp.Model,
			Provider:   resp.Provider,
			Cost:       resp.Cost,
			TokensUsed: resp.InputTokens + resp.OutputTokens,
			Accepted:   true,
			Grade:      "pending",
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("all retries exhausted: %w", lastErr)
	}
	return result, nil
}

// scanAndExtract runs the scanner and extracts patterns from findings.
func (p *Pipeline) scanAndExtract(ctx context.Context, req *Request, resp *Response) {
	if p.scanner == nil || req.Code == "" {
		return
	}
	_, span := p.tracer.StartSpan(ctx, "middleware.scan")
	defer p.tracer.EndSpan(span)

	report := p.scanner.Run(ctx, scanner.Input{
		Language: req.Language,
		Code:     req.Code,
		Filename: req.Filename,
	})
	observability.SetSpanAttr(span, "findings", fmt.Sprintf("%d", len(report.Findings)))

	if len(report.Findings) > 0 {
		patterns := p.extraction.ExtractFromFindings(report.Findings)
		p.mu.Lock()
		p.skillsExtracted += len(patterns)
		p.mu.Unlock()
		slog.Info("middleware: extracted patterns from scan",
			"findings", len(report.Findings),
			"patterns", len(patterns))
	}
}

// storeMemory stores the interaction in memory for future recall.
func (p *Pipeline) storeMemory(ctx context.Context, req *Request, resp *Response) {
	if p.memory == nil {
		return
	}
	title := fmt.Sprintf("%s: %s", req.TaskType, util.Truncate(req.Description, 100))
	content := fmt.Sprintf("Request: %s\nResponse: %s\nModel: %s\nGrade: %s",
		util.Truncate(req.Description, 500),
		util.Truncate(resp.Content, 500),
		resp.Model,
		resp.Grade,
	)
	importance := 0.5
	if resp.Grade == "A+" || resp.Grade == "A" {
		importance = 0.9
	} else if resp.Grade == "F" {
		importance = 0.3
	}
	err := p.memory.StoreEpisode(ctx, req.UserID, "middleware_request", title, content, importance)
	if err != nil {
		slog.Warn("middleware: failed to store memory", "error", err)
	}
}

// GetMetrics returns comprehensive pipeline metrics.
func (p *Pipeline) GetMetrics() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	cacheStats := p.responseCache.Stats()
	costStats := p.costIntel.CostByModel()
	perfSummary := p.metrics.Summary()

	return map[string]interface{}{
		"total_requests":    p.totalRequests,
		"total_retries":     p.totalRetries,
		"total_cost":        p.totalCost,
		"skills_extracted":  p.skillsExtracted,
		"cache_hits":        p.cacheHits,
		"rate_limit_rejects": p.rateLimitRejects,
		"cache_stats":       cacheStats,
		"cost_by_model":     costStats,
		"performance":       perfSummary,
		"health":            p.healthChecker.Summary(),
	}
}

// GetCache returns the response cache for external inspection.
func (p *Pipeline) GetCache() *cache.ResponseCache {
	return p.responseCache
}

// GetCostIntel returns the cost intelligence engine.
func (p *Pipeline) GetCostIntel() *costintel.Engine {
	return p.costIntel
}

// GetTracer returns the distributed tracer.
func (p *Pipeline) GetTracer() *observability.Tracer {
	return p.tracer
}

// GetMetricsCollector returns the performance metrics collector.
func (p *Pipeline) GetMetricsCollector() *observability.PerformanceMetrics {
	return p.metrics
}

// GetFeedback returns the feedback engine.
func (p *Pipeline) GetFeedback() *feedback.Engine {
	return p.feedback
}

// GetHealth returns the health checker.
func (p *Pipeline) GetHealth() *health.Health {
	return p.healthChecker
}

// critiqueAndRetry critiques the response and retries with feedback if rejected.
func (p *Pipeline) critiqueAndRetry(ctx context.Context, req *Request, resp *Response, enhancedPrompt string) {
	critique, critiqueErr := p.critic.Evaluate(ctx, req.Description, resp.Content, req.TaskType)
	if critiqueErr != nil {
		slog.Warn("middleware: critique failed", "error", critiqueErr)
		return
	}
	resp.Critique = critique
	resp.Grade = critique.Grade
	resp.Accepted = !critique.Reject

	// Retry with critique feedback if rejected
	if critique.Reject && resp.Retries < p.config.MaxRetries {
		slog.Info("middleware: response rejected, retrying with critique feedback",
			"score", critique.OverallScore, "grade", critique.Grade)
		feedbackPrompt := enhancedPrompt + "\n\n## Previous attempt was rejected. Feedback:\n" + critique.Feedback
		retryResp, retryErr := p.generateWithRetry(ctx, req, feedbackPrompt)
		if retryErr == nil {
			resp.Content = retryResp.Content
			resp.Model = retryResp.Model
			resp.Provider = retryResp.Provider
			resp.Cost = retryResp.Cost
			resp.TokensUsed = retryResp.TokensUsed
			resp.Retries++
		}
	}
}

// critiqueScore extracts the numeric score from a critique result.
func (p *Pipeline) critiqueScore(c *critic.CritiqueResult) float64 {
	if c == nil {
		return 0
	}
	return c.OverallScore
}

// String returns a debug representation.
func (p *Pipeline) String() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return fmt.Sprintf("Pipeline{requests=%d, retries=%d, cost=%.4f, skills=%d, cache_hits=%d}",
		p.totalRequests, p.totalRetries, p.totalCost, p.skillsExtracted, p.cacheHits)
}
