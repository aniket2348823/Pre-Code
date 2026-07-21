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
	// excludeFilenames, if non-empty, suppresses the rule for files matching any of these patterns.
	excludeFilenames []string
	// requireContext, if non-empty, means the rule only fires when ANY of these substrings appear in the line.
	requireContext []string
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

// isTestFile returns true if the filename looks like a Go test file.
func isTestFile(filename string) bool {
	return strings.HasSuffix(filename, "_test.go") ||
		strings.Contains(filename, "_test.") ||
		strings.Contains(filename, "/test/") ||
		strings.Contains(filename, "/tests/")
}

// isTestDataFile returns true if the file is in a testdata directory.
// Testdata files are fixture/data files that should have all findings
// fully suppressed (not just downgraded) since they are intentional test inputs.
func isTestDataFile(filename string) bool {
	return strings.Contains(filename, "/testdata/") ||
		strings.HasPrefix(filename, "testdata/") ||
		strings.Contains(filename, "\\testdata\\")
}

// isGeneratedFile returns true if the file appears to be generated.
func isGeneratedFile(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.Contains(lower, "generated") ||
		strings.Contains(lower, "vendor/") ||
		strings.Contains(lower, ".pb.go") ||
		strings.Contains(lower, "_generated") ||
		strings.Contains(lower, "mock_") ||
		strings.Contains(lower, "stub_")
}

func (b *BuiltinAnalyzer) Analyze(ctx context.Context, in Input) ([]Finding, error) {
	filename := in.Filename
	if filename == "" {
		filename = "input"
	}

	// Suppress ALL findings in generated/vendor files — these are never real vulnerabilities.
	if isGeneratedFile(filename) {
		return nil, nil
	}

	var out []Finding
	lines := strings.Split(in.Code, "\n")
	for _, r := range b.rules {
		// Suppress low-severity rules in test files (use rank, not string comparison).
		if isTestFile(filename) && SeverityRank(r.severity) <= SeverityRank(SeverityLow) {
			continue
		}

		for i, line := range lines {
			if r.pattern.MatchString(line) {
				// If rule has requireContext, at least one context substring must appear.
				if len(r.requireContext) > 0 {
					found := false
					for _, ctx := range r.requireContext {
						if strings.Contains(line, ctx) {
							found = true
							break
						}
					}
					if !found {
						continue
					}
				}

				// Check filename exclusions.
				excluded := false
				for _, pattern := range r.excludeFilenames {
					if strings.Contains(filename, pattern) {
						excluded = true
						break
					}
				}
				if excluded {
					continue
				}

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
					Fingerprint: ComputeFingerprint(filename, i+1, snip, r.name),
				})
			}
		}
	}
	return out, nil
}

