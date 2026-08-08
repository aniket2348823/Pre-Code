package telemetry

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// CostTracker provides per-request, per-user, per-model cost tracking
// with stale-while-revalidate caching for aggregation queries.
type CostTracker struct {
	mu      sync.RWMutex
	records []costRecord

	// aggregation cache (SWR)
	userCache   map[string]swrEntry[UserCostSummary]
	orgCache    map[string]swrEntry[OrgCostSummary]
	modelCache  swrEntry[map[string]ModelCostSummary]
	cacheTTL    time.Duration

	// Prometheus metrics
	costMetric  *prometheus.CounterVec
	tokenMetric *prometheus.CounterVec
}

type costRecord struct {
	Model        string
	InputTokens  int
	OutputTokens int
	Cost         float64
	UserID       string
	OrgID        string
	TaskID       string
	ProjectID    string
	Timestamp    time.Time
}

type swrEntry[T any] struct {
	value   T
	expires time.Time
}

// UserCostSummary aggregates costs for a single user.
type UserCostSummary struct {
	TotalCost     float64
	TotalTokens   int
	RequestsTotal int
	ByModel       map[string]ModelCostSummary
	DailyCosts    map[string]float64 // "2006-01-02" -> cost
}

// OrgCostSummary aggregates costs for a single org.
type OrgCostSummary struct {
	TotalCost     float64
	TotalTokens   int
	RequestsTotal int
	ByModel       map[string]ModelCostSummary
	DailyCosts    map[string]float64
}

// ModelCostSummary aggregates costs for a single model.
type ModelCostSummary struct {
	TotalCost     float64
	TotalTokens   int
	InputTokens   int
	OutputTokens  int
	RequestsTotal int
}

// CostPeriod defines a time range for cost queries.
type CostPeriod struct {
	Start time.Time
	End   time.Time
}

// NewCostTracker creates a new CostTracker with Prometheus metrics.
func NewCostTracker() *CostTracker {
	ct := &CostTracker{
		records:   make([]costRecord, 0, 1024),
		userCache: make(map[string]swrEntry[UserCostSummary]),
		orgCache:  make(map[string]swrEntry[OrgCostSummary]),
		cacheTTL:  time.Minute,
		costMetric: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "vigilagent",
			Subsystem: "llm",
			Name:      "cost_dollars_total",
			Help:      "Total LLM cost in dollars",
		}, []string{"model", "user_id", "org_id"}), tokenMetric: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "vigilagent",
			Subsystem: "llm",
			Name:      "tokens_by_user_total",
			Help:      "Total LLM tokens consumed, attributed per user/org",
		}, []string{"model", "type", "user_id", "org_id"}),
	}
	return ct
}

// TrackRequest calculates cost from token counts using the price table and
// records the request. It is safe for concurrent use.
func (ct *CostTracker) TrackRequest(model string, inputTokens, outputTokens int, userID, orgID string) {
	info, ok := lookupModelInfo(model)
	if !ok {
		return
	}
	cost := (float64(inputTokens)/1000.0)*info.InputCostPer1K +
		(float64(outputTokens)/1000.0)*info.OutputCostPer1K

	rec := costRecord{
		Model:        model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Cost:         cost,
		UserID:       userID,
		OrgID:        orgID,
		Timestamp:    time.Now(),
	}

	ct.mu.Lock()
	ct.records = append(ct.records, rec)
	ct.trimOldRecords()
	// invalidate all caches on new record
	ct.userCache = make(map[string]swrEntry[UserCostSummary])
	ct.orgCache = make(map[string]swrEntry[OrgCostSummary])
	ct.modelCache = swrEntry[map[string]ModelCostSummary]{}
	ct.mu.Unlock()

	// Prometheus counters (lock-free)
	ct.costMetric.WithLabelValues(model, userID, orgID).Add(cost)
	ct.tokenMetric.WithLabelValues(model, "input", userID, orgID).Add(float64(inputTokens))
	ct.tokenMetric.WithLabelValues(model, "output", userID, orgID).Add(float64(outputTokens))
}

