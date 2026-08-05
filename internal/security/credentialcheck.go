package security

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
)

// DetectionPattern defines a regex pattern and its label for credential matching.
type DetectionPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

// DefaultDetectionPatterns returns built-in patterns for common credential formats.
func DefaultDetectionPatterns() []DetectionPattern {
	return []DetectionPattern{
		{
			Name:    "aws_access_key",
			Pattern: regexp.MustCompile(`(?i)(?:aws[_\-]?access[_\-]?key[_\-]?(?:id|Id)?|(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16})`),
		},
		{
			Name:    "aws_secret_key",
			Pattern: regexp.MustCompile(`(?i)(?:aws[_\-]?secret[_\-]?access[_\-]?key|secret[_\-]?key)\s*[:=]\s*["']?([A-Za-z0-9/+=]{40})["']?`),
		},
		{
			Name:    "github_token",
			Pattern: regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{36,255}`),
		},
		{
			Name:    "github_fine_grained_pat",
			Pattern: regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82,255}`),
		},
		{
			Name:    "slack_token",
			Pattern: regexp.MustCompile(`xox[bporas]-[0-9]{10,13}-[0-9]{10,13}-[a-zA-Z0-9]{24,34}`),
		},
		{
			Name:    "google_api_key",
			Pattern: regexp.MustCompile(string([]byte{'A', 'I', 'z', 'a'}) + `[0-9A-Za-z\-_]{35}`),
		},
		{
			Name:    "stripe_key",
			Pattern: regexp.MustCompile(`(?:sk|pk)_(?:live|test)_[0-9a-zA-Z]{24,99}`),
		},
		{
			Name:    "private_key_block",
			Pattern: regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA )?PRIVATE KEY-----`),
		},
		{
			Name:    "jwt_token",
			Pattern: regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+`),
		},
		{
			Name:    "password_field",
			Pattern: regexp.MustCompile(`(?i)(?:password|passwd|pwd)\s*[:=]\s*["']([^"']{8,})["']`),
		},
		{
			Name:    "email_address",
			Pattern: regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
		},
		{
			Name:    "generic_secret",
			Pattern: regexp.MustCompile(`(?i)(?:api[_\-]?key|secret[_\-]?key|auth[_\-]?token|access[_\-]?token)\s*[:=]\s*["']([A-Za-z0-9\-_\.]{20,})["']`),
		},
	}
}

// RedactionRule defines how a detected pattern should be redacted.
type RedactionRule struct {
	PatternName string
	Replacement string
	Enabled     bool
}

// DefaultRedactionRules returns the default set of redaction rules.
func DefaultRedactionRules() []RedactionRule {
	return []RedactionRule{
		{PatternName: "aws_access_key", Replacement: "[REDACTED_AWS_KEY]", Enabled: true},
		{PatternName: "aws_secret_key", Replacement: "[REDACTED_AWS_SECRET]", Enabled: true},
		{PatternName: "github_token", Replacement: "[REDACTED_GITHUB_TOKEN]", Enabled: true},
		{PatternName: "github_fine_grained_pat", Replacement: "[REDACTED_GITHUB_TOKEN]", Enabled: true},
		{PatternName: "slack_token", Replacement: "[REDACTED_SLACK_TOKEN]", Enabled: true},
		{PatternName: "google_api_key", Replacement: "[REDACTED_GOOGLE_KEY]", Enabled: true},
		{PatternName: "stripe_key", Replacement: "[REDACTED_STRIPE_KEY]", Enabled: true},
		{PatternName: "private_key_block", Replacement: "[REDACTED_PRIVATE_KEY]", Enabled: true},
		{PatternName: "jwt_token", Replacement: "[REDACTED_JWT]", Enabled: true},
		{PatternName: "password_field", Replacement: "password: [REDACTED]", Enabled: true},
		{PatternName: "email_address", Replacement: "[REDACTED_EMAIL]", Enabled: false},
		{PatternName: "generic_secret", Replacement: "[REDACTED_SECRET]", Enabled: true},
	}
}

// LeakEvent represents a detected credential leak.
type LeakEvent struct {
	PatternName string
	LineNumber  int
	Context     string // surrounding text, secrets stripped
}

// CredentialScanner scans text for credential patterns.
type CredentialScanner struct {
	patterns []DetectionPattern
	rules    map[string]RedactionRule
}

// NewCredentialScanner creates a scanner with default patterns and rules.
func NewCredentialScanner() *CredentialScanner {
	rules := make(map[string]RedactionRule)
	for _, r := range DefaultRedactionRules() {
		rules[r.PatternName] = r
	}
	return &CredentialScanner{
		patterns: DefaultDetectionPatterns(),
		rules:    rules,
	}
}

// SetRedactionRule overrides or adds a redaction rule.
func (s *CredentialScanner) SetRedactionRule(rule RedactionRule) {
	s.rules[rule.PatternName] = rule
}

// ScanResult holds the output of a scan.
type ScanResult struct {
	Leaks        []LeakEvent
	RedactedBody string
}

