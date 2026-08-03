package scanner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSemgrepSeverity_AllValues(t *testing.T) {
	tests := []struct {
		input string
		want  Severity
	}{
		{"ERROR", SeverityHigh},
		{"WARNING", SeverityMedium},
		{"INFO", SeverityLow},
		{"CRITICAL", SeverityInfo},
		{"", SeverityInfo},
		{"UNKNOWN", SeverityInfo},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, semgrepSeverity(tt.input))
		})
	}
}

func TestSemgrepAnalyzer_Name(t *testing.T) {
	s := NewSemgrepAnalyzer(nil)
	assert.Equal(t, "semgrep", s.Name())
}

func TestSemgrepAnalyzer_NilRunner(t *testing.T) {
	s := NewSemgrepAnalyzer(nil)
	assert.NotNil(t, s.runner, "nil runner should default to ExecRunner")
	_, ok := s.runner.(ExecRunner)
	assert.True(t, ok, "should default to ExecRunner")
}

func TestSemgrepAnalyzer_CustomRunner(t *testing.T) {
	fr := &fakeRunner{stdout: `{"results":[]}`}
	s := NewSemgrepAnalyzer(fr)
	assert.Equal(t, fr, s.runner)
}

func TestSemgrepAnalyzer_EmptyResults(t *testing.T) {
	canned := `{"results":[]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	findings, err := s.Analyze(context.Background(), Input{Code: "x=1", Filename: "a.py"})
	assert.NoError(t, err)
	assert.Empty(t, findings)
}

func TestSemgrepAnalyzer_MultipleResults(t *testing.T) {
	canned := `{"results":[{"check_id":"rule1","path":"a.py","start":{"line":1},"extra":{"message":"msg1","severity":"ERROR","lines":"code1","metadata":{"category":"sec"}}},{"check_id":"rule2","path":"a.py","start":{"line":5},"extra":{"message":"msg2","severity":"WARNING","lines":"code2","metadata":{"category":"quality"}}}]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	findings, err := s.Analyze(context.Background(), Input{Code: "x=1", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 2)
	assert.Equal(t, "rule1", findings[0].RuleID)
	assert.Equal(t, SeverityHigh, findings[0].Severity)
	assert.Equal(t, "rule2", findings[1].RuleID)
	assert.Equal(t, SeverityMedium, findings[1].Severity)
}

func TestSemgrepAnalyzer_AllSeveritiesParsed(t *testing.T) {
	canned := `{"results":[{"check_id":"r1","path":"a.py","start":{"line":1},"extra":{"message":"e","severity":"ERROR","lines":"x","metadata":{"category":"c"}}},{"check_id":"r2","path":"a.py","start":{"line":2},"extra":{"message":"e","severity":"WARNING","lines":"x","metadata":{"category":"c"}}},{"check_id":"r3","path":"a.py","start":{"line":3},"extra":{"message":"e","severity":"INFO","lines":"x","metadata":{"category":"c"}}}]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	findings, err := s.Analyze(context.Background(), Input{Code: "x=1", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 3)
	assert.Equal(t, SeverityHigh, findings[0].Severity)
	assert.Equal(t, SeverityMedium, findings[1].Severity)
	assert.Equal(t, SeverityLow, findings[2].Severity)
}

func TestSemgrepAnalyzer_EmptyFilename(t *testing.T) {
	canned := `{"results":[{"check_id":"test.rule","path":"snippet.txt","start":{"line":1},"extra":{"message":"test","severity":"ERROR","lines":"code","metadata":{"category":"sec"}}}]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	findings, err := s.Analyze(context.Background(), Input{Code: "x"})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "snippet.txt", findings[0].Filename)
}

func TestSemgrepAnalyzer_FingerprintComputed(t *testing.T) {
	canned := `{"results":[{"check_id":"rule1","path":"a.py","start":{"line":10},"extra":{"message":"test","severity":"ERROR","lines":"dangerous()","metadata":{"category":"sec"}}}]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	findings, err := s.Analyze(context.Background(), Input{Code: "x", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.NotEmpty(t, findings[0].Fingerprint)
	assert.Len(t, findings[0].Fingerprint, 16)
}

func TestSemgrepAnalyzer_FingerprintMatchesComputed(t *testing.T) {
	canned := `{"results":[{"check_id":"rule1","path":"a.py","start":{"line":5},"extra":{"message":"test","severity":"ERROR","lines":"code()","metadata":{"category":"sec"}}}]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	findings, err := s.Analyze(context.Background(), Input{Code: "x", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 1)

	expected := ComputeFingerprint("a.py", 5, "code()", "rule1")
	assert.Equal(t, expected, findings[0].Fingerprint)
}

func TestSemgrepAnalyzer_AnalyzerTag(t *testing.T) {
	canned := `{"results":[{"check_id":"rule1","path":"a.py","start":{"line":1},"extra":{"message":"test","severity":"ERROR","lines":"x","metadata":{"category":"sec"}}}]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	findings, err := s.Analyze(context.Background(), Input{Code: "x", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, []string{"semgrep"}, findings[0].Analyzers)
}

func TestSemgrepAnalyzer_CommandArgs(t *testing.T) {
	fr := &fakeRunner{stdout: `{"results":[]}`}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	_, err := s.Analyze(context.Background(), Input{Code: "x", Filename: "test.py"})
	require.NoError(t, err)
	assert.Equal(t, "semgrep", fr.gotName)
	require.Len(t, fr.gotArgs, 5)
	assert.Equal(t, "--json", fr.gotArgs[0])
	assert.Equal(t, "--config", fr.gotArgs[1])
	assert.Equal(t, "auto", fr.gotArgs[2])
	assert.Equal(t, "-q", fr.gotArgs[3])
	assert.Contains(t, fr.gotArgs[4], "test.py")
}

func TestSemgrepAnalyzer_UnparseableOutputWithRunnerError(t *testing.T) {
	fr := &fakeRunner{stdout: "not json", err: assert.AnError}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	_, err := s.Analyze(context.Background(), Input{Code: "x", Filename: "a.py"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "semgrep")
}

func TestSemgrepAnalyzer_UnparseableOutputNoRunnerError(t *testing.T) {
	fr := &fakeRunner{stdout: "not json"}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	_, err := s.Analyze(context.Background(), Input{Code: "x", Filename: "a.py"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unparseable")
}

func TestSemgrepAnalyzer_Category(t *testing.T) {
	canned := `{"results":[{"check_id":"rule1","path":"a.py","start":{"line":1},"extra":{"message":"test","severity":"ERROR","lines":"x","metadata":{"category":"injection"}}}]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	findings, err := s.Analyze(context.Background(), Input{Code: "x", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "injection", findings[0].Category)
}

func TestSemgrepAnalyzer_Message(t *testing.T) {
	canned := `{"results":[{"check_id":"rule1","path":"a.py","start":{"line":1},"extra":{"message":"SQL injection detected","severity":"ERROR","lines":"x","metadata":{"category":"sec"}}}]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	findings, err := s.Analyze(context.Background(), Input{Code: "x", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "SQL injection detected", findings[0].Message)
}

func TestSemgrepAnalyzer_Snippet(t *testing.T) {
	canned := `{"results":[{"check_id":"rule1","path":"a.py","start":{"line":1},"extra":{"message":"test","severity":"ERROR","lines":"exec(user_input)","metadata":{"category":"sec"}}}]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	findings, err := s.Analyze(context.Background(), Input{Code: "x", Filename: "a.py"})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "exec(user_input)", findings[0].Snippet)
}
