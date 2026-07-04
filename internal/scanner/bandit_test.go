package scanner

import (
	"context"
	"testing"
)

func TestBanditNormalizesJSON(t *testing.T) {
	canned := `{"results":[{"filename":"snippet.py","issue_severity":"HIGH","issue_text":"Possible SQL injection","test_id":"B608","test_name":"hardcoded_sql_expressions","line_number":3,"code":"3 query = 'SELECT ' + x"}]}`
	fr := &fakeRunner{stdout: canned}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	if b.Name() != "bandit" || !b.Available() {
		t.Fatal("bandit analyzer name/availability wrong")
	}
	found, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x=1", Filename: "snippet.py"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("want 1 finding, got %d", len(found))
	}
	f := found[0]
	if f.RuleID != "B608" || f.Severity != SeverityHigh {
		t.Fatalf("bad normalization: %+v", f)
	}
	if f.Line != 3 || f.Analyzers[0] != "bandit" || f.Fingerprint == "" {
		t.Fatalf("bad fields: %+v", f)
	}
	if fr.gotName != "bandit" {
		t.Fatalf("expected to invoke bandit, got %s", fr.gotName)
	}
}

func TestBanditSkipsNonPython(t *testing.T) {
	b := NewBanditAnalyzer(&fakeRunner{stdout: `{"results":[]}`})
	b.exists = func() bool { return true }
	found, err := b.Analyze(context.Background(), Input{Language: "go", Code: "x"})
	if err != nil || len(found) != 0 {
		t.Fatalf("bandit should skip non-python: findings=%d err=%v", len(found), err)
	}
}

func TestBanditUnavailableWhenAbsent(t *testing.T) {
	b := NewBanditAnalyzer(nil)
	b.exists = func() bool { return false }
	if b.Available() {
		t.Fatal("bandit must report unavailable when binary absent")
	}
}
