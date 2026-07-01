package scanner

import (
	"regexp"
	"strings"
)

// Severity levels for detected vulnerabilities.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Vulnerability represents a detected security issue.
type Vulnerability struct {
	Type        string   `json:"type"`
	Severity    Severity `json:"severity"`
	Description string   `json:"description"`
	Line        int      `json:"line,omitempty"`
	Snippet     string   `json:"snippet,omitempty"`
	Fix         string   `json:"fix,omitempty"`
}

// ScanResult contains the full scan output.
type ScanResult struct {
	Safe            bool            `json:"safe"`
	Vulnerabilities []Vulnerability `json:"vulnerabilities"`
	ScannedBytes    int             `json:"scanned_bytes"`
	TotalIssues     int             `json:"total_issues"`
	CriticalCount   int             `json:"critical_count"`
}

// Scanner scans code for security vulnerabilities.
type Scanner struct {
	rules []Rule
}

// Rule defines a vulnerability detection pattern.
type Rule struct {
	Name        string
	Description string
	Severity    Severity
	Pattern     *regexp.Regexp
	Fix         string
	Category    string
}

// New creates a Scanner with all built-in rules.
func New() *Scanner {
	s := &Scanner{}
	s.registerBuiltinRules()
	return s
}

// Scan analyzes code and returns vulnerabilities found.
func (s *Scanner) Scan(code string) *ScanResult {
	result := &ScanResult{
		Safe:            true,
		Vulnerabilities: []Vulnerability{},
		ScannedBytes:    len(code),
	}

	lines := strings.Split(code, "\n")

	for _, rule := range s.rules {
		for lineNum, line := range lines {
			if rule.Pattern.MatchString(line) {
				vuln := Vulnerability{
					Type:        rule.Name,
					Severity:    rule.Severity,
					Description: rule.Description,
					Line:        lineNum + 1,
					Snippet:     strings.TrimSpace(line),
					Fix:         rule.Fix,
				}
				result.Vulnerabilities = append(result.Vulnerabilities, vuln)
				result.TotalIssues++
				if rule.Severity == SeverityCritical || rule.Severity == SeverityHigh {
					result.Safe = false
				}
				if rule.Severity == SeverityCritical {
					result.CriticalCount++
				}
			}
		}
	}

	return result
}