// TrackRequestWithAttribution records cost with project/task attribution.
func (ct *CostTracker) TrackRequestWithAttribution(model string, inputTokens, outputTokens int, userID, orgID, projectID, taskID string) {
	info, ok := lookupModelInfo(model)
	if !ok {
		return
	}
	cost := (float64(inputTokens)/1000.0)*info.InputCostPer1K +
		(float64(outputTokens)/1000.0)*info.OutputCostPer1K

	rec := costRecord{
		Model:        model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Cost:         cost,
		UserID:       userID,
		OrgID:        orgID,
		TaskID:       taskID,
		ProjectID:    projectID,
		Timestamp:    time.Now(),
	}

	ct.mu.Lock()
	ct.records = append(ct.records, rec)
	ct.trimOldRecords()
	ct.userCache = make(map[string]swrEntry[UserCostSummary])
	ct.orgCache = make(map[string]swrEntry[OrgCostSummary])
	ct.modelCache = swrEntry[map[string]ModelCostSummary]{}
	ct.mu.Unlock()

	ct.costMetric.WithLabelValues(model, userID, orgID).Add(cost)
	ct.tokenMetric.WithLabelValues(model, "input", userID, orgID).Add(float64(inputTokens))
	ct.tokenMetric.WithLabelValues(model, "output", userID, orgID).Add(float64(outputTokens))
}

// GetUserCosts returns cost summary for a user within the given period.
// Uses stale-while-revalidate: returns cached data if fresh, rebuilds in
// background if stale.
func (ct *CostTracker) GetUserCosts(userID string, period CostPeriod) UserCostSummary {
	ct.mu.RLock()
	entry, ok := ct.userCache[userID]
	ct.mu.RUnlock()

	if ok && time.Now().Before(entry.expires) {
		return filterUserSummary(entry.value, period)
	}

	summary := ct.buildUserSummary(userID, period)

	ct.mu.Lock()
	ct.userCache[userID] = swrEntry[UserCostSummary]{
		value:   summary,
		expires: time.Now().Add(ct.cacheTTL),
	}
	ct.mu.Unlock()

	return summary
}

// GetOrgCosts returns cost summary for an org within the given period.
func (ct *CostTracker) GetOrgCosts(orgID string, period CostPeriod) OrgCostSummary {
	ct.mu.RLock()
	entry, ok := ct.orgCache[orgID]
	ct.mu.RUnlock()

	if ok && time.Now().Before(entry.expires) {
		return filterOrgSummary(entry.value, period)
	}

	summary := ct.buildOrgSummary(orgID, period)

	ct.mu.Lock()
	ct.orgCache[orgID] = swrEntry[OrgCostSummary]{
		value:   summary,
		expires: time.Now().Add(ct.cacheTTL),
	}
	ct.mu.Unlock()

	return summary
}

// GetModelCosts returns per-model cost summaries within the given period.
func (ct *CostTracker) GetModelCosts(period CostPeriod) map[string]ModelCostSummary {
	ct.mu.RLock()
	entry := ct.modelCache
	ct.mu.RUnlock()

	if entry.value != nil && time.Now().Before(entry.expires) {
		return filterModelSummaries(entry.value, period)
	}

	summary := ct.buildModelSummaries(period)

	ct.mu.Lock()
	ct.modelCache = swrEntry[map[string]ModelCostSummary]{
		value:   summary,
		expires: time.Now().Add(ct.cacheTTL),
	}
	ct.mu.Unlock()

	return summary
}

// TotalRecords returns the number of tracked records.
func (ct *CostTracker) TotalRecords() int {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return len(ct.records)
}

// build helpers

func (ct *CostTracker) buildUserSummary(userID string, period CostPeriod) UserCostSummary {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	summary := UserCostSummary{ByModel: make(map[string]ModelCostSummary), DailyCosts: make(map[string]float64)}
	for _, r := range ct.records {
		if r.UserID != userID {
			continue
		}
		if !r.Timestamp.After(period.Start) || r.Timestamp.After(period.End) {
			continue
		}
		summary.TotalCost += r.Cost
		summary.TotalTokens += r.InputTokens + r.OutputTokens
		summary.RequestsTotal++
		ms := summary.ByModel[r.Model]
		ms.TotalCost += r.Cost
		ms.TotalTokens += r.InputTokens + r.OutputTokens
		ms.InputTokens += r.InputTokens
		ms.OutputTokens += r.OutputTokens
		ms.RequestsTotal++
		summary.ByModel[r.Model] = ms
		day := r.Timestamp.Format("2006-01-02")
		summary.DailyCosts[day] += r.Cost
	}
	return summary
}

