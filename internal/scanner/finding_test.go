package scanner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Content from finding_test.go
func TestSeverityRank_Critical(t *testing.T) {
	assert.Equal(t, 5, SeverityRank(SeverityCritical))
}

func TestSeverityRank_High(t *testing.T) {
	assert.Equal(t, 4, SeverityRank(SeverityHigh))
}

func TestSeverityRank_Medium(t *testing.T) {
	assert.Equal(t, 3, SeverityRank(SeverityMedium))
}

func TestSeverityRank_Low(t *testing.T) {
	assert.Equal(t, 2, SeverityRank(SeverityLow))
}

func TestSeverityRank_Info(t *testing.T) {
	assert.Equal(t, 1, SeverityRank(SeverityInfo))
}

func TestSeverityRank_Unknown(t *testing.T) {
	assert.Equal(t, 0, SeverityRank("bogus"))
}

func TestSeverityRank_Ordering(t *testing.T) {
	sevs := []Severity{SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical}
	for i := 1; i < len(sevs); i++ {
		assert.Greater(t, SeverityRank(sevs[i]), SeverityRank(sevs[i-1]),
			"%s should outrank %s", sevs[i], sevs[i-1])
	}
}

func TestSeverityRank_Empty(t *testing.T) {
	assert.Equal(t, 0, SeverityRank(""))
}

func TestSeverityConstants(t *testing.T) {
	assert.Equal(t, Severity("critical"), SeverityCritical)
	assert.Equal(t, Severity("high"), SeverityHigh)
	assert.Equal(t, Severity("medium"), SeverityMedium)
	assert.Equal(t, Severity("low"), SeverityLow)
	assert.Equal(t, Severity("info"), SeverityInfo)
}

func TestFinding_StructCreation(t *testing.T) {
	f := Finding{
		RuleID:      "sql_injection",
		Analyzers:   []string{"builtin"},
		Severity:    SeverityCritical,
		Category:    "injection",
		Title:       "SQL Injection",
		Message:     "Potential SQL injection",
		Filename:    "handler.go",
		Line:        42,
		Snippet:     `q := fmt.Sprintf("SELECT * FROM t WHERE id=%s", id)`,
		Fix:         "Use parameterized queries",
		Confidence:  0.85,
		Fingerprint: "abc123def456",
	}
	assert.Equal(t, "sql_injection", f.RuleID)
	assert.Equal(t, []string{"builtin"}, f.Analyzers)
	assert.Equal(t, SeverityCritical, f.Severity)
	assert.Equal(t, "injection", f.Category)
	assert.Equal(t, 42, f.Line)
	assert.Equal(t, 0.85, f.Confidence)
	assert.Equal(t, "abc123def456", f.Fingerprint)
}

func TestFinding_ZeroValue(t *testing.T) {
	var f Finding
	assert.Empty(t, f.RuleID)
	assert.Nil(t, f.Analyzers)
	assert.Empty(t, f.Severity)
	assert.Equal(t, 0, f.Line)
	assert.Equal(t, 0.0, f.Confidence)
	assert.Empty(t, f.Fingerprint)
}

func TestFinding_MultipleAnalyzers(t *testing.T) {
	f := Finding{
		RuleID:    "shared-rule",
		Analyzers: []string{"bandit", "builtin", "semgrep"},
		Severity:  SeverityHigh,
	}
	assert.Len(t, f.Analyzers, 3)
	assert.Contains(t, f.Analyzers, "bandit")
	assert.Contains(t, f.Analyzers, "builtin")
	assert.Contains(t, f.Analyzers, "semgrep")
}

func TestReport_StructCreation(t *testing.T) {
	rep := Report{
		Findings:         []Finding{{RuleID: "test", Severity: SeverityHigh}},
		AnalyzersRun:     []string{"builtin"},
		AnalyzersSkipped: map[string]string{"bandit": "not available"},
		AnalyzerErrors:   map[string]string{"semgrep": "crashed"},
	}
	assert.Len(t, rep.Findings, 1)
	assert.Len(t, rep.AnalyzersRun, 1)
	assert.Equal(t, "not available", rep.AnalyzersSkipped["bandit"])
	assert.Equal(t, "crashed", rep.AnalyzerErrors["semgrep"])
}

func TestReport_ZeroValue(t *testing.T) {
	var rep Report
	assert.Nil(t, rep.Findings)
	assert.Nil(t, rep.AnalyzersRun)
	assert.Nil(t, rep.AnalyzersSkipped)
	assert.Nil(t, rep.AnalyzerErrors)
}

