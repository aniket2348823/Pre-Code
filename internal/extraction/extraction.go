// Package extraction bridges the scanner and skillengine: when a vulnerability
// is found, this package extracts the abstract pattern and stores it as a
// reusable skill. Next time similar code appears, the pattern matches instantly
// with no LLM call needed. This is the accelerating-returns mechanism.
package extraction

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/scanner"
)

// Pattern represents an extracted vulnerability pattern.
type Pattern struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Category     string    `json:"category"`     // sql_injection, xss, hardcoded_secret, etc.
	Trigger      string    `json:"trigger"`      // abstract pattern that detects this issue
	Fix          string    `json:"fix"`          // the recommended fix template
	Verification string    `json:"verification"` // how to verify the fix works
	Confidence   float64   `json:"confidence"`   // 0.0–1.0
	UsageCount   int       `json:"usage_count"`
	SuccessRate  float64   `json:"success_rate"`
	Variants     []string  `json:"variants"`     // different concrete manifestations
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// MatchResult represents a pattern match against new code.
type MatchResult struct {
	Pattern    *Pattern `json:"pattern"`
	Confidence float64  `json:"confidence"`
	MatchType  string   `json:"match_type"` // "exact", "fuzzy", "semantic"
}

// SimilarityThreshold is the minimum Jaccard similarity for pattern deduplication.
const SimilarityThreshold = 0.7

// Engine extracts and matches vulnerability patterns.
type Engine struct {
	mu       sync.RWMutex
	patterns map[string]*Pattern
	// Category index for fast lookup
	categoryIndex map[string][]*Pattern
}

// NewEngine creates a new pattern extraction engine.
func NewEngine() *Engine {
	return &Engine{
		patterns:      make(map[string]*Pattern),
		categoryIndex: make(map[string][]*Pattern),
	}
}

// ExtractFromFindings processes scanner findings and extracts patterns.
func (e *Engine) ExtractFromFindings(findings []scanner.Finding) []*Pattern {
	e.mu.Lock()
	defer e.mu.Unlock()

	var extracted []*Pattern
	for _, f := range findings {
		pattern := e.extractPattern(f)
		if pattern == nil {
			continue
		}

		// Check if similar pattern exists
		existing := e.findSimilarPattern(pattern)
		if existing != nil {
			// Merge: add variant, update confidence
			existing.Variants = appendIfUnique(existing.Variants, f.Snippet)
			existing.UsageCount++
			existing.UpdatedAt = time.Now()
			// Update confidence with exponential moving average
			existing.Confidence = existing.Confidence*0.8 + pattern.Confidence*0.2
			extracted = append(extracted, existing)
		} else {
			// New pattern
			e.patterns[pattern.ID] = pattern
			e.categoryIndex[pattern.Category] = append(e.categoryIndex[pattern.Category], pattern)
			extracted = append(extracted, pattern)
		}
	}

	return extracted
}

// MatchPattern checks if a code snippet matches any known pattern.
func (e *Engine) MatchPattern(code string, language string) []MatchResult {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var results []MatchResult
	codeLower := strings.ToLower(code)

	for _, pattern := range e.patterns {
		match := e.matchPatternAgainstCode(pattern, codeLower, language)
		if match != nil {
			results = append(results, *match)
		}
	}

	// Sort by confidence descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Confidence > results[j].Confidence
	})

	return results
}

// GetPatternsByCategory returns all patterns in a category.
func (e *Engine) GetPatternsByCategory(category string) []*Pattern {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.categoryIndex[category]
}

// GetAllPatterns returns all patterns sorted by usage count.
func (e *Engine) GetAllPatterns() []*Pattern {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]*Pattern, 0, len(e.patterns))
	for _, p := range e.patterns {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UsageCount > out[j].UsageCount
	})
	return out
}

// Count returns the number of patterns.
func (e *Engine) Count() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.patterns)
}

// RecordOutcome records whether a pattern match was successful.
func (e *Engine) RecordOutcome(patternID string, accepted bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	pattern, ok := e.patterns[patternID]
	if !ok {
		return
	}

	pattern.UsageCount++
	alpha := 0.1
	if accepted {
		pattern.SuccessRate = pattern.SuccessRate*(1-alpha) + alpha
	} else {
		pattern.SuccessRate = pattern.SuccessRate * (1 - alpha)
	}
	pattern.UpdatedAt = time.Now()
}