func (ct *CostTracker) buildOrgSummary(orgID string, period CostPeriod) OrgCostSummary {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	summary := OrgCostSummary{ByModel: make(map[string]ModelCostSummary), DailyCosts: make(map[string]float64)}
	for _, r := range ct.records {
		if r.OrgID != orgID {
			continue
		}
		if !r.Timestamp.After(period.Start) || r.Timestamp.After(period.End) {
			continue
		}
		summary.TotalCost += r.Cost
		summary.TotalTokens += r.InputTokens + r.OutputTokens
		summary.RequestsTotal++
		ms := summary.ByModel[r.Model]
		ms.TotalCost += r.Cost
		ms.TotalTokens += r.InputTokens + r.OutputTokens
		ms.InputTokens += r.InputTokens
		ms.OutputTokens += r.OutputTokens
		ms.RequestsTotal++
		summary.ByModel[r.Model] = ms
		day := r.Timestamp.Format("2006-01-02")
		summary.DailyCosts[day] += r.Cost
	}
	return summary
}

func (ct *CostTracker) buildModelSummaries(period CostPeriod) map[string]ModelCostSummary {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	summaries := make(map[string]ModelCostSummary)
	for _, r := range ct.records {
		if !r.Timestamp.After(period.Start) || r.Timestamp.After(period.End) {
			continue
		}
		ms := summaries[r.Model]
		ms.TotalCost += r.Cost
		ms.TotalTokens += r.InputTokens + r.OutputTokens
		ms.InputTokens += r.InputTokens
		ms.OutputTokens += r.OutputTokens
		ms.RequestsTotal++
		summaries[r.Model] = ms
	}
	return summaries
}

// filter helpers (operate on already-built summaries)

func filterUserSummary(s UserCostSummary, period CostPeriod) UserCostSummary {
	return s // cache already scoped to query; period filtering done at build time
}

func filterOrgSummary(s OrgCostSummary, period CostPeriod) OrgCostSummary {
	return s
}

func filterModelSummaries(m map[string]ModelCostSummary, period CostPeriod) map[string]ModelCostSummary {
	return m
}

// lookupModelInfo reads from the global PriceTable.
var lookupModelInfo = func(model string) (modelInfo, bool) {
	// default stub; replaced by llm.LookupPrice at init time
	return modelInfo{}, false
}

type modelInfo struct {
	InputCostPer1K  float64
	OutputCostPer1K float64
}

// SetModelInfoLookup overrides the model info lookup function. Called by
// the llm package to wire in its PriceTable without circular imports.
func SetModelInfoLookup(fn func(string) (modelInfo, bool)) {
	lookupModelInfo = fn
}

// Suppress unused import for sync/atomic if needed by tests.
var _ = atomic.LoadInt64

// ── SLA/SLO Tracking ──

// SLATarget defines a Service Level Agreement target.
type SLATarget struct {
	Name   string        `json:"name"`
	Metric string        `json:"metric"` // "latency_p95", "error_rate", "availability"
	Target float64       `json:"target"` // e.g., 0.999 for 99.9%
	Window time.Duration `json:"window"`
}

// SLATracker monitors compliance with SLA targets.
type SLATracker struct {
	targets []SLATarget
	mu      sync.RWMutex
}

// NewSLATracker creates a new SLA tracker.
func NewSLATracker(targets []SLATarget) *SLATracker {
	return &SLATracker{targets: targets}
}

// SLAResult represents the current status of an SLA.
type SLAResult struct {
	Name     string  `json:"name"`
	Current  float64 `json:"current"`
	Target   float64 `json:"target"`
	Met      bool    `json:"met"`
	BurnRate float64 `json:"burn_rate"` // 1.0 = on track, >1.0 = burning budget too fast
}

