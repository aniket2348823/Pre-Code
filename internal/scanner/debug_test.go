package scanner

import (
	"context"
	"fmt"
	"testing"
)

func TestDebugErrorResponse(t *testing.T) {
	// Test the regex directly
	line := `	w.Write([]byte(fmt.Sprintf("error: %w", err)))`
	analyzer := NewBuiltinAnalyzer()
	for _, r := range analyzer.rules {
		if r.name == "error_in_response" {
			fmt.Printf("Pattern: %s\n", r.pattern.String())
			fmt.Printf("Line: %q\n", line)
			fmt.Printf("Match: %v\n", r.pattern.MatchString(line))
		}
	}

	// Test through the engine
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `func handler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(fmt.Sprintf("error: %w", err)))
}`
	fmt.Printf("Code: %q\n", code)
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "handler.go",
	})
	fmt.Printf("Findings: %d\n", len(report.Findings))
	for _, f := range report.Findings {
		fmt.Printf("  RuleID=%s Severity=%s\n", f.RuleID, f.Severity)
	}
}
