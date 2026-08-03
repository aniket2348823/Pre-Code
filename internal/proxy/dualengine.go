package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/llm"
)

// ═══════════════════════════════════════════════════════════════════════════
// DUAL-ENGINE ANALYSIS ARCHITECTURE
// ═══════════════════════════════════════════════════════════════════════════
//
// When a user sends a prompt to any LLM (ChatGPT, Claude, etc.):
//
//  1. LLM produces a response
//  2. VigilAgent intercepts the response
//  3. TWO engines run IN PARALLEL:
//     - Deterministic Engine: Semgrep, builtin rules, regex (fast, free)
//     - LLM Engine: GPT-4o-mini analyzing for semantic issues (slow, cheap)
//  4. Findings from both engines are MERGED
//  5. Corroboration scoring: both engines finding same issue = HIGH confidence
//  6. Enhanced response returned to user
//
// ═══════════════════════════════════════════════════════════════════════════

// Finding represents a single security/quality finding from either engine.
type Finding struct {
	RuleID     string  `json:"rule_id"`
	Engine     string  `json:"engine"`     // "deterministic" or "llm"
	Severity   string  `json:"severity"`   // critical, high, medium, low, info
	Category   string  `json:"category"`   // injection, secrets, crypto, etc.
	Message    string  `json:"message"`
	Fix        string  `json:"fix,omitempty"`
	Line       int     `json:"line,omitempty"`
	Confidence float64 `json:"confidence"` // 0.0 - 1.0
	Snippet    string  `json:"snippet,omitempty"`
}

// DualEngineResult holds the merged result from both engines.
type DualEngineResult struct {
	Findings    []Finding      `json:"findings"`
	Score       int            `json:"score"` // 0-100
	Grade       string         `json:"grade"` // A-F
	EngineStats EngineStats    `json:"engine_stats"`
	Summary     string         `json:"summary"`
	Metadata    ResultMetadata `json:"metadata"`
}

// EngineStats tracks timing and counts for each engine.
type EngineStats struct {
	Deterministic DeterministicStats `json:"deterministic"`
	LLM           LLMStats           `json:"llm"`
	TotalLatency  time.Duration      `json:"total_latency_ms"`
}

// DeterministicStats tracks the fast deterministic engine.
type DeterministicStats struct {
	FindingsCount int           `json:"findings_count"`
	Latency       time.Duration `json:"latency_ms"`
	EngineErrors  []string      `json:"engine_errors,omitempty"`
}

// LLMStats tracks the LLM analysis engine.
type LLMStats struct {
	FindingsCount int           `json:"findings_count"`
	Latency       time.Duration `json:"latency_ms"`
	Model         string        `json:"model"`
	Cost          float64       `json:"cost"`
	Error         string        `json:"error,omitempty"`
}

// ResultMetadata provides context about the analysis.
type ResultMetadata struct {
	CodeLength    int    `json:"code_length"`
	Language      string `json:"language"`
	AnalyzedAt    string `json:"analyzed_at"`
	Corroboration int    `json:"corroborated_findings"` // findings confirmed by both engines
}

// ═══════════════════════════════════════════════════════════════════════════
// DUAL-ENGINE RUNNER
// ═══════════════════════════════════════════════════════════════════════════

// DualEngineAnalyzer runs both engines in parallel and merges results.
type DualEngineAnalyzer struct {
	backendURL string
	apiKey     string
	client     *http.Client
	llmKey     string // user's LLM key for BYOK
	llmModel   string // model to use for LLM engine (default: gpt-4o-mini)
}

// NewDualEngineAnalyzer creates a new dual-engine analyzer.
func NewDualEngineAnalyzer(backendURL, apiKey, llmKey string) *DualEngineAnalyzer {
	return &DualEngineAnalyzer{
		backendURL: backendURL,
		apiKey:     apiKey,
		client:     &http.Client{Timeout: 30 * time.Second},
		llmKey:     llmKey,
		llmModel:   "gpt-4o-mini", // cheap, fast, good at code analysis
	}
}

