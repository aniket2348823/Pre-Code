package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// Severity ranks how serious a finding is.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// SeverityRank orders severities for sorting; higher is more severe.
func SeverityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// Finding is one normalized security issue from any analyzer.
type Finding struct {
	RuleID      string   `json:"rule_id"`
	Analyzers   []string `json:"analyzers"`
	Severity    Severity `json:"severity"`
	Category    string   `json:"category,omitempty"`
	Title       string   `json:"title"`
	Message     string   `json:"message"`
	Filename    string   `json:"filename,omitempty"`
	Line        int      `json:"line,omitempty"`
	Snippet     string   `json:"snippet,omitempty"`
	Fix         string   `json:"fix,omitempty"`
	Confidence  float64  `json:"confidence"`
	Fingerprint string   `json:"fingerprint"`
}

// Report is the engine's full output for one Input.
type Report struct {
	Findings         []Finding         `json:"findings"`
	AnalyzersRun     []string          `json:"analyzers_run"`
	AnalyzersSkipped map[string]string `json:"analyzers_skipped"` // name -> reason
	AnalyzerErrors   map[string]string `json:"analyzer_errors"`   // name -> error
}

// normalizeSnippet collapses runs of whitespace so cosmetic differences do not
// change a fingerprint.
func normalizeSnippet(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ComputeFingerprint derives a stable dedupe key from location + code, ignoring
// category so the same line flagged by different tools collapses into one.
func ComputeFingerprint(filename string, line int, snippet string) string {
	h := sha256.Sum256([]byte(filename + "|" + strconv.Itoa(line) + "|" + normalizeSnippet(snippet)))
	return hex.EncodeToString(h[:])[:16]
}
