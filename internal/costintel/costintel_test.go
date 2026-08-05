package costintel

import (
	"math"
	"sync"
	"testing"
	"time"
)

func TestNewEngine(t *testing.T) {
	e := NewEngine()
	if e == nil {
		t.Fatal("NewEngine should not return nil")
	}
	if len(e.pricing) == 0 {
		t.Error("expected default pricing to be loaded")
	}
}

func TestCalculateCost(t *testing.T) {
	e := NewEngine()
	cost := e.CalculateCost("gpt-4o", 1000, 500, 0)
	expected := 0.0025 + 0.005
	if cost != expected {
		t.Errorf("expected cost %f, got %f", expected, cost)
	}
}

func TestCalculateCostWithCache(t *testing.T) {
	e := NewEngine()
	e.SetPricing(&ModelPricing{
		Model:       "test-model",
		Provider:    "test",
		InputPer1K:  0.001,
		OutputPer1K: 0.002,
		CachedPer1K: 0.0005,
	})
	cost := e.CalculateCost("test-model", 2000, 1000, 500)
	expected := 0.002 + 0.002 + 0.00025
	if cost != expected {
		t.Errorf("expected cost %f, got %f", expected, cost)
	}
}

func TestCalculateCostUnknownModel(t *testing.T) {
	e := NewEngine()
	cost := e.CalculateCost("unknown-model", 1000, 500, 0)
	if cost != 0 {
		t.Errorf("expected 0 for unknown model, got %f", cost)
	}
}

func TestSetPricing(t *testing.T) {
	e := NewEngine()
	e.SetPricing(&ModelPricing{
		Model:       "custom-model",
		Provider:    "custom",
		InputPer1K:  0.01,
		OutputPer1K: 0.02,
	})
	p := e.GetPricing("custom-model")
	if p == nil {
		t.Fatal("expected pricing to be set")
	}
	if p.InputPer1K != 0.01 {
		t.Errorf("expected InputPer1K 0.01, got %f", p.InputPer1K)
	}
}

func TestGetPricingNotFound(t *testing.T) {
	e := NewEngine()
	p := e.GetPricing("nonexistent")
	if p != nil {
		t.Error("expected nil for nonexistent model")
	}
}

func TestRecordCost(t *testing.T) {
	e := NewEngine()
	e.RecordCost(CostRecord{
		ID:           "r1",
		Model:        "gpt-4o",
		InputTokens:  1000,
		OutputTokens: 500,
		CostUSD:      0.0075,
	})
	if e.TotalRecords() != 1 {
		t.Errorf("expected 1 record, got %d", e.TotalRecords())
	}
	if e.TotalCost() != 0.0075 {
		t.Errorf("expected total cost 0.0075, got %f", e.TotalCost())
	}
}

func TestCostByModel(t *testing.T) {
	e := NewEngine()
	e.RecordCost(CostRecord{ID: "r1", Model: "gpt-4o", CostUSD: 0.01})
	e.RecordCost(CostRecord{ID: "r2", Model: "gpt-4o", CostUSD: 0.02})
	e.RecordCost(CostRecord{ID: "r3", Model: "claude", CostUSD: 0.03})
	byModel := e.CostByModel()
	if byModel["gpt-4o"] != 0.03 {
		t.Errorf("expected gpt-4o cost 0.03, got %f", byModel["gpt-4o"])
	}
	if byModel["claude"] != 0.03 {
		t.Errorf("expected claude cost 0.03, got %f", byModel["claude"])
	}
}

func TestCostByTaskType(t *testing.T) {
	e := NewEngine()
	e.RecordCost(CostRecord{ID: "r1", TaskType: "code_generation", CostUSD: 0.01})
	e.RecordCost(CostRecord{ID: "r2", TaskType: "review", CostUSD: 0.02})
	byType := e.CostByTaskType()
	if byType["code_generation"] != 0.01 {
		t.Errorf("expected code_generation cost 0.01, got %f", byType["code_generation"])
	}
}