func builtinRules() []builtinRule {
	return []builtinRule{
		// ════════════════════════════════════════════════════════════════
		// INJECTION (CWE-89, CWE-78, CWE-79)
		// ════════════════════════════════════════════════════════════════
		{
			name:        "sql_injection",
			description: "Potential SQL injection via string concatenation or fmt.Sprintf in query",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`(?i)(fmt\.Sprintf|"?\s*\+\s*|\$\{).*(?:\bSELECT\b|\bINSERT\b|\bUPDATE\b|\bDELETE\b|\bDROP\b|\bEXEC\b|\bEXECUTE\b)`),
			fix:         "Use parameterized queries ($1, $2) instead of string interpolation",
			category:    "injection",
		},
		{
			name:        "sql_injection_raw_query",
			description: "Raw SQL query with variable interpolation",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`(?i)(db\.Exec|db\.Query|db\.QueryRow|\.ExecContext|\.QueryContext)\s*\(\s*fmt\.Sprintf`),
			fix:         "Pass parameters as separate arguments to Exec/Query instead of formatting the query string",
			category:    "injection",
		},
		{
			name:        "command_injection",
			description: "Potential command injection via unsanitized input in exec.Command",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`exec\.Command\([^)]*(?:req\.|r\.|params\.|input\.|fmt\.Sprintf)`),
			fix:         "Use allowlists for commands; never pass user input directly to exec.Command arguments",
			category:    "injection",
			requireContext: []string{"req.", "r.", "params.", "input.", "fmt.Sprintf"},
		},
		{
			name:        "xss_unsafe_html",
			description: "Unsafe HTML rendering that may allow XSS",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`template\.HTML\s*\(\s*[a-z]`),
			fix:         "Use template escaping or validate/sanitize input before rendering as HTML",
			category:    "xss",
		},
		{
			name:        "xss_unsafe_js",
			description: "Potential XSS via JavaScript innerHTML/outerHTML with user input",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`(?i)(innerHTML|outerHTML|document\.write)\s*=\s*[^;]*\+`),
			fix:         "Use textContent instead of innerHTML, or sanitize input before insertion",
			category:    "xss",
		},
		{
			name:        "xss_http_redirect",
			description: "Open redirect vulnerability — user input in redirect URL",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`http\.Redirect\([^,]+,\s*[^,]+,\s*[^,]*r\.URL`),
			fix:         "Validate redirect URLs against an allowlist before redirecting",
			category:    "xss",
			requireContext: []string{"r.URL"},
		},
		{
			name:        "template_injection",
			description: "Potential template injection — user input in template parsing",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`template\.(New|Must)\([^)]*(?:req\.|r\.|input\.)`),
			fix:         "Never pass user input directly to template constructors; use predefined templates",
			category:    "injection",
			requireContext: []string{"req.", "r.", "input."},
		},
		{
			name:        "log_injection",
			description: "Potential log injection via unsanitized user input",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`(?:log\.|slog\.|fmt\.Print|fmt\.Fprint)\w*\([^)]*(?:req\.|r\.|input\.|user\.)`),
			fix:         "Sanitize user input before logging; use structured logging with key-value pairs",
			category:    "injection",
			requireContext: []string{"req.", "r.", "input.", "user."},
		},

		// ════════════════════════════════════════════════════════════════
		// SECRETS (CWE-798, CWE-259, CWE-321)
		// ════════════════════════════════════════════════════════════════
		{
			name:        "hardcoded_password",
			description: "Hardcoded password or secret in source code",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`(?i)(password|passwd|secret|api_key|apikey|api[-_]?secret|private[-_]?key)\s*[:=]+\s*"[^"]{8,}"`),
			fix:         "Use environment variables or a secrets manager (e.g., HashiCorp Vault)",
			category:    "secrets",
			excludeFilenames: []string{"example", "sample", "mock_", "stub_"},
		},
		{
			name:        "hardcoded_connection_string",
			description: "Hardcoded database connection string with embedded credentials",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`(?i)(postgres|mysql|mongodb|redis)://[^:]+:[^@]+@`),
			fix:         "Load connection strings from environment variables or config files excluded from version control",
			category:    "secrets",
		},
		{
			name:        "aws_access_key",
			description: "Potential AWS access key hardcoded in source",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`"(AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}"`),
			fix:         "Use AWS IAM roles or environment variables; rotate the exposed key immediately",
			category:    "secrets",
		},
		{
			name:        "aws_secret_key",
			description: "Potential AWS secret access key hardcoded in source",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`(?i)(aws_secret_access_key|aws_secret)\s*[:=]+\s*"[A-Za-z0-9/+=]{40}"`),
			fix:         "Use AWS IAM roles or environment variables; rotate the exposed key immediately",
			category:    "secrets",
		},
		{
			name:        "github_token",
			description: "Potential GitHub personal access token hardcoded in source",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`"ghp_[A-Za-z0-9]{36}"|"gho_[A-Za-z0-9]{36}"|"github_pat_[A-Za-z0-9_]{82}"`),
			fix:         "Use GitHub Actions secrets or environment variables; revoke the exposed token immediately",
			category:    "secrets",
		},
		{
			name:        "slack_token",
			description: "Potential Slack token hardcoded in source",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`"xox[bpsa]-[A-Za-z0-9-]+"`),
			fix:         "Use environment variables; revoke the exposed token immediately",
			category:    "secrets",
		},
		{
			name:        "private_key_literal",
			description: "Private key material embedded in source code",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`),
			fix:         "Load private keys from encrypted files or a key vault; never embed in source",
			category:    "secrets",
		},
		{
			name:        "gcp_service_account_key",
			description: "GCP service account key embedded in source",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`"type"\s*:\s*"service_account"`),
			fix:         "Use GCP Workload Identity or environment-based key injection",
			category:    "secrets",
		},

		// ════════════════════════════════════════════════════════════════
		// CRYPTO (CWE-327, CWE-328, CWE-330, CWE-295)
		// ════════════════════════════════════════════════════════════════
		{
			name:        "weak_hash_md5",
			description: "Use of MD5 hashing which is cryptographically broken",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`crypto/md5|md5\.New\(\)|md5\.Sum\(`),
			fix:         "Use SHA-256 (crypto/sha256) or bcrypt for password hashing",
			category:    "crypto",
		},
		{
			name:        "weak_hash_sha1",
			description: "Use of SHA-1 which is vulnerable to collision attacks",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`crypto/sha1|sha1\.New\(\)|sha1\.Sum\(`),
			fix:         "Use SHA-256 or SHA-3 for new applications",
			category:    "crypto",
		},
		{
			name:        "weak_random",
			description: "Use of math/rand instead of crypto/rand for security-sensitive operations",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`"math/rand"|rand\.Intn\(|rand\.Float`),
			fix:         "Use crypto/rand for tokens, keys, and other security-sensitive random values",
			category:    "crypto",
		},
		{
			name:        "insecure_tls",
			description: "TLS verification disabled — man-in-the-middle vulnerability",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`InsecureSkipVerify\s*:\s*true`),
			fix:         "Never disable TLS verification in production; configure proper CA certificates",
			category:    "crypto",
		},
		{
			name:        "weak_jwt_secret",
			description: "JWT signed with a hardcoded HMAC secret",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`\.SignedString\(\s*"\s*[^"]{4,}`),
			fix:         "Use RSA or ECDSA signing keys loaded from a secure key store, with minimum 256-bit keys",
			category:    "crypto",
		},
		{
			name:        "weak_cipher_des",
			description: "Use of DES/3DES which are deprecated encryption algorithms",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`(?:crypto/des|des\.NewTripleDESCipher|des\.NewCipher)`),
			fix:         "Use AES-256-GCM for symmetric encryption",
			category:    "crypto",
		},
		{
			name:        "insecure_ecb_mode",
			description: "ECB mode encryption is insecure — patterns leak through encryption",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`(?i)(?:cipher\.ECB|ecb\.NewEncrypter|ECB\s+mode)`),
			fix:         "Use CBC or GCM mode for symmetric encryption",
			category:    "crypto",
		},
		{
			name:        "hardcoded_iv",
			description: "Hardcoded initialization vector for encryption",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`(?i)(?:iv|nonce|initialization.vector)\s*[:=]+\s*\[\]byte\s*\{[^}]{4,}\}`),
			fix:         "Generate IVs randomly for each encryption operation using crypto/rand",
			category:    "crypto",
		},

		// ════════════════════════════════════════════════════════════════
		// PATH TRAVERSAL (CWE-22)
		// ════════════════════════════════════════════════════════════════
		{
			name:        "path_traversal",
			description: "Potential path traversal via unsanitized user input in file operations",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`(os\.Open|os\.Create|os\.ReadFile|os\.WriteFile|ioutil\.ReadFile|filepath\.Join)\s*\([^)]*(?:req\.|r\.|params\.|input\.)`),
			fix:         "Validate and sanitize file paths; use filepath.Clean and verify the path stays within allowed directories",
			category:    "path_traversal",
			requireContext: []string{"req.", "r.", "params.", "input."},
		},
		{
			name:        "path_traversal_unsanitized",
			description: "File operation with potentially unsanitized path concatenation",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`(?:os\.Open|os\.Create|os\.ReadFile)\s*\([^)]*\+\s*(?:r\.|req\.|input\.)`),
			fix:         "Use filepath.Clean() and validate the resolved path stays within allowed directories",
			category:    "path_traversal",
			requireContext: []string{"r.", "req.", "input."},
		},
		{
			name:        "symlink_attack",
			description: "Potential symlink following in file operations",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`(?i)(?:os\.ReadFile|os\.Open|ioutil\.ReadFile)\s*\([^)]*(?:r\.|req\.)`),
			fix:         "Use os.Lstat to check for symlinks before following them; validate path boundaries",
			category:    "path_traversal",
			requireContext: []string{"r.", "req."},
		},

		// ════════════════════════════════════════════════════════════════
		// SSRF (CWE-918)
		// ════════════════════════════════════════════════════════════════
		{
			name:        "ssrf_http_get",
			description: "Potential SSRF — user-controlled URL passed to HTTP client",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`http\.(Get|Post|Head|Do)\s*\(\s*(?:req\.|r\.)`),
			fix:         "Validate URLs against an allowlist; block internal/private IP ranges",
			category:    "ssrf",
			requireContext: []string{"req.", "r."},
		},
		{
			name:        "ssrf_http_client",
			description: "HTTP client request with user-controlled URL via variable",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`(?:Client|HttpClient|http\.Client)\s*\.\s*(?:Get|Post|Do|GetWithContext|PostWithContext)\s*\(\s*(?:ctx,\s*)?(?:r\.|req\.|input\.|params\.)`),
			fix:         "Validate URLs against an allowlist; use URL parsing to block internal ranges",
			category:    "ssrf",
			requireContext: []string{"r.", "req.", "input.", "params."},
		},
		{
			name:        "ssrf_url_parse",
			description: "URL parsed from user input without validation",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`url\.Parse\s*\(\s*(?:r\.|req\.|input\.)(?:URL|Body|Form|Query)`),
			fix:         "Validate parsed URL scheme, host, and port against an allowlist",
			category:    "ssrf",
			requireContext: []string{"r.", "req.", "input."},
		},

		// ════════════════════════════════════════════════════════════════
		// DESERIALIZATION (CWE-502, CWE-20)
		// ════════════════════════════════════════════════════════════════
		{
			name:        "insecure_json_decode",
			description: "Decoding JSON from untrusted source without size limits",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`json\.NewDecoder\((?:req\.|r\.)Body\)\.Decode\(&[^)]+\)`),
			fix:         "Use http.MaxBytesReader to limit request body size before decoding",
			category:    "deserialization",
			requireContext: []string{"req.Body", "r.Body"},
		},
		{
			name:        "unsafe_xml_parse",
			description: "XML parsing vulnerable to XXE (XML External Entity) attacks",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`xml\.NewDecoder\(|xml\.Unmarshal\(`),
			fix:         "Use xml.Decoder with a strict charset reader; disable external entity processing",
			category:    "deserialization",
		},
		{
			name:        "unsafe_yaml_decode",
			description: "YAML decoding that may allow code execution via !! tags",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`yaml\.(Unmarshal|NewDecoder)\(`),
			fix:         "Use yaml.Unmarshal with a safe decoder that rejects !!python/object and similar tags",
			category:    "deserialization",
		},
		{
			name:        "gorilla_unsafe_mux",
			description: "Gorilla mux route variable used without sanitization",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`mux\.Vars\((?:r|req)\)\[`),
			fix:         "Validate and sanitize route variables before use; apply allowlists",
			category:    "deserialization",
		},

		// ════════════════════════════════════════════════════════════════
		// INFO DISCLOSURE (CWE-200, CWE-209)
		// ════════════════════════════════════════════════════════════════
		{
			name:        "error_in_response",
			description: "Internal error details exposed to HTTP response",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`w\.Write\(\s*\[\s*\]?\s*byte\(\s*fmt\.Sprintf\("[^"]*%[vw]`),
			fix:         "Log internal errors; return generic error messages to users",
			category:    "info_disclosure",
		},
		{
			name:        "stack_trace_exposure",
			description: "Stack trace or debug information potentially exposed to users",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`(?i)(?:debug\.PrintStack|runtime\.Stack|debug\.Stack)\(\)`),
			fix:         "Only log stack traces server-side; never expose debug information in HTTP responses",
			category:    "info_disclosure",
		},
		{
			name:        "verbose_error_handler",
			description: "HTTP error handler that exposes internal error details",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`http\.Error\(\w+,\s*(?:err\.Error|fmt\.Sprintf.*err)`),
			fix:         "Return generic error messages to users; log detailed errors server-side",
			category:    "info_disclosure",
			requireContext: []string{"err"},
		},
		{
			name:        "debug_endpoint_exposed",
			description: "Debug/pprof endpoint potentially exposed in production",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`net/http/pprof|_ "net/http/pprof"`),
			fix:         "Ensure debug endpoints are behind authentication or only available in development builds",
			category:    "info_disclosure",
		},

		// ════════════════════════════════════════════════════════════════
		// PERMISSIONS (CWE-732)
		// ════════════════════════════════════════════════════════════════
		{
			name:        "insecure_file_perms",
			description: "File created with overly permissive permissions",
			severity:    SeverityLow,
			pattern:     regexp.MustCompile(`os\.WriteFile\([^)]*0[67][67][67]`),
			fix:         "Use restrictive permissions (0600 for secrets, 0644 for config, 0755 for executables only)",
			category:    "permissions",
		},
		{
			name:        "world_readable_secret",
			description: "Secret or key file created with world-readable permissions",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`(?i)(?:os\.WriteFile|os\.Create)\s*\([^)]*(?:key|secret|token|credential)[^)]*,\s*0[0-7][67][0-7]`),
			fix:         "Use 0600 permissions for all secret files; never use group/world-readable permissions",
			category:    "permissions",
			requireContext: []string{"key", "secret", "token", "credential"},
		},
		{
			name:        "chmod_777",
			description: "chmod 777 grants world-writable permissions",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`(?:os\.Chmod|chmod)\s*\([^)]*0?777`),
			fix:         "Never use 777 permissions; use 0755 for directories, 0644 for files",
			category:    "permissions",
		},

		// ════════════════════════════════════════════════════════════════
		// GO-SPECIFIC ANTI-PATTERNS
		// ════════════════════════════════════════════════════════════════

		{
			name:        "goroutine_without_recovery",
			description: "Goroutine launched without defer/recover for panic handling",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`go\s+func\s*\(\s*\)\s*\{[^}]*\}\s*\(\)`),
			fix:         "Add defer recovery in goroutines to prevent cascading panics",
			category:    "quality",
		},
		{
			name:        "sql_rows_not_closed",
			description: "sql.Rows result not closed — will leak database connections",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`(?:db\.Query|db\.QueryContext|\.QueryRow)\s*\(`),
			fix:         "Always defer rows.Close() after a successful Query call",
			category:    "quality",
		},
		{
			name:        "mutex_not_unlocked",
			description: "Mutex Lock() without deferred Unlock() — risk of deadlock",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`\w+\.Lock\(\)\s*$`),
			fix:         "Use defer mu.Unlock() immediately after mu.Lock() to prevent deadlocks",
			category:    "quality",
		},
		{
			name:        "context_leak",
			description: "Background context used instead of request context — ignores cancellation",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`context\.Background\(\)`),
			fix:         "Use request context (r.Context()) instead of Background() to respect cancellation",
			category:    "quality",
		},

		{
			name:        "time_sleep_in_handler",
			description: "time.Sleep in request handler — blocks the goroutine pool",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`time\.Sleep\(`),
			fix:         "Use context.WithTimeout or rate limiters instead of sleep in handlers",
			category:    "quality",
		},



	}
}
