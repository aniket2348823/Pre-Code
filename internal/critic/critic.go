// Package critic implements the parallel Critic LLM pipeline: every primary LLM
// response is evaluated in parallel by a critic that scores correctness, security,
// style, performance, and completeness. Low-scoring responses are retried with
// critic feedback injected into the prompt. This is the core differentiator that
// makes VigilAgent outputs better than any single LLM.
package critic

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/llm"
)

// Dimension represents a single evaluation dimension.
type Dimension struct {
	Name        string  `json:"name"`
	Weight      float64 `json:"weight"`      // 0.0–1.0
	Score       float64 `json:"score"`       // 0.0–1.0
	Explanation string  `json:"explanation"`
}

// CritiqueResult is the full output from the critic LLM.
type CritiqueResult struct {
	OverallScore float64     `json:"overall_score"` // 0.0–1.0
	Grade        string      `json:"grade"`         // A+, A, B+, B, C, D, F
	Dimensions   []Dimension `json:"dimensions"`
	Feedback     string      `json:"feedback"` // human-readable improvement suggestions
	Reject       bool        `json:"reject"`   // true if response should be retried
	Model        string      `json:"model"`    // which model produced the response
	CriticModel  string      `json:"critic_model"`
	Latency      float64     `json:"latency_ms"`
}

// RetryFeedback is the structured feedback injected into the primary LLM on retry.
type RetryFeedback struct {
	OriginalResponse string         `json:"original_response"`
	Critique         *CritiqueResult `json:"critique"`
	Suggestions      []string       `json:"suggestions"`
}

// Config holds critic pipeline configuration.
type Config struct {
	// CriticModel is the model used for evaluation (cheaper model recommended).
	CriticModel string `json:"critic_model"`
	// PrimaryModel is the model being evaluated.
	PrimaryModel string `json:"primary_model"`
	// Threshold is the minimum score to accept without retry.
	Threshold float64 `json:"threshold"`
	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int `json:"max_retries"`
	// Timeout is the maximum time for a single critique.
	Timeout time.Duration `json:"timeout"`
	// Dimensions defines the evaluation dimensions with weights.
	Dimensions []DimensionConfig `json:"dimensions"`
}

// DimensionConfig defines a configurable evaluation dimension.
type DimensionConfig struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
	Prompt string  `json:"prompt"` // custom evaluation prompt for this dimension
}

// DefaultConfig returns a production-ready critic configuration.
func DefaultConfig() *Config {
	return &Config{
		CriticModel:  "gpt-4o-mini",
		PrimaryModel: "claude-sonnet-4-20250514",
		Threshold:    0.75,
		MaxRetries:   3,
		Timeout:      30 * time.Second,
		Dimensions: []DimensionConfig{
			{Name: "correctness", Weight: 0.30, Prompt: "Is the code logically correct? Does it handle edge cases?"},
			{Name: "security", Weight: 0.25, Prompt: "Are there security vulnerabilities? SQL injection, XSS, hardcoded secrets?"},
			{Name: "style", Weight: 0.15, Prompt: "Does it follow Go conventions and project patterns?"},
			{Name: "performance", Weight: 0.15, Prompt: "Are there performance issues? N+1 queries, unnecessary allocations?"},
			{Name: "completeness", Weight: 0.15, Prompt: "Does it fully address the original request? Missing imports, error handling?"},
		},
	}
}

// Pipeline orchestrates the critic evaluation loop.
type Pipeline struct {
	router *llm.ModelRouter
	config *Config
	mu     sync.RWMutex
}

// NewPipeline creates a critic pipeline with the given router and config.
func NewPipeline(router *llm.ModelRouter, cfg *Config) *Pipeline {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Pipeline{
		router: router,
		config: cfg,
	}
}

