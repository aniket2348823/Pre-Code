package feedback

import (
	"context"
	"testing"
	"time"
)

func TestNewEngine(t *testing.T) {
	e := NewEngine(nil)
	if e == nil {
		t.Fatal("NewEngine should not return nil")
	}
	if e.TotalOutcomes() != 0 {
		t.Error("new engine should have 0 outcomes")
	}
}

func TestRecordOutcome(t *testing.T) {
	e := NewEngine(nil)
	o := Outcome{
		RequestID:  "req-1",
		UserID:     "user-1",
		Accepted:   true,
		Model:      "claude-sonnet-4-20250514",
		TaskType:   "code_generation",
		Score:      0.85,
		Cost:       0.02,
		TokensUsed: 1500,
		DurationMs: 1200,
		CreatedAt:  time.Now(),
	}

	e.RecordOutcome(context.Background(), o)

	if e.TotalOutcomes() != 1 {
		t.Errorf("expected 1 outcome, got %d", e.TotalOutcomes())
	}
	if e.AcceptRate() != 1.0 {
		t.Errorf("expected 100%% accept rate, got %f", e.AcceptRate())
	}
}

func TestRecordOutcomeRejected(t *testing.T) {
	e := NewEngine(nil)
	e.RecordOutcome(context.Background(), Outcome{
		Accepted: false,
		Model:    "gpt-4o",
		TaskType: "review",
		Score:    0.4,
	})

	if e.AcceptRate() != 0.0 {
		t.Errorf("expected 0%% accept rate, got %f", e.AcceptRate())
	}
}

func TestModelStats(t *testing.T) {
	e := NewEngine(nil)
	e.RecordOutcome(context.Background(), Outcome{Accepted: true, Model: "claude", TaskType: "code_generation", Score: 0.9, Cost: 0.01})
	e.RecordOutcome(context.Background(), Outcome{Accepted: true, Model: "claude", TaskType: "code_generation", Score: 0.8, Cost: 0.02})
	e.RecordOutcome(context.Background(), Outcome{Accepted: false, Model: "gpt-4o", TaskType: "code_generation", Score: 0.5, Cost: 0.01})

	stats := e.GetModelStats()
	if len(stats) != 2 {
		t.Errorf("expected 2 model stats, got %d", len(stats))
	}
	claude := stats["claude"]
	if claude == nil {
		t.Fatal("expected claude stats")
	}
	if claude.TotalRequests != 2 {
		t.Errorf("expected 2 requests, got %d", claude.TotalRequests)
	}
	if claude.AcceptRate != 1.0 {
		t.Errorf("expected 100%% accept rate, got %f", claude.AcceptRate)
	}
}

func TestTaskStats(t *testing.T) {
	e := NewEngine(nil)
	e.RecordOutcome(context.Background(), Outcome{Accepted: true, Model: "claude", TaskType: "code_generation", Score: 0.9})
	e.RecordOutcome(context.Background(), Outcome{Accepted: false, Model: "gpt-4o", TaskType: "review", Score: 0.4})

	stats := e.GetTaskStats()
	if len(stats) != 2 {
		t.Errorf("expected 2 task stats, got %d", len(stats))
	}
}

func TestGetBestModel(t *testing.T) {
	e := NewEngine(nil)
	// Need at least 5 requests per model
	for i := 0; i < 6; i++ {
		e.RecordOutcome(context.Background(), Outcome{Accepted: true, Model: "claude", TaskType: "code_generation", Score: 0.9})
	}
	for i := 0; i < 6; i++ {
		e.RecordOutcome(context.Background(), Outcome{Accepted: i < 3, Model: "gpt-4o", TaskType: "code_generation", Score: 0.7})
	}

	best := e.GetBestModel("code_generation")
	if best != "claude" {
		t.Errorf("expected claude as best model, got %s", best)
	}
}

func TestGetRecentOutcomes(t *testing.T) {
	e := NewEngine(nil)
	for i := 0; i < 10; i++ {
		e.RecordOutcome(context.Background(), Outcome{Accepted: true, Model: "claude", TaskType: "code_generation"})
	}

	recent := e.GetRecentOutcomes(3)
	if len(recent) != 3 {
		t.Errorf("expected 3 recent outcomes, got %d", len(recent))
	}

	// Request more than available
	all := e.GetRecentOutcomes(100)
	if len(all) != 10 {
		t.Errorf("expected 10 outcomes, got %d", len(all))
	}
}

func TestAcceptRateEmpty(t *testing.T) {
	e := NewEngine(nil)
	if e.AcceptRate() != 0 {
		t.Error("empty engine should have 0 accept rate")
	}
}

func TestDecayStats(t *testing.T) {
	e := NewEngine(nil)
	e.RecordOutcome(context.Background(), Outcome{Accepted: true, Model: "claude", TaskType: "code_generation", Score: 1.0})

	e.DecayStats(0.9)

	stats := e.GetModelStats()
	claude := stats["claude"]
	if claude == nil {
		t.Fatal("expected claude stats")
	}
	if claude.AcceptRate > 0.91 || claude.AcceptRate < 0.89 {
		t.Errorf("expected ~0.9 after decay, got %f", claude.AcceptRate)
	}
}

func TestConcurrentRecordOutcome(t *testing.T) {
	e := NewEngine(nil)
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func() {
			e.RecordOutcome(context.Background(), Outcome{
				Accepted: true,
				Model:    "claude",
				TaskType: "code_generation",
				Score:    0.8,
			})
			done <- true
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
	if e.TotalOutcomes() != 100 {
		t.Errorf("expected 100 outcomes, got %d", e.TotalOutcomes())
	}
}
