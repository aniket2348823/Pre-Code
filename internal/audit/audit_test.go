package audit

import (
	"testing"
)

func TestNewTrail(t *testing.T) {
	tr := NewTrail()
	if tr == nil {
		t.Fatal("expected non-nil trail")
	}
	if tr.Count() != 0 {
		t.Error("expected empty trail")
	}
}

func TestRecord(t *testing.T) {
	tr := NewTrail()
	tr.Record("user-1", "request.processed", "req-1", true, nil)
	if tr.Count() != 1 {
		t.Errorf("expected 1 entry, got %d", tr.Count())
	}
}

func TestRecordError(t *testing.T) {
	tr := NewTrail()
	tr.RecordError("user-1", "request.failed", "req-1", "timeout")
	entries := tr.Recent(1)
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	if entries[0].Success {
		t.Error("expected failed entry")
	}
	if entries[0].Error != "timeout" {
		t.Errorf("expected timeout error, got %s", entries[0].Error)
	}
}

func TestByActor(t *testing.T) {
	tr := NewTrail()
	tr.Record("user-1", "action", "res", true, nil)
	tr.Record("user-2", "action", "res", true, nil)
	tr.Record("user-1", "action2", "res", true, nil)
	entries := tr.ByActor("user-1")
	if len(entries) != 2 {
		t.Errorf("expected 2 entries for user-1, got %d", len(entries))
	}
}

func TestByAction(t *testing.T) {
	tr := NewTrail()
	tr.Record("user-1", "scan", "res", true, nil)
	tr.Record("user-1", "critique", "res", true, nil)
	tr.Record("user-1", "scan", "res", false, nil)
	entries := tr.ByAction("scan")
	if len(entries) != 2 {
		t.Errorf("expected 2 scan entries, got %d", len(entries))
	}
}

func TestRecent(t *testing.T) {
	tr := NewTrail()
	for i := 0; i < 10; i++ {
		tr.Record("user", "action", "res", true, nil)
	}
	recent := tr.Recent(3)
	if len(recent) != 3 {
		t.Errorf("expected 3 recent, got %d", len(recent))
	}
	// Request more than available
	all := tr.Recent(100)
	if len(all) != 10 {
		t.Errorf("expected 10, got %d", len(all))
	}
}

func TestRecordWithDetails(t *testing.T) {
	tr := NewTrail()
	details := map[string]interface{}{
		"model":  "gpt-4o",
		"cost":   0.01,
		"tokens": 1500,
	}
	tr.Record("user-1", "llm.call", "req-1", true, details)
	entries := tr.Recent(1)
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	if entries[0].Details["model"] != "gpt-4o" {
		t.Error("expected model detail")
	}
}

func TestConcurrentRecord(t *testing.T) {
	tr := NewTrail()
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func() {
			tr.Record("user", "action", "res", true, nil)
			done <- true
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
	if tr.Count() != 100 {
		t.Errorf("expected 100 entries, got %d", tr.Count())
	}
}
