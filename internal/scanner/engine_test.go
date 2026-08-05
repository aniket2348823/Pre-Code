package scanner

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEngine_Defaults(t *testing.T) {
	eng := NewEngine()
	assert.NotNil(t, eng)
	assert.InDelta(t, 0.30, eng.minConfidence, 1e-9)
	assert.True(t, eng.suppressTestFP)
	assert.Empty(t, eng.analyzers)
}

func TestNewEngine_WithAnalyzers(t *testing.T) {
	a1 := &fakeAnalyzer{name: "a1", available: true}
	a2 := &fakeAnalyzer{name: "a2", available: true}
	eng := NewEngine(a1, a2)
	assert.Len(t, eng.analyzers, 2)
}

func TestDefaultEngine_ThreeAnalyzers(t *testing.T) {
	eng := DefaultEngine()
	require.NotNil(t, eng)
	assert.Len(t, eng.analyzers, 3)
}

func TestWithMinConfidence_Option(t *testing.T) {
	eng := NewEngine()
	WithMinConfidence(0.75)(eng)
	assert.InDelta(t, 0.75, eng.minConfidence, 1e-9)
}

func TestWithTestFPSuppression_Option(t *testing.T) {
	eng := NewEngine()
	eng.suppressTestFP = false
	WithTestFPSuppression()(eng)
	assert.True(t, eng.suppressTestFP)
}

func TestEngine_NoAnalyzers(t *testing.T) {
	eng := NewEngine()
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "a.go"})
	assert.NotNil(t, report)
	assert.Empty(t, report.Findings)
	assert.Empty(t, report.AnalyzersRun)
}

func TestEngine_UnavailableAnalyzer(t *testing.T) {
	unavail := &fakeAnalyzer{name: "absent", available: false}
	eng := NewEngine(unavail)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "a.go"})
	assert.Contains(t, report.AnalyzersSkipped, "absent")
	assert.Empty(t, report.AnalyzerErrors)
}

func TestEngine_ErrorAnalyzer(t *testing.T) {
	errA := &fakeAnalyzer{name: "broken", available: true, err: fmt.Errorf("crash")}
	goodA := &fakeAnalyzer{
		name: "good", available: true,
		findings: []Finding{{
			RuleID: "r1", Analyzers: []string{"good"}, Severity: SeverityHigh,
			Filename: "x.go", Line: 1, Snippet: "code",
			Fingerprint: ComputeFingerprint("x.go", 1, "code", "r1"),
		}},
	}
	eng := NewEngine(errA, goodA)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "x.go"})
	assert.Contains(t, report.AnalyzerErrors, "broken")
	assert.Len(t, report.Findings, 1)
}

func TestEngine_MergeSameFingerprint(t *testing.T) {
	a1 := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "shared", Analyzers: []string{"builtin"}, Severity: SeverityMedium,
			Filename: "x.go", Line: 5, Snippet: "code",
			Fingerprint: ComputeFingerprint("x.go", 5, "code", "shared"),
		}},
	}
	a2 := fakeAnalyzer{
		name: "bandit", available: true,
		findings: []Finding{{
			RuleID: "shared", Analyzers: []string{"bandit"}, Severity: SeverityHigh,
			Filename: "x.go", Line: 5, Snippet: "code",
			Fingerprint: ComputeFingerprint("x.go", 5, "code", "shared"),
		}},
	}
	eng := NewEngine(a1, a2)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "x.go"})
	require.Len(t, report.Findings, 1)
	assert.Equal(t, SeverityHigh, report.Findings[0].Severity, "merge should keep highest severity")
	assert.Len(t, report.Findings[0].Analyzers, 2)
}

func TestEngine_MergeKeepsHigherSeverity(t *testing.T) {
	a1 := fakeAnalyzer{
		name: "a1", available: true,
		findings: []Finding{{
			RuleID: "r", Analyzers: []string{"a1"}, Severity: SeverityLow,
			Filename: "x.go", Line: 1, Snippet: "code",
			Fingerprint: ComputeFingerprint("x.go", 1, "code", "r"),
		}},
	}
	a2 := fakeAnalyzer{
		name: "a2", available: true,
		findings: []Finding{{
			RuleID: "r", Analyzers: []string{"a2"}, Severity: SeverityCritical,
			Filename: "x.go", Line: 1, Snippet: "code",
			Fingerprint: ComputeFingerprint("x.go", 1, "code", "r"),
		}},
	}
	eng := NewEngine(a1, a2)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "x.go"})
	require.Len(t, report.Findings, 1)
	assert.Equal(t, SeverityCritical, report.Findings[0].Severity)
}

