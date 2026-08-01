package cost

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

// fakeUsageStore is an in-memory UsageStore for testing persistence wiring
// without a database.
type fakeUsageStore struct {
	mu      sync.Mutex
	data    map[string]float64
	addErr  error
	loadErr error
}

func newFakeStore(seed map[string]float64) *fakeUsageStore {
	d := make(map[string]float64)
	for k, v := range seed {
		d[k] = v
	}
	return &fakeUsageStore{data: d}
}

func (s *fakeUsageStore) LoadUsage(_ context.Context) (map[string]float64, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]float64, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out, nil
}

func (s *fakeUsageStore) AddUsage(_ context.Context, key string, delta float64) error {
	if s.addErr != nil {
		return s.addErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] += delta
	return nil
}

func TestCheckBudget_EnforcesOrgLimit(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 1.00)
	m.RecordCost("o1", "t1", 0.80)

	err := m.CheckBudget(context.Background(), "o1", "t1", 0.30)
	if err == nil {
		t.Fatal("expected budget exceeded error")
	}
	var be *BudgetExceededError
	if !asBudgetErr(err, &be) || be.Type != "org" {
		t.Fatalf("expected org BudgetExceededError, got %v", err)
	}

	if err := m.CheckBudget(context.Background(), "o1", "t1", 0.10); err != nil {
		t.Fatalf("expected within-budget check to pass, got %v", err)
	}
}

func TestBudget_LoadsPersistedUsageOnCheck(t *testing.T) {
	store := newFakeStore(map[string]float64{"org:o1": 0.95})
	m := NewBudgetManager(nil, 0, 0)
	m.SetStore(store)
	m.SetOrgBudget("o1", 1.00)

	err := m.CheckBudget(context.Background(), "o1", "t1", 0.10)
	if err == nil {
		t.Fatal("expected persisted usage to be loaded and enforced after restart")
	}
}

func TestRecordCost_PersistsToStore(t *testing.T) {
	store := newFakeStore(nil)
	m := NewBudgetManager(nil, 0, 0)
	m.SetStore(store)

	m.RecordCost("o1", "t1", 0.25)

	got, _ := store.LoadUsage(context.Background())
	if got["org:o1"] != 0.25 {
		t.Fatalf("expected org usage 0.25 persisted, got %v", got["org:o1"])
	}
	if got["task:t1"] != 0.25 {
		t.Fatalf("expected task usage 0.25 persisted, got %v", got["task:t1"])
	}
}

func TestOnExceeded_CallbackFires(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 1.00)
	m.RecordCost("o1", "t1", 0.80)

	var fired bool
	var gotErr *BudgetExceededError
	m.OnExceeded(func(ctx context.Context, err *BudgetExceededError) {
		fired = true
		gotErr = err
	})

	err := m.CheckBudget(context.Background(), "o1", "t1", 0.30)
	if err == nil {
		t.Fatal("expected budget exceeded error")
	}
	if !fired {
		t.Fatal("expected onExceeded callback to fire")
	}
	if gotErr == nil || gotErr.Type != "org" {
		t.Fatalf("expected org BudgetExceededError in callback, got %v", gotErr)
	}

	fired = false
	if err := m.CheckBudget(context.Background(), "o1", "t1", 0.05); err != nil {
		t.Fatalf("expected within-budget check to pass, got %v", err)
	}
	if fired {
		t.Fatal("callback should not fire when within budget")
	}
}

func asBudgetErr(err error, target **BudgetExceededError) bool {
	be, ok := err.(*BudgetExceededError)
	if ok {
		*target = be
	}
	return ok
}

// --- Deep tests merged below ---

func TestCheckBudget_ZeroBudget(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 0)
	err := m.CheckBudget(context.Background(), "o1", "t1", 0.01)
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

func TestTaskBudgetIndependentOfOrgBudget(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 2.00)
	m.SetTaskBudget("t1", 0.50)
	m.RecordCost("o1", "t1", 0.30)

	err := m.CheckBudget(context.Background(), "o1", "t1", 0.30)
	if err == nil {
		t.Error("task budget should be exceeded")
	}

	err = m.CheckBudget(context.Background(), "o1", "t2", 0.10)
	if err != nil {
		t.Errorf("org budget should not be exceeded, got %v", err)
	}
}

// --- New tests to boost coverage to 95%+ ---

