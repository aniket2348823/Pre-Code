package scanner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTestFile_Suffix(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"go test file", "handler_test.go", true},
		{"underscore test suffix", "utils_test.js", true},
		{"regular file", "handler.go", false},
		{"test in name only", "testing.go", false},
		{"test prefix", "test_app.py", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isTestFile(tt.filename))
		})
	}
}

func TestIsTestFile_TestInPath(t *testing.T) {
	assert.True(t, isTestFile("src/test/helpers.go"))
	assert.True(t, isTestFile("src/tests/helpers.go"))
	assert.False(t, isTestFile("src/handler.go"))
}

func TestIsTestFile_TestdataNotTest(t *testing.T) {
	assert.False(t, isTestFile("testdata/fixture.json"))
}

func TestIsTestDataFile_ForwardSlash(t *testing.T) {
	assert.True(t, isTestDataFile("foo/testdata/fixture.json"))
}

func TestIsTestDataFile_Prefix(t *testing.T) {
	assert.True(t, isTestDataFile("testdata/fixture.json"))
}

func TestIsTestDataFile_Backslash(t *testing.T) {
	assert.True(t, isTestDataFile(`foo\testdata\fixture.json`))
}

func TestIsTestDataFile_NotTestdata(t *testing.T) {
	assert.False(t, isTestDataFile("src/handler.go"))
}

func TestIsTestDataFile_Empty(t *testing.T) {
	assert.False(t, isTestDataFile(""))
}

func TestIsGeneratedFile_Generated(t *testing.T) {
	assert.True(t, isGeneratedFile("model_generated.go"))
	assert.True(t, isGeneratedFile("GENERATED_code.go"))
}

func TestIsGeneratedFile_Vendor(t *testing.T) {
	assert.True(t, isGeneratedFile("vendor/github.com/foo/bar.go"))
}

func TestIsGeneratedFile_Protobuf(t *testing.T) {
	assert.True(t, isGeneratedFile("api.pb.go"))
}

func TestIsGeneratedFile_Mock(t *testing.T) {
	assert.True(t, isGeneratedFile("mock_user.go"))
	assert.True(t, isGeneratedFile("MOCK_service.go"))
}

func TestIsGeneratedFile_Stub(t *testing.T) {
	assert.True(t, isGeneratedFile("stub_db.go"))
	assert.True(t, isGeneratedFile("STUB_repo.go"))
}

func TestIsGeneratedFile_Regular(t *testing.T) {
	assert.False(t, isGeneratedFile("handler.go"))
	assert.False(t, isGeneratedFile("main.go"))
}

func TestIsGeneratedFile_Empty(t *testing.T) {
	assert.False(t, isGeneratedFile(""))
}

func TestIsGeneratedFile_CaseInsensitive(t *testing.T) {
	assert.True(t, isGeneratedFile("MyMock_Service.go"))
	assert.True(t, isGeneratedFile("STUB_Code.go"))
	assert.True(t, isGeneratedFile("API.PB.GO"))
}

func TestBuiltinAnalyzer_RuleCount(t *testing.T) {
	a := NewBuiltinAnalyzer()
	require.NotEmpty(t, a.rules, "builtin should have rules")
	assert.Greater(t, len(a.rules), 10, "builtin should have many rules")
}

func TestBuiltinAnalyzer_RuleNamesUnique(t *testing.T) {
	a := NewBuiltinAnalyzer()
	seen := map[string]bool{}
	for _, r := range a.rules {
		assert.False(t, seen[r.name], "duplicate rule name: %s", r.name)
		seen[r.name] = true
	}
}

func TestBuiltinAnalyzer_RulesHavePatterns(t *testing.T) {
	a := NewBuiltinAnalyzer()
	for _, r := range a.rules {
		assert.NotNil(t, r.pattern, "rule %s should have a pattern", r.name)
	}
}

func TestBuiltinAnalyzer_RulesHaveSeverity(t *testing.T) {
	a := NewBuiltinAnalyzer()
	for _, r := range a.rules {
		assert.NotEmpty(t, r.severity, "rule %s should have severity", r.name)
	}
}

func TestBuiltinAnalyzer_RulesHaveCategory(t *testing.T) {
	a := NewBuiltinAnalyzer()
	for _, r := range a.rules {
		assert.NotEmpty(t, r.category, "rule %s should have category", r.name)
	}
}

func TestBuiltinAnalyzer_RulesHaveFix(t *testing.T) {
	a := NewBuiltinAnalyzer()
	for _, r := range a.rules {
		assert.NotEmpty(t, r.fix, "rule %s should have fix", r.name)
	}
}

func TestBuiltinAnalyzer_RulesHaveDescription(t *testing.T) {
	a := NewBuiltinAnalyzer()
	for _, r := range a.rules {
		assert.NotEmpty(t, r.description, "rule %s should have description", r.name)
	}
}

