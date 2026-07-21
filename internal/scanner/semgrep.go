package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SemgrepAnalyzer runs Semgrep. Multi-language; requires a rules config at
// runtime (deferred tuning). Not available natively on Windows (Docker/CI).
type SemgrepAnalyzer struct {
	runner Runner
	exists func() bool
}

func NewSemgrepAnalyzer(r Runner) *SemgrepAnalyzer {
	if r == nil {
		r = ExecRunner{}
	}
	return &SemgrepAnalyzer{runner: r, exists: func() bool { return toolExists("semgrep") }}
}

func (s *SemgrepAnalyzer) Name() string   { return "semgrep" }
func (s *SemgrepAnalyzer) Available() bool { return s.exists() }

type semgrepOutput struct {
	Results []struct {
		CheckID string `json:"check_id"`
		Path    string `json:"path"`
		Start   struct {
			Line int `json:"line"`
		} `json:"start"`
		Extra struct {
			Message  string `json:"message"`
			Severity string `json:"severity"`
			Lines    string `json:"lines"`
			Metadata struct {
				Category string `json:"category"`
			} `json:"metadata"`
		} `json:"extra"`
	} `json:"results"`
}

func semgrepSeverity(s string) Severity {
	switch s {
	case "ERROR":
		return SeverityHigh
	case "WARNING":
		return SeverityMedium
	case "INFO":
		return SeverityLow
	default:
		return SeverityInfo
	}
}

func (s *SemgrepAnalyzer) Analyze(ctx context.Context, in Input) ([]Finding, error) {
	dir, err := os.MkdirTemp("", "semgrep")
	if err != nil {
		return nil, fmt.Errorf("semgrep: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	name := in.Filename
	if name == "" {
		name = "snippet.txt"
	}
	path := filepath.Join(dir, filepath.Base(name))
	if err := os.WriteFile(path, []byte(in.Code), 0o644); err != nil {
		return nil, fmt.Errorf("semgrep: write temp: %w", err)
	}

	stdout, stderr, runErr := s.runner.Run(ctx, "semgrep", []string{"--json", "--config", "auto", "-q", path}, "")

	var out semgrepOutput
	if jsonErr := json.Unmarshal([]byte(stdout), &out); jsonErr != nil {
		if runErr != nil {
			return nil, fmt.Errorf("semgrep: %v: %s", runErr, stderr)
		}
		return nil, fmt.Errorf("semgrep: unparseable output: %w", jsonErr)
	}

	fn := in.Filename
	if fn == "" {
		fn = "snippet.txt"
	}
	findings := make([]Finding, 0, len(out.Results))
	for _, r := range out.Results {
		findings = append(findings, Finding{
			RuleID:      r.CheckID,
			Analyzers:   []string{"semgrep"},
			Severity:    semgrepSeverity(r.Extra.Severity),
			Category:    r.Extra.Metadata.Category,
			Title:       r.CheckID,
			Message:     r.Extra.Message,
			Filename:    fn,
			Line:        r.Start.Line,
			Snippet:     r.Extra.Lines,
			Fingerprint: ComputeFingerprint(fn, r.Start.Line, r.Extra.Lines, r.CheckID),
		})
	}
	return findings, nil
}