func TestBudgetCheck(t *testing.T) {
	e := NewEngine()
	e.SetBudget(&Budget{
		ID:       "monthly",
		Name:     "Monthly Budget",
		LimitUSD: 100.0,
		AlertAt:  0.8,
	})
	b, triggered := e.CheckBudget("monthly")
	if b == nil {
		t.Fatal("expected budget to exist")
	}
	if triggered {
		t.Error("should not be triggered at 0% spend")
	}
}

func TestBudgetCheckTriggered(t *testing.T) {
	e := NewEngine()
	b := &Budget{
		ID:       "monthly",
		Name:     "Monthly Budget",
		LimitUSD: 100.0,
		SpentUSD: 85.0,
		AlertAt:  0.8,
	}
	e.SetBudget(b)
	_, triggered := e.CheckBudget("monthly")
	if !triggered {
		t.Error("should be triggered at 85% spend with 80% alert")
	}
}

func TestBudgetNotFound(t *testing.T) {
	e := NewEngine()
	b, ok := e.CheckBudget("nonexistent")
	if b != nil || ok {
		t.Error("expected nil budget for nonexistent ID")
	}
}

func TestForecastCostNoData(t *testing.T) {
	e := NewEngine()
	f := e.ForecastCost(30)
	if f.PredictedCost != 0 {
		t.Errorf("expected 0 predicted cost with no data, got %f", f.PredictedCost)
	}
	if f.Confidence != 0 {
		t.Errorf("expected 0 confidence with no data, got %f", f.Confidence)
	}
}

func TestForecastCostWithData(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 30; i++ {
		e.RecordCost(CostRecord{
			ID:        "r" + string(rune('0'+i%10)),
			Model:     "gpt-4o",
			CostUSD:   1.0,
			CreatedAt: time.Now().AddDate(0, 0, -29+i),
		})
	}
	f := e.ForecastCost(30)
	if f.PredictedCost < 15 || f.PredictedCost > 45 {
		t.Errorf("expected predicted cost around 30 (±50%%), got %f", f.PredictedCost)
	}
	if f.Confidence <= 0 {
		t.Errorf("expected positive confidence, got %f", f.Confidence)
	}
}

func TestGetRecommendationsEmpty(t *testing.T) {
	e := NewEngine()
	recs := e.GetRecommendations()
	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations with no data, got %d", len(recs))
	}
}

func TestGetAnomaliesEmpty(t *testing.T) {
	e := NewEngine()
	anomalies := e.GetAnomalies()
	if len(anomalies) != 0 {
		t.Errorf("expected 0 anomalies, got %d", len(anomalies))
	}
}

func TestCostAnomalyDetection(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 20; i++ {
		e.RecordCost(CostRecord{
			ID:      "r" + string(rune('a'+i)),
			Model:   "gpt-4o",
			CostUSD: 0.01,
		})
	}
	e.RecordCost(CostRecord{
		ID:      "anomaly",
		Model:   "gpt-4o",
		CostUSD: 1.0,
	})
	anomalies := e.GetAnomalies()
	if len(anomalies) == 0 {
		t.Error("expected at least one anomaly")
	}
}

