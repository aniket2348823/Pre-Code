package critic

import (
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.CriticModel == "" {
		t.Error("CriticModel should not be empty")
	}
	if cfg.Threshold <= 0 || cfg.Threshold > 1 {
		t.Errorf("Threshold should be between 0 and 1, got %f", cfg.Threshold)
	}
	if cfg.MaxRetries < 0 {
		t.Errorf("MaxRetries should be >= 0, got %d", cfg.MaxRetries)
	}
	if len(cfg.Dimensions) == 0 {
		t.Error("should have at least one dimension")
	}
	// Verify weights sum to ~1.0
	totalWeight := 0.0
	for _, d := range cfg.Dimensions {
		totalWeight += d.Weight
	}
	if totalWeight < 0.99 || totalWeight > 1.01 {
		t.Errorf("dimension weights should sum to ~1.0, got %f", totalWeight)
	}
}

func TestGradeFromScore(t *testing.T) {
	tests := []struct {
		score float64
		grade string
	}{
		{0.97, "A+"},
		{0.92, "A"},
		{0.85, "B+"},
		{0.75, "B"},
		{0.65, "C"},
		{0.55, "D"},
		{0.30, "F"},
	}
	for _, tt := range tests {
		got := gradeFromScore(tt.score)
		if got != tt.grade {
			t.Errorf("gradeFromScore(%.2f) = %s, want %s", tt.score, got, tt.grade)
		}
	}
}

func TestNewPipeline(t *testing.T) {
	p := NewPipeline(nil, nil)
	if p == nil {
		t.Fatal("NewPipeline should not return nil")
	}
	if p.config == nil {
		t.Error("config should be set to default")
	}
	if p.config.CriticModel == "" {
		t.Error("default config should have CriticModel")
	}
}

func TestNewPipelineWithCustomConfig(t *testing.T) {
	cfg := &Config{
		CriticModel:  "custom-model",
		PrimaryModel: "primary-model",
		Threshold:    0.85,
		MaxRetries:   5,
	}
	p := NewPipeline(nil, cfg)
	if p.config.CriticModel != "custom-model" {
		t.Errorf("expected custom-model, got %s", p.config.CriticModel)
	}
	if p.config.Threshold != 0.85 {
		t.Errorf("expected 0.85, got %f", p.config.Threshold)
	}
	if p.config.MaxRetries != 5 {
		t.Errorf("expected 5, got %d", p.config.MaxRetries)
	}
}

func TestParseCritiqueValid(t *testing.T) {
	p := NewPipeline(nil, nil)
	content := `{
		"overall_score": 0.85,
		"grade": "B+",
		"dimensions": [
			{"name": "correctness", "weight": 0.3, "score": 0.9, "explanation": "Code is correct"},
			{"name": "security", "weight": 0.25, "score": 0.8, "explanation": "No vulnerabilities found"}
		],
		"feedback": "Good overall, minor improvements needed",
		"suggestions": ["Add error handling"]
	}`

	result, err := p.parseCritique(content)
	if err != nil {
		t.Fatalf("parseCritique failed: %v", err)
	}
	if result.OverallScore != 0.85 {
		t.Errorf("expected score 0.85, got %f", result.OverallScore)
	}
	if result.Grade != "B+" {
		t.Errorf("expected grade B+, got %s", result.Grade)
	}
	if len(result.Dimensions) != 2 {
		t.Errorf("expected 2 dimensions, got %d", len(result.Dimensions))
	}
	if result.Dimensions[0].Name != "correctness" {
		t.Errorf("expected first dimension 'correctness', got %s", result.Dimensions[0].Name)
	}
}

func TestParseCritiqueWithMarkdown(t *testing.T) {
	p := NewPipeline(nil, nil)
	content := "Here's the critique:\n```json\n{\"overall_score\": 0.9, \"grade\": \"A\", \"dimensions\": [], \"feedback\": \"excellent\"}\n```\nDone."

	result, err := p.parseCritique(content)
	if err != nil {
		t.Fatalf("parseCritique failed: %v", err)
	}
	if result.OverallScore != 0.9 {
		t.Errorf("expected 0.9, got %f", result.OverallScore)
	}
}

func TestParseCritiqueInvalid(t *testing.T) {
	p := NewPipeline(nil, nil)
	_, err := p.parseCritique("no json here")
	if err == nil {
		t.Error("expected error for invalid input")
	}
}

func TestParseCritiqueMissingGrade(t *testing.T) {
	p := NewPipeline(nil, nil)
	content := `{"overall_score": 0.88, "dimensions": [], "feedback": "ok"}`

	result, err := p.parseCritique(content)
	if err != nil {
		t.Fatalf("parseCritique failed: %v", err)
	}
	if result.Grade == "" {
		t.Error("grade should be derived from score when missing")
	}
	if result.Grade != "B+" {
		t.Errorf("expected B+, got %s", result.Grade)
	}
}

func TestBuildRetryFeedback(t *testing.T) {
	p := NewPipeline(nil, nil)
	critique := &CritiqueResult{
		OverallScore: 0.5,
		Grade:        "D",
		Feedback:     "Missing error handling, security issues",
		Dimensions: []Dimension{
			{Name: "correctness", Weight: 0.3, Score: 0.6, Explanation: "Missing edge cases"},
			{Name: "security", Weight: 0.25, Score: 0.3, Explanation: "SQL injection risk"},
		},
	}

	feedback := p.buildRetryFeedback("old response", critique)
	if feedback == "" {
		t.Error("feedback should not be empty")
	}
	if !containsString(feedback, "0.50") {
		t.Error("feedback should contain the score")
	}
	if !containsString(feedback, "D") {
		t.Error("feedback should contain the grade")
	}
}

func TestBuildCriticPrompt(t *testing.T) {
	p := NewPipeline(nil, nil)
	prompt := p.buildCriticPrompt("fix the bug", "code here", "bug_fix")
	if prompt == "" {
		t.Error("prompt should not be empty")
	}
	if !containsString(prompt, "fix the bug") {
		t.Error("prompt should contain the request")
	}
	if !containsString(prompt, "code here") {
		t.Error("prompt should contain the response")
	}
}

func TestDimensionWeights(t *testing.T) {
	cfg := DefaultConfig()
	seen := make(map[string]bool)
	for _, d := range cfg.Dimensions {
		if d.Name == "" {
			t.Error("dimension name should not be empty")
		}
		if d.Weight <= 0 || d.Weight > 1 {
			t.Errorf("dimension %s weight should be between 0 and 1, got %f", d.Name, d.Weight)
		}
		if seen[d.Name] {
			t.Errorf("duplicate dimension name: %s", d.Name)
		}
		seen[d.Name] = true
	}
}

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}
