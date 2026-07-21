package scanner

import (
	"strings"
)

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