// Analyze runs both engines in parallel and returns merged findings.
// The modelRouter is used for in-process LLM calls (no HTTP loopback).
func (d *DualEngineAnalyzer) Analyze(ctx context.Context, modelRouter *llm.ModelRouter, code, language string) *DualEngineResult {
	start := time.Now()

	var (
		detFindings []Finding
		llmFindings []Finding
		detStats    DeterministicStats
		llmStats    LLMStats
		detErr      error
		llmErr      error
		wg          sync.WaitGroup
	)

	// Run both engines in parallel
	wg.Add(2)

	go func() {
		defer wg.Done()
		detFindings, detStats, detErr = d.runDeterministicEngine(ctx, code, language)
	}()

	go func() {
		defer wg.Done()
		llmFindings, llmStats, llmErr = d.runLLMEngine(ctx, modelRouter, code, language)
	}()

	wg.Wait()

	// Log errors but don't fail — partial results are still useful
	if detErr != nil {
		slog.Warn("deterministic engine error", "error", detErr)
	}
	if llmErr != nil {
		slog.Warn("llm engine error", "error", llmErr)
	}

	// Merge findings with corroboration scoring
	merged := MergeFindings(detFindings, llmFindings)

	// Calculate score and grade
	score, grade := CalculateScore(merged)

	// Count corroborated findings (found by both engines)
	corroborated := 0
	for _, f := range merged {
		if strings.Contains(f.RuleID, "+llm") {
			corroborated++
		}
	}

	totalLatency := time.Since(start)

	return &DualEngineResult{
		Findings: merged,
		Score:    score,
		Grade:    grade,
		EngineStats: EngineStats{
			Deterministic: detStats,
			LLM:           llmStats,
			TotalLatency:  totalLatency,
		},
		Summary: BuildSummary(merged, score, grade, corroborated),
		Metadata: ResultMetadata{
			CodeLength:    len(code),
			Language:      language,
			AnalyzedAt:    time.Now().UTC().Format(time.RFC3339),
			Corroboration: corroborated,
		},
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// DETERMINISTIC ENGINE (Fast, Free, Catches Known Patterns)
// ═══════════════════════════════════════════════════════════════════════════

func (d *DualEngineAnalyzer) runDeterministicEngine(ctx context.Context, code, language string) ([]Finding, DeterministicStats, error) {
	start := time.Now()
	stats := DeterministicStats{}

	// Call backend's scanner engine (which has Semgrep, Bandit, Builtin)
	payload := map[string]string{
		"code":     code,
		"language": language,
	}
	bodyBytes, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", d.backendURL+"/api/v1/middleware/process", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, stats, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.client.Do(req)
	if err != nil {
		// Fallback to local pattern matching
		return d.localDeterministicScan(code, language), stats, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return d.localDeterministicScan(code, language), stats, nil
	}

	var result struct {
		Findings []struct {
			Severity string `json:"severity"`
			Message  string `json:"message"`
			Fix      string `json:"fix"`
			Line     int    `json:"line"`
			RuleID   string `json:"rule_id"`
			Category string `json:"category"`
			Snippet  string `json:"snippet"`
		} `json:"findings"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return d.localDeterministicScan(code, language), stats, nil
	}

	var findings []Finding
	for _, f := range result.Findings {
		findings = append(findings, Finding{
			RuleID:   f.RuleID,
			Engine:   "deterministic",
			Severity: f.Severity,
			Category: f.Category,
			Message:  f.Message,
			Fix:      f.Fix,
			Line:     f.Line,
			Snippet:  f.Snippet,
		})
	}

	stats.FindingsCount = len(findings)
	stats.Latency = time.Since(start)

	return findings, stats, nil
}

// localDeterministicScan is a fallback when the backend is unavailable.
// Uses regex patterns to catch common vulnerabilities.
func (d *DualEngineAnalyzer) localDeterministicScan(code, language string) []Finding {
	var findings []Finding

	// SQL Injection patterns
	sqlInjectionRe := regexp.MustCompile(`(?i)(SELECT|INSERT|UPDATE|DELETE|DROP)\s+.*\+\s*(?:req\.|params\.|input\.|user)`)
	if sqlInjectionRe.MatchString(code) {
		findings = append(findings, Finding{
			RuleID:     "local-sql-injection",
			Engine:     "deterministic",
			Severity:   "critical",
			Category:   "injection",
			Message:    "Potential SQL injection: string concatenation with user input in SQL query",
			Fix:        "Use parameterized queries instead of string concatenation",
			Confidence: 0.85,
		})
	}

	// XSS patterns
	xssRe := regexp.MustCompile(`(?i)(innerHTML|document\.write|eval\(|dangerouslySetInnerHTML)`)
	if xssRe.MatchString(code) {
		findings = append(findings, Finding{
			RuleID:     "local-xss",
			Engine:     "deterministic",
			Severity:   "high",
			Category:   "xss",
			Message:    "Potential XSS vulnerability: unescaped user input in HTML output",
			Fix:        "Use proper escaping or a templating engine that auto-escapes",
			Confidence: 0.80,
		})
	}

	// Hardcoded secrets
	secretRe := regexp.MustCompile(`(?i)(password|secret|api_key|apikey|token)\s*[:=]\s*["'][^"']{8,}["']`)
	if secretRe.MatchString(code) {
		findings = append(findings, Finding{
			RuleID:     "local-hardcoded-secret",
			Engine:     "deterministic",
			Severity:   "critical",
			Category:   "secrets",
			Message:    "Potential hardcoded secret detected",
			Fix:        "Move secrets to environment variables or a secrets manager",
			Confidence: 0.75,
		})
	}

	// Weak crypto
	weakCryptoRe := regexp.MustCompile(`(?i)(md5|sha1|des|rc4)\b`)
	if weakCryptoRe.MatchString(code) {
		findings = append(findings, Finding{
			RuleID:     "local-weak-crypto",
			Engine:     "deterministic",
			Severity:   "medium",
			Category:   "crypto",
			Message:    "Weak cryptographic algorithm detected",
			Fix:        "Use SHA-256+ or AES for modern applications",
			Confidence: 0.70,
		})
	}

	// Command injection
	cmdRe := regexp.MustCompile(`(?i)(os\.exec|exec\.|system\(|popen|subprocess\.call|subprocess\.Popen)`)
	if cmdRe.MatchString(code) {
		findings = append(findings, Finding{
			RuleID:     "local-cmd-injection",
			Engine:     "deterministic",
			Severity:   "critical",
			Category:   "injection",
			Message:    "Potential command injection: user input in shell command",
			Fix:        "Use parameterized commands or a safe execution library",
			Confidence: 0.80,
		})
	}

	return findings
}

// ═══════════════════════════════════════════════════════════════════════════
// LLM ENGINE (Slow, Cheap, Catches Semantic Issues)
// Uses ModelRouter in-process — no HTTP loopback.
// ═══════════════════════════════════════════════════════════════════════════

func (d *DualEngineAnalyzer) runLLMEngine(ctx context.Context, modelRouter *llm.ModelRouter, code, language string) ([]Finding, LLMStats, error) {
	start := time.Now()
	stats := LLMStats{
		Model: d.llmModel,
	}

	if modelRouter == nil {
		stats.Error = "no model router available"
		stats.Latency = time.Since(start)
		return nil, stats, fmt.Errorf("no model router available")
	}

	// Build the analysis prompt
	prompt := fmt.Sprintf(`You are a security code reviewer. Analyze this %s code for:
1. Security vulnerabilities (SQL injection, XSS, CSRF, etc.)
2. Logic bugs and edge cases
3. Performance issues
4. Best practice violations

Return a JSON array of findings. Each finding must have:
- "rule_id": a unique identifier (e.g., "llm-sql-injection-1")
- "severity": "critical", "high", "medium", "low", or "info"
- "category": "injection", "secrets", "xss", "logic", "performance", "best-practice"
- "message": clear description of the issue
- "fix": how to fix it
- "line": line number if applicable (0 if unknown)
- "confidence": 0.0-1.0 how confident you are

Return ONLY the JSON array, no other text.

Code:
`+"```"+language+"\n"+code+"\n```", language, code)

	// Use ModelRouter directly — no HTTP loopback
	task := &llm.Task{
		ID:          "dual-engine-llm-" + time.Now().Format("20060102150405"),
		Type:        "security",
		Description: "Dual-engine LLM code analysis",
		Messages:    []llm.Message{{Role: "user", Content: prompt}},
	}

	result, err := modelRouter.ExecuteWithFailover(ctx, task)
	if err != nil {
		stats.Error = err.Error()
		stats.Latency = time.Since(start)
		return nil, stats, err
	}

	// Parse LLM findings from response
	findings := ParseLLMFindings(result.Content)

	// If no findings could be parsed, return an error finding
	if len(findings) == 0 {
		findings = []Finding{{
			RuleID:     "llm-parse-fallback",
			Engine:     "llm",
			Severity:   "info",
			Category:   "analysis",
			Message:    "LLM analysis completed but findings could not be parsed as structured JSON",
			Confidence: 0.5,
		}}
	}

	stats.FindingsCount = len(findings)
	stats.Latency = time.Since(start)
	stats.Cost = result.Cost

	return findings, stats, nil
}

// ParseLLMFindings extracts findings from LLM response text.
// Returns a synthetic error finding if parsing fails.
func ParseLLMFindings(text string) []Finding {
	// Try to extract JSON array from response
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start == -1 || end == -1 || end <= start {
		return nil
	}

	jsonStr := text[start : end+1]

	var rawFindings []struct {
		RuleID     string  `json:"rule_id"`
		Severity   string  `json:"severity"`
		Category   string  `json:"category"`
		Message    string  `json:"message"`
		Fix        string  `json:"fix"`
		Line       int     `json:"line"`
		Confidence float64 `json:"confidence"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &rawFindings); err != nil {
		slog.Warn("failed to parse LLM findings JSON", "error", err)
		return nil
	}

	var findings []Finding
	for _, f := range rawFindings {
		// Prefix rule_id with "llm-" to distinguish from deterministic findings
		ruleID := f.RuleID
		if !strings.HasPrefix(ruleID, "llm-") {
			ruleID = "llm-" + ruleID
		}

		findings = append(findings, Finding{
			RuleID:     ruleID,
			Engine:     "llm",
			Severity:   f.Severity,
			Category:   f.Category,
			Message:    f.Message,
			Fix:        f.Fix,
			Line:       f.Line,
			Confidence: f.Confidence,
		})
	}

	return findings
}

// ═══════════════════════════════════════════════════════════════════════════
// FINDING MERGER (Corroboration-Based Confidence)
// ═══════════════════════════════════════════════════════════════════════════

// MergeFindings combines findings from both engines and applies corroboration scoring.
// When both engines find the same issue, confidence is boosted.
func MergeFindings(detFindings, llmFindings []Finding) []Finding {
	// Index LLM findings by category for corroboration lookup
	llmByCategory := make(map[string][]Finding)
	for _, f := range llmFindings {
		llmByCategory[f.Category] = append(llmByCategory[f.Category], f)
	}

	// Start with deterministic findings
	merged := make([]Finding, 0, len(detFindings)+len(llmFindings))
	for _, f := range detFindings {
		// Check if LLM also found something in this category
		if llmMatches, ok := llmByCategory[f.Category]; ok && len(llmMatches) > 0 {
			// CORROBORATED: Both engines found the same category of issue
			f.Confidence = clampFloat(f.Confidence+0.2, 0.0, 1.0) // boost confidence
			f.RuleID = f.RuleID + "+llm"                           // mark as corroborated
			// Use the more severe finding's message
			if len(llmMatches) > 0 && severityRank(llmMatches[0].Severity) > severityRank(f.Severity) {
				f.Severity = llmMatches[0].Severity
			}
		}
		merged = append(merged, f)
	}

	// Add LLM-only findings (not corroborated by deterministic)
	for _, f := range llmFindings {
		found := false
		for _, m := range merged {
			if m.Category == f.Category && !strings.Contains(m.RuleID, "+llm") {
				found = true
				break
			}
		}
		if !found {
			// LLM-only finding: lower confidence since not corroborated
			f.Confidence = clampFloat(f.Confidence, 0.0, 0.7)
			merged = append(merged, f)
		}
	}

	// Sort by severity (critical first), then confidence
	sort.SliceStable(merged, func(i, j int) bool {
		if severityRank(merged[i].Severity) != severityRank(merged[j].Severity) {
			return severityRank(merged[i].Severity) > severityRank(merged[j].Severity)
		}
		return merged[i].Confidence > merged[j].Confidence
	})

	return merged
}

// DeduplicateFindings removes duplicate findings based on category and severity.
func DeduplicateFindings(findings []Finding) []Finding {
	seen := make(map[string]bool)
	var result []Finding
	for _, f := range findings {
		key := fmt.Sprintf("%s:%s:%s", f.Category, f.Severity, f.Message)
		if !seen[key] {
			seen[key] = true
			result = append(result, f)
		}
	}
	return result
}

func severityRank(sev string) int {
	switch strings.ToLower(sev) {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

func clampFloat(val, min, max float64) float64 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

// ═══════════════════════════════════════════════════════════════════════════
// SCORE & GRADE CALCULATION
// ═══════════════════════════════════════════════════════════════════════════

// CalculateScore computes a 0-100 score and letter grade from findings.
func CalculateScore(findings []Finding) (int, string) {
	if len(findings) == 0 {
		return 100, "A"
	}

	// Start at 100, deduct points for each finding
	score := 100.0
	for _, f := range findings {
		switch f.Severity {
		case "critical":
			score -= 25 * f.Confidence
		case "high":
			score -= 15 * f.Confidence
		case "medium":
			score -= 8 * f.Confidence
		case "low":
			score -= 3 * f.Confidence
		case "info":
			score -= 1 * f.Confidence
		}
	}

	if score < 0 {
		score = 0
	}

	// Bonus for corroboration (both engines agreed)
	corroborated := 0
	for _, f := range findings {
		if strings.Contains(f.RuleID, "+llm") {
			corroborated++
		}
	}
	if corroborated > 0 {
		score += float64(corroborated) * 2 // small bonus for corroboration
	}
	if score > 100 {
		score = 100
	}

	grade := ScoreToGrade(score)
	return int(score), grade
}

// ScoreToGrade converts a numeric score to a letter grade.
func ScoreToGrade(score float64) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// SUMMARY BUILDER
// ═══════════════════════════════════════════════════════════════════════════

// BuildSummary creates a human-readable summary of findings.
func BuildSummary(findings []Finding, score int, grade string, corroborated int) string {
	if len(findings) == 0 {
		return "✅ No issues found. Code looks clean!"
	}

	var sb strings.Builder

	// Header
	sb.WriteString(fmt.Sprintf("🛡️ VigilAgent Analysis: Grade %s (%d%%)\n", grade, score))

	// Stats
	critical := 0
	high := 0
	medium := 0
	low := 0
	for _, f := range findings {
		switch f.Severity {
		case "critical":
			critical++
		case "high":
			high++
		case "medium":
			medium++
		case "low":
			low++
		}
	}

	sb.WriteString(fmt.Sprintf("📊 Findings: %d total", len(findings)))
	if critical > 0 {
		sb.WriteString(fmt.Sprintf(" | 🔴 %d critical", critical))
	}
	if high > 0 {
		sb.WriteString(fmt.Sprintf(" | 🟠 %d high", high))
	}
	if medium > 0 {
		sb.WriteString(fmt.Sprintf(" | 🟡 %d medium", medium))
	}
	if low > 0 {
		sb.WriteString(fmt.Sprintf(" | 🟢 %d low", low))
	}
	sb.WriteString("\n")

	if corroborated > 0 {
		sb.WriteString(fmt.Sprintf("🔗 Corroborated: %d findings confirmed by both engines\n", corroborated))
	}

	// Top findings (max 5)
	sb.WriteString("\n🔍 Key Findings:\n")
	limit := len(findings)
	if limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		f := findings[i]
		icon := SeverityIcon(f.Severity)
		corroboratedMark := ""
		if strings.Contains(f.RuleID, "+llm") {
			corroboratedMark = " ✓"
		}
		sb.WriteString(fmt.Sprintf("  %s [%s] %s%s\n", icon, strings.ToUpper(f.Severity), f.Message, corroboratedMark))
		if f.Fix != "" {
			sb.WriteString(fmt.Sprintf("     💡 Fix: %s\n", f.Fix))
		}
	}
	if len(findings) > 5 {
		sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(findings)-5))
	}

	sb.WriteString("\nPowered by VigilAgent Dual-Engine Analysis")

	return sb.String()
}

// SeverityIcon returns an emoji for a severity level.
func SeverityIcon(sev string) string {
	switch sev {
	case "critical":
		return "🔴"
	case "high":
		return "🟠"
	case "medium":
		return "🟡"
	case "low":
		return "🟢"
	case "info":
		return "ℹ️"
	default:
		return "❓"
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// INTEGRATION WITH PROXY SERVER
// ═══════════════════════════════════════════════════════════════════════════

// AnalyzeWithDualEngine is the entry point for the proxy to run parallel analysis.
// It accepts a ModelRouter for in-process LLM calls (no HTTP loopback).
func AnalyzeWithDualEngine(ctx context.Context, modelRouter *llm.ModelRouter, backendURL, apiKey, llmKey, code, language string) *DualEngineResult {
	analyzer := NewDualEngineAnalyzer(backendURL, apiKey, llmKey)
	return analyzer.Analyze(ctx, modelRouter, code, language)
}
