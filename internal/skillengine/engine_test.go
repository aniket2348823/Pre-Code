package skillengine

import "testing"

func TestExtractFromFinding_New(t *testing.T) {
	e := NewEngine()
	f := Finding{Severity: "critical", Message: "sql injection detected", Fix: "use parameterized queries", Analyzers: []string{"builtin"}}
	skill, created := e.ExtractFromFinding(f)
	if !created {
		t.Fatal("expected newly created skill")
	}
	if skill.UsageCount != 1 {
		t.Fatalf("expected usage 1, got %d", skill.UsageCount)
	}
}

func TestExtractFromFinding_Update(t *testing.T) {
	e := NewEngine()
	f := Finding{Severity: "critical", Message: "sql injection detected", Fix: "use parameterized queries", Analyzers: []string{"builtin"}}
	e.ExtractFromFinding(f)
	skill, created := e.ExtractFromFinding(f)
	if created {
		t.Fatal("expected existing skill to be updated, not created")
	}
	if skill.UsageCount != 2 {
		t.Fatalf("expected usage 2, got %d", skill.UsageCount)
	}
}

func TestRecordOutcome(t *testing.T) {
	e := NewEngine()
	f := Finding{Severity: "high", Message: "hardcoded password", Fix: "use env vars"}
	skill, _ := e.ExtractFromFinding(f)
	e.RecordOutcome(skill.ID, true)
	if skill.SuccessRate <= 0 {
		t.Fatal("success rate should increase after acceptance")
	}
}

func TestRank(t *testing.T) {
	e := NewEngine()
	e.ExtractFromFinding(Finding{Message: "a", Confidence: 0.9})
	e.ExtractFromFinding(Finding{Message: "b", Confidence: 0.5})
	rank := e.Rank()
	if len(rank) != 2 {
		t.Fatalf("expected 2 ranked skills, got %d", len(rank))
	}
	if rank[0].Score < rank[1].Score {
		t.Fatal("higher confidence skill should rank first")
	}
}

func TestCount(t *testing.T) {
	e := NewEngine()
	e.ExtractFromFinding(Finding{Message: "x"})
	e.ExtractFromFinding(Finding{Message: "y"})
	if e.Count() != 2 {
		t.Fatalf("expected 2 skills, got %d", e.Count())
	}
}

func TestFindByTrigger(t *testing.T) {
	e := NewEngine()
	e.ExtractFromFinding(Finding{Message: "sql injection found"})
	s, ok := e.FindByTrigger("sql injection found")
	if !ok || s == nil {
		t.Fatal("expected to find skill by trigger")
	}
}
