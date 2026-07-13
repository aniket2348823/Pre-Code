package cost

import (
	"context"
	"math"
	"sync"
	"testing"
)

func TestCheckBudget_ZeroBudget(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 0)
	err := m.CheckBudget(context.Background(), "o1", "t1", 0.01)
	// budget=0 disables the check (budget > 0 is false in source)
	if err != nil {
		t.Errorf("zero budget should not trigger (budget > 0 check), got %v", err)
	}
}

func TestCheckBudget_NegativeProposedCost(t *testing.T) {
	m := NewBudgetManager(nil, 100, 100)
	m.SetOrgBudget("o1", 1.00)
	err := m.CheckBudget(context.Background(), "o1", "t1", -5.0)
	if err != nil {
		t.Errorf("negative proposed cost should pass, got %v", err)
	}
}

func TestCheckBudget_ExactBoundary(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 1.00)
	m.RecordCost("o1", "t1", 0.50)
	err := m.CheckBudget(context.Background(), "o1", "t1", 0.50)
	if err != nil {
		t.Errorf("exact boundary should pass (0.50 + 0.50 = 1.00, not > 1.00), got %v", err)
	}
}

func TestCheckBudget_JustOverBoundary(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 1.00)
	m.RecordCost("o1", "t1", 0.50)
	err := m.CheckBudget(context.Background(), "o1", "t1", 0.51)
	if err == nil {
		t.Error("0.50 + 0.51 = 1.01 > 1.00 should trigger budget exceeded")
	}
}

func TestCheckBudget_NoBudgetSet(t *testing.T) {
	m := NewBudgetManager(nil, 100, 100)
	err := m.CheckBudget(context.Background(), "o1", "t1", 50.0)
	if err != nil {
		t.Errorf("should use default budget, got %v", err)
	}
}

// TestRecordCost_NegativeCost verifies that negative costs are recorded
// but do NOT trigger budget exceeded (budget check skips when usage + proposed < budget).
func TestRecordCost_NegativeCost(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.RecordCost("o1", "t1", -10.0)
	usage := m.GetUsage("o1")
	if usage != -10.0 {
		t.Errorf("expected usage -10.0, got %f", usage)
	}
}

func TestRecordCost_ZeroCost(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.RecordCost("o1", "t1", 0)
	usage := m.GetUsage("o1")
	if usage != 0 {
		t.Errorf("expected usage 0, got %f", usage)
	}
}

func TestResetUsage(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.RecordCost("o1", "t1", 5.0)
	m.ResetUsage()
	usage := m.GetUsage("o1")
	if usage != 0 {
		t.Errorf("expected usage 0 after reset, got %f", usage)
	}
}

func TestConcurrentRecordCost(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 10000)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.RecordCost("o1", "t1", 0.01)
		}()
	}
	wg.Wait()
	usage := m.GetUsage("o1")
	if math.Abs(usage-1.0) > 0.001 {
		t.Errorf("expected usage ~1.0 (100 * 0.01), got %f", usage)
	}
}

func TestGetSnapshot(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 10.0)
	m.RecordCost("o1", "t1", 5.0)
	snap := m.GetSnapshot()
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	usage, ok := snap["usage"].(map[string]float64)
	if !ok {
		t.Fatal("expected usage map")
	}
	if usage["org:o1"] != 5.0 {
		t.Errorf("expected org:o1 usage 5.0, got %f", usage["org:o1"])
	}
}

// TestTaskBudgetIndependentOfOrgBudget verifies that task budgets are tracked
// independently from org budgets. RecordCost records against both, but each
// budget type has its own limit and usage counter.
func TestTaskBudgetIndependentOfOrgBudget(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 2.00) // generous org budget
	m.SetTaskBudget("t1", 0.50) // tight task budget
	m.RecordCost("o1", "t1", 0.30)

	// Task budget: 0.30 + 0.30 = 0.60 > 0.50 → exceeded
	err := m.CheckBudget(context.Background(), "o1", "t1", 0.30)
	if err == nil {
		t.Error("task budget should be exceeded")
	}

	// Org budget still OK: 0.30 + 0.10 = 0.40 < 2.00
	err = m.CheckBudget(context.Background(), "o1", "t2", 0.10)
	if err != nil {
		t.Errorf("org budget should not be exceeded, got %v", err)
	}
}