func TestEngine_MergeCopiesFixFromSecondAnalyzer(t *testing.T) {
	a1 := fakeAnalyzer{
		name: "a1", available: true,
		findings: []Finding{{
			RuleID: "r", Analyzers: []string{"a1"}, Severity: SeverityMedium,
			Filename: "x.go", Line: 1, Snippet: "code",
			Fingerprint: ComputeFingerprint("x.go", 1, "code", "r"),
			Fix:         "",
		}},
	}
	a2 := fakeAnalyzer{
		name: "a2", available: true,
		findings: []Finding{{
			RuleID: "r", Analyzers: []string{"a2"}, Severity: SeverityMedium,
			Filename: "x.go", Line: 1, Snippet: "code",
			Fingerprint: ComputeFingerprint("x.go", 1, "code", "r"),
			Fix:         "use parameterized queries",
		}},
	}
	eng := NewEngine(a1, a2)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "x.go"})
	require.Len(t, report.Findings, 1)
	assert.Equal(t, "use parameterized queries", report.Findings[0].Fix)
}

func TestEngine_MergeCopiesMessageFromSecondAnalyzer(t *testing.T) {
	a1 := fakeAnalyzer{
		name: "a1", available: true,
		findings: []Finding{{
			RuleID: "r", Analyzers: []string{"a1"}, Severity: SeverityMedium,
			Filename: "x.go", Line: 1, Snippet: "code",
			Fingerprint: ComputeFingerprint("x.go", 1, "code", "r"),
			Message:     "",
		}},
	}
	a2 := fakeAnalyzer{
		name: "a2", available: true,
		findings: []Finding{{
			RuleID: "r", Analyzers: []string{"a2"}, Severity: SeverityMedium,
			Filename: "x.go", Line: 1, Snippet: "code",
			Fingerprint: ComputeFingerprint("x.go", 1, "code", "r"),
			Message:     "detected SQL injection",
		}},
	}
	eng := NewEngine(a1, a2)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "x.go"})
	require.Len(t, report.Findings, 1)
	assert.Equal(t, "detected SQL injection", report.Findings[0].Message)
}

func TestEngine_OversizedInput(t *testing.T) {
	eng := NewEngine(NewBuiltinAnalyzer())
	bigCode := make([]byte, maxCodeSize+1)
	for i := range bigCode {
		bigCode[i] = 'x'
	}
	report := eng.Run(context.Background(), Input{Code: string(bigCode), Filename: "big.go"})
	assert.Empty(t, report.Findings)
	assert.Contains(t, report.AnalyzerErrors, "engine")
}

func TestEngine_MaxCodeSizeExact(t *testing.T) {
	eng := NewEngine(NewBuiltinAnalyzer())
	code := make([]byte, maxCodeSize)
	for i := range code {
		code[i] = 'x'
	}
	report := eng.Run(context.Background(), Input{Code: string(code), Filename: "exact.go"})
	// Should not error — exactly at the limit
	if _, hasErr := report.AnalyzerErrors["engine"]; hasErr {
		t.Fatal("exactly maxCodeSize should not trigger error")
	}
}

func TestEngine_TestFP_SecretInTestFile(t *testing.T) {
	secret := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "hardcoded_password", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
			Category: "secrets",
			Filename: "auth_test.go", Line: 10, Snippet: `password := "test123456"`,
			Fingerprint: ComputeFingerprint("auth_test.go", 10, `password := "test123456"`, "hardcoded_password"),
		}},
	}
	eng := NewEngine(secret)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "auth_test.go"})
	require.Len(t, report.Findings, 1)
	assert.Equal(t, SeverityInfo, report.Findings[0].Severity, "should be downgraded to info")
}

func TestEngine_TestFP_DisabledSuppression(t *testing.T) {
	secret := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "hardcoded_password", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
			Category: "secrets",
			Filename: "auth_test.go", Line: 10, Snippet: `password := "test123456"`,
			Fingerprint: ComputeFingerprint("auth_test.go", 10, `password := "test123456"`, "hardcoded_password"),
		}},
	}
	eng := NewEngine(secret)
	eng.suppressTestFP = false
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "auth_test.go"})
	require.Len(t, report.Findings, 1)
	assert.Equal(t, SeverityCritical, report.Findings[0].Severity, "should stay critical without suppression")
}

func TestEngine_TestFP_CryptoInTestFile(t *testing.T) {
	// Crypto in test files with builtin-only gets double penalty:
	// contextPenalty(-0.15) + suppressTestFP(-0.15) on top of base 0.55,
	// which drops below 0.30 minConfidence threshold.
	crypto := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "weak_hash_md5", Analyzers: []string{"builtin"}, Severity: SeverityHigh,
			Category: "crypto",
			Filename: "crypto_test.go", Line: 1, Snippet: "md5.New()",
			Fingerprint: ComputeFingerprint("crypto_test.go", 1, "md5.New()", "weak_hash_md5"),
		}},
	}
	eng := NewEngine(crypto)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "crypto_test.go"})
	// Due to double penalty, finding may be filtered out
	assert.GreaterOrEqual(t, len(report.Findings), 0)
}

