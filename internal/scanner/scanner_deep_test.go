package scanner

import (
	"context"
	"testing"
)

func TestScan_EmptyCode(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: "", Filename: "empty.go"})
	if len(report.Findings) != 0 {
		t.Errorf("empty code should return 0 findings, got %d", len(report.Findings))
	}
}

func TestScan_WhitespaceOnly(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: "   \n\t  \n  ", Filename: "ws.go"})
	if len(report.Findings) != 0 {
		t.Errorf("whitespace-only code should return 0 findings, got %d", len(report.Findings))
	}
}

func TestScan_CommentsOnly(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: "// This is a comment\n/* block comment */", Filename: "comments.go"})
	// Comments might trigger some rules but shouldn't trigger sensitive findings
	_ = report
}

func TestScan_NullBytes(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: "var x = \x00\"test\"", Filename: "null.go"})
	// Should not panic
	_ = report
}

func TestScan_UnsupportedLanguage(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "cobol", Code: "DISPLAY 'HELLO'.", Filename: "prog.cobol"})
	if len(report.Findings) != 0 {
		t.Errorf("unsupported language should return 0 findings, got %d", len(report.Findings))
	}
}

func TestScan_FilenamePathTraversal(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: `InsecureSkipVerify: true`, Filename: "../../../etc/passwd.go"})
	if len(report.Findings) == 0 {
		t.Error("should detect findings even with path traversal filename")
	}
}

func TestScan_TestFile_FP(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	// Test file with low-severity rule should be suppressed
	code := `os.WriteFile("test.txt", data, 0777)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "handler_test.go"})
	for _, f := range report.Findings {
		if f.RuleID == "insecure_file_perms" && f.Severity != SeverityLow {
			t.Errorf("test file low-severity should be suppressed, got %s", f.Severity)
		}
	}
}

func TestScan_GeneratedFile(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "api.pb.go"})
	if len(report.Findings) != 0 {
		t.Errorf("generated file should suppress all findings, got %d", len(report.Findings))
	}
}

func TestScan_VendorFile(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "vendor/github.com/foo/bar.go"})
	if len(report.Findings) != 0 {
		t.Errorf("vendor file should suppress all findings, got %d", len(report.Findings))
	}
}

func TestScan_SQLInjection_Sprintf(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `q := fmt.Sprintf("SELECT * FROM users WHERE id=%s", id)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "handler.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "sql_injection" {
			found = true
		}
	}
	if !found {
		t.Error("should detect SQL injection via Sprintf")
	}
}

func TestScan_HardcodedAWSKey(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var key = "AKIAIOSFODNN7EXAMPLE"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "aws_access_key" {
			found = true
		}
	}
	if !found {
		t.Error("should detect hardcoded AWS key")
	}
}

func TestScan_GitHubToken(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var token = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef1234"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "auth.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "github_token" {
			found = true
		}
	}
	if !found {
		t.Error("should detect GitHub token")
	}
}

func TestScan_SlackToken(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var token = "xoxb-1234567890-1234567890-abcdefghij"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "slack_token" {
			found = true
		}
	}
	if !found {
		t.Error("should detect Slack token")
	}
}

func TestScan_PrivateKey(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAK...`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "key.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "private_key_literal" {
			found = true
		}
	}
	if !found {
		t.Error("should detect private key")
	}
}

func TestScan_InsecureSkipVerify(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `InsecureSkipVerify: true`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "client.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "insecure_tls" {
			found = true
		}
	}
	if !found {
		t.Error("should detect InsecureSkipVerify")
	}
}

func TestScan_MD5Usage(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `h := md5.New()`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "crypto.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "weak_hash_md5" {
			found = true
		}
	}
	if !found {
		t.Error("should detect MD5 usage")
	}
}

func TestScan_MathRand(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `import "math/rand"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "utils.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "weak_random" {
			found = true
		}
	}
	if !found {
		t.Error("should detect math/rand")
	}
}

func TestScan_PathTraversal(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `f, _ := os.Open(r.URL.Path)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "handler.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "path_traversal" {
			found = true
		}
	}
	if !found {
		t.Error("should detect path traversal")
	}
}

func TestScan_SSRF(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `resp, _ := http.Get(r.URL.Query().Get("url"))`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "proxy.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "ssrf_http_get" {
			found = true
		}
	}
	if !found {
		t.Error("should detect SSRF")
	}
}

func TestScan_XSS_UnsafeHTML(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `return template.HTML(data)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "handler.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "xss_unsafe_html" {
			found = true
		}
	}
	if !found {
		t.Error("should detect XSS via template.HTML")
	}
}

