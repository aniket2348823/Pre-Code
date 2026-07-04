# Deterministic Engine (Code Static Analysis) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor `internal/scanner` from a single regex struct into a pluggable multi-analyzer engine that emits unified, evidence-backed, confidence-scored findings and degrades gracefully when external tools are absent.

**Architecture:** An `Analyzer` interface with three adapters — `builtin` (regex, always on), `bandit`, `semgrep` (shell out via an injectable `Runner`). The `Engine` runs every available analyzer, normalizes output into one `Finding` schema, dedupes by fingerprint, scores confidence via corroboration, and returns a `Report` listing what ran, was skipped, or errored.

**Tech Stack:** Go stdlib (`os/exec`, `encoding/json`, `regexp`, `crypto/sha256`), `testing`. No new deps. External tools (bandit/semgrep) are optional at runtime and never required for tests.

## Global Constraints

- Package stays `scanner`; module `github.com/vigilagent/vigilagent`.
- Every test passes on Windows with **no** external tools installed (adapters tested via a fake `Runner`).
- Severity vocabulary: `critical|high|medium|low|info`.
- Confidence clamped to `[0.05, 0.99]` — never 0, never 1.0.
- Fingerprint = `sha256(filename | line | normalizedSnippet)[:16]` — **category excluded** so the same line flagged by different tools (which categorize differently) merges and corroborates.
- Old `Scanner`, `Scan`, `ScanResult`, `Vulnerability`, `Rule` are removed (no callers). The 20 regex patterns are preserved verbatim in `builtin.go`.
- Analyzers emit findings with `Confidence` left 0; the engine computes confidence during merge.

---

## File Structure

- `internal/scanner/finding.go` — `Severity`, `SeverityRank`, `Finding`, `Report`, `ComputeFingerprint`, `normalizeSnippet`.
- `internal/scanner/analyzer.go` — `Input`, `Analyzer` interface.
- `internal/scanner/builtin.go` — `BuiltinAnalyzer` + the 20 verbatim regex rules.
- `internal/scanner/runner.go` — `Runner`, `ExecRunner`, `toolExists`.
- `internal/scanner/bandit.go` — `BanditAnalyzer`.
- `internal/scanner/semgrep.go` — `SemgrepAnalyzer`.
- `internal/scanner/confidence.go` — `Confidence`, `baseConfidence`, `analyzerWeight`, `clampFloat`.
- `internal/scanner/engine.go` — `Engine`, `NewEngine`, `DefaultEngine`, `Run`, `mergeAndScore`.
- Old `internal/scanner/scanner.go` — deleted in Task 1.

---

## Task 1: Finding schema + fingerprint (replaces old types)

**Files:**
- Create: `internal/scanner/finding.go`
- Delete: `internal/scanner/scanner.go`
- Test: `internal/scanner/finding_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Severity` + consts; `SeverityRank(Severity) int`; `Finding` struct; `Report` struct; `ComputeFingerprint(filename string, line int, snippet string) string`; `normalizeSnippet(string) string`.

- [ ] **Step 1: Write the failing test**

```go
package scanner

import "testing"

func TestComputeFingerprint(t *testing.T) {
	// Same file/line/snippet (differing only in whitespace) → same fingerprint.
	a := ComputeFingerprint("x.go", 3, "query = a + b")
	b := ComputeFingerprint("x.go", 3, "query =   a  +  b")
	if a != b {
		t.Fatalf("whitespace should not change fingerprint: %s vs %s", a, b)
	}
	if len(a) != 16 {
		t.Fatalf("fingerprint length = %d want 16", len(a))
	}
	// Different line → different fingerprint.
	if ComputeFingerprint("x.go", 4, "query = a + b") == a {
		t.Fatal("different line should change fingerprint")
	}
}

func TestSeverityRank(t *testing.T) {
	if SeverityRank(SeverityCritical) <= SeverityRank(SeverityHigh) {
		t.Fatal("critical must outrank high")
	}
	if SeverityRank(SeverityInfo) <= SeverityRank("") {
		t.Fatal("info must outrank unknown")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run 'TestComputeFingerprint|TestSeverityRank' 2>&1 | tail`
Expected: build failure — old `scanner.go` still defines `Severity`; `ComputeFingerprint` undefined.