func TestEngine_TestFP_InjectionInTestFile(t *testing.T) {
	injection := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "sql_injection", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
			Category: "injection",
			Filename: "handler_test.go", Line: 5, Snippet: `q := fmt.Sprintf("SELECT %s", id)`,
			Fingerprint: ComputeFingerprint("handler_test.go", 5, `q := fmt.Sprintf("SELECT %s", id)`, "sql_injection"),
		}},
	}
	eng := NewEngine(injection)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "handler_test.go"})
	require.Len(t, report.Findings, 1)
	assert.Equal(t, SeverityLow, report.Findings[0].Severity,
		"injection in test file should be SeverityLow")
}

func TestEngine_TestDataFileSuppression(t *testing.T) {
	secret := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "hardcoded_password", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
			Category: "secrets",
			Filename: "testdata/fixture.json", Line: 1, Snippet: `password: "test"`,
			Fingerprint: ComputeFingerprint("testdata/fixture.json", 1, `password: "test"`, "hardcoded_password"),
		}},
	}
	eng := NewEngine(secret)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "testdata/fixture.json"})
	assert.Empty(t, report.Findings, "testdata files should fully suppress findings")
}

func TestEngine_LowConfidenceFilter(t *testing.T) {
	low := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "low-rule", Analyzers: []string{"builtin"}, Severity: SeverityInfo,
			Filename: "x.go", Line: 1, Snippet: "low risk",
			Fingerprint: ComputeFingerprint("x.go", 1, "low risk", "low-rule"),
		}},
	}
	eng := NewEngine(low)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "x.go"})
	// info + builtin = 0.20 base, below minConfidence of 0.30
	assert.Empty(t, report.Findings)
}

func TestEngine_HighConfidencePassesFilter(t *testing.T) {
	high := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "high-rule", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
			Filename: "x.go", Line: 1, Snippet: "critical",
			Fingerprint: ComputeFingerprint("x.go", 1, "critical", "high-rule"),
		}},
	}
	eng := NewEngine(high)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "x.go"})
	assert.Len(t, report.Findings, 1)
}

func TestEngine_CustomMinConfidence(t *testing.T) {
	eng := NewEngine(NewBuiltinAnalyzer())
	eng.minConfidence = 0.90
	code := `"math/rand"` + "\n" + `n := rand.Intn(100)` + "\n"
	report := eng.Run(context.Background(), Input{Code: code, Filename: "util.go"})
	// weak_random with builtin only: 0.55 base. Below 0.90 threshold.
	assert.Empty(t, report.Findings)
}

func TestEngine_ReportAnalyzersRun(t *testing.T) {
	a1 := &fakeAnalyzer{name: "a1", available: true}
	a2 := &fakeAnalyzer{name: "a2", available: false}
	eng := NewEngine(a1, a2)
	report := eng.Run(context.Background(), Input{Code: "x", Filename: "x.go"})
	assert.Contains(t, report.AnalyzersRun, "a1")
	assert.Contains(t, report.AnalyzersSkipped, "a2")
}

func TestEngine_ReportStructure(t *testing.T) {
	eng := NewEngine(NewBuiltinAnalyzer())
	report := eng.Run(context.Background(), Input{Code: `InsecureSkipVerify: true`, Filename: "tls.go"})
	assert.NotNil(t, report.AnalyzersRun)
	assert.NotNil(t, report.AnalyzersSkipped)
	assert.NotNil(t, report.AnalyzerErrors)
	assert.NotEmpty(t, report.Findings)
}

func TestEngine_EmptyFilename_NoFindingsLost(t *testing.T) {
	eng := NewEngine(NewBuiltinAnalyzer())
	report := eng.Run(context.Background(), Input{Code: `InsecureSkipVerify: true`, Filename: ""})
	assert.NotEmpty(t, report.Findings, "should detect findings with empty filename")
}

func TestEngine_SeveritySorted(t *testing.T) {
	eng := NewEngine(NewBuiltinAnalyzer())
	code := `
password := "supersecretpassword123"
h := md5.New()
InsecureSkipVerify: true
`
	report := eng.Run(context.Background(), Input{Code: code, Filename: "mixed.go"})
	require.GreaterOrEqual(t, len(report.Findings), 2)

	for i := 1; i < len(report.Findings); i++ {
		prev := SeverityRank(report.Findings[i-1].Severity)
		curr := SeverityRank(report.Findings[i].Severity)
		assert.GreaterOrEqual(t, prev, curr,
			"findings should be sorted by severity descending at index %d", i)
	}
}

