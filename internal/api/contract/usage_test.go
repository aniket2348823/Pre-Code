package contract

import (
	"encoding/json"
	"testing"
)

func TestGetUsageRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		req      GetUsageRequest
		hasErr   bool
		errField string
	}{
		{
			name:   "valid request",
			req:    GetUsageRequest{StartDate: "2026-06-01", EndDate: "2026-06-30"},
			hasErr: false,
		},
		{
			name:     "missing start_date",
			req:      GetUsageRequest{EndDate: "2026-06-30"},
			hasErr:   true,
			errField: "start_date",
		},
		{
			name:     "missing end_date",
			req:      GetUsageRequest{StartDate: "2026-06-01"},
			hasErr:   true,
			errField: "end_date",
		},
		{
			name:     "invalid granularity",
			req:      GetUsageRequest{StartDate: "2026-06-01", EndDate: "2026-06-30", Granularity: "hourly"},
			hasErr:   true,
			errField: "granularity",
		},
		{
			name:   "valid granularity daily",
			req:    GetUsageRequest{StartDate: "2026-06-01", EndDate: "2026-06-30", Granularity: GranularityDaily},
			hasErr: false,
		},
		{
			name:   "valid granularity weekly",
			req:    GetUsageRequest{StartDate: "2026-06-01", EndDate: "2026-06-30", Granularity: GranularityWeekly},
			hasErr: false,
		},
		{
			name:   "valid granularity monthly",
			req:    GetUsageRequest{StartDate: "2026-06-01", EndDate: "2026-06-30", Granularity: GranularityMonthly},
			hasErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.req.Validate()
			if tt.hasErr && !errs.HasErrors() {
				t.Errorf("expected validation error on field %q", tt.errField)
			}
			if !tt.hasErr && errs.HasErrors() {
				t.Errorf("unexpected errors: %v", errs.ToMap())
			}
		})
	}
}

func TestGetUsageRequest_ApplyDefaults(t *testing.T) {
	req := GetUsageRequest{StartDate: "2026-06-01", EndDate: "2026-06-30"}
	req.ApplyDefaults()

	if req.Granularity != GranularityDaily {
		t.Errorf("Granularity = %q, want %q", req.Granularity, GranularityDaily)
	}
}

func TestGetUsageRequest_ApplyDefaults_PreserveExisting(t *testing.T) {
	req := GetUsageRequest{StartDate: "2026-06-01", EndDate: "2026-06-30", Granularity: GranularityMonthly}
	req.ApplyDefaults()

	if req.Granularity != GranularityMonthly {
		t.Errorf("Granularity should be preserved at monthly, got %q", req.Granularity)
	}
}

func TestUsageSummary_BudgetRemains(t *testing.T) {
	tests := []struct {
		name        string
		summary     UsageSummary
		wantRemains float64
	}{
		{
			name:        "budget configured with remaining",
			summary:     UsageSummary{TotalCost: 30.0, BudgetLimit: 100.0},
			wantRemains: 70.0,
		},
		{
			name:        "budget fully consumed",
			summary:     UsageSummary{TotalCost: 100.0, BudgetLimit: 100.0},
			wantRemains: 0.0,
		},
		{
			name:        "budget exceeded clamps to zero",
			summary:     UsageSummary{TotalCost: 120.0, BudgetLimit: 100.0},
			wantRemains: 0.0,
		},
		{
			name:        "no budget configured",
			summary:     UsageSummary{TotalCost: 50.0, BudgetLimit: 0},
			wantRemains: 0.0,
		},
		{
			name:        "negative budget treated as none",
			summary:     UsageSummary{TotalCost: 50.0, BudgetLimit: -1},
			wantRemains: 0.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.summary.BudgetRemains(); got != tt.wantRemains {
				t.Errorf("BudgetRemains() = %v, want %v", got, tt.wantRemains)
			}
		})
	}
}

func TestModelBreakdown_JSONRoundTrip(t *testing.T) {
	original := ModelBreakdown{
		Model:    "claude-sonnet-4",
		Provider: "anthropic",
		Tasks:    150,
		Tokens:   2_500_000,
		Cost:     12.50,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ModelBreakdown
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Model != "claude-sonnet-4" {
		t.Errorf("Model = %q, want claude-sonnet-4", decoded.Model)
	}
	if decoded.Tokens != 2_500_000 {
		t.Errorf("Tokens = %d, want 2500000", decoded.Tokens)
	}
	if decoded.Cost != 12.50 {
		t.Errorf("Cost = %v, want 12.50", decoded.Cost)
	}
}

func TestBudgetConfig_Validate(t *testing.T) {
	tests := []struct {
		name     string
		config   BudgetConfig
		hasErr   bool
		errField string
	}{
		{
			name:   "valid config",
			config: BudgetConfig{MonthlyLimit: 100.0, AlertThreshold: 0.8, HardStop: true},
			hasErr: false,
		},
		{
			name:     "negative monthly limit",
			config:   BudgetConfig{MonthlyLimit: -1, AlertThreshold: 0.8},
			hasErr:   true,
			errField: "monthly_limit",
		},
		{
			name:     "alert threshold too high",
			config:   BudgetConfig{MonthlyLimit: 100, AlertThreshold: 1.5},
			hasErr:   true,
			errField: "alert_threshold",
		},
		{
			name:     "alert threshold negative",
			config:   BudgetConfig{MonthlyLimit: 100, AlertThreshold: -0.1},
			hasErr:   true,
			errField: "alert_threshold",
		},
		{
			name:   "zero threshold is valid (no alert)",
			config: BudgetConfig{MonthlyLimit: 100, AlertThreshold: 0},
			hasErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.config.Validate()
			if tt.hasErr && !errs.HasErrors() {
				t.Errorf("expected validation error on field %q", tt.errField)
			}
			if !tt.hasErr && errs.HasErrors() {
				t.Errorf("unexpected errors: %v", errs.ToMap())
			}
		})
	}
}

func TestGranularity_Valid(t *testing.T) {
	for _, g := range AllGranularities() {
		if !g.Valid() {
			t.Errorf("Granularity(%q).Valid() = false, expected true", g)
		}
	}
	if Granularity("hourly").Valid() {
		t.Error("Granularity(hourly).Valid() should be false")
	}
}

func TestUsageRecord_JSONRoundTrip(t *testing.T) {
	original := UsageRecord{
		Date:       "2026-06-15",
		TasksCount: 42,
		TokensUsed: 1_500_000,
		Cost:       7.25,
		ModelBreakdown: []ModelBreakdown{
			{Model: "gpt-4o-mini", Provider: "openai", Tasks: 30, Tokens: 500_000, Cost: 1.25},
			{Model: "claude-sonnet-4", Provider: "anthropic", Tasks: 12, Tokens: 1_000_000, Cost: 6.00},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded UsageRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.TasksCount != 42 {
		t.Errorf("TasksCount = %d, want 42", decoded.TasksCount)
	}
	if len(decoded.ModelBreakdown) != 2 {
		t.Fatalf("ModelBreakdown len = %d, want 2", len(decoded.ModelBreakdown))
	}
}