func TestBudgetExceededError_Error(t *testing.T) {
	be := &BudgetExceededError{
		Type:     "org",
		ID:       "org-123",
		Usage:    75.50,
		Budget:   100.00,
		Proposed: 30.00,
	}
	msg := be.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	if fmt.Sprintf("%s", be) == "" {
		t.Error("Error() should return non-empty string")
	}

	be2 := &BudgetExceededError{
		Type:     "task",
		ID:       "task-456",
		Usage:    10.00,
		Budget:   5.00,
		Proposed: 1.00,
	}
	msg2 := be2.Error()
	if msg2 == "" {
		t.Error("expected non-empty error message for task")
	}
}

func TestEnsureLoaded_AlreadyLoaded(t *testing.T) {
	store := newFakeStore(map[string]float64{"org:o1": 5.0})
	m := NewBudgetManager(nil, 0, 0)
	m.SetStore(store)

	// First call loads
	m.CheckBudget(context.Background(), "o1", "t1", 0.01)
	// Second call should use cached data
	m.CheckBudget(context.Background(), "o1", "t1", 0.01)
}

func TestEnsureLoaded_LoadError(t *testing.T) {
	store := &fakeUsageStore{loadErr: fmt.Errorf("db connection failed")}
	m := NewBudgetManager(nil, 0, 0)
	m.SetStore(store)
	m.SetOrgBudget("o1", 100.0)

	// Should not panic on load error — just leave loaded=false and continue
	err := m.CheckBudget(context.Background(), "o1", "t1", 10.0)
	if err != nil {
		t.Errorf("CheckBudget should pass when load fails (no persisted data), got %v", err)
	}
}

func TestEnsureLoaded_NilStore(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	// store is nil, ensureLoaded should early-return
	err := m.CheckBudget(context.Background(), "o1", "t1", 10.0)
	if err != nil {
		t.Errorf("CheckBudget with nil store should pass, got %v", err)
	}
}

func TestRecordCost_StoreAddError(t *testing.T) {
	store := &fakeUsageStore{addErr: fmt.Errorf("disk full")}
	m := NewBudgetManager(nil, 0, 0)
	m.SetStore(store)

	// Should not panic — addErr is logged, not propagated
	m.RecordCost("o1", "t1", 1.00)

	usage := m.GetUsage("o1")
	if usage != 1.00 {
		t.Errorf("expected in-memory usage 1.00 even with store error, got %f", usage)
	}
}

func TestRecordCost_ZeroCostSkipsStore(t *testing.T) {
	store := newFakeStore(nil)
	m := NewBudgetManager(nil, 0, 0)
	m.SetStore(store)

	// cost=0 should skip the persist path
	m.RecordCost("o1", "t1", 0)

	got, _ := store.LoadUsage(context.Background())
	if got["org:o1"] != 0 {
		t.Errorf("expected no store write for zero cost, got %v", got["org:o1"])
	}
}

func TestRecordCost_NilStoreSkipsPersist(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	// store is nil, should skip persist without panic
	m.RecordCost("o1", "t1", 5.0)
	usage := m.GetUsage("o1")
	if usage != 5.0 {
		t.Errorf("expected usage 5.0, got %f", usage)
	}
}

func TestSetStore_ResetsLoaded(t *testing.T) {
	store1 := newFakeStore(map[string]float64{"org:o1": 10.0})
	m := NewBudgetManager(nil, 0, 0)
	m.SetStore(store1)

	// Load from store1
	m.CheckBudget(context.Background(), "o1", "t1", 0.01)
	// Set a new store with different data
	store2 := newFakeStore(map[string]float64{"org:o1": 20.0})
	m.SetStore(store2)
	// Should reload from store2
	m.CheckBudget(context.Background(), "o1", "t1", 0.01)
	usage := m.GetUsage("o1")
	if usage != 20.0 {
		t.Errorf("expected usage from store2 (20.0), got %f", usage)
	}
}

func TestGetSnapshot_FullContents(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 50.0)
	m.SetOrgBudget("o2", 75.0)
	m.SetTaskBudget("t1", 10.0)
	m.RecordCost("o1", "t1", 5.0)
	m.RecordCost("o2", "t2", 15.0)

	snap := m.GetSnapshot()
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}

	usage := snap["usage"].(map[string]float64)
	if len(usage) != 4 {
		t.Errorf("expected 4 usage entries, got %d", len(usage))
	}

	orgBudgets := snap["org_budgets"].(map[string]float64)
	if len(orgBudgets) != 2 {
		t.Errorf("expected 2 org budgets, got %d", len(orgBudgets))
	}

	if _, ok := snap["updated_at"].(time.Time); !ok {
		t.Error("expected updated_at to be time.Time")
	}
}

