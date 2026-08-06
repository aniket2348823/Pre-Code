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

// ── Go string-concat SQL / shell-concat command injection (sample_scan.go gaps) ──

func TestBuiltinAnalyzer_SqlInjectionStringConcat_Fires(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `query := "SELECT id, email FROM users WHERE username = '" + username + "'"`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "dao.go"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "sql_injection_string_concat" {
			found = true
			break
		}
	}
	assert.True(t, found, "SQL literal followed by + concatenation should fire sql_injection_string_concat")
}

func TestBuiltinAnalyzer_SqlInjectionStringConcat_NoFalsePositive(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `rows, err := db.Query("SELECT * FROM users WHERE id = $1", id)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "db.go"})
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, "sql_injection_string_concat", f.RuleID,
			"parameterized query must not fire sql_injection_string_concat")
	}
}

func TestBuiltinAnalyzer_CommandInjectionShellConcat_Fires(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cmd := exec.Command("sh", "-c", "ping -c 3 "+targetHost)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "command_injection_shell_concat" {
			found = true
			break
		}
	}
	assert.True(t, found, "exec.Command(\"sh\", \"-c\", ... + var) should fire command_injection_shell_concat")
}

func TestBuiltinAnalyzer_CommandInjectionShellConcat_NoFalsePositive(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cmd := exec.Command("ls", "-la")`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, "command_injection_shell_concat", f.RuleID,
			"non-shell exec.Command must not fire command_injection_shell_concat")
	}
}

func TestBuiltinAnalyzer_CommandInjectionShellVariable_Fires(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cmd := exec.Command("sh", "-c", cmdVar)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "command_injection_shell_variable" {
			found = true
			break
		}
	}
	assert.True(t, found, "exec.Command(\"sh\", \"-c\", var) should fire command_injection_shell_variable")
}

func TestBuiltinAnalyzer_CommandInjectionShellVariable_FiresBashBin(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cmd := exec.Command("/bin/bash", "-c", buildCmd)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "command_injection_shell_variable" {
			found = true
			break
		}
	}
	assert.True(t, found, "exec.Command(\"/bin/bash\", \"-c\", var) should fire command_injection_shell_variable")
}

func TestBuiltinAnalyzer_CommandInjectionShellVariable_NoFPLiteral(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cmd := exec.Command("sh", "-c", "echo hello")`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, "command_injection_shell_variable", f.RuleID,
			"string-literal shell command must not fire command_injection_shell_variable")
	}
}

func TestBuiltinAnalyzer_CommandInjectionShellVariable_FiresContextCommand(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cmd := exec.CommandContext(ctx, "sh", "-c", script)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "command_injection_shell_variable" {
			found = true
			break
		}
	}
	assert.True(t, found, "exec.CommandContext(ctx, \"sh\", \"-c\", var) should fire command_injection_shell_variable")
}

func TestBuiltinAnalyzer_CommandInjectionShellVariable_FiresCmdC(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cmd := exec.Command("cmd", "/c", cmdVar)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "command_injection_shell_variable" {
			found = true
			break
		}
	}
	assert.True(t, found, "exec.Command(\"cmd\", \"/c\", var) should fire command_injection_shell_variable")
}

func TestBuiltinAnalyzer_CommandInjectionShellVariable_FiresPowerShell(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cmd := exec.Command("powershell", "-Command", script)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "command_injection_shell_variable" {
			found = true
			break
		}
	}
	assert.True(t, found, "exec.Command(\"powershell\", \"-Command\", var) should fire command_injection_shell_variable")
}

func TestBuiltinAnalyzer_CommandInjectionShellVariable_NoFPLiteralCmd(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cmd := exec.Command("cmd", "/c", "dir")`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, "command_injection_shell_variable", f.RuleID,
			"string-literal cmd /c command must not fire command_injection_shell_variable")
	}
}

func TestBuiltinAnalyzer_CommandInjectionShellConcat_FiresCmdC(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cmd := exec.Command("cmd", "/c", "dir "+targetHost)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "command_injection_shell_concat" {
			found = true
			break
		}
	}
	assert.True(t, found, "exec.Command(\"cmd\", \"/c\", \"...\" + var) should fire command_injection_shell_concat")
}

func TestBuiltinAnalyzer_CommandInjectionShellVariable_FiresFullPath(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cmd := exec.Command("/usr/bin/bash", "-c", cmdVar)
cmd2 := exec.Command("C:\\Windows\\System32\\cmd.exe", "/c", cmdVar2)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "command_injection_shell_variable" {
			found = true
			break
		}
	}
	assert.True(t, found, "full-path shell binaries should fire command_injection_shell_variable")
}

