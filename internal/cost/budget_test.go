package cost

import (
	"context"
	"sync"
	"testing"
)

// fakeUsageStore is an in-memory UsageStore for testing persistence wiring
// without a database.
type fakeUsageStore struct {
	mu     sync.Mutex
	data   map[string]float64
	addErr error
}

func newFakeStore(seed map[string]float64) *fakeUsageStore {
	d := make(map[string]float64)
	for k, v := range seed {
		d[k] = v
	}
	return &fakeUsageStore{data: d}
}

func (s *fakeUsageStore) LoadUsage(_ context.Context) (map[string]float64, error) {
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

	// 0.80 already spent; a 0.30 proposal would exceed the $1.00 org budget.
	err := m.CheckBudget(context.Background(), "o1", "t1", 0.30)
	if err == nil {
		t.Fatal("expected budget exceeded error")
	}
	var be *BudgetExceededError
	if !asBudgetErr(err, &be) || be.Type != "org" {
		t.Fatalf("expected org BudgetExceededError, got %v", err)
	}

	// A 0.10 proposal stays within budget.
	if err := m.CheckBudget(context.Background(), "o1", "t1", 0.10); err != nil {
		t.Fatalf("expected within-budget check to pass, got %v", err)
	}
}

func TestBudget_LoadsPersistedUsageOnCheck(t *testing.T) {
	// Simulate a restart: usage already persisted from a previous process.
	store := newFakeStore(map[string]float64{"org:o1": 0.95})
	m := NewBudgetManager(nil, 0, 0)
	m.SetStore(store)
	m.SetOrgBudget("o1", 1.00)

	// Even though this process recorded nothing, the persisted 0.95 must count,
	// so a 0.10 proposal is rejected.
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

	// This should exceed the budget and fire the callback.
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

	// Reset and verify callback does NOT fire when within budget.
	fired = false
	if err := m.CheckBudget(context.Background(), "o1", "t1", 0.05); err != nil {
		t.Fatalf("expected within-budget check to pass, got %v", err)
	}
	if fired {
		t.Fatal("callback should not fire when within budget")
	}
}

// asBudgetErr is a tiny errors.As shim to avoid importing errors in every test.
func asBudgetErr(err error, target **BudgetExceededError) bool {
	be, ok := err.(*BudgetExceededError)
	if ok {
		*target = be
	}
	return ok
}
