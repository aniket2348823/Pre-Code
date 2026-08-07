package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

// Content from finding.go
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

// ComputeFingerprint derives a stable dedupe key from location + code.
// ruleID is included so that different rules matching the same line produce
// distinct findings (e.g. sql_injection and sql_injection_raw_query).
func ComputeFingerprint(filename string, line int, snippet string, ruleID ...string) string {
	rid := ""
	if len(ruleID) > 0 {
		rid = ruleID[0]
	}
	h := sha256.Sum256([]byte(filename + "|" + strconv.Itoa(line) + "|" + rid + "|" + normalizeSnippet(snippet)))
	return hex.EncodeToString(h[:])[:16]
}

// Content from confidence.go
// ─── SARIF Export ─────────────────────────────────────────────────────────
// ExportSARIF renders findings as a SARIF 2.1.0 report — the interchange
// format used by CI systems (GitHub code scanning, GitLab SAST, Azure
// DevOps). This is how the gateway's CI enforcement layer consumes the
// same findings the engines produce.

// sarifLevel maps a finding severity to a SARIF result level.
func sarifLevel(sev Severity) string {
	switch sev {
	case SeverityCritical, SeverityHigh:
		return "error"
	case SeverityMedium:
		return "warning"
	default:
		return "note"
	}
}

// securitySeverity maps a severity to the GitHub security-severity score
// (0.0–10.0, the property GitHub code scanning understands).
func securitySeverity(sev Severity) string {
	switch sev {
	case SeverityCritical:
		return "9.0"
	case SeverityHigh:
		return "7.5"
	case SeverityMedium:
		return "5.0"
	case SeverityLow:
		return "3.1"
	default:
		return "1.0"
	}
}

type sarifRule struct {
	ID               string                 `json:"id"`
	ShortDescription map[string]string      `json:"shortDescription"`
	FullDescription  map[string]string      `json:"fullDescription"`
	Properties       map[string]interface{} `json:"properties,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int               `json:"startLine"`
	Snippet   map[string]string `json:"snippet,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation struct {
		ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
		Region           *sarifRegion          `json:"region,omitempty"`
	} `json:"physicalLocation"`
}

type sarifResult struct {
	RuleID       string                 `json:"ruleId"`
	RuleIndex    int                    `json:"ruleIndex"`
	Level        string                 `json:"level"`
	Message      map[string]string      `json:"message"`
	Locations    []sarifLocation        `json:"locations,omitempty"`
	Fingerprints map[string]string      `json:"fingerprints,omitempty"`
	Properties   map[string]interface{} `json:"properties,omitempty"`
}

// ExportSARIF converts findings into a SARIF 2.1.0 report. Each unique rule id
// becomes a driver rule; each finding becomes a result with an exact
// file/line location, fingerprint, and severity metadata.
func ExportSARIF(findings []Finding) ([]byte, error) {
	ruleIDs := make([]string, 0, len(findings))
	seen := make(map[string]bool)
	worst := make(map[string]Severity) // worst severity per rule → rule security-severity
	for _, f := range findings {
		if !seen[f.RuleID] {
			seen[f.RuleID] = true
			ruleIDs = append(ruleIDs, f.RuleID)
		}
		if cur, ok := worst[f.RuleID]; !ok || SeverityRank(f.Severity) > SeverityRank(cur) {
			worst[f.RuleID] = f.Severity
		}
	}

	rules := make([]sarifRule, 0, len(ruleIDs))
	ruleIndex := make(map[string]int, len(ruleIDs))
	for i, id := range ruleIDs {
		ruleIndex[id] = i
		title := id
		if t := strings.TrimSpace(title); t != "" {
			title = t
		}
		rules = append(rules, sarifRule{
			ID:               id,
			ShortDescription: map[string]string{"text": title},
			FullDescription:  map[string]string{"text": title},
			Properties: map[string]interface{}{
				"security-severity": securitySeverity(worst[id]),
			},
		})
	}

	results := make([]sarifResult, 0, len(findings))
	for _, f := range findings {
		msg := f.Message
		if msg == "" {
			msg = f.Title
		}
		if msg == "" {
			msg = f.RuleID
		}
		r := sarifResult{
			RuleID:       f.RuleID,
			RuleIndex:    ruleIndex[f.RuleID],
			Level:        sarifLevel(f.Severity),
			Message:      map[string]string{"text": msg},
			Fingerprints: map[string]string{"vigilagent/v1": f.Fingerprint},
			Properties: map[string]interface{}{
				"severity":          string(f.Severity),
				"security-severity": securitySeverity(f.Severity),
				"confidence":        f.Confidence,
			},
		}
		if f.Category != "" {
			r.Properties["category"] = f.Category
		}
		if f.Fix != "" {
			r.Properties["fix"] = f.Fix
		}
		if f.Filename != "" {
			loc := sarifLocation{}
			loc.PhysicalLocation.ArtifactLocation = sarifArtifactLocation{URI: f.Filename}
			if f.Line > 0 {
				region := &sarifRegion{StartLine: f.Line}
				if f.Snippet != "" {
					region.Snippet = map[string]string{"text": f.Snippet}
				}
				loc.PhysicalLocation.Region = region
			}
			r.Locations = []sarifLocation{loc}
		}
		results = append(results, r)
	}

	report := map[string]interface{}{
		"$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		"version": "2.1.0",
		"runs": []map[string]interface{}{
			{
				"tool": map[string]interface{}{
					"driver": map[string]interface{}{
						"name":           "VigilAgent",
						"informationUri": "https://github.com/vigilagent/vigilagent",
						"version":        "1.0.0",
						"rules":          rules,
					},
				},
				"results": results,
			},
		},
	}

	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return out, nil
}