func TestConcurrentRecordCost(t *testing.T) {
	e := NewEngine()
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func() {
			e.RecordCost(CostRecord{
				Model:   "gpt-4o",
				CostUSD: 0.01,
			})
			done <- true
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
	if e.TotalRecords() != 100 {
		t.Errorf("expected 100 records, got %d", e.TotalRecords())
	}
}

// --- Deep tests merged below ---

func TestCalculateCost_NegativeTokens(t *testing.T) {
	e := NewEngine()
	cost := e.CalculateCost("gpt-4o", -100, 50, 0)
	if cost < 0 {
		t.Errorf("negative tokens should not produce negative cost, got %f", cost)
	}
}

func TestCalculateCost_ZeroTokens(t *testing.T) {
	e := NewEngine()
	cost := e.CalculateCost("gpt-4o", 0, 0, 0)
	if cost != 0 {
		t.Errorf("zero tokens should produce zero cost, got %f", cost)
	}
}

func TestCalculateCost_LargeTokens(t *testing.T) {
	e := NewEngine()
	cost := e.CalculateCost("gpt-4o", 1000000000, 0, 0)
	if cost <= 0 {
		t.Errorf("1B tokens should produce positive cost, got %f", cost)
	}
}

func TestCalculateCost_UnknownModelDeep(t *testing.T) {
	e := NewEngine()
	cost := e.CalculateCost("unknown-model", 1000, 500, 0)
	if cost != 0 {
		t.Errorf("unknown model should return 0, got %f", cost)
	}
}

func TestBudgetCheck_ZeroBudget(t *testing.T) {
	e := NewEngine()
	e.SetBudget(&Budget{ID: "b1", LimitUSD: 0, AlertAt: 0.8})
	_, triggered := e.CheckBudget("b1")
	if !triggered {
		t.Error("zero budget should trigger immediately")
	}
}

func TestBudgetCheck_ExactBoundary(t *testing.T) {
	e := NewEngine()
	e.SetBudget(&Budget{ID: "b1", LimitUSD: 100, SpentUSD: 80, AlertAt: 0.8})
	_, triggered := e.CheckBudget("b1")
	if !triggered {
		t.Error("at exact boundary should trigger")
	}
}

func TestBudgetCheck_NoBudgetSet(t *testing.T) {
	e := NewEngine()
	b, ok := e.CheckBudget("nonexistent")
	if b != nil || ok {
		t.Error("nonexistent budget should return nil")
	}
}

func TestForecastCost_SingleDataPoint(t *testing.T) {
	e := NewEngine()
	e.RecordCost(CostRecord{ID: "r1", Model: "gpt-4o", CostUSD: 1.0, CreatedAt: time.Now()})
	f := e.ForecastCost(30)
	if f.Confidence > 0.1 {
		t.Errorf("single data point should have very low confidence, got %f", f.Confidence)
	}
}

func TestRecordCost_NegativeCost(t *testing.T) {
	e := NewEngine()
	e.RecordCost(CostRecord{ID: "r1", CostUSD: -5.0})
	if e.TotalCost() != -5 {
		t.Errorf("negative cost should be recorded, got %f", e.TotalCost())
	}
}

func TestRecordCost_ZeroCost(t *testing.T) {
	e := NewEngine()
	e.RecordCost(CostRecord{ID: "r1", CostUSD: 0})
	if e.TotalRecords() != 1 {
		t.Error("zero cost should still be recorded")
	}
}

func TestCostByModel_NoData(t *testing.T) {
	e := NewEngine()
	byModel := e.CostByModel()
	if len(byModel) != 0 {
		t.Errorf("expected 0 models, got %d", len(byModel))
	}
}

func TestCostByTaskType_NoData(t *testing.T) {
	e := NewEngine()
	byType := e.CostByTaskType()
	if len(byType) != 0 {
		t.Errorf("expected 0 task types, got %d", len(byType))
	}
}

func TestCostByTaskType_ZeroCosts(t *testing.T) {
	e := NewEngine()
	e.RecordCost(CostRecord{ID: "r1", TaskType: "test", CostUSD: 0})
	e.RecordCost(CostRecord{ID: "r2", TaskType: "test", CostUSD: 0})
	byType := e.CostByTaskType()
	if byType["test"] != 0 {
		t.Errorf("expected 0 cost, got %f", byType["test"])
	}
}

func TestGetRecommendations_Empty(t *testing.T) {
	e := NewEngine()
	recs := e.GetRecommendations()
	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations, got %d", len(recs))
	}
}

func TestConcurrentRecordCost_Deep(t *testing.T) {
	e := NewEngine()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.RecordCost(CostRecord{Model: "gpt-4o", CostUSD: 0.01})
		}()
	}
	wg.Wait()
	if e.TotalRecords() != 100 {
		t.Errorf("expected 100 records, got %d", e.TotalRecords())
	}
}