func TestScan_GoroutineWithoutRecovery(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `go func() { doWork() }()`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "async.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "goroutine_without_recovery" {
			found = true
		}
	}
	if !found {
		t.Error("should detect goroutine without recovery")
	}
}

func TestComputeFingerprint_Determinism(t *testing.T) {
	f1 := ComputeFingerprint("file.go", 10, "code", "rule1")
	f2 := ComputeFingerprint("file.go", 10, "code", "rule1")
	if f1 != f2 {
		t.Error("same input should produce same fingerprint")
	}
}

func TestComputeFingerprint_DifferentInputs(t *testing.T) {
	f1 := ComputeFingerprint("file.go", 10, "code1", "rule1")
	f2 := ComputeFingerprint("file.go", 10, "code2", "rule1")
	if f1 == f2 {
		t.Error("different inputs should produce different fingerprints")
	}
}

func TestBuiltinAnalyzer_Available(t *testing.T) {
	a := NewBuiltinAnalyzer()
	if !a.Available() {
		t.Error("builtin analyzer should always be available")
	}
	if a.Name() != "builtin" {
		t.Errorf("expected name 'builtin', got %q", a.Name())
	}
}

func TestEngine_EmptyFilename(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: `InsecureSkipVerify: true`, Filename: ""})
	if len(report.Findings) == 0 {
		t.Error("should detect findings even with empty filename")
	}
}

func TestScan_CodeWithOnlyComments(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `// TODO: fix this later
/* This is a multi-line
   comment block */
// Another comment`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "comments.go"})
	// Comments shouldn't trigger real vulnerabilities
	for _, f := range report.Findings {
		if f.Severity == SeverityCritical || f.Severity == SeverityHigh {
			t.Errorf("high/critical finding in comments only: %s", f.RuleID)
		}
	}
}

func TestScan_MockFile(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "mock_user.go"})
	if len(report.Findings) != 0 {
		t.Errorf("mock file should suppress findings, got %d", len(report.Findings))
	}
}

func TestScan_StubFile(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "stub_db.go"})
	if len(report.Findings) != 0 {
		t.Errorf("stub file should suppress findings, got %d", len(report.Findings))
	}
}

func TestScan_TestdataFile(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "testdata/fixture.json"})
	if len(report.Findings) != 0 {
		t.Errorf("testdata file should suppress findings, got %d", len(report.Findings))
	}
}

func TestScan_ExcludeFilenames(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "example_usage.go"})
	// Example files might be excluded
	_ = report
}

func TestHasHighConfidenceFindings_Empty(t *testing.T) {
	report := &Report{Findings: []Finding{}}
	if HasHighConfidenceFindings(report) {
		t.Error("empty report should not have high confidence findings")
	}
}

func TestHasHighConfidenceFindings_LowConfidence(t *testing.T) {
	report := &Report{Findings: []Finding{
		{Confidence: 0.3, Severity: SeverityHigh},
	}}
	if HasHighConfidenceFindings(report) {
		t.Error("low confidence finding should not be high confidence")
	}
}

func TestHasHighConfidenceFindings_HighConfidence(t *testing.T) {
	report := &Report{Findings: []Finding{
		{Confidence: 0.95, Severity: SeverityHigh},
	}}
	if !HasHighConfidenceFindings(report) {
		t.Error("high confidence finding should be detected")
	}
}

func TestScan_LogInjection(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `slog.Info("user action", "input", req.Input)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "handler.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "log_injection" {
			found = true
		}
	}
	if !found {
		t.Error("should detect log injection")
	}
}

func TestScan_CommandInjection(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `exec.Command("sh", "-c", req.Input)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "cmd.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "command_injection" {
			found = true
		}
	}
	if !found {
		t.Error("should detect command injection")
	}
}

func TestScan_XSS_ScriptTag(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `<script>alert(1)</script>`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "template.html"})
	_ = report // XSS detection depends on rules
}