func TestBuiltinAnalyzer_RulesHaveTitle(t *testing.T) {
	a := NewBuiltinAnalyzer()
	for _, r := range a.rules {
		assert.NotEmpty(t, r.name, "rule should have name (used as title)")
	}
}

func TestBuiltinAnalyzer_NoCodeNoFindings(t *testing.T) {
	a := NewBuiltinAnalyzer()
	findings, err := a.Analyze(nil, Input{Code: "", Filename: "empty.go"})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestBuiltinAnalyzer_WhitespaceOnly(t *testing.T) {
	a := NewBuiltinAnalyzer()
	findings, err := a.Analyze(nil, Input{Code: "   \n\t  \n  ", Filename: "ws.go"})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestBuiltinAnalyzer_SingleLineCode(t *testing.T) {
	a := NewBuiltinAnalyzer()
	findings, err := a.Analyze(nil, Input{Code: "x=1", Filename: "simple.py"})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestBuiltinAnalyzer_RequireContext_Present(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `exec.Command("sh", "-c", req.Input)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "command_injection" {
			found = true
			break
		}
	}
	assert.True(t, found, "command_injection should fire with req. context")
}

func TestBuiltinAnalyzer_RequireContext_Absent(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `exec.Command("sh", "-c", "echo hello")`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, "command_injection", f.RuleID, "command_injection should not fire without context")
	}
}

func TestBuiltinAnalyzer_ExcludeFilenames(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `var password = "hunter2supersecret123"`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "example_usage.go"})
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, "hardcoded_password", f.RuleID,
			"hardcoded_password should be excluded for example filename")
	}
}

func TestBuiltinAnalyzer_GeneratedFileSuppressesAll(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `InsecureSkipVerify: true`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "api.pb.go"})
	require.NoError(t, err)
	assert.Empty(t, findings, "generated file should suppress all findings")
}

func TestBuiltinAnalyzer_VendorFileSuppressesAll(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `InsecureSkipVerify: true`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "vendor/github.com/foo/bar.go"})
	require.NoError(t, err)
	assert.Empty(t, findings, "vendor file should suppress all findings")
}

func TestBuiltinAnalyzer_MockFileSuppressesAll(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `var password = "hunter2supersecret123"`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "mock_user.go"})
	require.NoError(t, err)
	assert.Empty(t, findings, "mock file should suppress all findings")
}

func TestBuiltinAnalyzer_StubFileSuppressesAll(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `var password = "hunter2supersecret123"`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "stub_db.go"})
	require.NoError(t, err)
	assert.Empty(t, findings, "stub file should suppress all findings")
}

func TestBuiltinAnalyzer_LowSeveritySuppressedInTest(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `os.WriteFile("test.txt", data, 0777)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "handler_test.go"})
	require.NoError(t, err)
	for _, f := range findings {
		if f.RuleID == "insecure_file_perms" {
			assert.Equal(t, SeverityLow, f.Severity,
				"low severity finding in test file should be SeverityLow from rank check")
		}
	}
}

func TestBuiltinAnalyzer_FindingFields(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `InsecureSkipVerify: true`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "tls.go"})
	require.NoError(t, err)
	require.NotEmpty(t, findings)

	f := findings[0]
	assert.NotEmpty(t, f.RuleID)
	assert.Equal(t, []string{"builtin"}, f.Analyzers)
	assert.NotEmpty(t, f.Severity)
	assert.NotEmpty(t, f.Category)
	assert.NotEmpty(t, f.Title)
	assert.NotEmpty(t, f.Message)
	assert.Equal(t, "tls.go", f.Filename)
	assert.Greater(t, f.Line, 0)
	assert.NotEmpty(t, f.Snippet)
	assert.NotEmpty(t, f.Fix)
	assert.NotEmpty(t, f.Fingerprint)
}

func TestBuiltinAnalyzer_LineNumberCorrect(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := "line1\nline2\nInsecureSkipVerify: true\nline4"
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "tls.go"})
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	assert.Equal(t, 3, findings[0].Line, "line number should be 3")
}

func TestBuiltinAnalyzer_MultipleFindings(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `password := "supersecretpassword123"
h := md5.New()
InsecureSkipVerify: true`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "handler.go"})
	require.NoError(t, err)
	ruleIDs := map[string]bool{}
	for _, f := range findings {
		ruleIDs[f.RuleID] = true
	}
	assert.True(t, ruleIDs["hardcoded_password"], "should detect hardcoded_password")
	assert.True(t, ruleIDs["weak_hash_md5"], "should detect weak_hash_md5")
	assert.True(t, ruleIDs["insecure_tls"], "should detect insecure_tls")
}
