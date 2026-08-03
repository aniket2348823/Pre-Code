package scanner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBanditSeverity_AllValues(t *testing.T) {
	tests := []struct {
		input string
		want  Severity
	}{
		{"HIGH", SeverityHigh},
		{"MEDIUM", SeverityMedium},
		{"LOW", SeverityLow},
		{"INFO", SeverityInfo},
		{"CRITICAL", SeverityInfo},
		{"", SeverityInfo},
		{"UNKNOWN", SeverityInfo},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, banditSeverity(tt.input))
		})
	}
}

func TestBanditAnalyzer_Name(t *testing.T) {
	b := NewBanditAnalyzer(nil)
	assert.Equal(t, "bandit", b.Name())
}

func TestBanditAnalyzer_NilRunner(t *testing.T) {
	b := NewBanditAnalyzer(nil)
	assert.NotNil(t, b.runner, "nil runner should default to ExecRunner")
	_, ok := b.runner.(ExecRunner)
	assert.True(t, ok, "should default to ExecRunner")
}

func TestBanditAnalyzer_CustomRunner(t *testing.T) {
	fr := &fakeRunner{stdout: `{"results":[]}`}
	b := NewBanditAnalyzer(fr)
	assert.Equal(t, fr, b.runner)
}

func TestBanditAnalyzer_SkipsNonPython(t *testing.T) {
	b := NewBanditAnalyzer(&fakeRunner{stdout: `{"results":[]}`})
	b.exists = func() bool { return true }

	tests := []string{"go", "javascript", "rust", "java", "c", "ruby"}
	for _, lang := range tests {
		t.Run(lang, func(t *testing.T) {
			findings, err := b.Analyze(context.Background(), Input{Language: lang, Code: "x=1"})
			assert.NoError(t, err)
			assert.Nil(t, findings, "bandit should skip non-python language: %s", lang)
		})
	}
}

func TestBanditAnalyzer_EmptyLanguageRunsPython(t *testing.T) {
	canned := `{"results":[]}`
	fr := &fakeRunner{stdout: canned}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	_, err := b.Analyze(context.Background(), Input{Language: "", Code: "x=1", Filename: "a.py"})
	assert.NoError(t, err)
	assert.Equal(t, "bandit", fr.gotName, "empty language should invoke bandit")
}

func TestBanditAnalyzer_PythonLanguageRuns(t *testing.T) {
	canned := `{"results":[]}`
	fr := &fakeRunner{stdout: canned}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	_, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x=1"})
	assert.NoError(t, err)
	assert.Equal(t, "bandit", fr.gotName)
}

func TestBanditAnalyzer_EmptyResults(t *testing.T) {
	canned := `{"results":[]}`
	fr := &fakeRunner{stdout: canned}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	findings, err := b.Analyze(context.Background(), Input{Language: "python", Code: "clean", Filename: "clean.py"})
	assert.NoError(t, err)
	assert.Empty(t, findings)
}

func TestBanditAnalyzer_MultipleResults(t *testing.T) {
	canned := `{"results":[{"filename":"a.py","issue_severity":"HIGH","issue_text":"SQL injection","test_id":"B608","test_name":"sql_injection","line_number":1,"code":"query"},{"filename":"a.py","issue_severity":"LOW","issue_text":"Hardcoded password","test_id":"B105","test_name":"hardcoded_password_string","line_number":5,"code":"pw"}]}`
	fr := &fakeRunner{stdout: canned}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	findings, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x=1", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 2)
	assert.Equal(t, "B608", findings[0].RuleID)
	assert.Equal(t, SeverityHigh, findings[0].Severity)
	assert.Equal(t, "B105", findings[1].RuleID)
	assert.Equal(t, SeverityLow, findings[1].Severity)
}

func TestBanditAnalyzer_AllSeveritiesParsed(t *testing.T) {
	canned := `{"results":[{"filename":"a.py","issue_severity":"HIGH","issue_text":"h","test_id":"H","test_name":"h","line_number":1,"code":"x"},{"filename":"a.py","issue_severity":"MEDIUM","issue_text":"m","test_id":"M","test_name":"m","line_number":2,"code":"y"},{"filename":"a.py","issue_severity":"LOW","issue_text":"l","test_id":"L","test_name":"l","line_number":3,"code":"z"}]}`
	fr := &fakeRunner{stdout: canned}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	findings, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x=1", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 3)
	assert.Equal(t, SeverityHigh, findings[0].Severity)
	assert.Equal(t, SeverityMedium, findings[1].Severity)
	assert.Equal(t, SeverityLow, findings[2].Severity)
}

func TestBanditAnalyzer_EmptyFilename(t *testing.T) {
	canned := `{"results":[{"filename":"snippet.py","issue_severity":"LOW","issue_text":"test","test_id":"B999","test_name":"test_rule","line_number":1,"code":"x"}]}`
	fr := &fakeRunner{stdout: canned}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	findings, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x"})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "snippet.py", findings[0].Filename, "should use default filename")
}

func TestBanditAnalyzer_FingerprintComputed(t *testing.T) {
	canned := `{"results":[{"filename":"a.py","issue_severity":"HIGH","issue_text":"test","test_id":"B100","test_name":"test","line_number":1,"code":"x"}]}`
	fr := &fakeRunner{stdout: canned}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	findings, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.NotEmpty(t, findings[0].Fingerprint)
	assert.Len(t, findings[0].Fingerprint, 16)
}

func TestBanditAnalyzer_FingerprintMatchesComputed(t *testing.T) {
	canned := `{"results":[{"filename":"a.py","issue_severity":"HIGH","issue_text":"test","test_id":"B100","test_name":"test","line_number":5,"code":"dangerous()"}]}`
	fr := &fakeRunner{stdout: canned}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	findings, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 1)

	expected := ComputeFingerprint("a.py", 5, "dangerous()", "B100")
	assert.Equal(t, expected, findings[0].Fingerprint)
}

func TestBanditAnalyzer_AnalyzerTag(t *testing.T) {
	canned := `{"results":[{"filename":"a.py","issue_severity":"HIGH","issue_text":"test","test_id":"B100","test_name":"test","line_number":1,"code":"x"}]}`
	fr := &fakeRunner{stdout: canned}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	findings, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, []string{"bandit"}, findings[0].Analyzers)
}

func TestBanditAnalyzer_CommandArgs(t *testing.T) {
	fr := &fakeRunner{stdout: `{"results":[]}`}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	_, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x", Filename: "test.py"})
	require.NoError(t, err)
	assert.Equal(t, "bandit", fr.gotName)
	require.Len(t, fr.gotArgs, 4)
	assert.Equal(t, "-f", fr.gotArgs[0])
	assert.Equal(t, "json", fr.gotArgs[1])
	assert.Equal(t, "-q", fr.gotArgs[2])
	assert.Contains(t, fr.gotArgs[3], "test.py")
}

func TestBanditAnalyzer_UnparseableOutputWithRunnerError(t *testing.T) {
	fr := &fakeRunner{stdout: "not json", err: assert.AnError}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	_, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x", Filename: "a.py"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bandit")
}

func TestBanditAnalyzer_UnparseableOutputNoRunnerError(t *testing.T) {
	fr := &fakeRunner{stdout: "not json"}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	_, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x", Filename: "a.py"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unparseable")
}
