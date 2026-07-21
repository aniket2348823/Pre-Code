package costintel

import (
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
	// gpt-4o: $0.0025/1K input, $0.01/1K output
	cost := e.CalculateCost("gpt-4o", 1000, 500, 0)
	expected := 0.0025 + 0.005 // input + output
	if cost != expected {
		t.Errorf("expected cost %f, got %f", expected, cost)
	}
}

func TestCalculateCostWithCache(t *testing.T) {
	e := NewEngine()
	// gpt-4o-mini: $0.00015/1K input, $0.0006/1K output, $0.000075/1K cached
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
	// With 30 days of $1/day data, prediction should be roughly $30
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
	// Record 20 normal-cost calls
	for i := 0; i < 20; i++ {
		e.RecordCost(CostRecord{
			ID:      "r" + string(rune('a'+i)),
			Model:   "gpt-4o",
			CostUSD: 0.01,
		})
	}
	// Record one anomalously expensive call
	e.RecordCost(CostRecord{
		ID:      "anomaly",
		Model:   "gpt-4o",
		CostUSD: 1.0, // 100x normal
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
