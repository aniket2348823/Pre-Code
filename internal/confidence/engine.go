// Package confidence implements the calibrated confidence engine: instead of
// binary "secure/not secure", it produces evidence-backed confidence scores
// that account for how many validations passed, how many analyzers corroborate
// each finding, and the severity distribution. This is what makes the output
// trustworthy — every confidence score has a traceable reason.
package confidence

import (
	"math"
	"sort"

	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/util"
)

// Evidence represents a single piece of evidence for or against confidence.
type Evidence struct {
	Source    string  `json:"source"`    // e.g. "schema", "requirements", "compliance", "scan"
	Verdict   string  `json:"verdict"`   // "pass", "fail", "warn"
	Severity  string  `json:"severity"`  // critical, high, medium, low, info
	Detail    string  `json:"detail"`
	Weight    float64 `json:"weight"`    // 0.0–1.0 importance weight
}

// Score is the calibrated confidence output.
type Score struct {
	Confidence float64    `json:"confidence"` // 0.0–1.0
	Grade      string     `json:"grade"`      // A+, A, B+, B, C, D, F
	Passed     int        `json:"passed"`
	Failed     int        `json:"failed"`
	Warned     int        `json:"warned"`
	Evidence   []Evidence `json:"evidence"`
	Reason     string     `json:"reason"` // human-readable summary
}

// Engine computes calibrated confidence scores from evidence.
type Engine struct{}

// NewEngine creates a confidence engine.
func NewEngine() *Engine {
	return &Engine{}
}

// Score computes a calibrated confidence score from a set of evidence items.
func (e *Engine) Score(evidence []Evidence) *Score {
	s := &Score{Evidence: evidence}

	for _, ev := range evidence {
		switch ev.Verdict {
		case "pass":
			s.Passed++
		case "fail":
			s.Failed++
		case "warn":
			s.Warned++
		}
	}

	// Weighted pass rate
	totalWeight := 0.0
	passedWeight := 0.0
	for _, ev := range evidence {
		w := ev.Weight
		if w <= 0 {
			w = 0.5 // default weight
		}
		totalWeight += w
		if ev.Verdict == "pass" {
			passedWeight += w
		}
	}

	if totalWeight > 0 {
		s.Confidence = passedWeight / totalWeight
	} else {
		s.Confidence = 1.0 // no evidence = assume pass
	}

	// Apply penalty for critical failures
	for _, ev := range evidence {
		if ev.Verdict == "fail" && ev.Severity == "critical" {
			s.Confidence *= 0.5 // 50% penalty per critical failure
		}
	}

	// Clamp to [0, 1]
	s.Confidence = math.Max(0, math.Min(1, s.Confidence))

	// Assign grade
	s.Grade = gradeFromScore(s.Confidence)

	// Build reason
	s.Reason = buildReason(s)

	return s
}

// ScoreFromFindings computes confidence from scanner findings.
func (e *Engine) ScoreFromFindings(findings []scanner.Finding) *Score {
	evidence := make([]Evidence, 0, len(findings))
	for _, f := range findings {
		verdict := "pass"
		if f.Severity == scanner.SeverityCritical || f.Severity == scanner.SeverityHigh {
			verdict = "fail"
		} else if f.Severity == scanner.SeverityMedium {
			verdict = "warn"
		}
		evidence = append(evidence, Evidence{
			Source:   "scan",
			Verdict:  verdict,
			Severity: string(f.Severity),
			Detail:   f.Message,
			Weight:   0.8,
		})
	}
	return e.Score(evidence)
}

// gradeFromScore maps a confidence score to a letter grade.
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

// buildReason creates a human-readable summary.
func buildReason(s *Score) string {
	if len(s.Evidence) == 0 {
		return "No evidence collected"
	}
	parts := []string{}
	parts = append(parts, util.Itoa(s.Passed)+" passed")
	if s.Failed > 0 {
		parts = append(parts, util.Itoa(s.Failed)+" failed")
	}
	if s.Warned > 0 {
		parts = append(parts, util.Itoa(s.Warned)+" warnings")
	}

	// Find most critical failure
	var worst Severity
	for _, ev := range s.Evidence {
		if ev.Verdict == "fail" {
			sv := parseSeverity(ev.Severity)
			if sv > worst {
				worst = sv
			}
		}
	}
	if worst >= SeverityCritical {
		parts = append(parts, "critical issues found")
	} else if worst >= SeverityHigh {
		parts = append(parts, "high severity issues found")
	}

	return util.Join(parts, ", ")
}

type Severity int

const (
	SeverityInfo Severity = iota
	SeverityLow
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

func parseSeverity(s string) Severity {
	switch s {
	case "critical":
		return SeverityCritical
	case "high":
		return SeverityHigh
	case "medium":
		return SeverityMedium
	case "low":
		return SeverityLow
	default:
		return SeverityInfo
	}
}



// SortEvidence orders evidence by severity (most severe first) then source.
func SortEvidence(evidence []Evidence) {
	sort.SliceStable(evidence, func(i, j int) bool {
		si := parseSeverity(evidence[i].Severity)
		sj := parseSeverity(evidence[j].Severity)
		if si != sj {
			return si > sj
		}
		return evidence[i].Source < evidence[j].Source
	})
}
