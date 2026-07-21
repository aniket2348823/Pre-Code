package costintel

import (
	"math"
	"sync"
	"testing"
	"time"
)

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

func TestCalculateCost_UnknownModel(t *testing.T) {
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
	// Record normal costs
	for i := 0; i < 20; i++ {
		e.RecordCost(CostRecord{
			ID:        "r" + string(rune('a'+i)),
			Model:     "gpt-4o",
			CostUSD:   0.01,
			CreatedAt: time.Now().AddDate(0, 0, -20+i),
		})
	}
	// Record spike
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
