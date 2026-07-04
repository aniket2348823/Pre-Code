package scanner

import (
	"context"
	"sort"
	"strings"
)

// Engine runs a set of analyzers and reconciles their findings.
type Engine struct {
	analyzers      []Analyzer
	minConfidence  float64 // minimum confidence to include in report (default 0.30)
	suppressTestFP bool    // suppress common false positives in test files
}

// EngineOption configures the engine.
type EngineOption func(*Engine)

// WithMinConfidence sets the minimum confidence threshold for reportable findings.
func WithMinConfidence(min float64) EngineOption {
	return func(e *Engine) {
		e.minConfidence = min
	}
}

// WithTestFPSuppression enables suppression of common false positives in test files.
func WithTestFPSuppression() EngineOption {
	return func(e *Engine) {
		e.suppressTestFP = true
	}
}

func NewEngine(analyzers ...Analyzer) *Engine {
	return &Engine{
		analyzers:      analyzers,
		minConfidence:  0.30,
		suppressTestFP: true,
	}
}

// DefaultEngine wires the builtin regex analyzer plus real-tool adapters using
// the real command runner. Adapters self-skip when their tool is absent.
func DefaultEngine() *Engine {
	return NewEngine(NewBuiltinAnalyzer(), NewBanditAnalyzer(nil), NewSemgrepAnalyzer(nil))
}

// Run analyzes the input with every available analyzer and returns a merged,
// scored report. Unavailable analyzers are skipped; erroring ones are recorded
// without aborting the scan.
func (e *Engine) Run(ctx context.Context, in Input) *Report {
	rep := &Report{
		AnalyzersSkipped: map[string]string{},
		AnalyzerErrors:   map[string]string{},
	}
	var raw []Finding
	for _, a := range e.analyzers {
		if !a.Available() {
			rep.AnalyzersSkipped[a.Name()] = a.Name() + " not available"
			continue
		}
		rep.AnalyzersRun = append(rep.AnalyzersRun, a.Name())
		fs, err := a.Analyze(ctx, in)
		if err != nil {
			rep.AnalyzerErrors[a.Name()] = err.Error()
			continue
		}
		raw = append(raw, fs...)
	}
	rep.Findings = e.mergeScoreAndFilter(raw, in.Filename)
	return rep
}

// mergeScoreAndFilter dedupes findings by fingerprint, computes confidence
// with file context, and filters out low-confidence false positives.
func (e *Engine) mergeScoreAndFilter(raw []Finding, filename string) []Finding {
	byFP := map[string]*Finding{}
	order := []string{}
	for _, f := range raw {
		if f.Fingerprint == "" {
			f.Fingerprint = ComputeFingerprint(f.Filename, f.Line, f.Snippet, f.RuleID)
		}
		if ex, ok := byFP[f.Fingerprint]; ok {
			ex.Analyzers = unionSorted(ex.Analyzers, f.Analyzers)
			if SeverityRank(f.Severity) > SeverityRank(ex.Severity) {
				ex.Severity = f.Severity
			}
			if ex.Fix == "" {
				ex.Fix = f.Fix
			}
			if ex.Message == "" {
				ex.Message = f.Message
			}
		} else {
			cp := f
			byFP[cp.Fingerprint] = &cp
			order = append(order, cp.Fingerprint)
		}
	}

	// Compute confidence with file context and filter.
	out := make([]Finding, 0, len(order))
	for _, fp := range order {
		f := byFP[fp]
		f.Confidence = ConfidenceWithFile(f.Severity, f.Analyzers, f.Filename, f.Snippet)

		// Apply suppression rules.
		if e.suppressTestFP && isTestFile(f.Filename) {
			f = suppressTestFP(f)
			if f == nil {
				continue
			}
		}

		// Filter by minimum confidence.
		if f.Confidence >= e.minConfidence {
			out = append(out, *f)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if SeverityRank(out[i].Severity) != SeverityRank(out[j].Severity) {
			return SeverityRank(out[i].Severity) > SeverityRank(out[j].Severity)
		}
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// suppressTestFP suppresses known false positives that commonly appear in test files.
// Returns nil if the finding should be dropped entirely.
func suppressTestFP(f *Finding) *Finding {
	// In test files, hardcoded strings are almost always test data, not real secrets.
	if f.Category == "secrets" && f.Severity >= SeverityCritical {
		// Only suppress if single-tool (no corroboration from real tools).
		if len(f.Analyzers) == 1 && f.Analyzers[0] == "builtin" {
			f.Severity = SeverityInfo
			f.Confidence = clampFloat(f.Confidence-0.2, 0.05, 0.99)
		}
	}
	// Weak crypto in test files is usually intentional (test fixtures).
	if f.Category == "crypto" && isTestFile(f.Filename) {
		f.Confidence = clampFloat(f.Confidence-0.15, 0.05, 0.99)
	}
	// Injection findings in test files are often test fixtures (e.g. SQL strings
	// used to verify query builders). Downgrade severity for single-tool findings.
	if f.Category == "injection" && isTestFile(f.Filename) {
		if len(f.Analyzers) == 1 && f.Analyzers[0] == "builtin" {
			f.Severity = SeverityLow
			f.Confidence = clampFloat(f.Confidence-0.2, 0.05, 0.99)
		}
	}
	return f
}

// unionSorted merges two analyzer-name slices into a sorted, deduped slice.
func unionSorted(a, b []string) []string {
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, x := range b {
		set[x] = true
	}
	out := make([]string, 0, len(set))
	for x := range set {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

// HasHighConfidenceFindings returns true if the report contains actionable findings.
func HasHighConfidenceFindings(rep *Report) bool {
	for _, f := range rep.Findings {
		if f.Confidence >= 0.50 && !strings.Contains(f.Filename, "_test.go") {
			return true
		}
	}
	return false
}
