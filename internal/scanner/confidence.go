package scanner

// baseConfidence maps severity to a starting confidence.
func baseConfidence(sev Severity) float64 {
	switch sev {
	case SeverityCritical:
		return 0.6
	case SeverityHigh:
		return 0.5
	case SeverityMedium:
		return 0.4
	case SeverityLow:
		return 0.3
	default:
		return 0.2
	}
}

// analyzerWeight adds credibility when a real external tool (not the noisier
// builtin regex) reported the finding.
func analyzerWeight(analyzers []string) float64 {
	for _, a := range analyzers {
		if a == "bandit" || a == "semgrep" {
			return 0.1
		}
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

// Confidence combines severity, real-tool weight, and cross-analyzer
// corroboration into a calibrated 0.05..0.99 score.
func Confidence(sev Severity, analyzers []string) float64 {
	c := baseConfidence(sev) + analyzerWeight(analyzers)
	if len(analyzers) >= 2 {
		c += 0.25 // independent tools agreeing is the strongest cheap signal
	}
	return clampFloat(c, 0.05, 0.99)
}
