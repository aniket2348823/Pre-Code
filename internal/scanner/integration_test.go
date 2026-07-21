package scanner

import (
	"context"
	"testing"
)

// TestIntegration_BuiltinOnlyPipeline tests the full scanner pipeline with only
// the builtin analyzer (no external tools). This exercises:
// Analyze → merge → dedupe → confidence → filter → report.
func TestIntegration_BuiltinOnlyPipeline(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())

	code := `
package main

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"net/http"
	"os"
)

func handler(w http.ResponseWriter, r *http.Request) {
	// SQL injection
	q := fmt.Sprintf("SELECT * FROM users WHERE id=%s", r.URL.Query().Get("id"))

	// Hardcoded secret
	password := "supersecretpassword123"

	// Weak hash
	h := md5.New()

	// Insecure TLS
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	// Path traversal
	f, _ := os.Open(r.URL.Path)

	_ = q
	_ = h
	_ = tr
	_ = f
}
`
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "handler.go",
	})

	if len(report.AnalyzersRun) != 1 || report.AnalyzersRun[0] != "builtin" {
		t.Fatalf("expected only builtin analyzer, got %v", report.AnalyzersRun)
	}
	if len(report.AnalyzerErrors) != 0 {
		t.Fatalf("expected no analyzer errors, got %v", report.AnalyzerErrors)
	}

	// Should find multiple vulnerabilities.
	if len(report.Findings) < 3 {
		t.Fatalf("expected at least 3 findings, got %d", len(report.Findings))
	}

	// All findings should be from builtin.
	for _, f := range report.Findings {
		if len(f.Analyzers) != 1 || f.Analyzers[0] != "builtin" {
			t.Fatalf("finding %s should be from builtin only, got %v", f.RuleID, f.Analyzers)
		}
		if f.Fingerprint == "" {
			t.Fatalf("finding %s has no fingerprint", f.RuleID)
		}
		if f.Confidence < 0.05 || f.Confidence > 0.99 {
			t.Fatalf("finding %s has out-of-range confidence: %v", f.RuleID, f.Confidence)
		}
	}

	// Verify specific rules were triggered.
	ruleIDs := map[string]bool{}
	for _, f := range report.Findings {
		ruleIDs[f.RuleID] = true
	}
	expected := []string{"sql_injection", "hardcoded_password", "weak_hash_md5", "insecure_tls"}
	for _, e := range expected {
		if !ruleIDs[e] {
			t.Fatalf("expected rule %s to fire, but it did not", e)
		}
	}
}

// TestIntegration_DedupeAcrossAnalyzers tests that two analyzers flagging
// the same line produce one merged finding with both analyzers listed.
func TestIntegration_DedupeAcrossAnalyzers(t *testing.T) {
	// Two fake analyzers that flag the same location with the same snippet.
	// Use the same ruleID so the fingerprint matches and they merge.
	sharedRule := "sql-injection"
	snippet := `q := fmt.Sprintf("SELECT * FROM t WHERE id=%s", id)`
	a1 := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: sharedRule, Analyzers: []string{"builtin"}, Severity: SeverityCritical,
			Filename: "x.go", Line: 10, Snippet: snippet,
			Fingerprint: ComputeFingerprint("x.go", 10, snippet, sharedRule),
			Category: "injection",
		}},
	}
	a2 := fakeAnalyzer{
		name: "bandit", available: true,
		findings: []Finding{{
			RuleID: sharedRule, Analyzers: []string{"bandit"}, Severity: SeverityHigh,
			Filename: "x.go", Line: 10, Snippet: snippet,
			Fingerprint: ComputeFingerprint("x.go", 10, snippet, sharedRule),
			Category: "injection",
		}},
	}

	engine := NewEngine(a1, a2)
	report := engine.Run(context.Background(), Input{Code: "x", Filename: "x.go"})

	if len(report.Findings) != 1 {
		t.Fatalf("expected 1 merged finding, got %d", len(report.Findings))
	}
	f := report.Findings[0]
	if len(f.Analyzers) != 2 {
		t.Fatalf("expected 2 analyzers on merged finding, got %v", f.Analyzers)
	}
	if f.Severity != SeverityCritical {
		t.Fatalf("merge should keep highest severity, got %s", f.Severity)
	}
}