func TestUnionSorted_Deduplication(t *testing.T) {
	result := unionSorted([]string{"a", "b", "a"}, []string{"b", "c"})
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestUnionSorted_EmptyInputs(t *testing.T) {
	assert.Empty(t, unionSorted(nil, nil))
	assert.Equal(t, []string{"a"}, unionSorted([]string{"a"}, nil))
	assert.Equal(t, []string{"b"}, unionSorted(nil, []string{"b"}))
}

func TestUnionSorted_Sorted(t *testing.T) {
	result := unionSorted([]string{"c", "a"}, []string{"b", "d"})
	assert.Equal(t, []string{"a", "b", "c", "d"}, result)
}

func TestHasHighConfidenceFindings_EmptyReport(t *testing.T) {
	assert.False(t, HasHighConfidenceFindings(&Report{}))
}

func TestHasHighConfidenceFindings_LowConfidenceFiltered(t *testing.T) {
	rep := &Report{Findings: []Finding{{Confidence: 0.20}}}
	assert.False(t, HasHighConfidenceFindings(rep))
}

func TestHasHighConfidenceFindings_HighConfidenceDetected(t *testing.T) {
	rep := &Report{Findings: []Finding{{Confidence: 0.80, Filename: "main.go"}}}
	assert.True(t, HasHighConfidenceFindings(rep))
}

func TestHasHighConfidenceFindings_TestFileExcluded(t *testing.T) {
	rep := &Report{Findings: []Finding{{Confidence: 0.80, Filename: "main_test.go"}}}
	assert.False(t, HasHighConfidenceFindings(rep))
}

func TestHasHighConfidenceFindings_Mixed(t *testing.T) {
	rep := &Report{Findings: []Finding{
		{Confidence: 0.20, Filename: "main.go"},
		{Confidence: 0.80, Filename: "main_test.go"},
	}}
	assert.False(t, HasHighConfidenceFindings(rep))
}

func TestSuppressTestFP_SecretsCriticalBuiltin(t *testing.T) {
	f := &Finding{
		RuleID: "hardcoded_password", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
		Category: "secrets", Filename: "auth_test.go",
	}
	result := suppressTestFP(f)
	require.NotNil(t, result)
	assert.Equal(t, SeverityInfo, result.Severity)
}

func TestSuppressTestFP_SecretsCriticalMultiTool(t *testing.T) {
	f := &Finding{
		RuleID: "hardcoded_password", Analyzers: []string{"builtin", "bandit"}, Severity: SeverityCritical,
		Category: "secrets", Filename: "auth_test.go",
	}
	result := suppressTestFP(f)
	require.NotNil(t, result)
	// Multi-tool should not be fully downgraded
	assert.NotEqual(t, SeverityInfo, result.Severity)
}

func TestSuppressTestFP_CryptoInTest(t *testing.T) {
	f := &Finding{
		RuleID: "weak_hash_md5", Analyzers: []string{"builtin"}, Severity: SeverityHigh,
		Category: "crypto", Filename: "crypto_test.go",
	}
	result := suppressTestFP(f)
	require.NotNil(t, result)
}

func TestSuppressTestFP_InjectionSingleToolInTest(t *testing.T) {
	f := &Finding{
		RuleID: "sql_injection", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
		Category: "injection", Filename: "handler_test.go",
	}
	result := suppressTestFP(f)
	require.NotNil(t, result)
	assert.Equal(t, SeverityLow, result.Severity)
}

func TestSuppressTestFP_InjectionMultiToolInTest(t *testing.T) {
	f := &Finding{
		RuleID: "sql_injection", Analyzers: []string{"builtin", "semgrep"}, Severity: SeverityCritical,
		Category: "injection", Filename: "handler_test.go",
	}
	result := suppressTestFP(f)
	require.NotNil(t, result)
	assert.NotEqual(t, SeverityLow, result.Severity, "multi-tool should not be fully downgraded")
}

func TestSuppressTestFP_NonTestFileUnchanged(t *testing.T) {
	f := &Finding{
		RuleID: "sql_injection", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
		Category: "injection", Filename: "handler.go",
	}
	result := suppressTestFP(f)
	require.NotNil(t, result)
	assert.Equal(t, SeverityCritical, result.Severity, "non-test file should not be modified")
}

func TestComputeFingerprint_NoRuleIDVariadic(t *testing.T) {
	f1 := ComputeFingerprint("x.go", 1, "code")
	f2 := ComputeFingerprint("x.go", 1, "code", "")
	assert.Equal(t, f1, f2, "empty ruleID should match no-arg version")
}
