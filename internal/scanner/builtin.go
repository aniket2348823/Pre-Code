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
	// suppressCheck, if non-nil, is called with the line-split code and the matched
	// line index; returning true suppresses the finding for that line. This is the
	// escape hatch for rules that need CROSS-LINE context a single regex cannot
	// express — e.g. 'a deferred Unlock()/Close() exists nearby, so this is safe'.
	suppressCheck func(lines []string, lineIdx int) bool
}

// Shared regexps for context-aware suppression checks (avoid recompiling per finding).
var mutexLockRe = regexp.MustCompile(`(\w+)\.Lock\(\)\s*$`)

// BuiltinAnalyzer runs the built-in regex rules. Always available; kept for
// org-specific patterns external tools cannot express.
type BuiltinAnalyzer struct {
	rules []builtinRule
}

func NewBuiltinAnalyzer() *BuiltinAnalyzer {
	return &BuiltinAnalyzer{rules: builtinRules()}
}

func (b *BuiltinAnalyzer) Name() string    { return "builtin" }
func (b *BuiltinAnalyzer) Available() bool { return true }

// isTestFile returns true if the filename looks like a Go test file.
// Fuzz harnesses (*_fuzz.go) are test-only too — they carry `testing` imports
// and intentionally fake secrets, so they must never gate production CI.
func isTestFile(filename string) bool {
	return strings.HasSuffix(filename, "_test.go") ||
		strings.HasSuffix(filename, "_fuzz.go") ||
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

// hasNosecMarker reports whether the matched line (or the line immediately
// above) carries a `#nosec`-style suppression comment (gosec-compatible, also
// works for shell scripts as `# nosec`) or a semgrep-style `nosemgrep:`
// marker. Both conventions document a deliberate, reviewed decision — never
// a blanket suppression.
func hasNosecMarker(lines []string, i int) bool {
	hasMarker := func(l string) bool {
		lower := strings.ToLower(l)
		// Bare "nosemgrep" matches semgrep's native convention (a comment
		// containing the word suppresses the line); rule-ID forms like
		// "nosemgrep: <rule>" are covered by the same substring. Intentionally
		// rule-agnostic, exactly like #nosec.
		return strings.Contains(lower, "#nosec") || strings.Contains(lower, "# nosec") || strings.Contains(lower, "nosemgrep")
	}
	// Check the matched line and up to 3 lines above — justification comments
	// are commonly 1-3 lines long with the marker on the first line.
	start := i - 3
	if start < 0 {
		start = 0
	}
	for _, l := range lines[start : i+1] {
		if hasMarker(l) {
			return true
		}
	}
	return false
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
	// Normalized path (forward slashes) so filename checks are separator-agnostic
	// (the Windows CLI walker passes backslash paths).
	normName := strings.ReplaceAll(filename, "\\", "/")

	// Suppress ALL findings in generated/vendor files — these are never real vulnerabilities.
	if isGeneratedFile(normName) {
		return nil, nil
	}

	var out []Finding
	lines := strings.Split(in.Code, "\n")
	for _, r := range b.rules {
		// Suppress low-severity rules in test files (use rank, not string comparison).
		if isTestFile(normName) && SeverityRank(r.severity) <= SeverityRank(SeverityLow) {
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

				// Check filename exclusions (rule-definition files and other documented
				// exemptions — e.g. a rule's own pattern string is not a vulnerability).
				excluded := false
				for _, pattern := range r.excludeFilenames {
					if strings.Contains(normName, pattern) {
						excluded = true
						break
					}
				}
				if excluded {
					continue
				}

				// Context-aware suppression (cross-line checks like nearby deferred unlocks).
				if r.suppressCheck != nil && r.suppressCheck(lines, i) {
					continue
				}

				// Comment-based suppression: a `#nosec` marker on the matched line (or
				// the line directly above) documents a deliberate, reviewed decision —
				// e.g. a sandbox tool that executes commands by design, or libpq-style
				// TLS modes. Without an escape hatch these lines would block every CI
				// run forever and force engineers to silence rules globally.
				if hasNosecMarker(lines, i) {
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
			name:             "sql_injection",
			description:      "Potential SQL injection via string concatenation or fmt.Sprintf in query",
			severity:         SeverityCritical,
			pattern:          regexp.MustCompile(`(?i)(fmt\.Sprintf|"?\s*\+\s*|\$\{).*(?:\bSELECT\b|\bINSERT\b|\bUPDATE\b|\bDELETE\b|\bDROP\b|\bEXEC\b|\bEXECUTE\b)`),
			fix:              "Use parameterized queries ($1, $2) instead of string interpolation",
			category:         "injection",
			excludeFilenames: []string{"skills/scanner.go"}, // the skill scanner's own rule-definition patterns
		},
		{
			name:        "sql_injection_raw_query",
			description: "Raw SQL query with variable interpolation",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`(?i)(db\.Exec|db\.Query|db\.QueryRow|\.ExecContext|\.QueryContext)\s*\(\s*fmt\.Sprintf`),
			fix:         "Pass parameters as separate arguments to Exec/Query instead of formatting the query string",
			category:    "injection",
		},
		// SQL keyword inside a string literal followed by + concatenation.
		// The base sql_injection rule requires the concat operator BEFORE the
		// SQL keyword, so `"SELECT ... '" + username` slips through — this
		// variant catches string-first concatenation regardless of variable
		// source (inherently defeats parameterization).
		{
			name:             "sql_injection_string_concat",
			description:      "Potential SQL injection via string concatenation after a SQL literal",
			severity:         SeverityCritical,
			pattern:          regexp.MustCompile(`(?i)"[^"]*\b(?:SELECT|INSERT|UPDATE|DELETE|DROP)\b[^"]*"\s*\+`),
			fix:              "Use parameterized queries ($1, $2) instead of concatenating the query string",
			category:         "injection",
			excludeFilenames: []string{"builtin.go"}, // rule-definition patterns self-match
		},
		{
			name:        "command_injection",
			description: "Potential command injection via unsanitized input in exec.Command",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`exec\.Command\([^)]*(?:req\.|r\.(?:Form|URL|Body|Header)|params\.|input\.|fmt\.Sprintf)`),
			fix:         "Use allowlists for commands; never pass user input directly to exec.Command arguments",
			category:    "injection",
			// NOTE: bare "r." was far too loose — it matches ANY string containing
			// "r." (e.g. "{{.Server.Version}}" in a static docker probe), producing
			// false positives on static commands. Bare "req." is kept (req.Input
			// etc. is the classic handler pattern); accessors on `r *http.Request`
			// must be explicit (r.FormValue, r.URL, r.Body, r.Header).
			requireContext: []string{"req.", "r.Form", "r.URL", "r.Body", "r.Header", "params.", "input.", "fmt.Sprintf"},
		}, // Shell-spawning exec.Command with string concatenation in its args.
		// `exec.Command("sh", "-c", "ping -c 3 "+host)` (Unix) or
		// `exec.Command("cmd", "/c", "dir "+path)` (Windows) builds a shell
		// command string — concatenation here is the classic injection pattern
		// even when the tainted variable is a local copy of user input.
		// The shell-name branch tolerates full paths (e.g. "/usr/bin/bash",
		// "C:\\Windows\\System32\\cmd.exe"). (?i:...) is scoped to the shell
		// literals only so exec.Command stays case-sensitive.
		{
			name:             "command_injection_shell_concat",
			description:      "Shell command built via string concatenation in exec.Command — command injection risk",
			severity:         SeverityCritical,
			pattern:          regexp.MustCompile(`exec\.Command\s*\([^)]*(?i:"(?:[^"]*/)?(?:sh|bash)"\s*,\s*"-c"|"(?:[^"]*[\\/])?cmd(?:\.exe)?"\s*,\s*"/c"|"(?:[^"]*[\\/])?powershell(?:\.exe)?"\s*,\s*"-Command")[^)]*\+`),
			fix:              "Pass command arguments as a string slice (no shell), or validate/allowlist the input; never concatenate into a shell command string",
			category:         "injection",
			excludeFilenames: []string{"builtin.go"}, // rule-definition patterns self-match
		},
		// Shell command passed as a VARIABLE or EXPRESSION:
		// `exec.Command("sh", "-c", cmdVar)` / `exec.CommandContext(ctx, "sh", "-c", expr)`
		// (Windows: `exec.Command("cmd", "/c", cmdVar)`, `powershell -Command`).
		// No concat marker on the line, so the tainted origin is invisible to
		// line-based scanning — any variable fed to a shell is a shell-parse risk
		// (GoSec G204 pattern). Covers both exec.Command and exec.CommandContext
		// (the latter tolerates the leading ctx argument). The shell-name branch
		// tolerates full paths (e.g. "/usr/bin/bash", "C:\\Windows\\System32\\cmd.exe")
		// and (?i:...) is scoped so exec.Command stays case-sensitive. `[^"\s]`
		// keeps string-literal commands ("echo hi") from firing while still
		// catching identifiers and call expressions. (Plain `[^"]` would
		// backtrack to match the space after the comma, falsely flagging
		// literal commands.)
		{
			name:             "command_injection_shell_variable",
			description:      "Shell command passed as a variable or expression to exec.Command(\"sh\", \"-c\", ...) / (\"cmd\", \"/c\", ...) — command injection risk",
			severity:         SeverityCritical,
			pattern:          regexp.MustCompile(`exec\.Command(?:Context)?\s*\(\s*(?:ctx,\s*)?(?i:"(?:[^"]*/)?(?:sh|bash)"\s*,\s*"-c"|"(?:[^"]*[\\/])?cmd(?:\.exe)?"\s*,\s*"/c"|"(?:[^"]*[\\/])?powershell(?:\.exe)?"\s*,\s*"-Command")\s*,\s*[^"\s]`),
			fix:              "Pass command arguments as a string slice (no shell), or validate the command string against a strict allowlist before execution",
			category:         "injection",
			excludeFilenames: []string{"builtin.go"}, // rule-definition patterns self-match
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
			name:           "xss_http_redirect",
			description:    "Open redirect vulnerability — user input in redirect URL",
			severity:       SeverityHigh,
			pattern:        regexp.MustCompile(`http\.Redirect\([^,]+,\s*[^,]+,\s*[^,]*r\.URL`),
			fix:            "Validate redirect URLs against an allowlist before redirecting",
			category:       "xss",
			requireContext: []string{"r.URL"},
		},
		{
			name:           "template_injection",
			description:    "Potential template injection — user input in template parsing",
			severity:       SeverityHigh,
			pattern:        regexp.MustCompile(`template\.(New|Must)\([^)]*(?:req\.|r\.|input\.)`),
			fix:            "Never pass user input directly to template constructors; use predefined templates",
			category:       "injection",
			requireContext: []string{"req.", "r.", "input."},
		},
		{
			name:        "log_injection",
			description: "Potential log injection via unsanitized user input",
			severity:    SeverityMedium,
			// requireContext no longer includes bare "r." — a substring `r\.`
			// matched the "r." inside ANY identifier ending in r (server.,
			// httpServer., hr., rl.), producing dozens of false positives on
			// structured slog key-value calls. Only req./input./user. contexts
			// are flagged now.
			pattern:        regexp.MustCompile(`(?:log\.|slog\.|fmt\.Print|fmt\.Fprint)\w*\([^)]*(?:req\.|input\.|user\.)`),
			fix:            "Sanitize user input before logging; use structured logging with key-value pairs",
			category:       "injection",
			requireContext: []string{"req.", "input.", "user."},
		},

		// ════════════════════════════════════════════════════════════════
		// PYTHON (CWE-78, CWE-94, CWE-502, CWE-918)
		// The deterministic engine must cover Python — the VSCode extension's
		// primary BYOK scan target. Rules use requireContext so they only fire
		// when user-input markers are present on the same line (low FP).
		// ════════════════════════════════════════════════════════════════
		{
			name:           "python_command_injection",
			description:    "Potential command injection via unsanitized input in os.system/os.popen/subprocess",
			severity:       SeverityCritical,
			pattern:        regexp.MustCompile(`(?i)(?:os\.system\s*\(|os\.popen\s*\(|commands\.getoutput\s*\(|subprocess\.(?:Popen|run|call|check_call|check_output)\s*\([^)]*shell\s*=\s*True)`),
			fix:            "Never pass user input to a shell; use subprocess with a string list and shell=False, and validate arguments against an allowlist",
			category:       "injection",
			requireContext: []string{"request.args", "request.form", "request.json", "request.get_json", "input(", "sys.argv", "os.environ", "getenv("},
		},
		{
			name:           "python_eval_exec",
			description:    "eval()/exec() with user-controllable input allows arbitrary code execution",
			severity:       SeverityCritical,
			pattern:        regexp.MustCompile(`(?i)\b(?:eval|exec)\s*\(`),
			fix:            "Avoid eval/exec entirely; use ast.literal_eval for safe literal parsing or a dedicated parser",
			category:       "injection",
			requireContext: []string{"request.args", "request.form", "request.json", "input(", "sys.argv", "os.environ", "getenv("},
		},
		{
			name:        "python_pickle_load",
			description: "Unpickling untrusted data can execute arbitrary code (CWE-502)",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`(?i)\b(?:pickle\.loads|pickle\.load|cPickle\.loads|cPickle\.load)\s*\(`),
			fix:         "Never unpickle untrusted data; use JSON or a safe serialization format",
			category:    "deserialization",
		},
		{
			name:        "python_unsafe_yaml",
			description: "yaml.load without a safe Loader can execute arbitrary code",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`(?i)yaml\.load\s*\(`),
			fix:         "Use yaml.safe_load or yaml.load with Loader=yaml.SafeLoader",
			category:    "deserialization",
		},
		// SQL-string formatting is inherently injection-prone regardless of the
		// variable source (bandit B608 behavior): f-string interpolation and
		// %-formatting into a query both defeat parameterization.
		{
			name:        "python_sql_injection_fstring",
			description: "Potential SQL injection via f-string interpolation in a query",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`(?i)(?:cursor\.execute|cursor\.executemany|\.execute_query)\s*\(\s*f["'][^"']*\{`),
			fix:         "Use parameterized queries (?, %s placeholders with a params tuple) instead of f-string interpolation",
			category:    "injection",
		},
		{
			name:        "python_sql_injection_format",
			description: "Potential SQL injection via %-formatting into a query",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`(?i)(?:cursor\.execute|cursor\.executemany|\.execute_query)\s*\(\s*r?["'][^"']*%[sd]["']\s*%`),
			fix:         "Use parameterized queries with a params tuple instead of %-formatting the query string",
			category:    "injection",
		},
		{
			name:           "python_ssrf",
			description:    "Potential SSRF — user-controlled URL passed to an HTTP client",
			severity:       SeverityHigh,
			pattern:        regexp.MustCompile(`(?i)\b(?:requests\.(?:get|post|put|delete|head|patch)|urllib\.(?:request\.urlopen|urlopen)|httpx\.(?:get|post|put|delete)|aiohttp\.ClientSession)\s*\(`),
			fix:            "Validate URLs against an allowlist; block private/internal IP ranges before making the request",
			category:       "ssrf",
			requireContext: []string{"request.args", "request.form", "request.json", "input(", "sys.argv", "getenv("},
		},
		// NOTE: intentionally NOT case-insensitive — Python sinks are lowercase
		// (open, os.remove, ...) while Go capitalizes them (os.Open, os.Remove).
		// (?i) would make this rule match Go's os.Open/os.Remove/os.Rename and
		// fire Python findings on Go code — the scanner's primary language.
		{
			name:           "python_path_traversal",
			description:    "Potential path traversal via user-controlled filename in file operations",
			severity:       SeverityHigh,
			pattern:        regexp.MustCompile(`\b(?:open\s*\(|os\.open\s*\(|os\.remove\s*\(|os\.rename\s*\(|shutil\.(?:copy|move|rmtree)\s*\(|send_file\s*\(|send_from_directory\s*\()`),
			fix:            "Validate the resolved path stays within an allowed base directory using os.path.realpath",
			category:       "path_traversal",
			requireContext: []string{"request.args", "request.form", "request.files", "filename", "input(", "sys.argv"},
		},

		// ════════════════════════════════════════════════════════════════
		// SECRETS (CWE-798, CWE-259, CWE-321)
		// ════════════════════════════════════════════════════════════════
		{
			name:             "hardcoded_password",
			description:      "Hardcoded password or secret in source code",
			severity:         SeverityCritical,
			pattern:          regexp.MustCompile(`(?i)(password|passwd|secret|api_key|apikey|api[-_]?secret|private[-_]?key)\s*[:=]+\s*"[^"]{8,}"`),
			fix:              "Use environment variables or a secrets manager (e.g., HashiCorp Vault)",
			category:         "secrets",
			excludeFilenames: []string{"example", "sample", "mock_", "stub_", "fuzz"},
			// `[:=]+` in the pattern also matches `==` comparisons — but a line like
			// `c.Database.Password == "vigilagent"` is a validation CHECK against a
			// known default, not a hardcoded credential. RE2 has no lookahead, so
			// suppress comparisons here explicitly.
			suppressCheck: func(lines []string, i int) bool {
				return strings.Contains(lines[i], "==") || strings.Contains(lines[i], "!=")
			},
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
			name:             "weak_hash_md5",
			description:      "Use of MD5 hashing which is cryptographically broken",
			severity:         SeverityHigh,
			pattern:          regexp.MustCompile(`crypto/md5|md5\.New\(\)|md5\.Sum\(`),
			fix:              "Use SHA-256 (crypto/sha256) or bcrypt for password hashing",
			category:         "crypto",
			excludeFilenames: []string{"builtin.go"}, // rule-definition patterns self-match
		},
		{
			// #nosec weak_hash_sha1: rule-definition pattern text (self-reference), not real usage
			name:             "weak_hash_sha1",
			description:      "Use of SHA-1 which is vulnerable to collision attacks",
			severity:         SeverityMedium,
			pattern:          regexp.MustCompile(`crypto/sha1|sha1\.New\(\)|sha1\.Sum\(`),
			fix:              "Use SHA-256 or SHA-3 for new applications",
			category:         "crypto",
			excludeFilenames: []string{"builtin.go"}, // rule-definition patterns self-match
		},
		{
			name:             "weak_random",
			description:      "Use of math/rand instead of crypto/rand for security-sensitive operations",
			severity:         SeverityHigh,
			pattern:          regexp.MustCompile(`"math/rand"|rand\.Intn\(|rand\.Float`),
			fix:              "Use crypto/rand for tokens, keys, and other security-sensitive random values",
			category:         "crypto",
			excludeFilenames: []string{"builtin.go"}, // rule-definition patterns self-match
		},
		{
			name:        "insecure_tls",
			description: "TLS verification disabled — man-in-the-middle vulnerability",
			severity:    SeverityCritical,
			pattern:     regexp.MustCompile(`InsecureSkipVerify\s*:\s*true`),
			fix:         "Never disable TLS verification in production; configure proper CA certificates",
			category:    "crypto",
			// Custom chain verification (VerifyPeerCertificate) in the same struct
			// literal means TLS IS verified — only hostname checking is delegated
			// (libpq "verify-ca" semantics). That is a deliberate, reviewed
			// configuration, not a MITM hole. Genuinely unverified modes must carry
			// a `#nosec` justification comment instead.
			suppressCheck: func(lines []string, i int) bool {
				start := i - 5
				if start < 0 {
					start = 0
				}
				end := i + 6
				if end > len(lines) {
					end = len(lines)
				}
				for _, l := range lines[start:end] {
					if strings.Contains(l, "VerifyPeerCertificate") {
						return true
					}
				}
				return false
			},
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
			name:             "weak_cipher_des",
			description:      "Use of DES/3DES which are deprecated encryption algorithms",
			severity:         SeverityHigh,
			pattern:          regexp.MustCompile(`(?:crypto/des|des\.NewTripleDESCipher|des\.NewCipher)`),
			fix:              "Use AES-256-GCM for symmetric encryption",
			category:         "crypto",
			excludeFilenames: []string{"builtin.go"}, // rule-definition patterns self-match
		},
		{
			name:             "insecure_ecb_mode",
			description:      "ECB mode encryption is insecure — patterns leak through encryption",
			severity:         SeverityHigh,
			pattern:          regexp.MustCompile(`(?i)(?:cipher\.ECB|ecb\.NewEncrypter|ECB\s+mode)`),
			fix:              "Use CBC or GCM mode for symmetric encryption",
			category:         "crypto",
			excludeFilenames: []string{"builtin.go"}, // rule-definition patterns self-match
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
			name:           "path_traversal",
			description:    "Potential path traversal via unsanitized user input in file operations",
			severity:       SeverityHigh,
			pattern:        regexp.MustCompile(`(os\.Open|os\.Create|os\.ReadFile|os\.WriteFile|ioutil\.ReadFile|filepath\.Join)\s*\([^)]*(?:req\.|r\.|params\.|input\.)`),
			fix:            "Validate and sanitize file paths; use filepath.Clean and verify the path stays within allowed directories",
			category:       "path_traversal",
			requireContext: []string{"req.", "r.", "params.", "input."},
		},
		{
			name:           "path_traversal_unsanitized",
			description:    "File operation with potentially unsanitized path concatenation",
			severity:       SeverityMedium,
			pattern:        regexp.MustCompile(`(?:os\.Open|os\.Create|os\.ReadFile)\s*\([^)]*\+\s*(?:r\.|req\.|input\.)`),
			fix:            "Use filepath.Clean() and validate the resolved path stays within allowed directories",
			category:       "path_traversal",
			requireContext: []string{"r.", "req.", "input."},
		},
		{
			name:           "symlink_attack",
			description:    "Potential symlink following in file operations",
			severity:       SeverityMedium,
			pattern:        regexp.MustCompile(`(?i)(?:os\.ReadFile|os\.Open|ioutil\.ReadFile)\s*\([^)]*(?:r\.|req\.)`),
			fix:            "Use os.Lstat to check for symlinks before following them; validate path boundaries",
			category:       "path_traversal",
			requireContext: []string{"r.", "req."},
		},

		// ════════════════════════════════════════════════════════════════
		// SSRF (CWE-918)
		// ════════════════════════════════════════════════════════════════
		{
			name:           "ssrf_http_get",
			description:    "Potential SSRF — user-controlled URL passed to HTTP client",
			severity:       SeverityHigh,
			pattern:        regexp.MustCompile(`http\.(Get|Post|Head|Do)\s*\(\s*(?:req\.|r\.)`),
			fix:            "Validate URLs against an allowlist; block internal/private IP ranges",
			category:       "ssrf",
			requireContext: []string{"req.", "r."},
		},
		{
			name:           "ssrf_http_client",
			description:    "HTTP client request with user-controlled URL via variable",
			severity:       SeverityMedium,
			pattern:        regexp.MustCompile(`(?:Client|HttpClient|http\.Client)\s*\.\s*(?:Get|Post|Do|GetWithContext|PostWithContext)\s*\(\s*(?:ctx,\s*)?(?:r\.|req\.|input\.|params\.)`),
			fix:            "Validate URLs against an allowlist; use URL parsing to block internal ranges",
			category:       "ssrf",
			requireContext: []string{"r.", "req.", "input.", "params."},
		},
		{
			name:           "ssrf_url_parse",
			description:    "URL parsed from user input without validation",
			severity:       SeverityMedium,
			pattern:        regexp.MustCompile(`url\.Parse\s*\(\s*(?:r\.|req\.|input\.)(?:URL|Body|Form|Query)`),
			fix:            "Validate parsed URL scheme, host, and port against an allowlist",
			category:       "ssrf",
			requireContext: []string{"r.", "req.", "input."},
		},

		// ════════════════════════════════════════════════════════════════
		// DESERIALIZATION (CWE-502, CWE-20)
		// ════════════════════════════════════════════════════════════════
		{
			name:           "insecure_json_decode",
			description:    "Decoding JSON from untrusted source without size limits",
			severity:       SeverityMedium,
			pattern:        regexp.MustCompile(`json\.NewDecoder\((?:req\.|r\.)Body\)\.Decode\(&[^)]+\)`),
			fix:            "Use http.MaxBytesReader to limit request body size before decoding",
			category:       "deserialization",
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
			name:           "verbose_error_handler",
			description:    "HTTP error handler that exposes internal error details",
			severity:       SeverityMedium,
			pattern:        regexp.MustCompile(`http\.Error\(\w+,\s*(?:err\.Error|fmt\.Sprintf.*err)`),
			fix:            "Return generic error messages to users; log detailed errors server-side",
			category:       "info_disclosure",
			requireContext: []string{"err"},
		},
		{
			name:        "debug_endpoint_exposed",
			description: "Debug/pprof endpoint potentially exposed in production",
			severity:    SeverityMedium,
			// #nosec debug_endpoint_exposed: rule-definition pattern text (self-reference), not real usage
			pattern:  regexp.MustCompile(`net/http/pprof|_ "net/http/pprof"`),
			fix:      "Ensure debug endpoints are behind authentication or only available in development builds",
			category: "info_disclosure",
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
			name:           "world_readable_secret",
			description:    "Secret or key file created with world-readable permissions",
			severity:       SeverityHigh,
			pattern:        regexp.MustCompile(`(?i)(?:os\.WriteFile|os\.Create)\s*\([^)]*(?:key|secret|token|credential)[^)]*,\s*0[0-7][67][0-7]`),
			fix:            "Use 0600 permissions for all secret files; never use group/world-readable permissions",
			category:       "permissions",
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
			pattern:     regexp.MustCompile(`\w+\.Query(?:Context)?\s*\(`),
			fix:         "Always defer rows.Close() after a successful Query call",
			category:    "quality",
			// NOTE: .QueryRow is intentionally NOT matched — it returns a *sql.Row
			// (auto-closed by Scan), so it can never leak a connection.
			// Suppress when: (1) the line is the net/url accessor `URL.Query()` —
			// it returns url.Values, not rows; (2) within the next 40 lines the
			// result is closed (`defer rows.Close()`, plain `rows.Close()`, or any
			// `.Close(`) or escapes to the caller (`return rows`) — the caller
			// then owns it. This covers the DB-layer Query wrappers that return
			// rows by design (their `return rows, err` can be 30+ lines below the
			// query once circuit-breaker + slow-query logging run between).
			suppressCheck: func(lines []string, i int) bool {
				if strings.Contains(lines[i], "URL.Query(") {
					return true
				}
				end := i + 41
				if end > len(lines) {
					end = len(lines)
				}
				for _, l := range lines[i+1 : end] {
					if strings.Contains(l, ".Close(") || strings.Contains(l, "return rows") {
						return true
					}
				}
				return false
			},
		},
		{
			name:        "mutex_not_unlocked",
			description: "Mutex Lock() without deferred Unlock() — risk of deadlock",
			severity:    SeverityHigh,
			pattern:     regexp.MustCompile(`\w+\.Lock\(\)\s*$`),
			fix:         "Use defer mu.Unlock() immediately after mu.Lock() to prevent deadlocks",
			category:    "quality",
			// Suppress when the SAME mutex is released (deferred OR explicit) within
			// the next 40 lines. `mu.Lock(); defer mu.Unlock()` and the
			// extract-clear-unlock idiom (`mu.Lock(); ...copy...; mu.Unlock()`) —
			// which can legitimately span 30+ lines in DB-persisting methods — are
			// both correct. Only a Lock() with NO nearby release should fire
			// (deadlock risk). Receivers are tolerated: `sm.mu.Lock()` ↔
			// `defer sm.mu.Unlock()`.
			suppressCheck: func(lines []string, i int) bool {
				m := mutexLockRe.FindStringSubmatch(lines[i])
				if m == nil {
					return false
				}
				varName := m[1]
				unlockRe := regexp.MustCompile(`(?:defer\s+)?(?:[A-Za-z_]\w*\.)*` + regexp.QuoteMeta(varName) + `\.Unlock\(\)`)
				end := i + 41
				if end > len(lines) {
					end = len(lines)
				}
				for _, l := range lines[i+1 : end] {
					if unlockRe.MatchString(l) {
						return true
					}
				}
				return false
			},
		},
		{
			name:        "context_leak",
			description: "Background context used instead of request context — ignores cancellation",
			severity:    SeverityMedium,
			pattern:     regexp.MustCompile(`context\.Background\(\)`),
			fix:         "Use request context (r.Context()) instead of Background() to respect cancellation",
			category:    "quality",
			// A comment line mentioning context.Background() (doc comments, nosec
			// justifications) is documentation, not a real leak — only code lines
			// fire. Go, shell, and C-style comments are all covered.
			suppressCheck: func(lines []string, i int) bool {
				trimmed := strings.TrimSpace(lines[i])
				return strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") ||
					strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "#")
			},
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