func TestSetPricing_ZeroCosts(t *testing.T) {
	e := NewEngine()
	e.SetPricing(&ModelPricing{Model: "free", InputPer1K: 0, OutputPer1K: 0})
	p := e.GetPricing("free")
	if p == nil {
		t.Fatal("expected pricing to be set")
	}
	if p.InputPer1K != 0 {
		t.Errorf("expected 0 input cost, got %f", p.InputPer1K)
	}
}

func TestSetPricing_NegativeCosts(t *testing.T) {
	e := NewEngine()
	e.SetPricing(&ModelPricing{Model: "negative", InputPer1K: -0.01, OutputPer1K: -0.02})
	p := e.GetPricing("negative")
	if p == nil {
		t.Fatal("expected pricing to be set")
	}
}

func TestForecastCost_Spike(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 20; i++ {
		e.RecordCost(CostRecord{
			ID:        "r" + string(rune('a'+i)),
			Model:     "gpt-4o",
			CostUSD:   0.01,
			CreatedAt: time.Now().AddDate(0, 0, -20+i),
		})
	}
	e.RecordCost(CostRecord{ID: "spike", Model: "gpt-4o", CostUSD: 10.0, CreatedAt: time.Now()})
	anomalies := e.GetAnomalies()
	if len(anomalies) == 0 {
		t.Error("expected at least one anomaly for spike")
	}
}

func TestBudgetCheck_RecordCostExceeds(t *testing.T) {
	e := NewEngine()
	e.SetBudget(&Budget{ID: "b1", LimitUSD: 100, AlertAt: 0.8})
	e.RecordCost(CostRecord{ID: "r1", CostUSD: 90})
	_, triggered := e.CheckBudget("b1")
	if !triggered {
		t.Error("90% spend should trigger alert")
	}
}

func TestForecastCost_StableTrend(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 30; i++ {
		e.RecordCost(CostRecord{
			ID:        "r" + string(rune('a'+i%26)),
			Model:     "gpt-4o",
			CostUSD:   1.0,
			CreatedAt: time.Now().AddDate(0, 0, -29+i),
		})
	}
	f := e.ForecastCost(30)
	if f.TrendDirection != "stable" {
		t.Errorf("expected stable trend, got %s", f.TrendDirection)
	}
	if math.Abs(f.PredictedCost-30) > 15 {
		t.Errorf("expected ~30 predicted cost, got %f", f.PredictedCost)
	}
}

func TestForecastCost_IncreasingTrend(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 30; i++ {
		e.RecordCost(CostRecord{
			ID:        "r" + string(rune('a'+i%26)),
			Model:     "gpt-4o",
			CostUSD:   float64(i) * 0.1,
			CreatedAt: time.Now().AddDate(0, 0, -29+i),
		})
	}
	f := e.ForecastCost(30)
	if f.TrendDirection != "increasing" {
		t.Errorf("expected increasing trend, got %s", f.TrendDirection)
	}
}

// --- NEW tests to boost coverage to 95%+ ---

func TestRecordCost_BudgetResetDaily(t *testing.T) {
	e := NewEngine()
	b := &Budget{
		ID:       "daily-budget",
		Name:     "Daily Budget",
		LimitUSD: 100.0,
		Period:   "daily",
		ResetAt:  time.Now().Add(-1 * time.Hour), // already expired
		AlertAt:  0.8,
	}
	e.SetBudget(b)

	e.RecordCost(CostRecord{ID: "r1", CostUSD: 50.0})

	b2, _ := e.CheckBudget("daily-budget")
	if b2.SpentUSD != 50.0 {
		t.Errorf("expected spent 50.0 after reset, got %f", b2.SpentUSD)
	}
	if time.Until(b2.ResetAt) < 0 || time.Until(b2.ResetAt) > 25*time.Hour {
		t.Errorf("expected ResetAt ~1 day from now, got %v", b2.ResetAt)
	}
}