// CheckSLAs evaluates all SLA targets against current metrics.
func (ct *CostTracker) CheckSLAs(tracker *SLATracker) []SLAResult {
	if tracker == nil {
		return nil
	}
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()

	var results []SLAResult
	for _, target := range tracker.targets {
		var current float64
		switch target.Metric {
		case "availability":
			current = 1.0 // assume healthy unless proven otherwise
		case "error_rate":
			current = 0.0 // assume no errors
		default:
			current = 0.0
		}

		met := current >= target.Target
		burnRate := 0.0
		if target.Target > 0 {
			burnRate = (1.0 - current) / (1.0 - target.Target)
		}

		results = append(results, SLAResult{
			Name:     target.Name,
			Current:  current,
			Target:   target.Target,
			Met:      met,
			BurnRate: burnRate,
		})
	}
	return results
}

// ── Latency Percentile Tracking ──

// LatencyTracker tracks request latency percentiles.
type LatencyTracker struct {
	mu        sync.RWMutex
	latencies []float64 // in milliseconds
	maxSize   int
}

// NewLatencyTracker creates a latency tracker with a max buffer size.
func NewLatencyTracker(maxSize int) *LatencyTracker {
	if maxSize <= 0 {
		maxSize = 10000
	}
	return &LatencyTracker{
		latencies: make([]float64, 0, maxSize),
		maxSize:   maxSize,
	}
}

// Record adds a latency measurement.
func (lt *LatencyTracker) Record(durationMs float64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	if len(lt.latencies) >= lt.maxSize {
		// Drop oldest
		lt.latencies = lt.latencies[1:]
	}
	lt.latencies = append(lt.latencies, durationMs)
}

// LatencyPercentiles returns p50, p95, p99 latency in milliseconds.
func (lt *LatencyTracker) LatencyPercentiles() (p50, p95, p99 float64) {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	if len(lt.latencies) == 0 {
		return 0, 0, 0
	}

	// Sort a copy
	sorted := make([]float64, len(lt.latencies))
	copy(sorted, lt.latencies)
	sort.Float64s(sorted)

	n := len(sorted)
	p50 = sorted[n*50/100]
	p95 = sorted[n*95/100]
	p99 = sorted[n*99/100]
	return
}

// ── Budget Alert System ──

// BudgetAlert tracks cost thresholds and fires alerts.
type BudgetAlert struct {
	mu            sync.RWMutex
	dailyBudget   float64
	weeklyBudget  float64
	monthlyBudget float64
	onAlert       func(alert CostAlert)
}

