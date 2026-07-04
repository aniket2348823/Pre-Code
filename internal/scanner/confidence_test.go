package scanner

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestConfidence(t *testing.T) {
	// critical, single builtin: base 0.6, no real-tool weight, no corroboration.
	if c := Confidence(SeverityCritical, []string{"builtin"}); !near(c, 0.6) {
		t.Fatalf("critical/builtin = %v want 0.6", c)
	}
	// critical, single bandit: 0.6 + 0.1 real-tool weight.
	if c := Confidence(SeverityCritical, []string{"bandit"}); !near(c, 0.7) {
		t.Fatalf("critical/bandit = %v want 0.7", c)
	}
	// medium, two analyzers: 0.4 + 0.1 + 0.25 corroboration = 0.75.
	if c := Confidence(SeverityMedium, []string{"builtin", "bandit"}); !near(c, 0.75) {
		t.Fatalf("medium/corroborated = %v want 0.75", c)
	}
	// upper clamp: critical + real tool + corroboration = 0.95, within cap.
	if c := Confidence(SeverityCritical, []string{"semgrep", "bandit"}); !near(c, 0.95) {
		t.Fatalf("critical/2-tools = %v want 0.95", c)
	}
	// never exceeds 0.99 or drops below 0.05.
	if c := Confidence(SeverityInfo, []string{"builtin"}); c < 0.05 {
		t.Fatalf("floor breached: %v", c)
	}
}