// baseConfidence maps severity to a starting confidence.
// These are deliberately lower for builtin regex (the noisiest source)
// since real tools have historically better precision.
func baseConfidence(sev Severity) float64 {
	switch sev {
	case SeverityCritical:
		return 0.65
	case SeverityHigh:
		return 0.55
	case SeverityMedium:
		return 0.40
	case SeverityLow:
		return 0.30
	default:
		return 0.20
	}
}

// analyzerWeight adds credibility when a real external tool (not the noisier
// builtin regex) reported the finding.
func analyzerWeight(analyzers []string) float64 {
	hasBuiltin := false
	hasRealTool := false
	for _, a := range analyzers {
		switch a {
		case "bandit", "semgrep":
			hasRealTool = true
		case "builtin":
			hasBuiltin = true
		}
	}
	if hasRealTool && !hasBuiltin {
		return 0.15 // pure real-tool finding — highest boost
	}
	if hasRealTool && hasBuiltin {
		return 0.10 // corroborated by real tool
	}
	return 0.0 // builtin-only — no boost
}

// contextPenalty returns a confidence penalty based on file context.
// Test files, example files, and benchmark files are lower-confidence
// because security patterns there are often intentional (test data, etc).
func contextPenalty(filename string) float64 {
	lower := strings.ToLower(filename)

	// Test files: moderate penalty
	if strings.HasSuffix(lower, "_test.go") || strings.Contains(lower, "_test.") {
		return -0.15
	}
	// Example/sample files: significant penalty
	if strings.Contains(lower, "example") || strings.Contains(lower, "sample") {
		return -0.20
	}
	// Benchmark files: significant penalty
	if strings.HasSuffix(lower, "_bench_test.go") || strings.Contains(lower, "bench") {
		return -0.20
	}
	// Documentation files: skip entirely (shouldn't be scanned, but just in case)
	if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".txt") {
		return -0.30
	}
	return 0.0
}

// snippetConfidence returns a modifier based on the snippet content.
// Hardcoded string literals are higher confidence than variable references.
func snippetConfidence(snippet string) float64 {
	// String literal assignments are high confidence for secrets.
	if strings.Contains(snippet, `:=`) || strings.Contains(snippet, `=`) {
		// Check if right-hand side is a string literal.
		if strings.Contains(snippet, `"`) && !strings.Contains(snippet, "env") && !strings.Contains(snippet, "os.Getenv") {
			return 0.05 // slight boost — literal assignment
		}
	}
	// Variable references (no literal) are lower confidence.
	if strings.Contains(snippet, "var ") || strings.Contains(snippet, "func ") {
		return -0.05
	}
	return 0.0
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Confidence combines severity, real-tool weight, cross-analyzer corroboration,
// and file context into a calibrated 0.05..0.99 score.
func Confidence(sev Severity, analyzers []string) float64 {
	c := baseConfidence(sev) + analyzerWeight(analyzers)
	if len(analyzers) >= 2 {
		c += 0.25 // independent tools agreeing is the strongest cheap signal
	}
	return clampFloat(c, 0.05, 0.99)
}

// ConfidenceWithFile is like Confidence but also applies file-context and snippet modifiers.
func ConfidenceWithFile(sev Severity, analyzers []string, filename string, snippet string) float64 {
	c := Confidence(sev, analyzers)
	c += contextPenalty(filename)
	c += snippetConfidence(snippet)
	return clampFloat(c, 0.05, 0.99)
}

// IsHighConfidence returns true if a finding has confidence above the reportable threshold.
func IsHighConfidence(confidence float64) bool {
	return confidence >= 0.30
}

// ShouldReport returns true if a finding should be included in a report.
// This filters out very low-confidence findings that are likely false positives.
func ShouldReport(f Finding) bool {
	return IsHighConfidence(f.Confidence)
}