- [ ] **Step 3: Delete the old file and write finding.go**

```bash
cd "/d/Antigravity 2/Precode" && git rm internal/scanner/scanner.go
```

Create `internal/scanner/finding.go`:

```go
package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// Severity ranks how serious a finding is.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// SeverityRank orders severities for sorting; higher is more severe.
func SeverityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// Finding is one normalized security issue from any analyzer.
type Finding struct {
	RuleID      string   `json:"rule_id"`
	Analyzers   []string `json:"analyzers"`
	Severity    Severity `json:"severity"`
	Category    string   `json:"category,omitempty"`
	Title       string   `json:"title"`
	Message     string   `json:"message"`
	Filename    string   `json:"filename,omitempty"`
	Line        int      `json:"line,omitempty"`
	Snippet     string   `json:"snippet,omitempty"`
	Fix         string   `json:"fix,omitempty"`
	Confidence  float64  `json:"confidence"`
	Fingerprint string   `json:"fingerprint"`
}

// Report is the engine's full output for one Input.
type Report struct {
	Findings         []Finding         `json:"findings"`
	AnalyzersRun     []string          `json:"analyzers_run"`
	AnalyzersSkipped map[string]string `json:"analyzers_skipped"` // name -> reason
	AnalyzerErrors   map[string]string `json:"analyzer_errors"`   // name -> error
}

// normalizeSnippet collapses runs of whitespace so cosmetic differences do not
// change a fingerprint.
func normalizeSnippet(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ComputeFingerprint derives a stable dedupe key from location + code, ignoring
// category so the same line flagged by different tools collapses into one.
func ComputeFingerprint(filename string, line int, snippet string) string {
	h := sha256.Sum256([]byte(filename + "|" + strconv.Itoa(line) + "|" + normalizeSnippet(snippet)))
	return hex.EncodeToString(h[:])[:16]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run 'TestComputeFingerprint|TestSeverityRank' 2>&1 | tail`
Expected: `PASS` / `ok`.

- [ ] **Step 5: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add internal/scanner/finding.go internal/scanner/finding_test.go
git commit -m "feat(scanner): unified Finding schema + fingerprint; drop old Scanner"
```

---

## Task 2: Analyzer interface + builtin regex analyzer

**Files:**
- Create: `internal/scanner/analyzer.go`
- Create: `internal/scanner/builtin.go`
- Test: `internal/scanner/builtin_test.go`

**Interfaces:**
- Consumes: `Finding`, `Severity`, `ComputeFingerprint` (Task 1).
- Produces: `Input{Language, Code, Filename string}`; `Analyzer` interface (`Name() string`, `Available() bool`, `Analyze(context.Context, Input) ([]Finding, error)`); `NewBuiltinAnalyzer() *BuiltinAnalyzer`.

- [ ] **Step 1: Write the failing test**

```go
package scanner

import (
	"context"
	"testing"
)