func TestRecordCost_BudgetResetWeekly(t *testing.T) {
	e := NewEngine()
	b := &Budget{
		ID:       "weekly-budget",
		Name:     "Weekly Budget",
		LimitUSD: 100.0,
		Period:   "weekly",
		ResetAt:  time.Now().Add(-1 * time.Hour),
		AlertAt:  0.8,
	}
	e.SetBudget(b)

	e.RecordCost(CostRecord{ID: "r1", CostUSD: 10.0})

	b2, _ := e.CheckBudget("weekly-budget")
	if b2.SpentUSD != 10.0 {
		t.Errorf("expected spent 10.0 after reset, got %f", b2.SpentUSD)
	}
}

func TestRecordCost_BudgetResetMonthly(t *testing.T) {
	e := NewEngine()
	b := &Budget{
		ID:       "monthly-budget",
		Name:     "Monthly Budget",
		LimitUSD: 100.0,
		Period:   "monthly",
		ResetAt:  time.Now().Add(-1 * time.Hour),
		AlertAt:  0.8,
	}
	e.SetBudget(b)

	e.RecordCost(CostRecord{ID: "r1", CostUSD: 25.0})

	b2, _ := e.CheckBudget("monthly-budget")
	if b2.SpentUSD != 25.0 {
		t.Errorf("expected spent 25.0 after reset, got %f", b2.SpentUSD)
	}
}

func TestRecordCost_BudgetResetDefaultPeriod(t *testing.T) {
	e := NewEngine()
	b := &Budget{
		ID:       "default-budget",
		Name:     "Default Budget",
		LimitUSD: 100.0,
		Period:   "unknown",
		ResetAt:  time.Now().Add(-1 * time.Hour),
		AlertAt:  0.8,
	}
	e.SetBudget(b)

	e.RecordCost(CostRecord{ID: "r1", CostUSD: 10.0})

	b2, _ := e.CheckBudget("default-budget")
	if b2.SpentUSD != 10.0 {
		t.Errorf("expected spent 10.0 after reset, got %f", b2.SpentUSD)
	}
}

func TestRecordCost_NoBudgetReset(t *testing.T) {
	e := NewEngine()
	b := &Budget{
		ID:       "future-budget",
		Name:     "Future Budget",
		LimitUSD: 100.0,
		ResetAt:  time.Now().Add(1 * time.Hour), // not yet expired
		AlertAt:  0.8,
	}
	e.SetBudget(b)

	e.RecordCost(CostRecord{ID: "r1", CostUSD: 10.0})

	b2, _ := e.CheckBudget("future-budget")
	if b2.SpentUSD != 10.0 {
		t.Errorf("expected spent 10.0, got %f", b2.SpentUSD)
	}
}

func TestRecordCost_ZeroCreatedAt(t *testing.T) {
	e := NewEngine()
	e.RecordCost(CostRecord{
		ID:        "r-zero-time",
		Model:     "gpt-4o",
		CostUSD:   0.01,
		CreatedAt: time.Time{}, // zero time
	})
	if e.TotalRecords() != 1 {
		t.Error("expected 1 record")
	}
	r := e.records[0]
	if r.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set to now")
	}
}

func TestSetBudget_DefaultAlertAt(t *testing.T) {
	e := NewEngine()
	e.SetBudget(&Budget{
		ID:       "b1",
		Name:     "Budget",
		LimitUSD: 100.0,
	})

	b, _ := e.CheckBudget("b1")
	if b == nil {
		t.Fatal("expected budget to exist")
	}
	if b.AlertAt != 0.8 {
		t.Errorf("expected default AlertAt 0.8, got %f", b.AlertAt)
	}
}

func TestSetBudget_CustomAlertAt(t *testing.T) {
	e := NewEngine()
	e.SetBudget(&Budget{
		ID:       "b1",
		Name:     "Budget",
		LimitUSD: 100.0,
		AlertAt:  0.5,
	})

	b, _ := e.CheckBudget("b1")
	if b.AlertAt != 0.5 {
		t.Errorf("expected AlertAt 0.5, got %f", b.AlertAt)
	}
}

