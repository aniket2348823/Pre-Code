package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// BanditAnalyzer runs the Bandit Python security linter.
type BanditAnalyzer struct {
	runner Runner
	exists func() bool
}

func NewBanditAnalyzer(r Runner) *BanditAnalyzer {
	if r == nil {
		r = ExecRunner{}
	}
	return &BanditAnalyzer{runner: r, exists: func() bool { return toolExists("bandit") }}
}

func (b *BanditAnalyzer) Name() string   { return "bandit" }
func (b *BanditAnalyzer) Available() bool { return b.exists() }

type banditOutput struct {
	Results []struct {
		Filename      string `json:"filename"`
		IssueSeverity string `json:"issue_severity"`
		IssueText     string `json:"issue_text"`
		TestID        string `json:"test_id"`
		TestName      string `json:"test_name"`
		LineNumber    int    `json:"line_number"`
		Code          string `json:"code"`
	} `json:"results"`
}

func banditSeverity(s string) Severity {
	switch s {
	case "HIGH":
		return SeverityHigh
	case "MEDIUM":
		return SeverityMedium
	case "LOW":
		return SeverityLow
	default:
		return SeverityInfo
	}
}

func (b *BanditAnalyzer) Analyze(ctx context.Context, in Input) ([]Finding, error) {
	if in.Language != "" && in.Language != "python" {
		return nil, nil // bandit only understands Python
	}
	dir, err := os.MkdirTemp("", "bandit")
	if err != nil {
		return nil, fmt.Errorf("bandit: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	name := in.Filename
	if name == "" {
		name = "snippet.py"
	}
	path := filepath.Join(dir, filepath.Base(name))
	if err := os.WriteFile(path, []byte(in.Code), 0o644); err != nil {
		return nil, fmt.Errorf("bandit: write temp: %w", err)
	}

	stdout, stderr, runErr := b.runner.Run(ctx, "bandit", []string{"-f", "json", "-q", path}, "")

	var out banditOutput
	if jsonErr := json.Unmarshal([]byte(stdout), &out); jsonErr != nil {
		if runErr != nil {
			return nil, fmt.Errorf("bandit: %v: %s", runErr, stderr)
		}
		return nil, fmt.Errorf("bandit: unparseable output: %w", jsonErr)
	}

	findings := make([]Finding, 0, len(out.Results))
	for _, r := range out.Results {
		fn := in.Filename
		if fn == "" {
			fn = "snippet.py"
		}
		findings = append(findings, Finding{
			RuleID:      r.TestID,
			Analyzers:   []string{"bandit"},
			Severity:    banditSeverity(r.IssueSeverity),
			Category:    r.TestName,
			Title:       r.TestName,
			Message:     r.IssueText,
			Filename:    fn,
			Line:        r.LineNumber,
			Snippet:     r.Code,
			Fingerprint: ComputeFingerprint(fn, r.LineNumber, r.Code),
		})
	}
	return findings, nil
}