// registerBuiltinRules adds all built-in vulnerability detection patterns.
func (s *Scanner) registerBuiltinRules() {
	s.rules = append(s.rules,
		// SQL Injection
		Rule{
			Name:        "sql_injection",
			Description: "Potential SQL injection via string concatenation or fmt.Sprintf in query",
			Severity:    SeverityCritical,
			Pattern:     regexp.MustCompile(`(?i)(fmt\.Sprintf|"\s*\+\s*|\$\{).*\b(SELECT|INSERT|UPDATE|DELETE|DROP|EXEC|EXECUTE)\b`),
			Fix:         "Use parameterized queries ($1, $2) instead of string interpolation",
			Category:    "injection",
		},
		Rule{
			Name:        "sql_injection_raw_query",
			Description: "Raw SQL query with variable interpolation",
			Severity:    SeverityCritical,
			Pattern:     regexp.MustCompile(`(?i)(db\.Exec|db\.Query|db\.QueryRow|\.ExecContext|\.QueryContext)\s*\(\s*fmt\.Sprintf`),
			Fix:         "Pass parameters as separate arguments to Exec/Query instead of formatting the query string",
			Category:    "injection",
		},

		// Cross-Site Scripting (XSS)
		Rule{
			Name:        "xss_unsafe_html",
			Description: "Unsafe HTML rendering that may allow XSS",
			Severity:    SeverityHigh,
			Pattern:     regexp.MustCompile(`(template\.HTML|template\.HTMLAttr|html\.Template)\s*\(\s*(?!template)`),
			Fix:         "Use template escaping or validate/sanitize input before rendering as HTML",
			Category:    "xss",
		},
		Rule{
			Name:        "xss_unsafe_js",
			Description: "Potential XSS via JavaScript template literal with user input",
			Severity:    SeverityMedium,
			Pattern:     regexp.MustCompile(`(?i)(innerHTML|outerHTML|document\.write)\s*=\s*[^;]*\+`),
			Fix:         "Use textContent instead of innerHTML, or sanitize input before insertion",
			Category:    "xss",
		},
		Rule{
			Name:        "xss_http_redirect",
			Description: "Open redirect vulnerability — user input in redirect URL",
			Severity:    SeverityHigh,
			Pattern:     regexp.MustCompile(`http\.Redirect\([^,]+,\s*[^,]*r\.URL`),
			Fix:         "Validate redirect URLs against an allowlist before redirecting",
			Category:    "xss",
		},

		// Hardcoded Secrets
		Rule{
			Name:        "hardcoded_password",
			Description: "Hardcoded password or secret in source code",
			Severity:    SeverityCritical,
			Pattern:     regexp.MustCompile(`(?i)(password|passwd|secret|api_key|apikey|api[-_]?secret|token|private[-_]?key)\s*[:=]\s*["'][^"']{8,}["']`),
			Fix:         "Use environment variables or a secrets manager (e.g., HashiCorp Vault)",
			Category:    "secrets",
		},
		Rule{
			Name:        "hardcoded_connection_string",
			Description: "Hardcoded database connection string with embedded credentials",
			Severity:    SeverityCritical,
			Pattern:     regexp.MustCompile(`(?i)(postgres|mysql|mongodb|redis)://[^:]+:[^@]+@`),
			Fix:         "Load connection strings from environment variables or config files excluded from version control",
			Category:    "secrets",
		},
		Rule{
			Name:        "aws_access_key",
			Description: "Potential AWS access key hardcoded in source",
			Severity:    SeverityCritical,
			Pattern:     regexp.MustCompile(`(?i)(AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}`),
			Fix:         "Use AWS IAM roles or environment variables; rotate the exposed key immediately",
			Category:    "secrets",
		},

		// Insecure Cryptography
		Rule{
			Name:        "weak_hash_md5",
			Description: "Use of MD5 hashing which is cryptographically broken",
			Severity:    SeverityHigh,
			Pattern:     regexp.MustCompile(`crypto/md5|md5\.New\(\)|md5\.Sum\(`),
			Fix:         "Use SHA-256 (crypto/sha256) or bcrypt for password hashing",
			Category:    "crypto",
		},
		Rule{
			Name:        "weak_hash_sha1",
			Description: "Use of SHA-1 which is vulnerable to collision attacks",
			Severity:    SeverityMedium,
			Pattern:     regexp.MustCompile(`crypto/sha1|sha1\.New\(\)|sha1\.Sum\(`),
			Fix:         "Use SHA-256 or SHA-3 for new applications",
			Category:    "crypto",
		},
		Rule{
			Name:        "weak_random",
			Description: "Use of math/rand instead of crypto/rand for security-sensitive operations",
			Severity:    SeverityHigh,
			Pattern:     regexp.MustCompile(`math/rand|rand\.Intn\(|rand\.Read\(`),
			Fix:         "Use crypto/rand for tokens, keys, and other security-sensitive random values",
			Category:    "crypto",
		},
		Rule{
			Name:        "insecure_tls",
			Description: "TLS verification disabled — man-in-the-middle vulnerability",
			Severity:    SeverityCritical,
			Pattern:     regexp.MustCompile(`InsecureSkipVerify\s*:\s*true`),
			Fix:         "Never disable TLS verification in production; configure proper CA certificates",
			Category:    "crypto",
		},
		Rule{
			Name:        "weak_jwt_secret",
			Description: "JWT signed with HMAC using a short or hardcoded secret",
			Severity:    SeverityHigh,
			Pattern:     regexp.MustCompile(`jwt\.Sign\([^)]*\)\.SignedString\(\s*\[\s]*byte\s*\(`),
			Fix:         "Use RSA or ECDSA signing keys loaded from a secure key store, with minimum 256-bit keys",
			Category:    "crypto",
		},

		// Path Traversal
		Rule{
			Name:        "path_traversal",
			Description: "Potential path traversal via unsanitized user input in file operations",
			Severity:    SeverityHigh,
			Pattern:     regexp.MustCompile(`(os\.Open|os\.Create|os\.ReadFile|os\.WriteFile|ioutil\.ReadFile|filepath\.Join)\s*\([^)]*(?:req\.|r\.|params\.|input\.)`),
			Fix:         "Validate and sanitize file paths; use filepath.Clean and verify the path stays within allowed directories",
			Category:    "path_traversal",
		},
		Rule{
			Name:        "command_injection",
			Description: "Potential command injection via unsanitized input in exec.Command",
			Severity:    SeverityCritical,
			Pattern:     regexp.MustCompile(`exec\.Command\([^)]*(?:req\.|r\.|params\.|input\.|fmt\.Sprintf)`),
			Fix:         "Use allowlists for commands; never pass user input directly to exec.Command arguments",
			Category:    "injection",
		},

		// SSRF
		Rule{
			Name:        "ssrf_http_get",
			Description: "Potential SSRF — user-controlled URL passed to HTTP client",
			Severity:    SeverityHigh,
			Pattern:     regexp.MustCompile(`http\.(Get|Post|Head|Do)\s*\(\s*(?:req\.|r\.)`),
			Fix:         "Validate URLs against an allowlist; block internal/private IP ranges",
			Category:    "ssrf",
		},

		// Insecure Deserialization
		Rule{
			Name:        "insecure_json_decode",
			Description: "Decoding JSON from untrusted source without size limits",
			Severity:    SeverityMedium,
			Pattern:     regexp.MustCompile(`json\.NewDecoder\((?:req\.|r\.)Body\)\.Decode\(&[^)]+\)(?!.*MaxBytesReader)`),
			Fix:         "Use http.MaxBytesReader to limit request body size before decoding",
			Category:    "deserialization",
		},

		// Race Conditions
		Rule{
			Name:        "race_condition_map",
			Description: "Concurrent map access without synchronization",
			Severity:    SeverityMedium,
			Pattern:     regexp.MustCompile(`(?i)(go\s+func|go\s+\w+\()`),
			Fix:         "Use sync.Mutex or sync.Map for concurrent map access",
			Category:    "race",
		},

		// Information Disclosure
		Rule{
			Name:        "error_info_leak",
			Description: "Internal error details exposed to users",
			Severity:    SeverityMedium,
			Pattern:     regexp.MustCompile(`fmt\.Errorf\("[^"]*%w[^"]*"\s*,\s*err\)`),
			Fix:         "Log internal errors; return generic error messages to users",
			Category:    "info_disclosure",
		},

		// Insecure File Permissions
		Rule{
			Name:        "insecure_file_perms",
			Description: "File created with overly permissive permissions",
			Severity:    SeverityLow,
			Pattern:     regexp.MustCompile(`os\.WriteFile\([^)]*0[67][67][67]`),
			Fix:         "Use restrictive permissions (0600 for secrets, 0644 for config, 0755 for executables only)",
			Category:    "permissions",
		},
	)
}
