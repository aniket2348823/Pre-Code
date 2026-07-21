package scanner

import "context"

// Input is the code to analyze plus optional hints.
type Input struct {
	Language string // "go", "python", … ("" = analyzer decides or skips)
	Code     string
	Filename string // optional; analyzers synthesize a temp name if empty
}

// Analyzer is one pluggable source of findings.
type Analyzer interface {
	Name() string
	Available() bool
	Analyze(ctx context.Context, in Input) ([]Finding, error)
}
