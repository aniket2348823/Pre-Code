package scanner

import (
	"context"
	"errors"
	"testing"
)

type fakeAnalyzer struct {
	name      string
	available bool
	findings  []Finding
	err       error
}

func (f fakeAnalyzer) Name() string   { return f.name }
func (f fakeAnalyzer) Available() bool { return f.available }
func (f fakeAnalyzer) Analyze(ctx context.Context, in Input) ([]Finding, error) {
	return f.findings, f.err
}

func mkFinding(analyzer string, sev Severity) Finding {
	// Use a shared ruleID so two analyzers reporting the same vuln produce
	// matching fingerprints and merge correctly.
	return Finding{
		RuleID: "shared-rule", Analyzers: []string{analyzer}, Severity: sev,
		Filename: "x.py", Line: 3, Snippet: "danger()",
		Fingerprint: ComputeFingerprint("x.py", 3, "danger()", "shared-rule"),
		Fix:         analyzer + "-fix",
	}
}

func TestEngineMergesCorroboratesAndScores(t *testing.T) {
	a := fakeAnalyzer{name: "builtin", available: true, findings: []Finding{mkFinding("builtin", SeverityMedium)}}
	b := fakeAnalyzer{name: "bandit", available: true, findings: []Finding{mkFinding("bandit", SeverityHigh)}}
	rep := NewEngine(a, b).Run(context.Background(), Input{Code: "x"})

	if len(rep.Findings) != 1 {
		t.Fatalf("expected 1 merged finding, got %d", len(rep.Findings))
	}
	f := rep.Findings[0]
	if len(f.Analyzers) != 2 {
		t.Fatalf("expected both analyzers on merged finding, got %v", f.Analyzers)
	}
	if f.Severity != SeverityHigh {
		t.Fatalf("merge must keep highest severity, got %s", f.Severity)
	}
	// high + real-tool corroboration + corroboration = 0.55 + 0.10 + 0.25 = 0.90
	if f.Confidence < 0.85 || f.Confidence > 0.95 {
		t.Fatalf("corroborated confidence = %v want ~0.90", f.Confidence)
	}
	if len(rep.AnalyzersRun) != 2 {
		t.Fatalf("expected 2 analyzers run, got %v", rep.AnalyzersRun)
	}
}

func TestEngineIsolatesErrorsAndSkips(t *testing.T) {
	good := fakeAnalyzer{name: "builtin", available: true, findings: []Finding{mkFinding("builtin", SeverityLow)}}
	broken := fakeAnalyzer{name: "bandit", available: true, err: errors.New("tool crashed")}
	absent := fakeAnalyzer{name: "semgrep", available: false}
	rep := NewEngine(good, broken, absent).Run(context.Background(), Input{Code: "x"})

	if len(rep.Findings) != 1 {
		t.Fatalf("good analyzer's finding must survive, got %d", len(rep.Findings))
	}
	if _, ok := rep.AnalyzerErrors["bandit"]; !ok {
		t.Fatal("broken analyzer must be recorded in AnalyzerErrors")
	}
	if _, ok := rep.AnalyzersSkipped["semgrep"]; !ok {
		t.Fatal("absent analyzer must be recorded in AnalyzersSkipped")
	}
}

func TestEngineFiltersLowConfidence(t *testing.T) {
	// Very low confidence finding from info severity with builtin only should be filtered.
	low := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "low-rule", Analyzers: []string{"builtin"}, Severity: SeverityInfo,
			Filename: "x.py", Line: 1, Snippet: "low risk",
			Fingerprint: ComputeFingerprint("x.py", 1, "low risk", "low-rule"),
		}},
	}
	eng := NewEngine(low)
	rep := eng.Run(context.Background(), Input{Code: "x"})
	// info + builtin = 0.20 base, below minConfidence of 0.30.
	if len(rep.Findings) != 0 {
		t.Fatalf("expected 0 findings (filtered), got %d", len(rep.Findings))
	}
}

func TestEngineNoFindingsAboveThreshold(t *testing.T) {
	// A high-severity finding from builtin only should pass the filter.
	high := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "high-rule", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
			Filename: "x.py", Line: 1, Snippet: "critical issue",
			Fingerprint: ComputeFingerprint("x.py", 1, "critical issue", "high-rule"),
		}},
	}
	eng := NewEngine(high)
	rep := eng.Run(context.Background(), Input{Code: "x"})
	if len(rep.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(rep.Findings))
	}
}

func TestEngineTestFPSuppression(t *testing.T) {
	// Secrets in test files should be downgraded but still present.
	secretsInTest := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "hardcoded_password", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
			Category: "secrets",
			Filename: "auth_test.go", Line: 10, Snippet: `password := "test123456"`,
			Fingerprint: ComputeFingerprint("auth_test.go", 10, `password := "test123456"`, "hardcoded_password"),
		}},
	}
	eng := NewEngine(secretsInTest)
	rep := eng.Run(context.Background(), Input{Code: "x", Filename: "auth_test.go"})

	// Should have 1 finding but with downgraded severity.
	if len(rep.Findings) != 1 {
		t.Fatalf("expected 1 finding (suppressed, not removed), got %d", len(rep.Findings))
	}
	if rep.Findings[0].Severity != SeverityInfo {
		t.Fatalf("test file secret should be downgraded to info, got %s", rep.Findings[0].Severity)
	}
}

func TestHasHighConfidenceFindings(t *testing.T) {
	rep := &Report{
		Findings: []Finding{
			{Confidence: 0.80, Filename: "main.go"},
		},
	}
	if !HasHighConfidenceFindings(rep) {
		t.Fatal("should detect high-confidence findings")
	}

	rep2 := &Report{
		Findings: []Finding{
			{Confidence: 0.80, Filename: "main_test.go"},
		},
	}
	if HasHighConfidenceFindings(rep2) {
		t.Fatal("test file findings should not count as high confidence")
	}
}