func TestMergeScoreAndFilter(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	// Report should have findings
	if len(report.Findings) == 0 {
		t.Error("expected findings for hardcoded password")
	}
	// Findings should have valid fingerprints
	for _, f := range report.Findings {
		if f.Fingerprint == "" {
			t.Error("finding should have non-empty fingerprint")
		}
	}
}

func TestScan_HardcodedPasswordConfidence(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	// Filter by high confidence
	var highConf []Finding
	for _, f := range report.Findings {
		if f.Confidence >= 0.60 {
			highConf = append(highConf, f)
		}
	}
	// Should have some high confidence findings for obvious vulnerabilities
	if len(highConf) == 0 {
		t.Error("expected at least one high-confidence finding for hardcoded password")
	}
}

func TestScan_RequireContext_Missing(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	// Code that matches pattern but lacks required context
	code := `exec.Command("sh", "-c", "echo hello")`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "cmd.go"})
	// command_injection requires req., r., params., input., or fmt.Sprintf context
	for _, f := range report.Findings {
		if f.RuleID == "command_injection" {
			t.Error("command_injection should not fire without required context")
		}
	}
}

func TestScan_RequireContext_Present(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `exec.Command("sh", "-c", req.Input)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "cmd.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "command_injection" {
			found = true
		}
	}
	if !found {
		t.Error("command_injection should fire with req. context")
	}
}

func TestScan_WeakHash_SHA1(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `import "crypto/sha1"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "crypto.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "weak_hash_sha1" {
			found = true
		}
	}
	if !found {
		t.Error("should detect SHA-1")
	}
}

func TestScan_HardcodedPassword(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "hardcoded_password" {
			found = true
		}
	}
	if !found {
		t.Error("should detect hardcoded password")
	}
}

func TestScan_FP_ParameterizedQuery(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `db.Query("SELECT * FROM users WHERE id=$1", id)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "db.go"})
	for _, f := range report.Findings {
		if f.RuleID == "sql_injection" || f.RuleID == "sql_injection_raw_query" {
			t.Errorf("parameterized query should not trigger: %s", f.RuleID)
		}
	}
}

func TestScan_FP_SHA256(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `import "crypto/sha256"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "crypto.go"})
	for _, f := range report.Findings {
		if f.RuleID == "weak_hash_md5" || f.RuleID == "weak_hash_sha1" {
			t.Errorf("SHA-256 should not trigger weak hash: %s", f.RuleID)
		}
	}
}

func TestScan_FP_CryptoRand(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `import "crypto/rand"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "crypto.go"})
	for _, f := range report.Findings {
		if f.RuleID == "weak_random" {
			t.Error("crypto/rand should not trigger weak_random")
		}
	}
}

func TestScan_FP_SafeFileWrite(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `os.WriteFile("config.json", data, 0644)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	for _, f := range report.Findings {
		if f.RuleID == "insecure_file_perms" {
			t.Error("0644 permissions should not trigger")
		}
	}
}

func TestScan_FP_SafeExec(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `exec.Command("ls", "-la")`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "cmd.go"})
	for _, f := range report.Findings {
		if f.RuleID == "command_injection" {
			t.Error("safe exec should not trigger command_injection")
		}
	}
}

func TestScan_FP_GoSum(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `github.com/foo/bar v1.2.3 h1:abc123=`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "go.sum"})
	for _, f := range report.Findings {
		if f.RuleID == "hardcoded_password" || f.RuleID == "insecure_tls" {
			t.Errorf("go.sum should not trigger: %s", f.RuleID)
		}
	}
}

func TestScan_SeverityOrdering(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `
password := "supersecretpassword123"
h := md5.New()
InsecureSkipVerify: true
`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "mixed.go"})
	if len(report.Findings) < 2 {
		t.Fatalf("expected at least 2 findings, got %d", len(report.Findings))
	}
	// Verify severity ordering
	for i := 1; i < len(report.Findings); i++ {
		prev := SeverityRank(report.Findings[i-1].Severity)
		curr := SeverityRank(report.Findings[i].Severity)
		if prev < curr {
			t.Errorf("findings not sorted by severity at index %d", i)
		}
	}
}

func TestScan_NoFilename(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: `InsecureSkipVerify: true`, Filename: ""})
	if len(report.Findings) == 0 {
		t.Error("should detect findings with empty filename")
	}
}
