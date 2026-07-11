package agent

import (
	"testing"
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

	// First failure — retry count increments, stays in executing
	if err := sm.Transition(task, EventStepFailed); err != nil {
		t.Fatal(err)
	}
	if task.State != StateExecuting {
		t.Errorf("after first failure: state = %v, want executing", task.State)
	}
	if task.RetryCount != 1 {
		t.Errorf("after first failure: retry_count = %d, want 1", task.RetryCount)
	}

	// Second failure — retry count hits max, moves to failed
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
