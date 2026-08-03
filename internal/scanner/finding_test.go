package scanner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
		file   string
		line   int
		snip   string
		rule   string
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
		RuleID:    "test",
		Severity:  SeverityHigh,
		Line:      42,
		Confidence: 0.8,
	}
	assert.Equal(t, "test", f.RuleID)
	assert.Equal(t, SeverityHigh, f.Severity)
	assert.Equal(t, 42, f.Line)
	assert.Equal(t, 0.8, f.Confidence)
}