func TestDetectAnomaly_FewRecords(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 3; i++ {
		e.RecordCost(CostRecord{
			ID:      "r" + string(rune('a'+i)),
			Model:   "gpt-4o",
			CostUSD: 0.01,
		})
	}
	anomalies := e.GetAnomalies()
	if len(anomalies) != 0 {
		t.Errorf("expected 0 anomalies with few records, got %d", len(anomalies))
	}
}

func TestDetectAnomaly_FewModelRecords(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 20; i++ {
		model := "other-model"
		if i < 3 {
			model = "gpt-4o"
		}
		e.RecordCost(CostRecord{
			ID:      "r" + string(rune('a'+i)),
			Model:   model,
			CostUSD: 0.01,
		})
	}
	anomalies := e.GetAnomalies()
	if len(anomalies) != 0 {
		t.Errorf("expected 0 anomalies with <5 records for model, got %d", len(anomalies))
	}
}

func TestDetectAnomaly_ZeroStdDev(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 20; i++ {
		e.RecordCost(CostRecord{
			ID:      "r" + string(rune('a'+i)),
			Model:   "gpt-4o",
			CostUSD: 0.01,
		})
	}
	anomalies := e.GetAnomalies()
	if len(anomalies) != 0 {
		t.Errorf("expected 0 anomalies with zero stddev, got %d", len(anomalies))
	}
}

func TestDetectAnomaly_CriticalSeverity(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 20; i++ {
		e.RecordCost(CostRecord{
			ID:      "r" + string(rune('a'+i)),
			Model:   "gpt-4o",
			CostUSD: 0.01,
		})
	}
	e.RecordCost(CostRecord{
		ID:      "critical-anomaly",
		Model:   "gpt-4o",
		CostUSD: 100.0,
	})
	anomalies := e.GetAnomalies()
	if len(anomalies) == 0 {
		t.Fatal("expected at least one anomaly")
	}
	found := false
	for _, a := range anomalies {
		if a.Severity == "critical" {
			found = true
		}
	}
	if !found {
		t.Error("expected critical severity anomaly")
	}
}

func TestGetRecommendations_ModelSelection(t *testing.T) {
	e := NewEngine()
	// Use expensive model many times
	for i := 0; i < 30; i++ {
		e.RecordCost(CostRecord{
			ID:           "r" + string(rune('a'+i%26)),
			Model:        "claude-opus-4-20250514",
			InputTokens:  1000,
			OutputTokens: 500,
			CostUSD:      1.0,
		})
	}
	recs := e.GetRecommendations()
	found := false
	for _, r := range recs {
		if r.Category == "model_selection" {
			found = true
			if r.SavingsUSD <= 0 {
				t.Error("expected positive savings")
			}
		}
	}
	if !found {
		t.Error("expected model_selection recommendation")
	}
}

func TestGetRecommendations_Batching(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 30; i++ {
		e.RecordCost(CostRecord{
			ID:           "r" + string(rune('a'+i%26)),
			Model:        "gpt-4o-mini",
			InputTokens:  100,
			OutputTokens: 5000,
			CostUSD:      0.01,
		})
	}
	recs := e.GetRecommendations()
	found := false
	for _, r := range recs {
		if r.Category == "batching" {
			found = true
		}
	}
	if !found {
		t.Error("expected batching recommendation")
	}
}

func TestGetRecommendations_Caching(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 30; i++ {
		e.RecordCost(CostRecord{
			ID:           "r" + string(rune('a'+i%26)),
			Model:        "gpt-4o",
			InputTokens:  10000,
			OutputTokens: 100,
			CachedTokens: 10,
			CostUSD:      0.01,
		})
	}
	recs := e.GetRecommendations()
	found := false
	for _, r := range recs {
		if r.Category == "caching" {
			found = true
		}
	}
	if !found {
		t.Error("expected caching recommendation")
	}
}

