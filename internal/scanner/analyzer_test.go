package scanner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInput_ZeroValue(t *testing.T) {
	var in Input
	assert.Empty(t, in.Language)
	assert.Empty(t, in.Code)
	assert.Empty(t, in.Filename)
}

func TestInput_AllFields(t *testing.T) {
	in := Input{
		Language: "python",
		Code:     "import os",
		Filename: "app.py",
	}
	assert.Equal(t, "python", in.Language)
	assert.Equal(t, "import os", in.Code)
	assert.Equal(t, "app.py", in.Filename)
}

func TestInput_EmptyLanguageAnalyzerDecides(t *testing.T) {
	in := Input{Code: "x=1"}
	assert.Empty(t, in.Language, "empty language means analyzer decides or skips")
}

func TestAnalyzerInterface_Builtin(t *testing.T) {
	a := NewBuiltinAnalyzer()
	assert.Equal(t, "builtin", a.Name())
	assert.True(t, a.Available())
}

func TestAnalyzerInterface_Bandit(t *testing.T) {
	b := NewBanditAnalyzer(&fakeRunner{stdout: `{"results":[]}`})
	b.exists = func() bool { return true }
	assert.Equal(t, "bandit", b.Name())
	assert.True(t, b.Available())
}

func TestAnalyzerInterface_Semgrep(t *testing.T) {
	s := NewSemgrepAnalyzer(&fakeRunner{stdout: `{"results":[]}`})
	s.exists = func() bool { return true }
	assert.Equal(t, "semgrep", s.Name())
	assert.True(t, s.Available())
}

func TestAnalyzerInterface_BanditUnavailable(t *testing.T) {
	b := NewBanditAnalyzer(nil)
	b.exists = func() bool { return false }
	assert.False(t, b.Available())
}

func TestAnalyzerInterface_SemgrepUnavailable(t *testing.T) {
	s := NewSemgrepAnalyzer(nil)
	s.exists = func() bool { return false }
	assert.False(t, s.Available())
}

func TestAnalyzer_PassToEngine(t *testing.T) {
	eng := NewEngine(NewBuiltinAnalyzer())
	report := eng.Run(context.Background(), Input{Code: `InsecureSkipVerify: true`, Filename: "tls.go"})
	require.NotEmpty(t, report.Findings, "engine should forward Input to analyzer")
}
