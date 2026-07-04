package scanner

import (
	"context"
	"regexp"
	"strings"
)

type builtinRule struct {
	name        string
	description string
	severity    Severity
	pattern     *regexp.Regexp
	fix         string
	category    string
}

// BuiltinAnalyzer runs the built-in regex rules. Always available; kept for
// org-specific patterns external tools cannot express.
type BuiltinAnalyzer struct {
	rules []builtinRule
}

func NewBuiltinAnalyzer() *BuiltinAnalyzer {
	return &BuiltinAnalyzer{rules: builtinRules()}
}

func (b *BuiltinAnalyzer) Name() string   { return "builtin" }
func (b *BuiltinAnalyzer) Available() bool { return true }

func (b *BuiltinAnalyzer) Analyze(ctx context.Context, in Input) ([]Finding, error) {
	filename := in.Filename
	if filename == "" {
		filename = "input"
	}
	var out []Finding
	lines := strings.Split(in.Code, "\n")
	for _, r := range b.rules {
		for i, line := range lines {
			if r.pattern.MatchString(line) {
				snip := strings.TrimSpace(line)
				out = append(out, Finding{
					RuleID:      r.name,
					Analyzers:   []string{"builtin"},
					Severity:    r.severity,
					Category:    r.category,
					Title:       r.name,
					Message:     r.description,
					Filename:    filename,
					Line:        i + 1,
					Snippet:     snip,
					Fix:         r.fix,
					Fingerprint: ComputeFingerprint(filename, i+1, snip),
				})
			}
		}
	}
	return out, nil
}

