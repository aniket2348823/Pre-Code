package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// SkillScanner scans skill packages for security issues before publishing.
type SkillScanner struct {
	maxSize        int64
	bannedPatterns []*regexp.Regexp
}

// ScanResult contains the security scan results.
type ScanResult struct {
	Passed    bool          `json:"passed"`
	Score     float64       `json:"score"`
	Issues    []ScanIssue   `json:"issues"`
	ScannedAt time.Time     `json:"scanned_at"`
	Duration  time.Duration `json:"duration"`
}

// ScanIssue represents a single security finding.
type ScanIssue struct {
	Severity string `json:"severity"` // critical, high, medium, low
	Category string `json:"category"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Fix      string `json:"fix"`
}

// NewSkillScanner creates a scanner with default security rules.
func NewSkillScanner() *SkillScanner {
	return &SkillScanner{
		maxSize: 10 * 1024 * 1024, // 10MB max package size
		bannedPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(os\.exec|exec\.Command|subprocess\.call|system\()`),
			regexp.MustCompile(`(?i)(eval\(|exec\()`),
			regexp.MustCompile(`(?i)(password|secret|api_key|token)\s*[:=]\s*"[^"]{8,}"`),
			regexp.MustCompile(`(?i)(rm\s+-rf|del\s+/[qsy]|format\s+[cC]:)`),
			regexp.MustCompile(`(?i)(curl|wget)\s+.*\|\s*(bash|sh)`),
			regexp.MustCompile(`(?i)(0\.0\.0\.0|127\.0\.0\.1|localhost)`),
			regexp.MustCompile(`(?i)(DROP\s+TABLE|DELETE\s+FROM|TRUNCATE)`),
		},
	}
}

// ScanPackage scans a skill package for security issues.
func (s *SkillScanner) ScanPackage(ctx context.Context, packageData []byte) (*ScanResult, error) {
	start := time.Now()
	result := &ScanResult{
		Passed:    true,
		Score:     1.0,
		ScannedAt: start,
	}

	// Check package size
	if int64(len(packageData)) > s.maxSize {
		result.Passed = false
		result.Score = 0
		result.Issues = append(result.Issues, ScanIssue{
			Severity: "critical",
			Category: "size",
			Message:  fmt.Sprintf("Package size %d exceeds maximum %d bytes", len(packageData), s.maxSize),
			Fix:      "Reduce package size or split into multiple packages",
		})
		result.Duration = time.Since(start)
		return result, nil
	}

	// Extract and scan files
	gr, err := gzip.NewReader(bytes.NewReader(packageData))
	if err != nil {
		return nil, fmt.Errorf("failed to read gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		// Read file content (limit to 1MB per file)
		data, err := io.ReadAll(io.LimitReader(tr, 1024*1024))
		if err != nil {
			continue
		}

		s.scanFileContent(header.Name, data, result)
	}

	result.Duration = time.Since(start)
	if len(result.Issues) > 0 {
		// Clamp score: each issue reduces score by 0.1, minimum 0
		score := 1.0 - float64(len(result.Issues))*0.1
		if score < 0 {
			score = 0
		}
		result.Score = score
		result.Passed = result.Score >= 0.5
	}
	return result, nil
}

// scanFileContent checks a single file for security issues.
func (s *SkillScanner) scanFileContent(filename string, data []byte, result *ScanResult) {
	content := string(data)
	lines := strings.Split(content, "\n")

	for lineNum, line := range lines {
		for _, pattern := range s.bannedPatterns {
			if pattern.MatchString(line) {
				severity := "medium"
				if strings.Contains(strings.ToLower(filename), "manifest") {
					severity = "high"
				}
				result.Issues = append(result.Issues, ScanIssue{
					Severity: severity,
					Category: "security_pattern",
					Message:  fmt.Sprintf("Potentially dangerous pattern detected"),
					File:     filename,
					Line:     lineNum + 1,
					Fix:      "Review and remove or sandbox the flagged code",
				})
			}
		}
	}

	// Check for hardcoded secrets
	secretPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(sk-[a-zA-Z0-9]{20,})`),
		regexp.MustCompile(`(?i)(ghp_[a-zA-Z0-9]{36})`),
		regexp.MustCompile(`(?i)(xox[bpsa]-[a-zA-Z0-9-]+)`),
	}
	for _, pattern := range secretPatterns {
		if pattern.MatchString(content) {
			result.Issues = append(result.Issues, ScanIssue{
				Severity: "critical",
				Category: "secrets",
				Message:  "Potential API key or secret found in package",
				File:     filename,
				Fix:      "Remove secrets and use environment variables",
			})
		}
	}
}
