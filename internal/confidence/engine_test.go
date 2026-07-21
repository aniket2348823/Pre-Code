package confidence

import "testing"

func TestScore_AllPass(t *testing.T) {
	e := NewEngine()
	s := e.Score([]Evidence{
		{Source: "schema", Verdict: "pass", Severity: "info"},
		{Source: "requirements", Verdict: "pass", Severity: "info"},
		{Source: "compliance", Verdict: "pass", Severity: "info"},
	})
	if s.Confidence < 0.99 {
		t.Fatalf("expected confidence ~1.0, got %f", s.Confidence)
	}
	if s.Grade != "A+" {
		t.Fatalf("expected grade A+, got %s", s.Grade)
	}
}

func TestScore_CriticalFailure(t *testing.T) {
	e := NewEngine()
	s := e.Score([]Evidence{
		{Source: "scan", Verdict: "fail", Severity: "critical", Detail: "sql injection"},
		{Source: "schema", Verdict: "pass", Severity: "info"},
	})
	if s.Confidence >= 0.8 {
		t.Fatalf("critical failure should reduce confidence significantly, got %f", s.Confidence)
	}
	if s.Failed != 1 {
		t.Fatalf("expected 1 failed, got %d", s.Failed)
	}
}

func TestScore_NoEvidence(t *testing.T) {
	e := NewEngine()
	s := e.Score(nil)
	if s.Confidence != 1.0 {
		t.Fatalf("no evidence should default to 1.0, got %f", s.Confidence)
	}
}

func TestScore_Weighted(t *testing.T) {
	e := NewEngine()
	s := e.Score([]Evidence{
		{Source: "critical_check", Verdict: "fail", Severity: "high", Weight: 1.0},
		{Source: "minor_check", Verdict: "pass", Severity: "info", Weight: 0.1},
	})
	// Critical check failing with high weight should drag confidence down
	if s.Confidence > 0.5 {
		t.Fatalf("high-weight failure should reduce confidence, got %f", s.Confidence)
	}
}

func TestScoreFromFindings(t *testing.T) {
	// import scanner types directly
	import_scanner_findings := []struct {
		severity string
		message  string
	}{
		{"critical", "sql injection"},
		{"info", "unused variable"},
	}
	_ = import_scanner_findings

	// Use the real ScoreFromFindings with minimal findings
	e := NewEngine()
	s := e.ScoreFromFindings(nil)
	if s.Confidence != 1.0 {
		t.Fatalf("no findings should mean 1.0 confidence, got %f", s.Confidence)
	}
}

func TestGradeFromScore(t *testing.T) {
	tests := []struct {
		score float64
		grade string
	}{
		{0.95, "A+"},
		{0.90, "A"},
		{0.80, "B+"},
		{0.70, "B"},
		{0.60, "C"},
		{0.50, "D"},
		{0.30, "F"},
	}
	for _, tt := range tests {
		g := gradeFromScore(tt.score)
		if g != tt.grade {
			t.Errorf("gradeFromScore(%f) = %s, want %s", tt.score, g, tt.grade)
		}
	}
}