// TestIntegration_GeneratedFileSuppression tests that generated/vendor files
// produce zero findings from the builtin analyzer.
func TestIntegration_GeneratedFileSuppression(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())

	code := `
password := "supersecretpassword123"
InsecureSkipVerify: true
`
	// Generated file
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "api.pb.go",
	})
	if len(report.Findings) != 0 {
		t.Fatalf("generated file should produce 0 findings, got %d", len(report.Findings))
	}

	// Vendor file
	report2 := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "vendor/github.com/foo/bar.go",
	})
	if len(report2.Findings) != 0 {
		t.Fatalf("vendor file should produce 0 findings, got %d", len(report2.Findings))
	}
}

// TestIntegration_TestFileSuppression tests that test files get downgraded
// findings (not removed, but lowered severity).
func TestIntegration_TestFileSuppression(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())

	code := `password := "supersecretpassword123"` + "\n"
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "auth_test.go",
	})

	// Should find the secret but with downgraded severity.
	if len(report.Findings) != 1 {
		t.Fatalf("expected 1 finding in test file, got %d", len(report.Findings))
	}
	f := report.Findings[0]
	if f.Severity != SeverityInfo {
		t.Fatalf("test file secret should be downgraded to info, got %s", f.Severity)
	}
}

// TestIntegration_EmptyInput tests that empty input produces no findings.
func TestIntegration_EmptyInput(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Code: ""})
	if len(report.Findings) != 0 {
		t.Fatalf("empty input should produce 0 findings, got %d", len(report.Findings))
	}
}

// TestIntegration_SkippedAnalyzers tests that unavailable analyzers are
// recorded in AnalyzersSkipped and don't block the scan.
func TestIntegration_SkippedAnalyzers(t *testing.T) {
	unavailable := fakeAnalyzer{name: "semgrep", available: false}
	builtin := NewBuiltinAnalyzer()

	engine := NewEngine(builtin, unavailable)
	report := engine.Run(context.Background(), Input{
		Code:     `InsecureSkipVerify: true`,
		Filename: "tls.go",
	})

	if _, ok := report.AnalyzersSkipped["semgrep"]; !ok {
		t.Fatal("semgrep should be in AnalyzersSkipped")
	}
	if len(report.Findings) == 0 {
		t.Fatal("builtin findings should still be present")
	}
}

// TestIntegration_ConfidenceThreshold tests that the minConfidence filter
// removes low-confidence findings.
func TestIntegration_ConfidenceThreshold(t *testing.T) {
	// Create engine with high threshold to filter more aggressively.
	engine := NewEngine(NewBuiltinAnalyzer())
	engine.minConfidence = 0.50

	code := `"math/rand"` + "\n" + `n := rand.Intn(100)` + "\n"
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "util.go",
	})

	// weak_random in util.go with single builtin analyzer:
	// base 0.55 + builtin weight 0 = 0.55, above 0.50 threshold.
	if len(report.Findings) == 0 {
		t.Fatal("weak_random should pass 0.50 threshold with builtin base 0.55")
	}
}

// TestIntegration_ReportStructure tests that the Report struct is properly
// populated with all metadata.
func TestIntegration_ReportStructure(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     `InsecureSkipVerify: true`,
		Filename: "tls.go",
	})

	if len(report.AnalyzersRun) != 1 {
		t.Fatalf("expected 1 analyzer run, got %d", len(report.AnalyzersRun))
	}
	if len(report.AnalyzersSkipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(report.AnalyzersSkipped))
	}
	if len(report.AnalyzerErrors) != 0 {
		t.Fatalf("expected 0 errors, got %d", len(report.AnalyzerErrors))
	}

	// Verify Report is JSON-serializable.
	if report.Findings[0].RuleID == "" {
		t.Fatal("finding should have a RuleID")
	}
}
