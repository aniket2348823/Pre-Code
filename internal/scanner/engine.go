package scanner

import (
	"context"
	"sort"
)

// Engine runs a set of analyzers and reconciles their findings.
type Engine struct {
	analyzers []Analyzer
}

func NewEngine(analyzers ...Analyzer) *Engine {
	return &Engine{analyzers: analyzers}
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
	rep.Findings = mergeAndScore(raw)
	return rep
}

// mergeAndScore dedupes findings by fingerprint (union of analyzers, highest
// severity, first actionable fix), computes confidence, and sorts by severity
// then line.
func mergeAndScore(raw []Finding) []Finding {
	byFP := map[string]*Finding{}
	order := []string{}
	for _, f := range raw {
		if f.Fingerprint == "" {
			f.Fingerprint = ComputeFingerprint(f.Filename, f.Line, f.Snippet)
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
	out := make([]Finding, 0, len(order))
	for _, fp := range order {
		f := byFP[fp]
		f.Confidence = Confidence(f.Severity, f.Analyzers)
		out = append(out, *f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if SeverityRank(out[i].Severity) != SeverityRank(out[j].Severity) {
			return SeverityRank(out[i].Severity) > SeverityRank(out[j].Severity)
		}
		return out[i].Line < out[j].Line
	})
	return out
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