// extractPattern creates an abstract pattern from a concrete finding.
func (e *Engine) extractPattern(f scanner.Finding) *Pattern {
	if f.Message == "" && f.Title == "" {
		return nil
	}

	// Determine category from severity and message
	category := categorizeFinding(f)
	if category == "" {
		return nil
	}

	// Create abstract trigger from the finding message
	trigger := abstractTrigger(f.Message, f.Title)

	// Generate fix template
	fix := generateFixTemplate(f)

	// Compute deterministic ID
	id := computePatternID(trigger, category)

	return &Pattern{
		ID:           id,
		Name:         f.Title,
		Category:     category,
		Trigger:      trigger,
		Fix:          fix,
		Verification: "verify: " + fix,
		Confidence:   f.Confidence,
		UsageCount:   1,
		SuccessRate:  0.5,
		Variants:     []string{f.Snippet},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

// findSimilarPattern looks for an existing pattern that matches the new one.
func (e *Engine) findSimilarPattern(new *Pattern) *Pattern {
	// First check exact category match
	candidates := e.categoryIndex[new.Category]
	for _, existing := range candidates {
		if existing.Trigger == new.Trigger {
			return existing
		}
	}

	// Fuzzy match: check if triggers are similar
	for _, existing := range candidates {
		if similarityScore(existing.Trigger, new.Trigger) > SimilarityThreshold {
			return existing
		}
	}

	return nil
}

// matchPatternAgainstCode checks if a pattern matches a code snippet.
func (e *Engine) matchPatternAgainstCode(pattern *Pattern, code string, language string) *MatchResult {
	// Check trigger keywords against code
	triggerWords := strings.Fields(strings.ToLower(pattern.Trigger))
	matchCount := 0
	for _, word := range triggerWords {
		if len(word) < 3 {
			continue // skip short words
		}
		if strings.Contains(code, word) {
			matchCount++
		}
	}

	if matchCount == 0 {
		return nil
	}

	confidence := float64(matchCount) / float64(len(triggerWords))
	if confidence < 0.5 {
		return nil
	}

	matchType := "fuzzy"
	if confidence > 0.9 {
		matchType = "exact"
	}

	return &MatchResult{
		Pattern:    pattern,
		Confidence: confidence,
		MatchType:  matchType,
	}
}

// categorizeFinding determines the vulnerability category from a finding.
func categorizeFinding(f scanner.Finding) string {
	msg := strings.ToLower(f.Message + " " + f.Title)

	switch {
	case strings.Contains(msg, "sql injection") || strings.Contains(msg, "sqli"):
		return "sql_injection"
	case strings.Contains(msg, "xss") || strings.Contains(msg, "cross-site scripting"):
		return "xss"
	case strings.Contains(msg, "hardcoded") || strings.Contains(msg, "secret") || strings.Contains(msg, "password"):
		return "hardcoded_secret"
	case strings.Contains(msg, "path traversal") || strings.Contains(msg, "directory traversal"):
		return "path_traversal"
	case strings.Contains(msg, "command injection") || strings.Contains(msg, "os command"):
		return "command_injection"
	case strings.Contains(msg, "insecure deserialization"):
		return "insecure_deserialization"
	case strings.Contains(msg, "ssrf") || strings.Contains(msg, "server-side request"):
		return "ssrf"
	case strings.Contains(msg, "csrf") || strings.Contains(msg, "cross-site request"):
		return "csrf"
	case strings.Contains(msg, "weak crypto") || strings.Contains(msg, "md5") || strings.Contains(msg, "sha1"):
		return "weak_crypto"
	case strings.Contains(msg, "missing rate limit"):
		return "missing_rate_limit"
	case strings.Contains(msg, "missing auth") || strings.Contains(msg, "broken auth"):
		return "broken_auth"
	case strings.Contains(msg, "sensitive data") || strings.Contains(msg, "pii"):
		return "sensitive_data_exposure"
	default:
		return "" // uncategorized findings don't become patterns
	}
}

// abstractTrigger creates an abstract trigger string from a finding message.
func abstractTrigger(message, title string) string {
	// Remove specific identifiers (file names, line numbers, variable names)
	trigger := strings.ToLower(title)
	if trigger == "" {
		trigger = strings.ToLower(message)
	}

	// Remove common non-pattern words
	noise := []string{"at line", "in file", "found", "detected", "detected:", "potential", "possible"}
	for _, n := range noise {
		trigger = strings.ReplaceAll(trigger, n, "")
	}

	// Collapse whitespace
	trigger = strings.Join(strings.Fields(trigger), " ")
	return strings.TrimSpace(trigger)
}

// generateFixTemplate creates a fix template from a finding.
func generateFixTemplate(f scanner.Finding) string {
	if f.Fix != "" {
		return f.Fix
	}

	// Generate generic fix based on category
	category := categorizeFinding(f)
	switch category {
	case "sql_injection":
		return "Use parameterized queries instead of string concatenation"
	case "xss":
		return "Sanitize user input and use context-aware output encoding"
	case "hardcoded_secret":
		return "Move secrets to environment variables or a secrets manager"
	case "path_traversal":
		return "Validate and sanitize file paths, use filepath.Clean()"
	case "command_injection":
		return "Use exec.Command with separate args, never interpolate user input"
	case "weak_crypto":
		return "Replace MD5/SHA1 with SHA256 or bcrypt for passwords"
	case "missing_rate_limit":
		return "Add rate limiting middleware to the endpoint"
	case "broken_auth":
		return "Implement proper session management and authentication checks"
	default:
		return "Review and fix the identified security issue"
	}
}

// computePatternID creates a deterministic ID from trigger and category.
func computePatternID(trigger, category string) string {
	h := sha256.Sum256([]byte(category + ":" + trigger))
	return "pat-" + hex.EncodeToString(h[:8])
}

// similarityScore computes a simple Jaccard similarity between two strings.
func similarityScore(a, b string) float64 {
	wordsA := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(a)) {
		if len(w) >= 3 {
			wordsA[w] = true
		}
	}
	wordsB := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(b)) {
		if len(w) >= 3 {
			wordsB[w] = true
		}
	}

	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}

	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}

	union := len(wordsA) + len(wordsB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// appendIfUnique adds a string to a slice only if it's not already present.
func appendIfUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}
