package scanner

import (
	"context"
	"testing"
)

func TestBuiltinDetectsKnownVulns(t *testing.T) {
	code := "" +
		`q := fmt.Sprintf("SELECT * FROM users WHERE id=%d", id)` + "\n" +
		`password := "supersecret123"` + "\n" +
		`h := md5.New()` + "\n"
	a := NewBuiltinAnalyzer()
	if a.Name() != "builtin" || !a.Available() {
		t.Fatalf("builtin must be named 'builtin' and always available")
	}
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "x.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	want := map[string]bool{"sql_injection": false, "hardcoded_password": false, "weak_hash_md5": false}
	for _, f := range found {
		if _, ok := want[f.RuleID]; ok {
			want[f.RuleID] = true
		}
		if len(f.Analyzers) != 1 || f.Analyzers[0] != "builtin" {
			t.Fatalf("finding %s missing builtin analyzer tag: %v", f.RuleID, f.Analyzers)
		}
		if f.Fingerprint == "" {
			t.Fatalf("finding %s has no fingerprint", f.RuleID)
		}
	}
	for rule, seen := range want {
		if !seen {
			t.Fatalf("expected builtin to detect %s", rule)
		}
	}
}

func TestBuiltinSuppressesTestFileSecrets(t *testing.T) {
	code := `password := "supersecret123"` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "auth_test.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	// Should still detect but the engine will down-grade severity in test files.
	if len(found) == 0 {
		t.Fatal("expected builtin to still detect secrets in test files")
	}
}

func TestBuiltinIgnoresGeneratedFiles(t *testing.T) {
	code := `password := "supersecret123"` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "api.pb.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("generated file should produce no findings, got %d", len(found))
	}
}

func TestBuiltinIgnoresVendorFiles(t *testing.T) {
	code := `InsecureSkipVerify: true` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "vendor/github.com/foo/bar.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("vendor file should produce no findings, got %d", len(found))
	}
}

func TestBuiltinDoesNotFlagRandRead(t *testing.T) {
	code := `n, err := rand.Read(buf)` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "crypto.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	for _, f := range found {
		if f.RuleID == "weak_random" {
			t.Fatal("rand.Read (crypto/rand) should NOT be flagged as weak_random")
		}
	}
}

func TestBuiltinDoesNotFlagErrorWrapping(t *testing.T) {
	code := `return fmt.Errorf("failed to connect: %w", err)` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "db.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	for _, f := range found {
		if f.RuleID == "error_info_leak" || f.RuleID == "error_in_response" {
			t.Fatal("standard error wrapping should NOT be flagged")
		}
	}
}

func TestBuiltinSqlInjectionDetects(t *testing.T) {
	code := `q := fmt.Sprintf("SELECT * FROM users WHERE id=%d", id)` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "query.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("expected sql_injection finding")
	}
}

func TestBuiltinHardcodedPasswordDetects(t *testing.T) {
	code := `password := "mysecretpass123"` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	foundIt := false
	for _, f := range found {
		if f.RuleID == "hardcoded_password" {
			foundIt = true
			break
		}
	}
	if !foundIt {
		t.Fatal("expected hardcoded_password finding")
	}
}

func TestBuiltinWeakRandomDetectsMathRand(t *testing.T) {
	code := `"math/rand"` + "\n" + `n := rand.Intn(100)` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "util.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	foundIt := false
	for _, f := range found {
		if f.RuleID == "weak_random" {
			foundIt = true
			break
		}
	}
	if !foundIt {
		t.Fatal("expected weak_random finding for math/rand")
	}
}

func TestBuiltinInsecureTLS(t *testing.T) {
	code := `InsecureSkipVerify: true` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "http.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("expected insecure_tls finding")
	}
}

func TestBuiltinHardcodedConnectionString(t *testing.T) {
	code := `dsn := "postgres://user:pass@localhost/db"` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "db.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	foundIt := false
	for _, f := range found {
		if f.RuleID == "hardcoded_connection_string" {
			foundIt = true
			break
		}
	}
	if !foundIt {
		t.Fatal("expected hardcoded_connection_string finding")
	}
}

func TestBuiltinWeakJwtSecret(t *testing.T) {
	code := `token, _ := jwt.Sign(claims).SignedString("myhardcodedsecret")` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "auth.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	foundIt := false
	for _, f := range found {
		if f.RuleID == "weak_jwt_secret" {
			foundIt = true
			break
		}
	}
	if !foundIt {
		t.Fatal("expected weak_jwt_secret finding")
	}
}