func TestBuiltinDetectsKnownVulns(t *testing.T) {
	code := "" +
		"q := fmt.Sprintf(\"SELECT * FROM users WHERE id=%d\", id)\n" +
		"password := \"supersecret123\"\n" +
		"h := md5.New()\n"
	a := NewBuiltinAnalyzer()
	if a.Name() != "builtin" || !a.Available() {
		t.Fatalf("builtin must be named 'builtin' and always available")
	}
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "x.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	want := map[string]bool{"sql_injection": false, "hardcoded_password": false, "weak_hash_md5": false}
	for _, f := range found {
		if _, ok := want[f.RuleID]; ok {
			want[f.RuleID] = true
		}
		if len(f.Analyzers) != 1 || f.Analyzers[0] != "builtin" {
			t.Fatalf("finding %s missing builtin analyzer tag: %v", f.RuleID, f.Analyzers)
		}
		if f.Fingerprint == "" {
			t.Fatalf("finding %s has no fingerprint", f.RuleID)
		}
	}
	for rule, seen := range want {
		if !seen {
			t.Fatalf("expected builtin to detect %s", rule)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run TestBuiltinDetects 2>&1 | tail`
Expected: build failure — `NewBuiltinAnalyzer`, `Input` undefined.

- [ ] **Step 3: Write analyzer.go**

Create `internal/scanner/analyzer.go`:

```go
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
```

- [ ] **Step 4: Write builtin.go (20 rules verbatim)**

Create `internal/scanner/builtin.go`:

```go
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

func (b *BuiltinAnalyzer) Name() string      { return "builtin" }
func (b *BuiltinAnalyzer) Available() bool    { return true }

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
		{name: "xss_unsafe_html", description: "Unsafe HTML rendering that may allow XSS", severity: SeverityHigh, pattern: regexp.MustCompile(`(template\.HTML|template\.HTMLAttr|html\.Template)\s*\(\s*(?!template)`), fix: "Use template escaping or validate/sanitize input before rendering as HTML", category: "xss"},
		{name: "xss_unsafe_js", description: "Potential XSS via JavaScript template literal with user input", severity: SeverityMedium, pattern: regexp.MustCompile(`(?i)(innerHTML|outerHTML|document\.write)\s*=\s*[^;]*\+`), fix: "Use textContent instead of innerHTML, or sanitize input before insertion", category: "xss"},
		{name: "xss_http_redirect", description: "Open redirect vulnerability — user input in redirect URL", severity: SeverityHigh, pattern: regexp.MustCompile(`http\.Redirect\([^,]+,\s*[^,]*r\.URL`), fix: "Validate redirect URLs against an allowlist before redirecting", category: "xss"},
		{name: "hardcoded_password", description: "Hardcoded password or secret in source code", severity: SeverityCritical, pattern: regexp.MustCompile(`(?i)(password|passwd|secret|api_key|apikey|api[-_]?secret|token|private[-_]?key)\s*[:=]\s*["'][^"']{8,}["']`), fix: "Use environment variables or a secrets manager (e.g., HashiCorp Vault)", category: "secrets"},
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
		{name: "insecure_json_decode", description: "Decoding JSON from untrusted source without size limits", severity: SeverityMedium, pattern: regexp.MustCompile(`json\.NewDecoder\((?:req\.|r\.)Body\)\.Decode\(&[^)]+\)(?!.*MaxBytesReader)`), fix: "Use http.MaxBytesReader to limit request body size before decoding", category: "deserialization"},
		{name: "race_condition_map", description: "Concurrent map access without synchronization", severity: SeverityMedium, pattern: regexp.MustCompile(`(?i)(go\s+func|go\s+\w+\()`), fix: "Use sync.Mutex or sync.Map for concurrent map access", category: "race"},
		{name: "error_info_leak", description: "Internal error details exposed to users", severity: SeverityMedium, pattern: regexp.MustCompile(`fmt\.Errorf\("[^"]*%w[^"]*"\s*,\s*err\)`), fix: "Log internal errors; return generic error messages to users", category: "info_disclosure"},
		{name: "insecure_file_perms", description: "File created with overly permissive permissions", severity: SeverityLow, pattern: regexp.MustCompile(`os\.WriteFile\([^)]*0[67][67][67]`), fix: "Use restrictive permissions (0600 for secrets, 0644 for config, 0755 for executables only)", category: "permissions"},
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run TestBuiltinDetects 2>&1 | tail`
Expected: `PASS`. (Note: the `race_condition_map` rule is intentionally broad; the test only asserts the three named rules fire, so extra findings are fine.)

- [ ] **Step 6: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add internal/scanner/analyzer.go internal/scanner/builtin.go internal/scanner/builtin_test.go
git commit -m "feat(scanner): Analyzer interface + builtin regex analyzer (20 rules)"
```

---

## Task 3: Command runner + tool detection + shared fake

**Files:**
- Create: `internal/scanner/runner.go`
- Test: `internal/scanner/runner_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Runner` interface (`Run(ctx, name string, args []string, stdin string) (stdout, stderr string, err error)`); `ExecRunner` (concrete, real exec); `toolExists(name string) bool`. Also defines `fakeRunner` in the test file, reused by later tasks' tests.

- [ ] **Step 1: Write the failing test**

```go
package scanner

import (
	"context"
	"testing"
)

// fakeRunner is a shared test double for shell-out adapters. Later tasks reuse it.
type fakeRunner struct {
	stdout, stderr string
	err            error
	gotName        string
	gotArgs        []string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, stdin string) (string, string, error) {
	f.gotName = name
	f.gotArgs = args
	return f.stdout, f.stderr, f.err
}

func TestToolExists(t *testing.T) {
	if !toolExists("go") {
		t.Fatal("expected 'go' to be on PATH in a Go dev environment")
	}
	if toolExists("definitely-not-a-real-tool-xyz-9999") {
		t.Fatal("expected a nonsense binary to be absent")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run TestToolExists 2>&1 | tail`
Expected: build failure — `toolExists` undefined.

- [ ] **Step 3: Write runner.go**

Create `internal/scanner/runner.go`:

```go
package scanner

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// Runner executes an external command. Injectable so adapters test without the
// real tool installed.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin string) (stdout, stderr string, err error)
}

// ExecRunner runs commands for real via os/exec.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string, stdin string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.String(), errBuf.String(), err
}

// toolExists reports whether a binary is resolvable on PATH.
func toolExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run TestToolExists 2>&1 | tail`
Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add internal/scanner/runner.go internal/scanner/runner_test.go
git commit -m "feat(scanner): injectable command Runner + tool detection"
```

---

## Task 4: Bandit adapter

**Files:**
- Create: `internal/scanner/bandit.go`
- Test: `internal/scanner/bandit_test.go`

**Interfaces:**
- Consumes: `Analyzer`, `Input`, `Finding`, `Runner`, `ComputeFingerprint`, `Severity` (Tasks 1-3); `fakeRunner` (Task 3 test).
- Produces: `NewBanditAnalyzer(r Runner) *BanditAnalyzer` (nil `r` → `ExecRunner{}`). Implements `Analyzer`. Exposes settable `exists func() bool` for tests.

**Bandit JSON shape (real):** `{"results":[{"filename","issue_severity":"HIGH|MEDIUM|LOW","issue_text","test_id":"B608","test_name","line_number","code"}]}`. Bandit exits non-zero when it finds issues, so the adapter parses stdout JSON regardless of exit code and only errors when JSON is absent/unparseable.

- [ ] **Step 1: Write the failing test**

```go
package scanner

import (
	"context"
	"testing"
)

func TestBanditNormalizesJSON(t *testing.T) {
	canned := `{"results":[{"filename":"snippet.py","issue_severity":"HIGH","issue_text":"Possible SQL injection","test_id":"B608","test_name":"hardcoded_sql_expressions","line_number":3,"code":"3 query = 'SELECT ' + x"}]}`
	fr := &fakeRunner{stdout: canned}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }

	if b.Name() != "bandit" || !b.Available() {
		t.Fatal("bandit analyzer name/availability wrong")
	}
	found, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x=1", Filename: "snippet.py"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("want 1 finding, got %d", len(found))
	}
	f := found[0]
	if f.RuleID != "B608" || f.Severity != SeverityHigh {
		t.Fatalf("bad normalization: %+v", f)
	}
	if f.Line != 3 || f.Analyzers[0] != "bandit" || f.Fingerprint == "" {
		t.Fatalf("bad fields: %+v", f)
	}
	if fr.gotName != "bandit" {
		t.Fatalf("expected to invoke bandit, got %s", fr.gotName)
	}
}

func TestBanditSkipsNonPython(t *testing.T) {
	b := NewBanditAnalyzer(&fakeRunner{stdout: `{"results":[]}`})
	b.exists = func() bool { return true }
	found, err := b.Analyze(context.Background(), Input{Language: "go", Code: "x"})
	if err != nil || len(found) != 0 {
		t.Fatalf("bandit should skip non-python: findings=%d err=%v", len(found), err)
	}
}

func TestBanditUnavailableWhenAbsent(t *testing.T) {
	b := NewBanditAnalyzer(nil)
	b.exists = func() bool { return false }
	if b.Available() {
		t.Fatal("bandit must report unavailable when binary absent")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run TestBandit 2>&1 | tail`
Expected: build failure — `NewBanditAnalyzer` undefined.

- [ ] **Step 3: Write bandit.go**

Create `internal/scanner/bandit.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run TestBandit 2>&1 | tail`
Expected: `PASS` for all three tests.

- [ ] **Step 5: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add internal/scanner/bandit.go internal/scanner/bandit_test.go
git commit -m "feat(scanner): bandit adapter (temp file + json normalize)"
```

---

## Task 5: Semgrep adapter

**Files:**
- Create: `internal/scanner/semgrep.go`
- Test: `internal/scanner/semgrep_test.go`

**Interfaces:**
- Consumes: same as Task 4.
- Produces: `NewSemgrepAnalyzer(r Runner) *SemgrepAnalyzer` (nil → `ExecRunner{}`). Implements `Analyzer`. Settable `exists func() bool`.

**Semgrep JSON shape (real):** `{"results":[{"check_id","path","start":{"line"},"extra":{"message","severity":"ERROR|WARNING|INFO","lines","metadata":{"category"}}}]}`. Runs `semgrep --json --config auto <file>`; parses stdout regardless of exit code (semgrep exits non-zero when findings exist). Config tuning for offline use is deferred; tests use the fake runner so config choice is irrelevant to them.

- [ ] **Step 1: Write the failing test**

```go
package scanner

import (
	"context"
	"testing"
)

func TestSemgrepNormalizesJSON(t *testing.T) {
	canned := `{"results":[{"check_id":"python.lang.security.audit.exec-detected","path":"snippet.py","start":{"line":5},"extra":{"message":"Detected exec() usage","severity":"ERROR","lines":"exec(user_input)","metadata":{"category":"security"}}}]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }

	found, err := s.Analyze(context.Background(), Input{Language: "python", Code: "exec(x)", Filename: "snippet.py"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("want 1 finding, got %d", len(found))
	}
	f := found[0]
	if f.RuleID != "python.lang.security.audit.exec-detected" || f.Severity != SeverityHigh {
		t.Fatalf("bad normalization: %+v", f)
	}
	if f.Line != 5 || f.Category != "security" || f.Analyzers[0] != "semgrep" {
		t.Fatalf("bad fields: %+v", f)
	}
	if fr.gotName != "semgrep" {
		t.Fatalf("expected semgrep invocation, got %s", fr.gotName)
	}
}

func TestSemgrepUnavailableWhenAbsent(t *testing.T) {
	s := NewSemgrepAnalyzer(nil)
	s.exists = func() bool { return false }
	if s.Available() {
		t.Fatal("semgrep must report unavailable when binary absent")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run TestSemgrep 2>&1 | tail`
Expected: build failure — `NewSemgrepAnalyzer` undefined.

- [ ] **Step 3: Write semgrep.go**

Create `internal/scanner/semgrep.go`:

```go
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
			Fingerprint: ComputeFingerprint(fn, r.Start.Line, r.Extra.Lines),
		})
	}
	return findings, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run TestSemgrep 2>&1 | tail`
Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add internal/scanner/semgrep.go internal/scanner/semgrep_test.go
git commit -m "feat(scanner): semgrep adapter (temp file + json normalize)"
```

---

## Task 6: Confidence scoring

**Files:**
- Create: `internal/scanner/confidence.go`
- Test: `internal/scanner/confidence_test.go`

**Interfaces:**
- Consumes: `Severity` (Task 1).
- Produces: `Confidence(sev Severity, analyzers []string) float64`; helpers `baseConfidence`, `analyzerWeight`, `clampFloat`.

- [ ] **Step 1: Write the failing test**

```go
package scanner

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestConfidence(t *testing.T) {
	// critical, single builtin: base 0.6, no real-tool weight, no corroboration.
	if c := Confidence(SeverityCritical, []string{"builtin"}); !near(c, 0.6) {
		t.Fatalf("critical/builtin = %v want 0.6", c)
	}
	// critical, single bandit: 0.6 + 0.1 real-tool weight.
	if c := Confidence(SeverityCritical, []string{"bandit"}); !near(c, 0.7) {
		t.Fatalf("critical/bandit = %v want 0.7", c)
	}
	// medium, two analyzers: 0.4 + 0.1 + 0.25 corroboration = 0.75.
	if c := Confidence(SeverityMedium, []string{"builtin", "bandit"}); !near(c, 0.75) {
		t.Fatalf("medium/corroborated = %v want 0.75", c)
	}
	// upper clamp: critical + real tool + corroboration = 0.95, within cap.
	if c := Confidence(SeverityCritical, []string{"semgrep", "bandit"}); !near(c, 0.95) {
		t.Fatalf("critical/2-tools = %v want 0.95", c)
	}
	// never exceeds 0.99 or drops below 0.05.
	if c := Confidence(SeverityInfo, []string{"builtin"}); c < 0.05 {
		t.Fatalf("floor breached: %v", c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run TestConfidence 2>&1 | tail`
Expected: build failure — `Confidence` undefined.

- [ ] **Step 3: Write confidence.go**

Create `internal/scanner/confidence.go`:

```go
package scanner

// baseConfidence maps severity to a starting confidence.
func baseConfidence(sev Severity) float64 {
	switch sev {
	case SeverityCritical:
		return 0.6
	case SeverityHigh:
		return 0.5
	case SeverityMedium:
		return 0.4
	case SeverityLow:
		return 0.3
	default:
		return 0.2
	}
}

// analyzerWeight adds credibility when a real external tool (not the noisier
// builtin regex) reported the finding.
func analyzerWeight(analyzers []string) float64 {
	for _, a := range analyzers {
		if a == "bandit" || a == "semgrep" {
			return 0.1
		}
	}
	return 0.0
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Confidence combines severity, real-tool weight, and cross-analyzer
// corroboration into a calibrated 0.05..0.99 score.
func Confidence(sev Severity, analyzers []string) float64 {
	c := baseConfidence(sev) + analyzerWeight(analyzers)
	if len(analyzers) >= 2 {
		c += 0.25 // independent tools agreeing is the strongest cheap signal
	}
	return clampFloat(c, 0.05, 0.99)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run TestConfidence 2>&1 | tail`
Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add internal/scanner/confidence.go internal/scanner/confidence_test.go
git commit -m "feat(scanner): corroboration-based confidence scoring"
```

---

## Task 7: Engine orchestration (merge, dedupe, score, report)

**Files:**
- Create: `internal/scanner/engine.go`
- Test: `internal/scanner/engine_test.go`

**Interfaces:**
- Consumes: `Analyzer`, `Input`, `Finding`, `Report`, `SeverityRank`, `ComputeFingerprint`, `Confidence`, `NewBuiltinAnalyzer`, `NewBanditAnalyzer`, `NewSemgrepAnalyzer` (all prior tasks).
- Produces: `NewEngine(analyzers ...Analyzer) *Engine`; `DefaultEngine() *Engine`; `(*Engine).Run(ctx, Input) *Report`; internal `mergeAndScore([]Finding) []Finding`.

- [ ] **Step 1: Write the failing test**

```go
package scanner

import (
	"context"
	"errors"
	"testing"
)

type fakeAnalyzer struct {
	name      string
	available bool
	findings  []Finding
	err       error
}

func (f fakeAnalyzer) Name() string   { return f.name }
func (f fakeAnalyzer) Available() bool { return f.available }
func (f fakeAnalyzer) Analyze(ctx context.Context, in Input) ([]Finding, error) {
	return f.findings, f.err
}

func mkFinding(analyzer string, sev Severity) Finding {
	// same location/snippet across analyzers → identical fingerprint → merge.
	return Finding{
		RuleID: analyzer + "-rule", Analyzers: []string{analyzer}, Severity: sev,
		Filename: "x.py", Line: 3, Snippet: "danger()",
		Fingerprint: ComputeFingerprint("x.py", 3, "danger()"),
		Fix:         analyzer + "-fix",
	}
}

func TestEngineMergesCorroboratesAndScores(t *testing.T) {
	a := fakeAnalyzer{name: "builtin", available: true, findings: []Finding{mkFinding("builtin", SeverityMedium)}}
	b := fakeAnalyzer{name: "bandit", available: true, findings: []Finding{mkFinding("bandit", SeverityHigh)}}
	rep := NewEngine(a, b).Run(context.Background(), Input{Code: "x"})

	if len(rep.Findings) != 1 {
		t.Fatalf("expected 1 merged finding, got %d", len(rep.Findings))
	}
	f := rep.Findings[0]
	if len(f.Analyzers) != 2 {
		t.Fatalf("expected both analyzers on merged finding, got %v", f.Analyzers)
	}
	if f.Severity != SeverityHigh {
		t.Fatalf("merge must keep highest severity, got %s", f.Severity)
	}
	// high + real-tool + corroboration = 0.5 + 0.1 + 0.25 = 0.85.
	if f.Confidence < 0.84 || f.Confidence > 0.86 {
		t.Fatalf("corroborated confidence = %v want ~0.85", f.Confidence)
	}
	if len(rep.AnalyzersRun) != 2 {
		t.Fatalf("expected 2 analyzers run, got %v", rep.AnalyzersRun)
	}
}

func TestEngineIsolatesErrorsAndSkips(t *testing.T) {
	good := fakeAnalyzer{name: "builtin", available: true, findings: []Finding{mkFinding("builtin", SeverityLow)}}
	broken := fakeAnalyzer{name: "bandit", available: true, err: errors.New("tool crashed")}
	absent := fakeAnalyzer{name: "semgrep", available: false}
	rep := NewEngine(good, broken, absent).Run(context.Background(), Input{Code: "x"})

	if len(rep.Findings) != 1 {
		t.Fatalf("good analyzer's finding must survive, got %d", len(rep.Findings))
	}
	if _, ok := rep.AnalyzerErrors["bandit"]; !ok {
		t.Fatal("broken analyzer must be recorded in AnalyzerErrors")
	}
	if _, ok := rep.AnalyzersSkipped["semgrep"]; !ok {
		t.Fatal("absent analyzer must be recorded in AnalyzersSkipped")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run TestEngine 2>&1 | tail`
Expected: build failure — `NewEngine` undefined.

- [ ] **Step 3: Write engine.go**

Create `internal/scanner/engine.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ -run TestEngine 2>&1 | tail`
Expected: `PASS` for both tests.

- [ ] **Step 5: Full package test + vet + build**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/scanner/ 2>&1 | tail -3 && go vet ./internal/scanner/ 2>&1 | tail && go build ./... 2>&1 | tail; echo "build: $?"`
Expected: `ok .../internal/scanner`, no vet output, `build: 0`.

- [ ] **Step 6: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add internal/scanner/engine.go internal/scanner/engine_test.go
git commit -m "feat(scanner): engine orchestration — merge, dedupe, score, report"
```

---

## Self-Review

**Spec coverage:**
- Pluggable `Analyzer` interface → Task 2. ✓
- builtin (20 verbatim rules, always on) → Task 2. ✓
- bandit adapter (temp file, json, exit-code tolerance, python-only) → Task 4. ✓
- semgrep adapter (temp file, json, exit-code tolerance) → Task 5. ✓
- injectable Runner + toolExists + fake for tests → Task 3. ✓
- unified Finding + evidence(snippet)+Fingerprint → Task 1. ✓
- confidence: severity base + real-tool weight + corroboration + clamps → Task 6. ✓
- engine: run-available, merge, dedupe, score, Report{Run,Skipped,Errors} → Task 7. ✓
- graceful degradation (skip absent, isolate errors) → Task 7 test. ✓
- old types removed → Task 1 (`git rm scanner.go`). ✓
- Windows/no-tools: every test uses builtin or fakeRunner; no test shells out. ✓

**Placeholder scan:** none. Every code step is complete; JSON shapes are concrete.

**Type consistency:** `Finding`, `Input`, `Report`, `Analyzer`, `Runner`, `Confidence(sev, analyzers)`, `ComputeFingerprint(filename, line, snippet)`, `NewBuiltinAnalyzer`, `NewBanditAnalyzer(r)`, `NewSemgrepAnalyzer(r)`, `NewEngine`, `DefaultEngine`, `mergeAndScore`, `unionSorted` used consistently across tasks. Adapters set `Confidence` to 0; only `mergeAndScore` computes it — no double-scoring. `fakeRunner` (Task 3) and `fakeAnalyzer` (Task 7) live in test files of the same package, visible where reused.

**Spec deviation (intentional, noted):** the spec wrote `Fingerprint = hash(category+file+line+snippet)`; this plan **drops category** from the fingerprint so a line flagged by two tools that categorize differently still merges and corroborates — which is the whole point of the confidence model. The spec's fingerprint line should be updated to match. (Reviewer: accept the plan's version.)