// ScanBytes scans a byte slice for credential leaks, returns leaks and redacted content.
func (s *CredentialScanner) ScanBytes(data []byte) ScanResult {
	text := string(data)
	lines := strings.Split(text, "\n")
	var leaks []LeakEvent
	var buf bytes.Buffer

	for i, line := range lines {
		scanned := false
		for _, p := range s.patterns {
			if p.Pattern.MatchString(line) {
				rule, ok := s.rules[p.Name]
				if ok && !rule.Enabled {
					continue
				}
				replacement := "[REDACTED]"
				if ok {
					replacement = rule.Replacement
				}
				redacted := p.Pattern.ReplaceAllString(line, replacement)
				if redacted != line {
					leaks = append(leaks, LeakEvent{
						PatternName: p.Name,
						LineNumber:  i + 1,
						Context:     truncate(cleanSecrets(line), 120),
					})
					line = redacted
					scanned = true
				}
			}
		}
		buf.WriteString(line)
		if i < len(lines)-1 {
			buf.WriteByte('\n')
		}
		_ = scanned
	}

	return ScanResult{
		Leaks:        leaks,
		RedactedBody: buf.String(),
	}
}

// ScanString is a convenience wrapper over ScanBytes.
func (s *CredentialScanner) ScanString(text string) ScanResult {
	return s.ScanBytes([]byte(text))
}

// cleanSecrets strips actual secret values from a line for safe logging.
func cleanSecrets(line string) string {
	// Replace anything after : or = that looks like a secret.
	line = regexp.MustCompile(`(?i)(password|secret|key|token)\s*[:=]\s*\S+`).ReplaceAllString(line, `$1=[REDACTED]`)
	return line
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// redactorWriter wraps an http.ResponseWriter to scan the body before sending.
type redactorWriter struct {
	scanner    *CredentialScanner
	underlying http.ResponseWriter
	buf        bytes.Buffer
	wrote      bool
	statusCode int
	logFunc    func(LeakEvent)
}

// newRedactorWriter creates a wrapping response writer that intercepts writes.
func newRedactorWriter(w http.ResponseWriter, scanner *CredentialScanner, logFunc func(LeakEvent)) *redactorWriter {
	return &redactorWriter{
		scanner:    scanner,
		underlying: w,
		statusCode: http.StatusOK,
		logFunc:    logFunc,
	}
}

func (rw *redactorWriter) Header() http.Header {
	return rw.underlying.Header()
}

func (rw *redactorWriter) Write(b []byte) (int, error) {
	if rw.wrote {
		// After the initial flush, redact each subsequent chunk individually
		// so header-first or streamed responses never bypass redaction.
		result := rw.scanner.ScanBytes(b)
		for _, leak := range result.Leaks {
			if rw.logFunc != nil {
				rw.logFunc(leak)
			}
		}
		return rw.underlying.Write([]byte(result.RedactedBody))
	}
	rw.buf.Write(b)
	return len(b), nil
}

func (rw *redactorWriter) WriteHeader(code int) {
	rw.statusCode = code
	if !rw.wrote {
		rw.flush()
	}
	rw.underlying.WriteHeader(code)
	rw.wrote = true
}

func (rw *redactorWriter) flush() {
	if rw.buf.Len() == 0 {
		return
	}
	result := rw.scanner.ScanBytes(rw.buf.Bytes())
	for _, leak := range result.Leaks {
		if rw.logFunc != nil {
			rw.logFunc(leak)
		}
	}
	rw.underlying.Write([]byte(result.RedactedBody))
	rw.buf.Reset()
	rw.wrote = true
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (rw *redactorWriter) Unwrap() http.ResponseWriter {
	return rw.underlying
}

// CredentialLeakMiddleware returns middleware that scans response bodies for
// credential leaks and redacts them before sending to the client.
func CredentialLeakMiddleware(scanner *CredentialScanner) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := newRedactorWriter(w, scanner, func(leak LeakEvent) {
				slog.Warn("credential leak detected and redacted",
					"pattern", leak.PatternName,
					"line", leak.LineNumber,
					"path", r.URL.Path,
					"method", r.Method,
				)
			})
			next.ServeHTTP(rw, r)
			if !rw.wrote {
				rw.flush()
			}
		})
	}
}

// ScanResponse is a helper to scan an http.Response body for credential leaks.
// It reads, scans, and replaces the body.
func ScanResponse(resp *http.Response, scanner *CredentialScanner) ([]LeakEvent, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	result := scanner.ScanBytes(body)
	resp.Body = io.NopCloser(bytes.NewBufferString(result.RedactedBody))
	return result.Leaks, nil
}

// ScanJSONResponse is a helper that scans a JSON-encoded http.Response body,
// redacting any detected credentials while preserving valid JSON structure.
func ScanJSONResponse(resp *http.Response, scanner *CredentialScanner) ([]LeakEvent, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	// First pass: scan raw bytes.
	rawResult := scanner.ScanBytes(body)

	// Second pass: try to parse as JSON to redact string values.
	var jsonData interface{}
	if json.Unmarshal(body, &jsonData) == nil {
		redacted := scanJSONValue(jsonData, scanner)
		redactedBytes, err := json.Marshal(redacted)
		if err == nil {
			resp.Body = io.NopCloser(bytes.NewBuffer(redactedBytes))
			return rawResult.Leaks, nil
		}
	}

	resp.Body = io.NopCloser(bytes.NewBufferString(rawResult.RedactedBody))
	return rawResult.Leaks, nil
}

func scanJSONValue(v interface{}, scanner *CredentialScanner) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, child := range val {
			val[k] = scanJSONValue(child, scanner)
		}
	case []interface{}:
		for i, child := range val {
			val[i] = scanJSONValue(child, scanner)
		}
	case string:
		result := scanner.ScanString(val)
		return result.RedactedBody
	}
	return v
}