func TestGetRecommendations_SortedBySavings(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 30; i++ {
		e.RecordCost(CostRecord{
			ID:           "r" + string(rune('a'+i%26)),
			Model:        "claude-opus-4-20250514",
			InputTokens:  10000,
			OutputTokens: 100,
			CachedTokens: 10,
			CostUSD:      1.0,
		})
	}
	recs := e.GetRecommendations()
	for i := 1; i < len(recs); i++ {
		if recs[i].SavingsUSD > recs[i-1].SavingsUSD {
			t.Errorf("recommendations not sorted: rec[%d]=%.4f > rec[%d]=%.4f",
				i, recs[i].SavingsUSD, i-1, recs[i-1].SavingsUSD)
		}
	}
}

func TestGetRecommendations_CheapModelOnly(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 30; i++ {
		e.RecordCost(CostRecord{
			ID:           "r" + string(rune('a'+i%26)),
			Model:        "gpt-4o-mini",
			InputTokens:  1000,
			OutputTokens: 500,
			CostUSD:      0.001,
		})
	}
	recs := e.GetRecommendations()
	for _, r := range recs {
		if r.Category == "model_selection" {
			t.Error("no model_selection rec expected for cheap model")
		}
	}
}

func TestForecastCost_DecreasingTrend(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 30; i++ {
		e.RecordCost(CostRecord{
			ID:        "r" + string(rune('a'+i%26)),
			Model:     "gpt-4o",
			CostUSD:   float64(30-i) * 0.1,
			CreatedAt: time.Now().AddDate(0, 0, -29+i),
		})
	}
	f := e.ForecastCost(30)
	if f.TrendDirection != "decreasing" {
		t.Errorf("expected decreasing trend, got %s", f.TrendDirection)
	}
}

func TestForecastCost_AllSameCost(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 10; i++ {
		e.RecordCost(CostRecord{
			ID:        "r" + string(rune('0'+i)),
			Model:     "gpt-4o",
			CostUSD:   1.0,
			CreatedAt: time.Now().AddDate(0, 0, -9+i),
		})
	}
	f := e.ForecastCost(10)
	if f.TrendDirection != "stable" {
		t.Errorf("expected stable trend, got %s", f.TrendDirection)
	}
}

func TestCheapestModel(t *testing.T) {
	e := NewEngine()
	e.SetPricing(&ModelPricing{Model: "expensive", InputPer1K: 0.1})
	e.SetPricing(&ModelPricing{Model: "cheap", InputPer1K: 0.0001})
	e.SetPricing(&ModelPricing{Model: "mid", InputPer1K: 0.01})

	// Trigger recommendations to exercise cheapestModel
	for i := 0; i < 30; i++ {
		e.RecordCost(CostRecord{
			ID:          "r" + string(rune('a'+i%26)),
			Model:       "expensive",
			InputTokens: 1000,
			CostUSD:     1.0,
		})
	}
	recs := e.GetRecommendations()
	if len(recs) == 0 {
		t.Error("expected recommendations")
	}
}

func TestGetPricing_ReturnsCopy(t *testing.T) {
	e := NewEngine()
	p := e.GetPricing("gpt-4o")
	if p == nil {
		t.Fatal("expected pricing")
	}
	p.InputPer1K = 999.0
	p2 := e.GetPricing("gpt-4o")
	if p2.InputPer1K == 999.0 {
		t.Error("GetPricing should return a copy, not a reference")
	}
}

func TestRecordCost_MultipleBudgetsReset(t *testing.T) {
	e := NewEngine()
	e.SetBudget(&Budget{ID: "b1", LimitUSD: 100, ResetAt: time.Now().Add(-1 * time.Hour), AlertAt: 0.8, Period: "daily"})
	e.SetBudget(&Budget{ID: "b2", LimitUSD: 200, ResetAt: time.Now().Add(1 * time.Hour), AlertAt: 0.9, Period: "monthly"})

	e.RecordCost(CostRecord{ID: "r1", CostUSD: 10.0})

	b1, _ := e.CheckBudget("b1")
	if b1.SpentUSD != 10.0 {
		t.Errorf("b1: expected 10.0, got %f", b1.SpentUSD)
	}
	b2, _ := e.CheckBudget("b2")
	if b2.SpentUSD != 10.0 {
		t.Errorf("b2: expected 10.0, got %f", b2.SpentUSD)
	}
}

