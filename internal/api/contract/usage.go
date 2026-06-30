package contract

// ---------------------------------------------------------------------------
// Usage / Billing resource types — API contract §2.6
// ---------------------------------------------------------------------------

// Granularity controls how usage data is aggregated.
type Granularity string

const (
	GranularityDaily   Granularity = "daily"
	GranularityWeekly  Granularity = "weekly"
	GranularityMonthly Granularity = "monthly"
)

// AllGranularities returns every valid granularity value.
func AllGranularities() []Granularity {
	return []Granularity{GranularityDaily, GranularityWeekly, GranularityMonthly}
}

// Valid returns true when the granularity is one of the known values.
func (g Granularity) Valid() bool {
	for _, v := range AllGranularities() {
		if g == v {
			return true
		}
	}
	return false
}

// GetUsageRequest holds query parameters for GET /v1/usage.
type GetUsageRequest struct {
	StartDate   string      `json:"start_date"`
	EndDate     string      `json:"end_date"`
	ProjectID   string      `json:"project_id,omitempty"`
	Granularity Granularity `json:"granularity,omitempty"`
}

// Validate checks required fields and enum values.
func (r *GetUsageRequest) Validate() ValidationErrors {
	var errs ValidationErrors
	if r.StartDate == "" {
		errs.Add("start_date", "start_date is required (YYYY-MM-DD)")
	}
	if r.EndDate == "" {
		errs.Add("end_date", "end_date is required (YYYY-MM-DD)")
	}
	if r.Granularity != "" && !r.Granularity.Valid() {
		errs.Add("granularity", "granularity must be one of: daily, weekly, monthly")
	}
	return errs
}

// ApplyDefaults fills zero-value fields with documented defaults.
func (r *GetUsageRequest) ApplyDefaults() {
	if r.Granularity == "" {
		r.Granularity = GranularityDaily
	}
}

// GetUsageResponse is the response for GET /v1/usage.
type GetUsageResponse struct {
	Usage   []UsageRecord `json:"usage"`
	Summary UsageSummary  `json:"summary"`
}

// UsageRecord is one row of usage data.
type UsageRecord struct {
	Date           string           `json:"date"`
	TasksCount     int              `json:"tasks_count"`
	TokensUsed     int64            `json:"tokens_used"`
	Cost           float64          `json:"cost"`
	ModelBreakdown []ModelBreakdown `json:"model_breakdown,omitempty"`
}

// UsageSummary aggregates usage across the requested period.
type UsageSummary struct {
	TotalTasks      int     `json:"total_tasks"`
	TotalTokens     int64   `json:"total_tokens"`
	TotalCost       float64 `json:"total_cost"`
	BudgetLimit     float64 `json:"budget_limit,omitempty"`
	BudgetRemaining float64 `json:"budget_remaining,omitempty"`
}

// BudgetRemains calculates remaining = limit - total_cost.
// Returns 0 when no budget is configured.
func (s UsageSummary) BudgetRemains() float64 {
	if s.BudgetLimit <= 0 {
		return 0
	}
	rem := s.BudgetLimit - s.TotalCost
	if rem < 0 {
		return 0
	}
	return rem
}

// ModelBreakdown shows usage attributed to a specific model.
type ModelBreakdown struct {
	Model    string  `json:"model"`
	Provider string  `json:"provider"`
	Tasks    int     `json:"tasks"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
}

// BudgetConfig holds per-user/project budget settings.
type BudgetConfig struct {
	MonthlyLimit   float64 `json:"monthly_limit"`
	AlertThreshold float64 `json:"alert_threshold"` // 0.0–1.0 (e.g., 0.8 = 80%)
	HardStop       bool    `json:"hard_stop"`
}

// Validate checks budget configuration.
func (b *BudgetConfig) Validate() ValidationErrors {
	var errs ValidationErrors
	if b.MonthlyLimit < 0 {
		errs.Add("monthly_limit", "monthly_limit must be non-negative")
	}
	if b.AlertThreshold < 0 || b.AlertThreshold > 1 {
		errs.Add("alert_threshold", "alert_threshold must be between 0.0 and 1.0")
	}
	return errs
}
