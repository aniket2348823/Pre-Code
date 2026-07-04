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
	// same location/snippet across analyzers → identical fingerprint → merge.
	return Finding{
		RuleID: analyzer + "-rule", Analyzers: []string{analyzer}, Severity: sev,
		Filename: "x.py", Line: 3, Snippet: "danger()",
		Fingerprint: ComputeFingerprint("x.py", 3, "danger()"),
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
	// high + real-tool + corroboration = 0.5 + 0.1 + 0.25 = 0.85.
	if f.Confidence < 0.84 || f.Confidence > 0.86 {
		t.Fatalf("corroborated confidence = %v want ~0.85", f.Confidence)
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
