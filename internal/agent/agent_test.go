package agent

import (
	"sync"
	"testing"
	"time"
)

func TestStateMachine_Transitions(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		name      string
		from      TaskState
		event     Event
		wantState TaskState
		wantErr   bool
	}{
		{"pending -> start -> planning", StatePending, EventStart, StatePlanning, false},
		{"pending -> cancel -> cancelled", StatePending, EventCancel, StateCancelled, false},
		{"planning -> plan_ready -> executing", StatePlanning, EventPlanReady, StateExecuting, false},
		{"planning -> step_failed -> failed", StatePlanning, EventStepFailed, StateFailed, false},
		{"executing -> hitl_required -> waiting_hitl", StateExecuting, EventHITLRequired, StateWaitingHITL, false},
		{"executing -> step_complete stays executing", StateExecuting, EventStepComplete, StateExecuting, false},
		{"executing -> cancel -> cancelled", StateExecuting, EventCancel, StateCancelled, false},
		{"waiting_hitl -> hitl_approved -> executing", StateWaitingHITL, EventHITLApproved, StateExecuting, false},
		{"waiting_hitl -> hitl_rejected -> failed", StateWaitingHITL, EventHITLRejected, StateFailed, false},
		{"reviewing -> review_passed -> completed", StateReviewing, EventReviewPassed, StateCompleted, false},
		{"reviewing -> review_failed -> executing", StateReviewing, EventReviewFailed, StateExecuting, false},
		{"pending -> step_complete -> error", StatePending, EventStepComplete, StatePending, true},
		{"completed -> start -> error", StateCompleted, EventStart, StateCompleted, true},
		{"cancelled -> cancel -> error", StateCancelled, EventCancel, StateCancelled, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{
				State:      tt.from,
				Plan:       &Plan{TotalSteps: 5},
				MaxRetries: 3,
			}
			err := sm.Transition(task, tt.event)
			if (err != nil) != tt.wantErr {
				t.Errorf("Transition() error = %v, wantErr %v", err, tt.wantErr)
			}
			if task.State != tt.wantState {
				t.Errorf("Transition() state = %v, want %v", task.State, tt.wantState)
			}
		})
	}
}

func TestStateMachine_UpdatedAt(t *testing.T) {
	sm := NewStateMachine()
	task := &Task{State: StatePending, Plan: &Plan{TotalSteps: 1}}

	if err := sm.Transition(task, EventStart); err != nil {
		t.Fatal(err)
	}
	if task.UpdatedAt.IsZero() {
		t.Error("UpdatedAt not set after transition")
	}
}

func TestIsTerminal(t *testing.T) {
	terminalStates := []TaskState{StateCompleted, StateFailed, StateCancelled}
	for _, state := range terminalStates {
		if !IsTerminal(state) {
			t.Errorf("IsTerminal(%v) = false, want true", state)
		}
	}
	nonTerminalStates := []TaskState{StatePending, StatePlanning, StateExecuting, StateWaitingHITL, StateReviewing}
	for _, state := range nonTerminalStates {
		if IsTerminal(state) {
			t.Errorf("IsTerminal(%v) = true, want false", state)
		}
	}
}

func TestValidTransitions(t *testing.T) {
	sm := NewStateMachine()

	pendingEvents := sm.ValidTransitions(StatePending)
	if len(pendingEvents) != 2 {
		t.Errorf("StatePending valid transitions = %d, want 2", len(pendingEvents))
	}

	executingEvents := sm.ValidTransitions(StateExecuting)
	if len(executingEvents) != 5 {
		t.Errorf("StateExecuting valid transitions = %d, want 5", len(executingEvents))
	}

	completedEvents := sm.ValidTransitions(StateCompleted)
	if len(completedEvents) != 0 {
		t.Errorf("StateCompleted valid transitions = %d, want 0", len(completedEvents))
	}
}

func TestStateMachine_RetryCount(t *testing.T) {
	sm := NewStateMachine()
	task := &Task{
		State:      StateExecuting,
		Plan:       &Plan{TotalSteps: 5},
		MaxRetries: 2,
		RetryCount: 0,
	}

	if err := sm.Transition(task, EventStepFailed); err != nil {
		t.Fatal(err)
	}
	if task.State != StateExecuting {
		t.Errorf("after first failure: state = %v, want executing", task.State)
	}
	if task.RetryCount != 1 {
		t.Errorf("after first failure: retry_count = %d, want 1", task.RetryCount)
	}

	if err := sm.Transition(task, EventStepFailed); err != nil {
		t.Fatal(err)
	}
	if task.State != StateFailed {
		t.Errorf("after max retries: state = %v, want failed", task.State)
	}
}

func TestBuildDefaultPlan(t *testing.T) {
	a := &Agent{maxIter: 20}
	task := &Task{
		ID:          "test-1",
		Title:       "Fix the bug",
		Description: "Fix the authentication bug",
	}

	plan := a.buildDefaultPlan(task)
	if plan == nil {
		t.Fatal("buildDefaultPlan returned nil")
	}
	if plan.TotalSteps != 5 {
		t.Errorf("plan.TotalSteps = %d, want 5", plan.TotalSteps)
	}
	if len(plan.Steps) != 5 {
		t.Errorf("plan.Steps length = %d, want 5", len(plan.Steps))
	}

	expectedTools := []string{"list_directory", "search_code", "read_file", "edit_file", "run_command"}
	for i, step := range plan.Steps {
		if step.Tool != expectedTools[i] {
			t.Errorf("step %d tool = %q, want %q", i, step.Tool, expectedTools[i])
		}
		if step.Index != i {
			t.Errorf("step %d index = %d", i, step.Index)
		}
	}
}

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
	_ = task.State
}

func TestStateTransition_NilPlan(t *testing.T) {
	sm := NewStateMachine()
	task := &Task{State: StatePlanning, MaxRetries: 3}
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