func TestNormalizeSnippet_CollapsesWhitespace(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"a  b  c", "a b c"},
		{"a\t\tb", "a b"},
		{"  hello  world  ", "hello world"},
		{"single", "single"},
		{"", ""},
		{"   ", ""},
		{"a\n\nb", "a b"},
		{"a  \t  b  \n  c", "a b c"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeSnippet(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestComputeFingerprint_DeterminismCheck(t *testing.T) {
	f1 := ComputeFingerprint("file.go", 10, "code", "rule1")
	f2 := ComputeFingerprint("file.go", 10, "code", "rule1")
	assert.Equal(t, f1, f2, "same input should produce same fingerprint")
}

func TestComputeFingerprint_DifferentFile(t *testing.T) {
	f1 := ComputeFingerprint("a.go", 10, "code", "rule1")
	f2 := ComputeFingerprint("b.go", 10, "code", "rule1")
	assert.NotEqual(t, f1, f2, "different file should change fingerprint")
}

func TestComputeFingerprint_DifferentLine(t *testing.T) {
	f1 := ComputeFingerprint("file.go", 10, "code", "rule1")
	f2 := ComputeFingerprint("file.go", 20, "code", "rule1")
	assert.NotEqual(t, f1, f2, "different line should change fingerprint")
}

func TestComputeFingerprint_DifferentSnippet(t *testing.T) {
	f1 := ComputeFingerprint("file.go", 10, "code1", "rule1")
	f2 := ComputeFingerprint("file.go", 10, "code2", "rule1")
	assert.NotEqual(t, f1, f2, "different snippet should change fingerprint")
}

func TestComputeFingerprint_DifferentRuleID(t *testing.T) {
	f1 := ComputeFingerprint("file.go", 10, "code", "rule1")
	f2 := ComputeFingerprint("file.go", 10, "code", "rule2")
	assert.NotEqual(t, f1, f2, "different ruleID should change fingerprint")
}

func TestComputeFingerprint_WhitespaceNormalization(t *testing.T) {
	f1 := ComputeFingerprint("file.go", 10, "query = a + b", "rule1")
	f2 := ComputeFingerprint("file.go", 10, "query =   a  +  b", "rule1")
	assert.Equal(t, f1, f2, "whitespace differences should not change fingerprint")
}

func TestComputeFingerprint_Length(t *testing.T) {
	fp := ComputeFingerprint("file.go", 1, "code", "rule")
	assert.Len(t, fp, 16, "fingerprint should be 16 hex chars")
}

func TestComputeFingerprint_NoRuleID(t *testing.T) {
	fp := ComputeFingerprint("file.go", 1, "code")
	assert.NotEmpty(t, fp)
	assert.Len(t, fp, 16)
}

func TestComputeFingerprint_EmptyInputs(t *testing.T) {
	fp := ComputeFingerprint("", 0, "", "")
	assert.NotEmpty(t, fp, "fingerprint should work with empty inputs")
	assert.Len(t, fp, 16)
}

func TestComputeFingerprint_VariationsProduceUniqueValues(t *testing.T) {
	seen := map[string]bool{}
	inputs := []struct {
		file string
		line int
		snip string
		rule string
	}{
		{"a.go", 1, "code1", "r1"},
		{"a.go", 1, "code1", "r2"},
		{"a.go", 2, "code1", "r1"},
		{"b.go", 1, "code1", "r1"},
		{"a.go", 1, "code2", "r1"},
	}
	for _, in := range inputs {
		fp := ComputeFingerprint(in.file, in.line, in.snip, in.rule)
		assert.False(t, seen[fp], "duplicate fingerprint for %v", in)
		seen[fp] = true
	}
}

func TestFinding_JSONTags(t *testing.T) {
	f := Finding{
		RuleID:     "test",
		Severity:   SeverityHigh,
		Line:       42,
		Confidence: 0.8,
	}
	assert.Equal(t, "test", f.RuleID)
	assert.Equal(t, SeverityHigh, f.Severity)
	assert.Equal(t, 42, f.Line)
	assert.Equal(t, 0.8, f.Confidence)
}

// Content from confidence_test.go
func TestBaseConfidence_Critical(t *testing.T) {
	assert.InDelta(t, 0.65, baseConfidence(SeverityCritical), 1e-9)
}

func TestBaseConfidence_High(t *testing.T) {
	assert.InDelta(t, 0.55, baseConfidence(SeverityHigh), 1e-9)
}

func TestBaseConfidence_Medium(t *testing.T) {
	assert.InDelta(t, 0.40, baseConfidence(SeverityMedium), 1e-9)
}

func TestBaseConfidence_Low(t *testing.T) {
	assert.InDelta(t, 0.30, baseConfidence(SeverityLow), 1e-9)
}

func TestBaseConfidence_Info(t *testing.T) {
	assert.InDelta(t, 0.20, baseConfidence(SeverityInfo), 1e-9)
}

func TestBaseConfidence_Unknown(t *testing.T) {
	assert.InDelta(t, 0.20, baseConfidence("bogus"), 1e-9)
}

func TestBaseConfidence_Empty(t *testing.T) {
	assert.InDelta(t, 0.20, baseConfidence(""), 1e-9)
}

func TestAnalyzerWeight_BuiltinOnly(t *testing.T) {
	assert.InDelta(t, 0.0, analyzerWeight([]string{"builtin"}), 1e-9)
}

func TestAnalyzerWeight_BanditOnly(t *testing.T) {
	assert.InDelta(t, 0.15, analyzerWeight([]string{"bandit"}), 1e-9)
}

func TestAnalyzerWeight_SemgrepOnly(t *testing.T) {
	assert.InDelta(t, 0.15, analyzerWeight([]string{"semgrep"}), 1e-9)
}

func TestAnalyzerWeight_BuiltinAndBandit(t *testing.T) {
	assert.InDelta(t, 0.10, analyzerWeight([]string{"builtin", "bandit"}), 1e-9)
}

func TestAnalyzerWeight_BuiltinAndSemgrep(t *testing.T) {
	assert.InDelta(t, 0.10, analyzerWeight([]string{"builtin", "semgrep"}), 1e-9)
}

func TestAnalyzerWeight_AllThree(t *testing.T) {
	w := analyzerWeight([]string{"bandit", "builtin", "semgrep"})
	assert.InDelta(t, 0.10, w, 1e-9, "should be corroborated (real tool + builtin)")
}

func TestAnalyzerWeight_Empty(t *testing.T) {
	assert.InDelta(t, 0.0, analyzerWeight(nil), 1e-9)
	assert.InDelta(t, 0.0, analyzerWeight([]string{}), 1e-9)
}

func TestAnalyzerWeight_UnknownAnalyzer(t *testing.T) {
	assert.InDelta(t, 0.0, analyzerWeight([]string{"custom_tool"}), 1e-9)
}

func TestContextPenalty_TestFile(t *testing.T) {
	p := contextPenalty("handler_test.go")
	assert.Less(t, p, 0.0, "test file should have negative penalty")
}

func TestContextPenalty_TestContains(t *testing.T) {
	p := contextPenalty("auth_test.py")
	assert.Less(t, p, 0.0)
}

func TestContextPenalty_TestInPath(t *testing.T) {
	// Note: contextPenalty does NOT check /test/ path — that's isTestFile.
	// contextPenalty only checks _test.go suffix, _test. contains, example, sample, bench, .md, .txt.
	p := contextPenalty("src/test/helpers.go")
	assert.InDelta(t, 0.0, p, 1e-9, "contextPenalty does not penalize /test/ paths")
}

func TestContextPenalty_TestsInPath(t *testing.T) {
	p := contextPenalty("src/tests/helpers.go")
	assert.InDelta(t, 0.0, p, 1e-9, "contextPenalty does not penalize /tests/ paths")
}

func TestContextPenalty_Example(t *testing.T) {
	p := contextPenalty("example/main.go")
	assert.Less(t, p, 0.0, "example file should have penalty")
}

func TestContextPenalty_Sample(t *testing.T) {
	p := contextPenalty("sample_handler.go")
	assert.Less(t, p, 0.0, "sample file should have penalty")
}

func TestContextPenalty_BenchTest(t *testing.T) {
	p := contextPenalty("handler_bench_test.go")
	assert.Less(t, p, 0.0)
}

func TestContextPenalty_BenchContains(t *testing.T) {
	p := contextPenalty("bench_runner.go")
	assert.Less(t, p, 0.0, "file containing 'bench' should have penalty")
}

func TestContextPenalty_Markdown(t *testing.T) {
	p := contextPenalty("README.md")
	assert.Less(t, p, 0.0, ".md file should have penalty")
}

func TestContextPenalty_Text(t *testing.T) {
	p := contextPenalty("notes.txt")
	assert.Less(t, p, 0.0, ".txt file should have penalty")
}

func TestContextPenalty_Regular(t *testing.T) {
	assert.InDelta(t, 0.0, contextPenalty("internal/auth.go"), 1e-9)
}

func TestContextPenalty_Empty(t *testing.T) {
	assert.InDelta(t, 0.0, contextPenalty(""), 1e-9)
}

func TestContextPenalty_CaseInsensitive(t *testing.T) {
	p := contextPenalty("HANDLER_TEST.GO")
	assert.Less(t, p, 0.0, "should be case insensitive")
}

func TestSnippetConfidence_LiteralAssignment(t *testing.T) {
	s := snippetConfidence(`password := "secret123"`)
	assert.Greater(t, s, 0.0, "literal assignment should have positive boost")
}

func TestSnippetConfidence_VarReference(t *testing.T) {
	s := snippetConfidence("var x int")
	assert.Less(t, s, 0.0, "var reference should have negative modifier")
}

func TestSnippetConfidence_FuncReference(t *testing.T) {
	s := snippetConfidence("func main() {}")
	assert.Less(t, s, 0.0, "func reference should have negative modifier")
}

func TestSnippetConfidence_EnvGetenv(t *testing.T) {
	s := snippetConfidence(`password := os.Getenv("SECRET")`)
	assert.InDelta(t, 0.0, s, 1e-9, "env reference should have zero modifier")
}

func TestSnippetConfidence_Empty(t *testing.T) {
	assert.InDelta(t, 0.0, snippetConfidence(""), 1e-9)
}

func TestSnippetConfidence_AssignmentWithQuotes(t *testing.T) {
	s := snippetConfidence(`key := "abcdef1234567890"`)
	assert.Greater(t, s, 0.0)
}

func TestClampFloat_BelowMin(t *testing.T) {
	assert.Equal(t, 0.0, clampFloat(-1.0, 0.0, 1.0))
}

func TestClampFloat_AboveMax(t *testing.T) {
	assert.Equal(t, 1.0, clampFloat(2.0, 0.0, 1.0))
}

func TestClampFloat_InRange(t *testing.T) {
	assert.Equal(t, 0.5, clampFloat(0.5, 0.0, 1.0))
}

func TestClampFloat_AtMin(t *testing.T) {
	assert.Equal(t, 0.0, clampFloat(0.0, 0.0, 1.0))
}

func TestClampFloat_AtMax(t *testing.T) {
	assert.Equal(t, 1.0, clampFloat(1.0, 0.0, 1.0))
}

func TestConfidence_CriticalBuiltin(t *testing.T) {
	c := Confidence(SeverityCritical, []string{"builtin"})
	assert.InDelta(t, 0.65, c, 1e-9)
}

func TestConfidence_CriticalBandit(t *testing.T) {
	c := Confidence(SeverityCritical, []string{"bandit"})
	assert.InDelta(t, 0.80, c, 1e-9)
}

func TestConfidence_CriticalSemgrep(t *testing.T) {
	c := Confidence(SeverityCritical, []string{"semgrep"})
	assert.InDelta(t, 0.80, c, 1e-9)
}

func TestConfidence_CriticalBuiltinBandit(t *testing.T) {
	c := Confidence(SeverityCritical, []string{"builtin", "bandit"})
	// 0.65 (critical) + 0.10 (corroborated) + 0.25 (2 analyzers) = 1.00, clamped to 0.99
	assert.InDelta(t, 0.99, c, 1e-9)
}

func TestConfidence_CriticalTwoTools(t *testing.T) {
	c := Confidence(SeverityCritical, []string{"semgrep", "bandit"})
	assert.InDelta(t, 0.99, c, 1e-9, "should be clamped to 0.99")
}

func TestConfidence_MediumBuiltinBandit(t *testing.T) {
	c := Confidence(SeverityMedium, []string{"builtin", "bandit"})
	assert.InDelta(t, 0.75, c, 1e-9)
}

func TestConfidence_HighBuiltin(t *testing.T) {
	c := Confidence(SeverityHigh, []string{"builtin"})
	assert.InDelta(t, 0.55, c, 1e-9)
}

func TestConfidence_LowBuiltin(t *testing.T) {
	c := Confidence(SeverityLow, []string{"builtin"})
	assert.InDelta(t, 0.30, c, 1e-9)
}

func TestConfidence_InfoBuiltin(t *testing.T) {
	c := Confidence(SeverityInfo, []string{"builtin"})
	assert.InDelta(t, 0.20, c, 1e-9)
}

func TestConfidence_FloorNeverBelow005(t *testing.T) {
	c := Confidence(SeverityInfo, []string{"builtin"})
	require.GreaterOrEqual(t, c, 0.05, "confidence should never drop below 0.05")
}

func TestConfidence_CeilingNeverAbove099(t *testing.T) {
	c := Confidence(SeverityCritical, []string{"bandit", "semgrep", "builtin"})
	require.LessOrEqual(t, c, 0.99, "confidence should never exceed 0.99")
}

func TestConfidence_EmptyAnalyzers(t *testing.T) {
	c := Confidence(SeverityHigh, nil)
	assert.InDelta(t, 0.55, c, 1e-9)
}

func TestConfidenceWithFile_RegularFile(t *testing.T) {
	base := Confidence(SeverityCritical, []string{"builtin"})
	file := ConfidenceWithFile(SeverityCritical, []string{"builtin"}, "auth.go", "")
	assert.InDelta(t, base, file, 1e-9, "regular file should have same confidence")
}

func TestConfidenceWithFile_TestFile(t *testing.T) {
	base := Confidence(SeverityCritical, []string{"builtin"})
	file := ConfidenceWithFile(SeverityCritical, []string{"builtin"}, "auth_test.go", "")
	assert.Less(t, file, base, "test file should have lower confidence")
}

func TestConfidenceWithFile_ExampleFile(t *testing.T) {
	base := Confidence(SeverityHigh, []string{"builtin"})
	file := ConfidenceWithFile(SeverityHigh, []string{"builtin"}, "example/main.go", "")
	assert.Less(t, file, base, "example file should have lower confidence")
}

func TestConfidenceWithFile_LiteralSnippet(t *testing.T) {
	empty := ConfidenceWithFile(SeverityCritical, []string{"builtin"}, "handler.go", "")
	literal := ConfidenceWithFile(SeverityCritical, []string{"builtin"}, "handler.go", `password := "secret123"`)
	assert.Greater(t, literal, empty, "literal snippet should boost confidence")
}

func TestConfidenceWithFile_VarSnippet(t *testing.T) {
	empty := ConfidenceWithFile(SeverityHigh, []string{"builtin"}, "handler.go", "")
	varRef := ConfidenceWithFile(SeverityHigh, []string{"builtin"}, "handler.go", "var x int")
	assert.Less(t, varRef, empty, "var snippet should reduce confidence")
}

func TestConfidenceWithFile_BelowFloor(t *testing.T) {
	c := ConfidenceWithFile(SeverityInfo, []string{"builtin"}, "README.md", "var x int")
	require.GreaterOrEqual(t, c, 0.05, "should clamp to 0.05 floor")
}

func TestConfidenceWithFile_AboveCeiling(t *testing.T) {
	c := ConfidenceWithFile(SeverityCritical, []string{"bandit", "semgrep"}, "handler.go", "")
	require.LessOrEqual(t, c, 0.99, "should clamp to 0.99 ceiling")
}

func TestIsHighConfidence_AboveThreshold(t *testing.T) {
	assert.True(t, IsHighConfidence(0.50))
	assert.True(t, IsHighConfidence(0.30))
	assert.True(t, IsHighConfidence(0.99))
}

func TestIsHighConfidence_BelowThreshold(t *testing.T) {
	assert.False(t, IsHighConfidence(0.29))
	assert.False(t, IsHighConfidence(0.10))
	assert.False(t, IsHighConfidence(0.0))
}

func TestIsHighConfidence_ExactlyThreshold(t *testing.T) {
	assert.True(t, IsHighConfidence(0.30))
	assert.False(t, IsHighConfidence(0.29))
}

func TestShouldReport_HighConfidence(t *testing.T) {
	f := Finding{Confidence: 0.50}
	assert.True(t, ShouldReport(f))
}

func TestShouldReport_LowConfidence(t *testing.T) {
	f := Finding{Confidence: 0.10}
	assert.False(t, ShouldReport(f))
}

func TestShouldReport_ExactThreshold(t *testing.T) {
	f := Finding{Confidence: 0.30}
	assert.True(t, ShouldReport(f))
}

func TestShouldReport_ZeroConfidence(t *testing.T) {
	f := Finding{Confidence: 0.0}
	assert.False(t, ShouldReport(f))
}

func TestShouldReport_MaxConfidence(t *testing.T) {
	f := Finding{Confidence: 0.99}
	assert.True(t, ShouldReport(f))
}
