package scanner

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestConfidence(t *testing.T) {
	// critical, single builtin: base 0.65, no real-tool weight, no corroboration.
	if c := Confidence(SeverityCritical, []string{"builtin"}); !near(c, 0.65) {
		t.Fatalf("critical/builtin = %v want 0.65", c)
	}
	// critical, single bandit: 0.65 + 0.15 real-tool weight (pure tool).
	if c := Confidence(SeverityCritical, []string{"bandit"}); !near(c, 0.80) {
		t.Fatalf("critical/bandit = %v want 0.80", c)
	}
	// medium, two analyzers: 0.40 + 0.10 (real-tool corroboration) + 0.25 = 0.75.
	if c := Confidence(SeverityMedium, []string{"builtin", "bandit"}); !near(c, 0.75) {
		t.Fatalf("medium/corroborated = %v want 0.75", c)
	}
	// critical + 2 real tools: 0.65 + 0.15 + 0.25 = 1.05, clamped to 0.99.
	if c := Confidence(SeverityCritical, []string{"semgrep", "bandit"}); !near(c, 0.99) {
		t.Fatalf("critical/2-tools = %v want 0.99", c)
	}
	// never exceeds 0.99 or drops below 0.05.
	if c := Confidence(SeverityInfo, []string{"builtin"}); c < 0.05 {
		t.Fatalf("floor breached: %v", c)
	}
}

func TestConfidenceWithFile(t *testing.T) {
	// Test file gets penalty.
	base := Confidence(SeverityCritical, []string{"builtin"})
	testFile := ConfidenceWithFile(SeverityCritical, []string{"builtin"}, "auth_test.go", "")
	if testFile >= base {
		t.Fatalf("test file should have lower confidence: %v >= %v", testFile, base)
	}

	// Generated file gets no findings from engine (suppressed by builtin).
	// But contextPenalty still applies if called directly.
	genFile := ConfidenceWithFile(SeverityHigh, []string{"builtin"}, "api.pb.go", "some code")
	if genFile >= base {
		t.Fatalf("generated file should have lower confidence")
	}

	// Regular file gets no penalty.
	regular := ConfidenceWithFile(SeverityCritical, []string{"builtin"}, "auth.go", "")
	if !near(regular, base) {
		t.Fatalf("regular file should have same confidence: %v != %v", regular, base)
	}
}

func TestContextPenalty(t *testing.T) {
	if p := contextPenalty("auth_test.go"); p >= 0 {
		t.Fatalf("test file should have negative penalty, got %v", p)
	}
	if p := contextPenalty("example/main.go"); p >= 0 {
		t.Fatalf("example file should have negative penalty, got %v", p)
	}
	if p := contextPenalty("internal/auth.go"); p != 0 {
		t.Fatalf("regular file should have zero penalty, got %v", p)
	}
}

func TestSnippetConfidence(t *testing.T) {
	// String literal assignment gets boost.
	if s := snippetConfidence(`password := "secret123"`); s <= 0 {
		t.Fatalf("literal assignment should have positive boost, got %v", s)
	}
}

func TestIsHighConfidence(t *testing.T) {
	if !IsHighConfidence(0.50) {
		t.Fatal("0.50 should be high confidence")
	}
	if IsHighConfidence(0.10) {
		t.Fatal("0.10 should not be high confidence")
	}
}

func TestShouldReport(t *testing.T) {
	f := Finding{Confidence: 0.35}
	if !ShouldReport(f) {
		t.Fatal("0.35 should be reportable")
	}
	f2 := Finding{Confidence: 0.10}
	if ShouldReport(f2) {
		t.Fatal("0.10 should not be reportable")
	}
}
