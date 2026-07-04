package scanner

import (
	"context"
	"testing"
)

func TestBuiltinDetectsKnownVulns(t *testing.T) {
	code := "" +
		"q := fmt.Sprintf(\"SELECT * FROM users WHERE id=%d\", id)\n" +
		"password := \"supersecret123\"\n" +
		"h := md5.New()\n"
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
