// Package costintel provides cost intelligence for LLM operations.
// It tracks pricing across providers, forecasts costs, detects anomalies,
// and provides optimization recommendations to reduce spend.
package costintel

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// ModelPricing defines the cost per token for a model.
type ModelPricing struct {
	Model         string  `json:"model"`
	Provider      string  `json:"provider"`
	InputPer1K    float64 `json:"input_per_1k"`    // cost per 1K input tokens
	OutputPer1K   float64 `json:"output_per_1k"`   // cost per 1K output tokens
	CachedPer1K   float64 `json:"cached_per_1k"`   // cost per 1K cached tokens
	ContextWindow int     `json:"context_window"`   // max context tokens
	RateLimit     int     `json:"rate_limit"`       // requests per minute
}

// CostRecord tracks a single LLM API call cost.
type CostRecord struct {
	ID           string    `json:"id"`
	Model        string    `json:"model"`
	Provider     string    `json:"provider"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	CachedTokens int       `json:"cached_tokens"`
	CostUSD      float64   `json:"cost_usd"`
	TaskType     string    `json:"task_type"`
	Success      bool      `json:"success"`
	DurationMs   float64   `json:"duration_ms"`
	CreatedAt    time.Time `json:"created_at"`
}

// Budget tracks spending against a budget limit.
type Budget struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	LimitUSD      float64   `json:"limit_usd"`
	SpentUSD      float64   `json:"spent_usd"`
	Period        string    `json:"period"` // daily, weekly, monthly
	ResetAt       time.Time `json:"reset_at"`
	AlertAt       float64   `json:"alert_at"` // alert when spend reaches this % of limit
}

// CostForecast predicts future spending based on historical data.
type CostForecast struct {
	PeriodDays     int     `json:"period_days"`
	PredictedCost  float64 `json:"predicted_cost"`
	Confidence     float64 `json:"confidence"`
	TrendDirection string  `json:"trend_direction"` // increasing, decreasing, stable
	TrendPercent   float64 `json:"trend_percent"`
}

// OptimizationRecommendation suggests cost reduction strategies.
type OptimizationRecommendation struct {
	Category    string  `json:"category"`    // routing, caching, model_selection, batching
	Title       string  `json:"title"`
	Description string  `json:"description"`
	SavingsUSD  float64 `json:"savings_usd"` // estimated monthly savings
	SavingsPct  float64 `json:"savings_pct"` // estimated savings percentage
	Priority    string  `json:"priority"`    // high, medium, low
}

// CostAnomaly represents an unusual cost spike.
type CostAnomaly struct {
	RecordID    string  `json:"record_id"`
	Model       string  `json:"model"`
	CostUSD     float64 `json:"cost_usd"`
	ExpectedUSD float64 `json:"expected_usd"`
	Deviation   float64 `json:"deviation"` // standard deviations from mean
	Severity    string  `json:"severity"`  // warning, critical
	CreatedAt   time.Time `json:"created_at"`
}

// Engine provides cost intelligence operations.
type Engine struct {
	mu       sync.RWMutex
	pricing  map[string]*ModelPricing // model -> pricing
	records  []CostRecord
	budgets  map[string]*Budget
	anomalies []CostAnomaly
}

// NewEngine creates a cost intelligence engine with default pricing.
func NewEngine() *Engine {
	e := &Engine{
		pricing: make(map[string]*ModelPricing),
		budgets: make(map[string]*Budget),
	}
	e.loadDefaultPricing()
	return e
}

// loadDefaultPricing populates standard model pricing.
func (e *Engine) loadDefaultPricing() {
	defaults := []*ModelPricing{
		{Model: "gpt-4o", Provider: "openai", InputPer1K: 0.0025, OutputPer1K: 0.01, ContextWindow: 128000, RateLimit: 500},
		{Model: "gpt-4o-mini", Provider: "openai", InputPer1K: 0.00015, OutputPer1K: 0.0006, ContextWindow: 128000, RateLimit: 500},
		{Model: "claude-sonnet-4-20250514", Provider: "anthropic", InputPer1K: 0.003, OutputPer1K: 0.015, ContextWindow: 200000, RateLimit: 400},
		{Model: "claude-haiku-3.5", Provider: "anthropic", InputPer1K: 0.0008, OutputPer1K: 0.004, ContextWindow: 200000, RateLimit: 400},
		{Model: "claude-opus-4-20250514", Provider: "anthropic", InputPer1K: 0.015, OutputPer1K: 0.075, ContextWindow: 200000, RateLimit: 400},
	}
	for _, p := range defaults {
		e.pricing[p.Model] = p
	}
}

// SetPricing adds or updates model pricing.
func (e *Engine) SetPricing(p *ModelPricing) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pricing[p.Model] = p
}

// GetPricing returns pricing for a model.
func (e *Engine) GetPricing(model string) *ModelPricing {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p, ok := e.pricing[model]
	if !ok {
		return nil
	}
	cp := *p
	return &cp
}

// CalculateCost computes the cost for an API call.
func (e *Engine) CalculateCost(model string, inputTokens, outputTokens, cachedTokens int) float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p, ok := e.pricing[model]
	if !ok {
		return 0
	}
	inputCost := float64(inputTokens) / 1000.0 * p.InputPer1K
	outputCost := float64(outputTokens) / 1000.0 * p.OutputPer1K
	cachedCost := float64(cachedTokens) / 1000.0 * p.CachedPer1K
	return inputCost + outputCost + cachedCost
}

// RecordCost records a single API call cost.
func (e *Engine) RecordCost(r CostRecord) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	e.records = append(e.records, r)

	// Check budgets and auto-reset if period expired
	now := time.Now()
	for _, b := range e.budgets {
		if now.After(b.ResetAt) {
			b.SpentUSD = 0
			switch b.Period {
			case "daily":
				b.ResetAt = now.AddDate(0, 0, 1)
			case "weekly":
				b.ResetAt = now.AddDate(0, 0, 7)
			case "monthly":
				b.ResetAt = now.AddDate(0, 1, 0)
			default:
				b.ResetAt = now.AddDate(0, 1, 0)
			}
		}
		b.SpentUSD += r.CostUSD
	}

	// Check for anomalies
	e.detectAnomaly(r)
}

// detectAnomaly checks if a record is anomalously expensive.
func (e *Engine) detectAnomaly(r CostRecord) {
	if len(e.records) < 10 {
		return // need minimum data
	}

	// Compute mean and stddev for this model
	var sum, sumSq float64
	var count int
	for _, rec := range e.records {
		if rec.Model == r.Model {
			sum += rec.CostUSD
			sumSq += rec.CostUSD * rec.CostUSD
			count++
		}
	}
	if count < 5 {
		return
	}
	mean := sum / float64(count)
	variance := sumSq/float64(count) - mean*mean
	stddev := math.Sqrt(math.Max(0, variance))

	if stddev == 0 {
		return
	}
	deviation := (r.CostUSD - mean) / stddev

	if deviation > 3.0 {
		severity := "warning"
		if deviation > 4.0 {
			severity = "critical"
		}
		e.anomalies = append(e.anomalies, CostAnomaly{
			RecordID:    r.ID,
			Model:       r.Model,
			CostUSD:     r.CostUSD,
			ExpectedUSD: mean,
			Deviation:   deviation,
			Severity:    severity,
			CreatedAt:   r.CreatedAt,
		})
	}
}

// SetBudget creates or updates a spending budget.
func (e *Engine) SetBudget(b *Budget) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if b.AlertAt == 0 {
		b.AlertAt = 0.8 // default 80%
	}
	e.budgets[b.ID] = b
}

// CheckBudget returns budget status and whether alert threshold is exceeded.
func (e *Engine) CheckBudget(budgetID string) (*Budget, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	b, ok := e.budgets[budgetID]
	if !ok {
		return nil, false
	}
	cp := *b
	alertTriggered := b.SpentUSD >= b.LimitUSD*b.AlertAt
	return &cp, alertTriggered
}

// ForecastCost predicts spending over the next N days.
func (e *Engine) ForecastCost(days int) *CostForecast {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.records) == 0 {
		return &CostForecast{PeriodDays: days, Confidence: 0}
	}

	// Group records by day
	type dayCost struct {
		date  time.Time
		cost  float64
		count int
	}
	dailyMap := make(map[string]*dayCost)
	for _, r := range e.records {
		key := r.CreatedAt.Format("2006-01-02")
		dc, ok := dailyMap[key]
		if !ok {
			dc = &dayCost{date: r.CreatedAt.Truncate(24 * time.Hour)}
			dailyMap[key] = dc
		}
		dc.cost += r.CostUSD
		dc.count++
	}

	dailyCosts := make([]float64, 0, len(dailyMap))
	for _, dc := range dailyMap {
		dailyCosts = append(dailyCosts, dc.cost)
	}
	sort.Float64s(dailyCosts)

	// Simple linear trend
	n := float64(len(dailyCosts))
	var sumX, sumY, sumXY, sumX2 float64
	for i, c := range dailyCosts {
		x := float64(i)
		sumX += x
		sumY += c
		sumXY += x * c
		sumX2 += x * x
	}

	var slope, mean float64
	if n*sumX2-sumX*sumX != 0 {
		slope = (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)
		mean = sumY / n
	}

	trendPct := 0.0
	trendDir := "stable"
	if mean > 0 {
		trendPct = slope / mean * 100
		if trendPct > 5 {
			trendDir = "increasing"
		} else if trendPct < -5 {
			trendDir = "decreasing"
		}
	}

	predicted := mean * float64(days)
	if len(dailyCosts) > 1 {
		predicted += slope * float64(days) * float64(days-1) / 2
	}

	confidence := math.Min(1.0, n/30.0) // full confidence after 30 days

	return &CostForecast{
		PeriodDays:     days,
		PredictedCost:  math.Max(0, predicted),
		Confidence:     confidence,
		TrendDirection: trendDir,
		TrendPercent:   trendPct,
	}
}

// GetRecommendations analyzes spending patterns and suggests optimizations.
func (e *Engine) GetRecommendations() []OptimizationRecommendation {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var recs []OptimizationRecommendation

	// Analyze model usage
	modelCosts := make(map[string]float64)
	modelCounts := make(map[string]int)
	for _, r := range e.records {
		modelCosts[r.Model] += r.CostUSD
		modelCounts[r.Model]++
	}

	// Check if expensive models are used for simple tasks
	for model, cost := range modelCosts {
		if pricing, ok := e.pricing[model]; ok {
			// If this model costs more than 2x the cheapest, suggest optimization
			cheapest := e.cheapestModel()
			if cheapest != nil && pricing.InputPer1K > cheapest.InputPer1K*2 {
				estimatedSavings := cost * 0.3 // estimate 30% savings
				recs = append(recs, OptimizationRecommendation{
					Category:    "model_selection",
					Title:       fmt.Sprintf("Consider routing simple tasks to %s", cheapest.Model),
					Description: fmt.Sprintf("Model %s is used %d times ($%.2f). Using %s for simple tasks could save ~30%%.", model, modelCounts[model], cost, cheapest.Model),
					SavingsUSD:  estimatedSavings,
					SavingsPct:  30,
					Priority:    "high",
				})
			}
		}
	}

	// Check for high output token usage (potential over-generation)
	var totalInput, totalOutput float64
	for _, r := range e.records {
		totalInput += float64(r.InputTokens)
		totalOutput += float64(r.OutputTokens)
	}
	if totalOutput > totalInput*2 && len(e.records) > 20 {
		recs = append(recs, OptimizationRecommendation{
			Category:    "batching",
			Title:       "Batch similar requests to reduce input token duplication",
			Description: "Output tokens significantly exceed input tokens, suggesting many small requests. Batching could reduce total token usage.",
			SavingsUSD:  totalInput * 0.001 / 1000 * 0.15, // estimate 10% savings on input
			SavingsPct:  10,
			Priority:    "medium",
		})
	}

	// Cache optimization
	var totalCached int
	for _, r := range e.records {
		totalCached += r.CachedTokens
	}
	totalInputInt := int(totalInput)
	if totalInputInt > 0 && totalCached < totalInputInt/4 {
		recs = append(recs, OptimizationRecommendation{
			Category:    "caching",
			Title:       "Increase prompt caching utilization",
			Description: fmt.Sprintf("Only %.1f%% of input tokens are cached. Increasing cache hit rate to 50%% could significantly reduce costs.", float64(totalCached)/float64(totalInputInt)*100),
			SavingsUSD:  totalInput * 0.001 / 1000 * 0.2,
			SavingsPct:  20,
			Priority:    "medium",
		})
	}

	// Sort by savings descending
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].SavingsUSD > recs[j].SavingsUSD
	})

	return recs
}

// cheapestModel returns the model with the lowest input cost.
func (e *Engine) cheapestModel() *ModelPricing {
	var cheapest *ModelPricing
	for _, p := range e.pricing {
		if cheapest == nil || p.InputPer1K < cheapest.InputPer1K {
			cheapest = p
		}
	}
	if cheapest != nil {
		cp := *cheapest
		return &cp
	}
	return nil
}

// TotalCost returns the total cost across all records.
func (e *Engine) TotalCost() float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var total float64
	for _, r := range e.records {
		total += r.CostUSD
	}
	return total
}

// TotalRecords returns the number of recorded API calls.
func (e *Engine) TotalRecords() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.records)
}

// GetAnomalies returns detected cost anomalies.
func (e *Engine) GetAnomalies() []CostAnomaly {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]CostAnomaly, len(e.anomalies))
	copy(out, e.anomalies)
	return out
}

// CostByModel returns cost breakdown by model.
func (e *Engine) CostByModel() map[string]float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make(map[string]float64)
	for _, r := range e.records {
		result[r.Model] += r.CostUSD
	}
	return result
}

// CostByTaskType returns cost breakdown by task type.
func (e *Engine) CostByTaskType() map[string]float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make(map[string]float64)
	for _, r := range e.records {
		result[r.TaskType] += r.CostUSD
	}
	return result
}
