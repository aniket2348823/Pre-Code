package proxy

import (
	"strings"
	"testing"
)

func TestExtractCodeBlocks(t *testing.T) {
	text := "Here is some code:\n```go\nfmt.Println(\"Hello\")\n```\nAnd more:\n```python\nprint(\"Hi\")\n```"
	blocks := ExtractCodeBlocks(text)
	if len(blocks) != 2 {
		t.Fatalf("Expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Language != "go" {
		t.Errorf("Expected go, got %s", blocks[0].Language)
	}
	if !strings.Contains(blocks[0].Code, "fmt.Println") {
		t.Errorf("Expected code to contain fmt.Println, got %s", blocks[0].Code)
	}
}

func TestFormatAnalysisSummary(t *testing.T) {
	res := []*AnalysisResult{
		{
			Grade:          "✅ Grade A",
			Score:          95,
			CriticalIssues: 0,
			Suggestions:    2,
			Reviewers: map[string]string{
				"Security":     "✅",
				"Architecture": "✅",
				"Performance":  "⚠️",
			},
		},
	}
	summary := FormatAnalysisSummary(res)
	if !strings.Contains(summary, "✅ Grade A") {
		t.Errorf("Expected summary to contain grade, got %s", summary)
	}
	if !strings.Contains(summary, "Security ✅") {
		t.Errorf("Expected summary to contain reviewers, got %s", summary)
	}
}

func TestProviderRouting(t *testing.T) {
	cfg := &Config{OpenAIKey: "test-openai", AnthropicKey: "test-anthropic"}
	p := RouteRequest("gpt-4o", cfg)
	if p == nil || p.Name != "openai" {
		t.Errorf("Expected openai provider")
	}

	p = RouteRequest("claude-3-5-sonnet", cfg)
	if p == nil || p.Name != "anthropic" {
		t.Errorf("Expected anthropic provider")
	}
}

func TestProxyHandler(t *testing.T) {
	// Stub for proxy testing
}

func TestStreamingProxy(t *testing.T) {
	// Stub for streaming proxy testing
}