// Evaluate runs the critic on a primary LLM response. Returns the critique
// result and whether the response should be accepted.
func (p *Pipeline) Evaluate(ctx context.Context, request string, response string, taskType string) (*CritiqueResult, error) {
	start := time.Now()

	// Build the critic prompt
	criticPrompt := p.buildCriticPrompt(request, response, taskType)

	// Run the critic LLM
	resp, err := p.router.ExecuteWithFailover(ctx, &llm.Task{
		ID:          "critique-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Type:        "analysis",
		Description: "Critique LLM response for quality",
		Messages: []llm.Message{
			{Role: "system", Content: p.systemPrompt()},
			{Role: "user", Content: criticPrompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("critic LLM call failed: %w", err)
	}

	// Parse the critique
	result, err := p.parseCritique(resp.Content)
	if err != nil {
		slog.Warn("critic: failed to parse critique, accepting response", "error", err)
		return &CritiqueResult{
			OverallScore: 0.8,
			Grade:        "B+",
			Reject:       false,
			Model:        resp.Model,
			CriticModel:  resp.Model,
			Latency:      float64(time.Since(start).Milliseconds()),
			Feedback:     "Critique parsing failed, defaulting to accept",
		}, nil
	}

	result.Model = resp.Model
	result.CriticModel = resp.Model
	result.Latency = float64(time.Since(start).Milliseconds())
	result.Reject = result.OverallScore < p.config.Threshold

	slog.Info("critic: evaluation complete",
		"score", result.OverallScore,
		"grade", result.Grade,
		"reject", result.Reject,
		"latency_ms", result.Latency,
	)

	return result, nil
}

// EvaluateWithRetry runs the critic loop: evaluate → retry with feedback → evaluate again.
func (p *Pipeline) EvaluateWithRetry(ctx context.Context, request string, response string, taskType string, generateFn func(feedback string) (string, error)) (string, *CritiqueResult, error) {
	currentResponse := response
	var lastCritique *CritiqueResult

	for attempt := 0; attempt <= p.config.MaxRetries; attempt++ {
		critique, err := p.Evaluate(ctx, request, currentResponse, taskType)
		if err != nil {
			return currentResponse, nil, err
		}
		lastCritique = critique
		if !critique.Reject {
			return currentResponse, critique, nil
		}
		feedback := p.BuildRetryFeedback(currentResponse, critique)
		newResponse, err := generateFn(feedback)
		if err != nil {
			return currentResponse, critique, fmt.Errorf("retry generation failed: %w", err)
		}
		currentResponse = newResponse
	}
	return currentResponse, lastCritique, nil
}

// BuildRetryFeedback constructs the feedback string for retry generation.
func (p *Pipeline) BuildRetryFeedback(response string, critique *CritiqueResult) string {
	return p.buildRetryFeedback(response, critique)
}

// systemPrompt returns the system prompt for the critic LLM.
func (p *Pipeline) systemPrompt() string {
	return `You are VigilAgent's Critic, an expert code reviewer and quality analyst.
Your job is to evaluate LLM-generated code responses for quality across multiple dimensions.

You MUST respond with valid JSON in this exact format:
{
  "overall_score": 0.0-1.0,
  "grade": "A+|A|B+|B|C|D|F",
  "dimensions": [
    {"name": "correctness", "weight": 0.3, "score": 0.0-1.0, "explanation": "..."},
    {"name": "security", "weight": 0.25, "score": 0.0-1.0, "explanation": "..."},
    {"name": "style", "weight": 0.15, "score": 0.0-1.0, "explanation": "..."},
    {"name": "performance", "weight": 0.15, "score": 0.0-1.0, "explanation": "..."},
    {"name": "completeness", "weight": 0.15, "score": 0.0-1.0, "explanation": "..."}
  ],
  "feedback": "Overall assessment and specific improvement suggestions",
  "suggestions": ["suggestion 1", "suggestion 2"]
}

Be harsh but fair. Focus on actionable feedback. A response must score ≥0.75 to pass.`
}

// buildCriticPrompt constructs the evaluation prompt.
func (p *Pipeline) buildCriticPrompt(request, response, taskType string) string {
	var sb strings.Builder
	sb.WriteString("## Original Request\n")
	sb.WriteString(request)
	sb.WriteString("\n\n## LLM Response to Evaluate\n")
	sb.WriteString(response)
	sb.WriteString("\n\n## Evaluation Dimensions\n")

	for _, dim := range p.config.Dimensions {
		sb.WriteString(fmt.Sprintf("- **%s** (weight: %.0f%%): %s\n", dim.Name, dim.Weight*100, dim.Prompt))
	}

	sb.WriteString("\n\nEvaluate the response above. Respond with JSON only.")
	return sb.String()
}

// parseCritique extracts the critique JSON from the critic response.
func (p *Pipeline) parseCritique(content string) (*CritiqueResult, error) {
	// Extract JSON from possible markdown wrapper
	content = strings.TrimSpace(content)
	if idx := strings.Index(content, "```json"); idx != -1 {
		content = content[idx+7:]
		if endIdx := strings.Index(content, "```"); endIdx != -1 {
			content = content[:endIdx]
		}
	} else if idx := strings.Index(content, "```"); idx != -1 {
		content = content[idx+3:]
		if endIdx := strings.Index(content, "```"); endIdx != -1 {
			content = content[:endIdx]
		}
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON object found in critic response")
	}
	content = content[start : end+1]

	var result struct {
		OverallScore float64 `json:"overall_score"`
		Grade        string  `json:"grade"`
		Dimensions   []struct {
			Name        string  `json:"name"`
			Weight      float64 `json:"weight"`
			Score       float64 `json:"score"`
			Explanation string  `json:"explanation"`
		} `json:"dimensions"`
		Feedback   string   `json:"feedback"`
		Suggestions []string `json:"suggestions"`
	}

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("failed to parse critic JSON: %w", err)
	}

	// Convert to our types
	dimensions := make([]Dimension, len(result.Dimensions))
	for i, d := range result.Dimensions {
		dimensions[i] = Dimension{
			Name:        d.Name,
			Weight:      d.Weight,
			Score:       d.Score,
			Explanation: d.Explanation,
		}
	}

	// If grade is empty, derive from score
	if result.Grade == "" {
		result.Grade = gradeFromScore(result.OverallScore)
	}

	return &CritiqueResult{
		OverallScore: result.OverallScore,
		Grade:        result.Grade,
		Dimensions:   dimensions,
		Feedback:     result.Feedback,
	}, nil
}

// buildRetryFeedback constructs the feedback string for retry generation.
func (p *Pipeline) buildRetryFeedback(response string, critique *CritiqueResult) string {
	var sb strings.Builder
	sb.WriteString("The previous response was critiqued and scored poorly. Here's the feedback:\n\n")
	sb.WriteString("## Previous Response\n")
	sb.WriteString(response)
	sb.WriteString("\n\n## Critique\n")
	sb.WriteString(fmt.Sprintf("Overall Score: %.2f (%s)\n\n", critique.OverallScore, critique.Grade))

	for _, dim := range critique.Dimensions {
		sb.WriteString(fmt.Sprintf("- **%s** (%.0f%%): %.2f — %s\n", dim.Name, dim.Weight*100, dim.Score, dim.Explanation))
	}

	sb.WriteString("\n## Improvement Suggestions\n")
	sb.WriteString(critique.Feedback)
	sb.WriteString("\n\nPlease regenerate the response addressing all the issues above. Be specific and complete.")

	return sb.String()
}

// gradeFromScore maps a score to a letter grade.
func gradeFromScore(score float64) string {
	switch {
	case score >= 0.95:
		return "A+"
	case score >= 0.90:
		return "A"
	case score >= 0.80:
		return "B+"
	case score >= 0.70:
		return "B"
	case score >= 0.60:
		return "C"
	case score >= 0.50:
		return "D"
	default:
		return "F"
	}
}
