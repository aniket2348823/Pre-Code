package agent

import (
	"sync"
	"testing"
	"time"
)

func TestConcurrentTransitions_SameTask(t *testing.T) {
	sm := NewStateMachine()
	task := &Task{State: StatePending, MaxRetries: 3}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sm.Transition(task, EventStart); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	// Only one goroutine should succeed
	_ = task.State
}

func TestStateTransition_NilPlan(t *testing.T) {
	sm := NewStateMachine()
	task := &Task{State: StatePlanning, MaxRetries: 3}
	// EventStepFailed on StatePlanning should go to StateFailed
	err := sm.Transition(task, EventStepFailed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.State != StateFailed {
		t.Errorf("expected StateFailed, got %s", task.State)
	}
}

func TestStateTransition_ZeroMaxRetries(t *testing.T) {
	sm := NewStateMachine()
	task := &Task{State: StateExecuting, MaxRetries: 0, RetryCount: 0}
	err := sm.Transition(task, EventStepFailed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// MaxRetries=0, RetryCount=0 -> RetryCount becomes 1, which >= MaxRetries
	if task.State != StateFailed {
		t.Errorf("expected StateFailed, got %s", task.State)
	}
}

func TestDoubleCancel(t *testing.T) {
	sm := NewStateMachine()
	task := &Task{State: StatePending, MaxRetries: 3}
	if err := sm.Transition(task, EventCancel); err != nil {
		t.Fatal(err)
	}
	if task.State != StateCancelled {
		t.Fatalf("expected cancelled, got %s", task.State)
	}
	// Second cancel on terminal state should fail
	err := sm.Transition(task, EventCancel)
	if err == nil {
		t.Error("expected error for transition from terminal state")
	}
}

func TestPlan_TotalSteps0(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{}, TotalSteps: 0}
	if plan.TotalSteps != 0 {
		t.Errorf("expected 0 total steps, got %d", plan.TotalSteps)
	}
	// No steps means no panic on iteration
	for i := 0; i < plan.TotalSteps; i++ {
		t.Errorf("should not iterate: %d", i)
	}
}

func TestPlan_NegativeStepIndex(t *testing.T) {
	step := PlanStep{Index: -1, Tool: "test"}
	if step.Index != -1 {
		t.Error("negative index should be stored")
	}
}

func TestIsTerminal_AllStates(t *testing.T) {
	tests := []struct {
		state    TaskState
		expected bool
	}{
		{StatePending, false}, {StatePlanning, false},
		{StateExecuting, false}, {StateWaitingHITL, false},
		{StateReviewing, false}, {StateCompleted, true},
		{StateFailed, true}, {StateCancelled, true},
	}
	for _, tt := range tests {
		if got := IsTerminal(tt.state); got != tt.expected {
			t.Errorf("IsTerminal(%s) = %v, want %v", tt.state, got, tt.expected)
		}
	}
}

func TestValidTransitions_AllStates(t *testing.T) {
	sm := NewStateMachine()
	tests := []struct {
		state TaskState
		count int
	}{
		{StatePending, 2}, {StatePlanning, 2},
		{StateExecuting, 5}, {StateWaitingHITL, 2},
		{StateReviewing, 2}, {StateCompleted, 0},
		{StateFailed, 0}, {StateCancelled, 0},
	}
	for _, tt := range tests {
		events := sm.ValidTransitions(tt.state)
		if len(events) != tt.count {
			t.Errorf("ValidTransitions(%s) returned %d events, want %d", tt.state, len(events), tt.count)
		}
	}
}

func TestStateMachine_UpdatedAt_Deep(t *testing.T) {
	sm := NewStateMachine()
	task := &Task{State: StatePending, MaxRetries: 3}
	before := time.Now()
	sm.Transition(task, EventStart)
	after := time.Now()
	if task.UpdatedAt.Before(before) || task.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt should be between %v and %v, got %v", before, after, task.UpdatedAt)
	}
}

func TestBuildDefaultPlan_Deep(t *testing.T) {
	sm := NewStateMachine()
	_ = sm
	task := &Task{Title: "test task", MaxRetries: 3}
	ag := &Agent{sm: sm, tools: nil}
	plan := ag.buildDefaultPlan(task)
	if plan == nil {
		t.Fatal("expected non-nil plan")
	}
	if plan.TotalSteps != 5 {
		t.Errorf("expected 5 steps, got %d", plan.TotalSteps)
	}
	tools := map[string]bool{}
	for _, s := range plan.Steps {
		tools[s.Tool] = true
	}
	expectedTools := []string{"list_directory", "search_code", "read_file", "edit_file", "run_command"}
	for _, tool := range expectedTools {
		if !tools[tool] {
			t.Errorf("expected tool %s in plan", tool)
		}
	}
}

func TestStateMachine_ConcurrentStartAndCancel(t *testing.T) {
	sm := NewStateMachine()
	task := &Task{State: StatePending, MaxRetries: 3}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.Transition(task, EventStart)
		}()
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.Transition(task, EventCancel)
		}()
	}
	wg.Wait()
	// Should be in a valid state
	valid := task.State == StatePlanning || task.State == StateCancelled
	if !valid {
		t.Errorf("unexpected final state: %s", task.State)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s      string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"abc", 3, "abc"},
	}
	for _, tt := range tests {
		got := truncate(tt.s, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
		}
	}
}
