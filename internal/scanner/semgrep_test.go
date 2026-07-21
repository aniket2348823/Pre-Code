package scanner

import (
	"context"
	"testing"
)

func TestSemgrepNormalizesJSON(t *testing.T) {
	canned := `{"results":[{"check_id":"python.lang.security.audit.exec-detected","path":"snippet.py","start":{"line":5},"extra":{"message":"Detected exec() usage","severity":"ERROR","lines":"exec(user_input)","metadata":{"category":"security"}}}]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	found, err := s.Analyze(context.Background(), Input{Language: "python", Code: "exec(x)", Filename: "snippet.py"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("want 1 finding, got %d", len(found))
	}
	f := found[0]
	if f.RuleID != "python.lang.security.audit.exec-detected" || f.Severity != SeverityHigh {
		t.Fatalf("bad normalization: %+v", f)
	}
	if f.Line != 5 || f.Category != "security" || f.Analyzers[0] != "semgrep" {
		t.Fatalf("bad fields: %+v", f)
	}
	if fr.gotName != "semgrep" {
		t.Fatalf("expected semgrep invocation, got %s", fr.gotName)
	}
}

func TestSemgrepUnavailableWhenAbsent(t *testing.T) {
	s := NewSemgrepAnalyzer(nil)
	s.exists = func() bool { return false }
	if s.Available() {
		t.Fatal("semgrep must report unavailable when binary absent")
	}
}
