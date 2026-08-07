package testfixtures

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/vigilagent/vigilagent/internal/scanner"
)

// builtinEngine builds the deterministic builtin engine used for golden
// expectations (no external tools like semgrep/bandit required in CI).
func builtinEngine() *scanner.Engine {
	return scanner.NewEngine(scanner.NewBuiltinAnalyzer())
}

// TestVulnerableFixturesProduceExpectedFindings proves the deterministic engine
// flags every AI-generated vulnerable snippet with the golden rule + severity.
func TestVulnerableFixturesProduceExpectedFindings(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(m.Vulnerable) == 0 {
		t.Fatal("manifest has no vulnerable fixtures")
	}

	eng := builtinEngine()
	for _, fx := range m.Vulnerable {
		code, err := Read(fx.File)
		if err != nil {
			t.Fatalf("%s: %v", fx.File, err)
		}
		report := eng.Run(context.Background(), scanner.Input{
			Language: fx.Language,
			Code:     code,
			Filename: fx.File,
		})
		if len(fx.Expect) == 0 {
			t.Errorf("%s: no golden expectations declared", fx.File)
			continue
		}
		for _, exp := range fx.Expect {
			if !containsFinding(report.Findings, exp) {
				t.Errorf("%s: expected finding rule~%q severity=%q; got %s",
					fx.File, exp.Rule, exp.Severity, findingSummary(report.Findings))
			}
		}
	}
}

// TestCleanFixturesProduceNoSeriousFindings guards against false positives:
// clean snippets must not raise medium+ findings.
func TestCleanFixturesProduceNoSeriousFindings(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(m.Clean) == 0 {
		t.Fatal("manifest has no clean fixtures")
	}

	eng := builtinEngine()
	for _, fx := range m.Clean {
		code, err := Read(fx.File)
		if err != nil {
			t.Fatalf("%s: %v", fx.File, err)
		}
		report := eng.Run(context.Background(), scanner.Input{
			Language: fx.Language,
			Code:     code,
			Filename: fx.File,
		})
		for _, f := range report.Findings {
			if scanner.SeverityRank(f.Severity) >= scanner.SeverityRank(scanner.SeverityMedium) {
				t.Errorf("%s: unexpected finding [%s] %s: %s", fx.File, f.Severity, f.RuleID, f.Message)
			}
		}
	}
}

func containsFinding(findings []scanner.Finding, exp Expectation) bool {
	for _, f := range findings {
		if strings.Contains(f.RuleID, exp.Rule) && string(f.Severity) == exp.Severity {
			return true
		}
	}
	return false
}

func findingSummary(findings []scanner.Finding) string {
	if len(findings) == 0 {
		return "no findings"
	}
	var sb strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&sb, "[%s] %s (%s); ", f.Severity, f.RuleID, f.Message)
	}
	return sb.String()
}