func TestCheckBudget_BothOrgAndTaskExceeded(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 1.00)
	m.SetTaskBudget("t1", 0.50)
	m.RecordCost("o1", "t1", 0.80)

	// This should exceed both budgets — org checked first
	err := m.CheckBudget(context.Background(), "o1", "t1", 0.30)
	if err == nil {
		t.Fatal("expected budget exceeded")
	}
	var be *BudgetExceededError
	if !asBudgetErr(err, &be) {
		t.Fatal("expected BudgetExceededError")
	}
	// Org should be checked first
	if be.Type != "org" {
		t.Errorf("expected org budget exceeded first, got %s", be.Type)
	}
}

func TestCheckBudget_TaskExceededButOrgOK(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetOrgBudget("o1", 100.0)
	m.SetTaskBudget("t1", 1.00)
	m.RecordCost("o1", "t1", 0.90)

	err := m.CheckBudget(context.Background(), "o1", "t1", 0.20)
	if err == nil {
		t.Fatal("expected task budget exceeded")
	}
	var be *BudgetExceededError
	if !asBudgetErr(err, &be) || be.Type != "task" {
		t.Fatalf("expected task BudgetExceededError, got %v", err)
	}
}

func TestOnExceeded_TaskCallback(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.SetTaskBudget("t1", 1.00)
	m.RecordCost("o1", "t1", 0.90)

	var fired bool
	var gotErr *BudgetExceededError
	m.OnExceeded(func(ctx context.Context, err *BudgetExceededError) {
		fired = true
		gotErr = err
	})

	err := m.CheckBudget(context.Background(), "o1", "t1", 0.20)
	if err == nil {
		t.Fatal("expected task budget exceeded")
	}
	if !fired {
		t.Fatal("expected callback to fire for task exceeded")
	}
	if gotErr.Type != "task" {
		t.Errorf("expected task type in callback, got %s", gotErr.Type)
	}
}

func TestGetOrgBudget_DefaultAndCustom(t *testing.T) {
	m := NewBudgetManager(nil, 50.0, 25.0)
	if got := m.GetOrgBudget("unknown"); got != 50.0 {
		t.Errorf("expected default 50.0, got %f", got)
	}
	m.SetOrgBudget("known", 100.0)
	if got := m.GetOrgBudget("known"); got != 100.0 {
		t.Errorf("expected 100.0 for known org, got %f", got)
	}
}

func TestGetTaskBudget_DefaultAndCustom(t *testing.T) {
	m := NewBudgetManager(nil, 50.0, 25.0)
	if got := m.GetTaskBudget("unknown"); got != 25.0 {
		t.Errorf("expected default 25.0, got %f", got)
	}
	m.SetTaskBudget("known", 75.0)
	if got := m.GetTaskBudget("known"); got != 75.0 {
		t.Errorf("expected 75.0 for known task, got %f", got)
	}
}

func TestRecordCost_MultipleCalls(t *testing.T) {
	m := NewBudgetManager(nil, 0, 0)
	m.RecordCost("o1", "t1", 1.0)
	m.RecordCost("o1", "t1", 2.0)
	m.RecordCost("o1", "t2", 3.0)

	if got := m.GetUsage("o1"); got != 6.0 {
		t.Errorf("expected org usage 6.0, got %f", got)
	}
}

func TestRecordCost_PersistenceWithStore(t *testing.T) {
	store := newFakeStore(nil)
	m := NewBudgetManager(nil, 0, 0)
	m.SetStore(store)

	m.RecordCost("o1", "t1", 1.0)
	m.RecordCost("o1", "t1", 2.0)

	got, _ := store.LoadUsage(context.Background())
	if got["org:o1"] != 3.0 {
		t.Errorf("expected persisted org usage 3.0, got %v", got["org:o1"])
	}
	if got["task:t1"] != 3.0 {
		t.Errorf("expected persisted task usage 3.0, got %v", got["task:t1"])
	}
}

func TestNewBudgetManager_NilPool(t *testing.T) {
	m := NewBudgetManager(nil, 10.0, 5.0)
	if m.defaultOrg != 10.0 {
		t.Errorf("expected default org 10.0, got %f", m.defaultOrg)
	}
	if m.defaultTask != 5.0 {
		t.Errorf("expected default task 5.0, got %f", m.defaultTask)
	}
	if m.store != nil {
		t.Error("expected nil store with nil pool")
	}
}