func TestRecordCost_RecordsSliceGrows(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 50; i++ {
		e.RecordCost(CostRecord{
			ID:        "r" + string(rune('a'+i%26)),
			Model:     "gpt-4o",
			CostUSD:   0.01,
			CreatedAt: time.Now(),
		})
	}
	if e.TotalRecords() != 50 {
		t.Errorf("expected 50 records, got %d", e.TotalRecords())
	}
}

func TestForecastCost_NegativeSlopeDivByZero(t *testing.T) {
	e := NewEngine()
	e.RecordCost(CostRecord{
		ID:        "r1",
		Model:     "gpt-4o",
		CostUSD:   0,
		CreatedAt: time.Now(),
	})
	f := e.ForecastCost(30)
	if f.PredictedCost != 0 {
		t.Errorf("expected 0 predicted cost with zero data, got %f", f.PredictedCost)
	}
}

func TestBudgetCheck_JustUnderAlertThreshold(t *testing.T) {
	e := NewEngine()
	e.SetBudget(&Budget{ID: "b1", LimitUSD: 100, SpentUSD: 79, AlertAt: 0.8})
	_, triggered := e.CheckBudget("b1")
	if triggered {
		t.Error("79% should not trigger 80% alert")
	}
}

func TestBudgetCheck_JustOverAlertThreshold(t *testing.T) {
	e := NewEngine()
	e.SetBudget(&Budget{ID: "b1", LimitUSD: 100, SpentUSD: 81, AlertAt: 0.8})
	_, triggered := e.CheckBudget("b1")
	if !triggered {
		t.Error("81% should trigger 80% alert")
	}
}

func TestTotalCost_MultipleRecords(t *testing.T) {
	e := NewEngine()
	e.RecordCost(CostRecord{ID: "r1", CostUSD: 0.1})
	e.RecordCost(CostRecord{ID: "r2", CostUSD: 0.2})
	e.RecordCost(CostRecord{ID: "r3", CostUSD: 0.3})
	if math.Abs(e.TotalCost()-0.6) > 1e-9 {
		t.Errorf("expected 0.6, got %f", e.TotalCost())
	}
}

func TestGetAnomalies_ReturnsCopy(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 20; i++ {
		e.RecordCost(CostRecord{ID: "r" + string(rune('a'+i)), Model: "gpt-4o", CostUSD: 0.01})
	}
	e.RecordCost(CostRecord{ID: "spike", Model: "gpt-4o", CostUSD: 10.0})
	anomalies := e.GetAnomalies()
	if len(anomalies) == 0 {
		t.Fatal("expected anomalies")
	}
	anomalies[0].Severity = "tampered"
	orig := e.GetAnomalies()
	if len(orig) > 0 && orig[0].Severity == "tampered" {
		t.Error("GetAnomalies should return a copy")
	}
}

func TestRecordCost_NegativeCostWithBudget(t *testing.T) {
	e := NewEngine()
	e.SetBudget(&Budget{ID: "b1", LimitUSD: 100, AlertAt: 0.8})
	e.RecordCost(CostRecord{ID: "r1", CostUSD: -10.0})
	b, _ := e.CheckBudget("b1")
	if b.SpentUSD != -10.0 {
		t.Errorf("expected -10.0 spent, got %f", b.SpentUSD)
	}
}

func TestForecastCost_ConfidenceCapped(t *testing.T) {
	e := NewEngine()
	for i := 0; i < 100; i++ {
		e.RecordCost(CostRecord{
			ID:        "r" + string(rune('a'+i%26)),
			Model:     "gpt-4o",
			CostUSD:   1.0,
			CreatedAt: time.Now().AddDate(0, 0, -99+i),
		})
	}
	f := e.ForecastCost(30)
	if f.Confidence > 1.0 {
		t.Errorf("confidence should not exceed 1.0, got %f", f.Confidence)
	}
}