func builtinRules() []builtinRule {
	return []builtinRule{
		{name: "sql_injection", description: "Potential SQL injection via string concatenation or fmt.Sprintf in query", severity: SeverityCritical, pattern: regexp.MustCompile(`(?i)(fmt\.Sprintf|"\s*\+\s*|\$\{).*\b(SELECT|INSERT|UPDATE|DELETE|DROP|EXEC|EXECUTE)\b`), fix: "Use parameterized queries ($1, $2) instead of string interpolation", category: "injection"},
		{name: "sql_injection_raw_query", description: "Raw SQL query with variable interpolation", severity: SeverityCritical, pattern: regexp.MustCompile(`(?i)(db\.Exec|db\.Query|db\.QueryRow|\.ExecContext|\.QueryContext)\s*\(\s*fmt\.Sprintf`), fix: "Pass parameters as separate arguments to Exec/Query instead of formatting the query string", category: "injection"},
		{name: "xss_unsafe_html", description: "Unsafe HTML rendering that may allow XSS", severity: SeverityHigh, pattern: regexp.MustCompile(`(template\.HTML|template\.HTMLAttr|html\.Template)\s*\(\s*[^t]`), fix: "Use template escaping or validate/sanitize input before rendering as HTML", category: "xss"},
		{name: "xss_unsafe_js", description: "Potential XSS via JavaScript template literal with user input", severity: SeverityMedium, pattern: regexp.MustCompile(`(?i)(innerHTML|outerHTML|document\.write)\s*=\s*[^;]*\+`), fix: "Use textContent instead of innerHTML, or sanitize input before insertion", category: "xss"},
		{name: "xss_http_redirect", description: "Open redirect vulnerability — user input in redirect URL", severity: SeverityHigh, pattern: regexp.MustCompile(`http\.Redirect\([^,]+,\s*[^,]*r\.URL`), fix: "Validate redirect URLs against an allowlist before redirecting", category: "xss"},
		{name: "hardcoded_password", description: "Hardcoded password or secret in source code", severity: SeverityCritical, pattern: regexp.MustCompile(`(?i)(password|passwd|secret|api_key|apikey|api[-_]?secret|token|private[-_]?key)\s*[:=]+\s*["'][^"']{8,}["']`), fix: "Use environment variables or a secrets manager (e.g., HashiCorp Vault)", category: "secrets"},
		{name: "hardcoded_connection_string", description: "Hardcoded database connection string with embedded credentials", severity: SeverityCritical, pattern: regexp.MustCompile(`(?i)(postgres|mysql|mongodb|redis)://[^:]+:[^@]+@`), fix: "Load connection strings from environment variables or config files excluded from version control", category: "secrets"},
		{name: "aws_access_key", description: "Potential AWS access key hardcoded in source", severity: SeverityCritical, pattern: regexp.MustCompile(`(?i)(AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}`), fix: "Use AWS IAM roles or environment variables; rotate the exposed key immediately", category: "secrets"},
		{name: "weak_hash_md5", description: "Use of MD5 hashing which is cryptographically broken", severity: SeverityHigh, pattern: regexp.MustCompile(`crypto/md5|md5\.New\(\)|md5\.Sum\(`), fix: "Use SHA-256 (crypto/sha256) or bcrypt for password hashing", category: "crypto"},
		{name: "weak_hash_sha1", description: "Use of SHA-1 which is vulnerable to collision attacks", severity: SeverityMedium, pattern: regexp.MustCompile(`crypto/sha1|sha1\.New\(\)|sha1\.Sum\(`), fix: "Use SHA-256 or SHA-3 for new applications", category: "crypto"},
		{name: "weak_random", description: "Use of math/rand instead of crypto/rand for security-sensitive operations", severity: SeverityHigh, pattern: regexp.MustCompile(`math/rand|rand\.Intn\(|rand\.Read\(`), fix: "Use crypto/rand for tokens, keys, and other security-sensitive random values", category: "crypto"},
		{name: "insecure_tls", description: "TLS verification disabled — man-in-the-middle vulnerability", severity: SeverityCritical, pattern: regexp.MustCompile(`InsecureSkipVerify\s*:\s*true`), fix: "Never disable TLS verification in production; configure proper CA certificates", category: "crypto"},
		{name: "weak_jwt_secret", description: "JWT signed with HMAC using a short or hardcoded secret", severity: SeverityHigh, pattern: regexp.MustCompile(`jwt\.Sign\([^)]*\)\.SignedString\(\s*\[\s]*byte\s*\(`), fix: "Use RSA or ECDSA signing keys loaded from a secure key store, with minimum 256-bit keys", category: "crypto"},
		{name: "path_traversal", description: "Potential path traversal via unsanitized user input in file operations", severity: SeverityHigh, pattern: regexp.MustCompile(`(os\.Open|os\.Create|os\.ReadFile|os\.WriteFile|ioutil\.ReadFile|filepath\.Join)\s*\([^)]*(?:req\.|r\.|params\.|input\.)`), fix: "Validate and sanitize file paths; use filepath.Clean and verify the path stays within allowed directories", category: "path_traversal"},
		{name: "command_injection", description: "Potential command injection via unsanitized input in exec.Command", severity: SeverityCritical, pattern: regexp.MustCompile(`exec\.Command\([^)]*(?:req\.|r\.|params\.|input\.|fmt\.Sprintf)`), fix: "Use allowlists for commands; never pass user input directly to exec.Command arguments", category: "injection"},
		{name: "ssrf_http_get", description: "Potential SSRF — user-controlled URL passed to HTTP client", severity: SeverityHigh, pattern: regexp.MustCompile(`http\.(Get|Post|Head|Do)\s*\(\s*(?:req\.|r\.)`), fix: "Validate URLs against an allowlist; block internal/private IP ranges", category: "ssrf"},
		{name: "insecure_json_decode", description: "Decoding JSON from untrusted source without size limits", severity: SeverityMedium, pattern: regexp.MustCompile(`json\.NewDecoder\((?:req\.|r\.)Body\)\.Decode\(&[^)]+\)`), fix: "Use http.MaxBytesReader to limit request body size before decoding", category: "deserialization"},
		{name: "race_condition_map", description: "Concurrent map access without synchronization", severity: SeverityMedium, pattern: regexp.MustCompile(`(?i)(go\s+func|go\s+\w+\()`), fix: "Use sync.Mutex or sync.Map for concurrent map access", category: "race"},
		{name: "error_info_leak", description: "Internal error details exposed to users", severity: SeverityMedium, pattern: regexp.MustCompile(`fmt\.Errorf\("[^"]*%w[^"]*"\s*,\s*err\)`), fix: "Log internal errors; return generic error messages to users", category: "info_disclosure"},
		{name: "insecure_file_perms", description: "File created with overly permissive permissions", severity: SeverityLow, pattern: regexp.MustCompile(`os\.WriteFile\([^)]*0[67][67][67]`), fix: "Use restrictive permissions (0600 for secrets, 0644 for config, 0755 for executables only)", category: "permissions"},
	}
}