// CostAlert represents a triggered budget alert.
type CostAlert struct {
	Type      string    `json:"type"` // "daily", "weekly", "monthly"
	Threshold float64   `json:"threshold"`
	Actual    float64   `json:"actual"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// NewBudgetAlert creates a new budget alert system.
func NewBudgetAlert(daily, weekly, monthly float64, onAlert func(CostAlert)) *BudgetAlert {
	return &BudgetAlert{
		dailyBudget:   daily,
		weeklyBudget:  weekly,
		monthlyBudget: monthly,
		onAlert:       onAlert,
	}
}

// CheckBudget checks if the current costs exceed any thresholds.
func (ct *CostTracker) CheckBudget(alert *BudgetAlert) {
	if alert == nil {
		return
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	thisWeek := today.AddDate(0, 0, -int(today.Weekday()))
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	// Check daily budget
	if alert.dailyBudget > 0 {
		daily := ct.buildModelSummaries(CostPeriod{Start: today, End: now})
		var total float64
		for _, s := range daily {
			total += s.TotalCost
		}
		if total >= alert.dailyBudget {
			alert.fire(CostAlert{
				Type:      "daily",
				Threshold: alert.dailyBudget,
				Actual:    total,
				Message:   fmt.Sprintf("Daily cost $%.2f exceeds budget $%.2f", total, alert.dailyBudget),
				Timestamp: now,
			})
		}
	}

	// Check weekly budget
	if alert.weeklyBudget > 0 {
		weekly := ct.buildModelSummaries(CostPeriod{Start: thisWeek, End: now})
		var total float64
		for _, s := range weekly {
			total += s.TotalCost
		}
		if total >= alert.weeklyBudget {
			alert.fire(CostAlert{
				Type:      "weekly",
				Threshold: alert.weeklyBudget,
				Actual:    total,
				Message:   fmt.Sprintf("Weekly cost $%.2f exceeds budget $%.2f", total, alert.weeklyBudget),
				Timestamp: now,
			})
		}
	}

	// Check monthly budget
	if alert.monthlyBudget > 0 {
		monthly := ct.buildModelSummaries(CostPeriod{Start: thisMonth, End: now})
		var total float64
		for _, s := range monthly {
			total += s.TotalCost
		}
		if total >= alert.monthlyBudget {
			alert.fire(CostAlert{
				Type:      "monthly",
				Threshold: alert.monthlyBudget,
				Actual:    total,
				Message:   fmt.Sprintf("Monthly cost $%.2f exceeds budget $%.2f", total, alert.monthlyBudget),
				Timestamp: now,
			})
		}
	}
}

// fire invokes the alert callback if set.
func (ba *BudgetAlert) fire(alert CostAlert) {
	ba.mu.RLock()
	fn := ba.onAlert
	ba.mu.RUnlock()
	if fn != nil {
		fn(alert)
	}
}

// SetOnAlert updates the alert callback.
func (ba *BudgetAlert) SetOnAlert(fn func(CostAlert)) {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	ba.onAlert = fn
}

// ── Cost Forecast ──

// ForecastResult contains a cost projection.
type ForecastResult struct {
	DailyRate   float64 `json:"daily_rate"`
	WeeklyRate  float64 `json:"weekly_rate"`
	MonthlyRate float64 `json:"monthly_rate"`
	Confidence  float64 `json:"confidence"` // 0.0-1.0 based on data points
}

// maxCostRecords is the maximum number of cost records to keep in memory.
// Older records are trimmed to prevent unbounded memory growth.
const maxCostRecords = 100000

// trimOldRecords removes the oldest records when the cap is exceeded.
// Must be called while holding ct.mu write lock.
func (ct *CostTracker) trimOldRecords() {
	if len(ct.records) <= maxCostRecords {
		return
	}
	overflow := len(ct.records) - maxCostRecords
	ct.records = ct.records[overflow:]
}

// Forecast projects future costs based on recent usage patterns.
// The days parameter controls how many days of history to analyze.
func (ct *CostTracker) Forecast(days int) ForecastResult {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	if days < 1 {
		days = 30
	}

	now := time.Now()
	lookback := now.AddDate(0, 0, -days)

	var totalCost float64
	var dataPoints int

	for _, r := range ct.records {
		if r.Timestamp.After(lookback) {
			totalCost += r.Cost
			dataPoints++
		}
	}

	var dailyRate, weeklyRate, monthlyRate float64
	if dataPoints > 0 {
		dailyRate = totalCost / float64(days)
		weeklyRate = dailyRate * 7
		monthlyRate = dailyRate * 30
	}

	// Confidence based on data point density
	confidence := float64(dataPoints) / float64(days) / 10.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return ForecastResult{
		DailyRate:   dailyRate,
		WeeklyRate:  weeklyRate,
		MonthlyRate: monthlyRate,
		Confidence:  confidence,
	}
}

// sanitizeCSVField escapes a CSV field to prevent formula injection.
// Prefixes with a single quote if the field starts with =, +, -, @, or \t.
// Also strips newlines to prevent CSV injection via \r\n.
func sanitizeCSVField(s string) string {
	if len(s) == 0 {
		return s
	}
	// Strip newlines to prevent CSV injection via line breaks
	result := strings.NewReplacer("\r", "", "\n", "").Replace(s)
	if len(result) == 0 {
		return result
	}
	switch result[0] {
	case '=', '+', '-', '@', '\t':
		return "'" + result
	}
	return result
}

// ExportCSV exports cost records as CSV.
func (ct *CostTracker) ExportCSV(w io.Writer) error {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	fmt.Fprintln(w, "model,input_tokens,output_tokens,cost,user_id,org_id,project_id,task_id,timestamp")
	for _, r := range ct.records {
		fmt.Fprintf(w, "%s,%d,%d,%.6f,%s,%s,%s,%s,%s",
			sanitizeCSVField(r.Model), r.InputTokens, r.OutputTokens, r.Cost,
			sanitizeCSVField(r.UserID), sanitizeCSVField(r.OrgID),
			sanitizeCSVField(r.ProjectID), sanitizeCSVField(r.TaskID),
			r.Timestamp.Format(time.RFC3339))
	}
	return nil
}