func TestBuiltinAnalyzer_CommandInjectionShellConcat_FiresFullPath(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cmd := exec.Command("/usr/bin/bash", "-c", "echo "+host)
cmd2 := exec.Command("C:\\Windows\\System32\\cmd.exe", "/c", "dir "+path)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "command_injection_shell_concat" {
			found = true
			break
		}
	}
	assert.True(t, found, "full-path shell binaries with concat should fire command_injection_shell_concat")
}

// (?i:...) is scoped to the shell literals only — exec.Command must stay
// case-sensitive so a method named Exec.Command does not false-positive.
func TestBuiltinAnalyzer_CommandInjectionShellVariable_CaseSensitiveExec(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `Exec.Command("sh", "-c", cmdVar)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "cmd.go"})
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, "command_injection_shell_variable", f.RuleID,
			"capitalized Exec.Command must not fire command_injection_shell_variable")
	}
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

// ── Python rules (deterministic engine must cover the VSCode extension's
// primary BYOK scan target) ────────────────────────────────────────────────

func TestBuiltinAnalyzer_PythonCommandInjection_Fires(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `import os
os.system(request.args.get("cmd"))`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "vuln.py", Language: "python"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "python_command_injection" {
			found = true
			break
		}
	}
	assert.True(t, found, "python_command_injection should fire for os.system(request.args...)")
}

func TestBuiltinAnalyzer_PythonCommandInjection_NoUserInput(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `import os
os.system("ls -la")`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "ok.py"})
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, "python_command_injection", f.RuleID,
			"python_command_injection must not fire without user-input context")
	}
}

func TestBuiltinAnalyzer_PythonSubprocessShellTrue_Fires(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `import subprocess
subprocess.run(request.form["cmd"], shell=True)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "sub.py"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "python_command_injection" {
			found = true
			break
		}
	}
	assert.True(t, found, "subprocess(..., shell=True) with user input should fire python_command_injection")
}

func TestBuiltinAnalyzer_PythonEvalExec_Fires(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `result = eval(request.args.get("expr"))`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "eval.py"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "python_eval_exec" {
			found = true
			break
		}
	}
	assert.True(t, found, "eval() with request context should fire python_eval_exec")
}

func TestBuiltinAnalyzer_PythonPickleLoad_Fires(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `data = pickle.loads(raw_bytes)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "pickle.py"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "python_pickle_load" {
			found = true
			break
		}
	}
	assert.True(t, found, "pickle.loads should fire python_pickle_load unconditionally")
}

func TestBuiltinAnalyzer_PythonUnsafeYaml_Fires(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cfg = yaml.load(open("config.yaml"))`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "conf.py"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "python_unsafe_yaml" {
			found = true
			break
		}
	}
	assert.True(t, found, "yaml.load should fire python_unsafe_yaml")
}

func TestBuiltinAnalyzer_PythonSqlInjectionFstring_Fires(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cursor.execute(f"SELECT * FROM users WHERE id = {uid}")`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "db.py"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "python_sql_injection_fstring" {
			found = true
			break
		}
	}
	assert.True(t, found, "cursor.execute(f\"...\") should fire python_sql_injection_fstring")
}

func TestBuiltinAnalyzer_PythonSqlInjectionFormat_Fires(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `cursor.execute("SELECT * FROM users WHERE id = %s" % uid)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "db.py"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "python_sql_injection_format" {
			found = true
			break
		}
	}
	assert.True(t, found, "%-formatting in cursor.execute should fire python_sql_injection_format")
}

func TestBuiltinAnalyzer_PythonSsrf_Fires(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `resp = requests.get(request.args.get("url"))`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "http.py"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "python_ssrf" {
			found = true
			break
		}
	}
	assert.True(t, found, "requests.get(request.args...) should fire python_ssrf")
}

func TestBuiltinAnalyzer_PythonPathTraversal_Fires(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `with open(request.files["f"].filename, "wb") as fh:`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "upload.py"})
	require.NoError(t, err)
	found := false
	for _, f := range findings {
		if f.RuleID == "python_path_traversal" {
			found = true
			break
		}
	}
	assert.True(t, found, "open() with user-controlled filename should fire python_path_traversal")
}

// Regression: the rule must NOT match Go's capitalized os.Open/os.Remove/os.Rename
// (the scanner's primary language). Case-sensitivity is deliberate.
func TestBuiltinAnalyzer_PythonPathTraversal_NoGoCollision(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `f, err := os.Open(filename)
os.Remove(filename)
os.Rename(oldName, newName)`
	findings, err := a.Analyze(nil, Input{Code: code, Filename: "handler.go"})
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, "python_path_traversal", f.RuleID,
			"python_path_traversal must not fire on Go's capitalized os.* APIs")
	}
}
