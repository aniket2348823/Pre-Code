package extraction

import (
	"testing"

	"github.com/vigilagent/vigilagent/internal/scanner"
)

func TestCategorizeFinding(t *testing.T) {
	tests := []struct {
		name     string
		finding  scanner.Finding
		expected string
	}{
		{
			name:     "SQL injection",
			finding:  scanner.Finding{Title: "SQL Injection in query builder"},
			expected: "sql_injection",
		},
		{
			name:     "XSS",
			finding:  scanner.Finding{Title: "Cross-Site Scripting vulnerability"},
			expected: "xss",
		},
		{
			name:     "hardcoded secret",
			finding:  scanner.Finding{Title: "Hardcoded API key found"},
			expected: "hardcoded_secret",
		},
		{
			name:     "path traversal",
			finding:  scanner.Finding{Title: "Path traversal vulnerability"},
			expected: "path_traversal",
		},
		{
			name:     "command injection",
			finding:  scanner.Finding{Title: "OS Command Injection detected"},
			expected: "command_injection",
		},
		{
			name:     "weak crypto",
			finding:  scanner.Finding{Title: "Use of weak hash MD5"},
			expected: "weak_crypto",
		},
		{
			name:     "missing rate limit",
			finding:  scanner.Finding{Title: "Missing rate limiting on endpoint"},
			expected: "missing_rate_limit",
		},
		{
			name:     "broken auth",
			finding:  scanner.Finding{Title: "Broken authentication mechanism"},
			expected: "broken_auth",
		},
		{
			name:     "uncategorized",
			finding:  scanner.Finding{Title: "Some random issue"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := categorizeFinding(tt.finding)
			if got != tt.expected {
				t.Errorf("categorizeFinding() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractPattern(t *testing.T) {
	e := NewEngine()
	f := scanner.Finding{
		Title:      "SQL Injection in user query",
		Message:    "SQL Injection detected in user.go:42",
		Fix:        "Use parameterized queries",
		Confidence: 0.9,
		Snippet:    "db.Query(\"SELECT * FROM users WHERE id = \" + userID)",
	}

	p := e.extractPattern(f)
	if p == nil {
		t.Fatal("extractPattern should not return nil for valid finding")
	}
	if p.Category != "sql_injection" {
		t.Errorf("expected sql_injection, got %s", p.Category)
	}
	if p.Confidence != 0.9 {
		t.Errorf("expected 0.9, got %f", p.Confidence)
	}
	if p.Fix != "Use parameterized queries" {
		t.Errorf("expected fix template, got %s", p.Fix)
	}
	if len(p.Variants) != 1 {
		t.Errorf("expected 1 variant, got %d", len(p.Variants))
	}
}

func TestExtractPatternUncategorized(t *testing.T) {
	e := NewEngine()
	f := scanner.Finding{
		Title:   "Some random issue",
		Message: "unrelated problem",
	}
	p := e.extractPattern(f)
	if p != nil {
		t.Error("uncategorized finding should return nil")
	}
}

func TestExtractFromFindings(t *testing.T) {
	e := NewEngine()
	findings := []scanner.Finding{
		{Title: "SQL Injection in query", Message: "SQL injection detected", Confidence: 0.9},
		{Title: "XSS in template", Message: "Cross-site scripting", Confidence: 0.8},
		{Title: "Hardcoded password", Message: "Hardcoded secret found", Confidence: 0.85},
	}

	patterns := e.ExtractFromFindings(findings)
	if len(patterns) != 3 {
		t.Errorf("expected 3 patterns, got %d", len(patterns))
	}

	// Check category index
	sqlPatterns := e.GetPatternsByCategory("sql_injection")
	if len(sqlPatterns) != 1 {
		t.Errorf("expected 1 sql_injection pattern, got %d", len(sqlPatterns))
	}
}

func TestExtractFromFindingsDuplicate(t *testing.T) {
	e := NewEngine()
	findings := []scanner.Finding{
		{Title: "SQL Injection in query 1", Message: "SQL injection detected", Confidence: 0.9, Snippet: "db.Query(\"SELECT * FROM users\")"},
		{Title: "SQL Injection in query 2", Message: "SQL injection detected", Confidence: 0.8, Snippet: "db.Query(\"SELECT * FROM orders\")"},
	}

	patterns := e.ExtractFromFindings(findings)
	// Should merge into one pattern (two references returned)
	if len(patterns) != 2 {
		t.Errorf("expected 2 pattern references (one original, one merge), got %d", len(patterns))
	}
	// Original should have UsageCount 2
	sqlPatterns := e.GetPatternsByCategory("sql_injection")
	if len(sqlPatterns) != 1 {
		t.Fatalf("expected 1 sql_injection pattern, got %d", len(sqlPatterns))
	}
	if sqlPatterns[0].UsageCount != 2 {
		t.Errorf("expected UsageCount 2, got %d", sqlPatterns[0].UsageCount)
	}
	if len(sqlPatterns[0].Variants) != 2 {
		t.Errorf("expected 2 variants, got %d", len(sqlPatterns[0].Variants))
	}
}

func TestMatchPattern(t *testing.T) {
	e := NewEngine()
	// Seed with a pattern whose trigger words appear in the test code
	e.ExtractFromFindings([]scanner.Finding{
		{Title: "SQL injection in user input", Message: "SQL injection detected", Confidence: 0.9},
	})

	code := "sql injection vulnerability in user input field"
	results := e.MatchPattern(code, "go")
	if len(results) == 0 {
		t.Error("expected at least one match")
	}
	if results[0].Confidence < 0.5 {
		t.Errorf("expected confidence > 0.5, got %f", results[0].Confidence)
	}
}

func TestMatchPatternNoMatch(t *testing.T) {
	e := NewEngine()
	e.ExtractFromFindings([]scanner.Finding{
		{Title: "SQL Injection in query", Message: "SQL injection detected", Confidence: 0.9},
	})

	code := "func hello() { fmt.Println(\"hello\") }"
	results := e.MatchPattern(code, "go")
	// Should not match because code doesn't contain trigger words like 'sql' or 'injection'
	for _, r := range results {
		if r.Confidence > 0.5 {
			t.Errorf("unexpected high confidence match: %f", r.Confidence)
		}
	}
}

func TestRecordOutcome(t *testing.T) {
	e := NewEngine()
	patterns := e.ExtractFromFindings([]scanner.Finding{
		{Title: "SQL Injection", Message: "SQL injection detected", Confidence: 0.9},
	})
	if len(patterns) == 0 {
		t.Fatal("expected at least one pattern")
	}

	patternID := patterns[0].ID
	e.RecordOutcome(patternID, true)

	all := e.GetAllPatterns()
	if len(all) == 0 {
		t.Fatal("expected at least one pattern")
	}
	if all[0].UsageCount != 2 { // 1 from extraction + 1 from outcome
		t.Errorf("expected UsageCount 2, got %d", all[0].UsageCount)
	}
}

func TestRecordOutcomeNonexistent(t *testing.T) {
	e := NewEngine()
	// Should not panic
	e.RecordOutcome("nonexistent", true)
}

func TestCount(t *testing.T) {
	e := NewEngine()
	if e.Count() != 0 {
		t.Errorf("expected 0, got %d", e.Count())
	}
	e.ExtractFromFindings([]scanner.Finding{
		{Title: "SQL Injection", Message: "SQL injection detected", Confidence: 0.9},
	})
	if e.Count() != 1 {
		t.Errorf("expected 1, got %d", e.Count())
	}
}

func TestGenerateFixTemplate(t *testing.T) {
	tests := []struct {
		name     string
		finding  scanner.Finding
		expected string
	}{
		{
			name:     "SQL injection fix",
			finding:  scanner.Finding{Title: "SQL Injection"},
			expected: "Use parameterized queries instead of string concatenation",
		},
		{
			name:     "XSS fix",
			finding:  scanner.Finding{Title: "XSS vulnerability"},
			expected: "Sanitize user input and use context-aware output encoding",
		},
		{
			name:     "hardcoded secret fix",
			finding:  scanner.Finding{Title: "Hardcoded API key"},
			expected: "Move secrets to environment variables or a secrets manager",
		},
		{
			name:     "existing fix preserved",
			finding:  scanner.Finding{Title: "SQL Injection", Fix: "Custom fix"},
			expected: "Custom fix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateFixTemplate(tt.finding)
			if got != tt.expected {
				t.Errorf("generateFixTemplate() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSimilarityScore(t *testing.T) {
	tests := []struct {
		a, b     string
		expected float64
	}{
		{"sql injection in query", "sql injection in query", 1.0},
		{"sql injection in query", "xss in template", 0.0},
		{"sql injection in user table", "sql injection in query builder", 0.3333333333333333},
	}

	for _, tt := range tests {
		got := similarityScore(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("similarityScore(%q, %q) = %f, want %f", tt.a, tt.b, got, tt.expected)
		}
	}
}

func TestComputePatternID(t *testing.T) {
	id1 := computePatternID("sql injection", "sql_injection")
	id2 := computePatternID("sql injection", "sql_injection")
	if id1 != id2 {
		t.Error("same inputs should produce same ID")
	}
	if id1 == "" {
		t.Error("ID should not be empty")
	}
}
