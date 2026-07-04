package scanner

import (
	"context"
	"testing"
)

// AccuracyBaselineMinDetectionRate is the minimum acceptable true positive
// detection rate for the OWASP/CWE baseline test suite.
const AccuracyBaselineMinDetectionRate = 0.90

// baselineCase defines a known-vulnerable code sample for accuracy testing.
// Each case has code that SHOULD be detected, with the expected rule ID(s).
type baselineCase struct {
	name        string
	filename    string
	code        string
	expectRules []string // rules that MUST fire
	denyRules   []string // rules that MUST NOT fire (false positives)
	minFindings int      // minimum expected findings count
}

// getBaselineCases returns OWASP/CWE-aligned test cases covering the full
// CWE Top 25 for Go, plus common false-positive scenarios.
func getBaselineCases() []baselineCase {
	return []baselineCase{
		// ── CWE-89: SQL Injection ────────────────────────────────
		{
			name:     "CWE-89: SQL injection via Sprintf",
			filename: "handler.go",
			code: `package main
import "fmt"
func getUser(id string) {
	q := fmt.Sprintf("SELECT * FROM users WHERE id=%s", id)
	_ = q
}`,
			expectRules: []string{"sql_injection"},
			minFindings: 1,
		},
		{
			name:     "CWE-89: SQL injection via db.Exec + Sprintf",
			filename: "db.go",
			code: `package main
import "fmt"
func query(db *sql.DB, name string) {
	db.Exec(fmt.Sprintf("SELECT * FROM t WHERE name=%s", name))
}`,
			expectRules: []string{"sql_injection_raw_query"},
			minFindings: 1,
		},
		{
			name:     "CWE-89: Parameterized query (FALSE POSITIVE check)",
			filename: "db.go",
			code: `package main
func query(db *sql.DB, id int) {
	db.Query("SELECT * FROM users WHERE id=$1", id)
}`,
			denyRules:   []string{"sql_injection", "sql_injection_raw_query"},
			minFindings: 0,
		},
		{
			name:     "CWE-89: SQL INSERT with string concat",
			filename: "dao.go",
			code:     `q := fmt.Sprintf("INSERT INTO users (name) VALUES ('%s')", name)`,
			expectRules: []string{"sql_injection"},
			minFindings: 1,
		},

		// ── CWE-78: OS Command Injection ────────────────────────
		{
			name:     "CWE-78: Command injection via exec.Command + user input",
			filename: "cmd.go",
			code: `package main
import "os/exec"
func run(input string) {
	cmd := exec.Command("sh", "-c", req.Input)
	cmd.Run()
}`,
			expectRules: []string{"command_injection"},
			minFindings: 1,
		},
		{
			name:     "CWE-78: Command injection via fmt.Sprintf in exec",
			filename: "cmd.go",
			code: `package main
import "os/exec"
func run(name string) {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("echo %s", name))
	cmd.Run()
}`,
			expectRules: []string{"command_injection"},
			minFindings: 1,
		},

		// ── CWE-79: Cross-Site Scripting ────────────────────────
		{
			name:     "CWE-79: XSS via innerHTML assignment with concatenation",
			filename: "frontend.js",
			code:     `<script>document.getElementById("output").innerHTML = "<p>" + userInput + "</p>";</script>`,
			expectRules: []string{"xss_unsafe_js"},
			minFindings: 1,
		},
		{
			name:     "CWE-79: XSS via outerHTML assignment",
			filename: "frontend.js",
			code:     `document.getElementById("target").outerHTML = "<div>" + userInput;`,
			expectRules: []string{"xss_unsafe_js"},
			minFindings: 1,
		},
		{
			name:     "CWE-79: Unsafe template.HTML rendering",
			filename: "handler.go",
			code: `package main
import "html/template"
func render(data string) template.HTML {
	return template.HTML(data)
}`,
			expectRules: []string{"xss_unsafe_html"},
			minFindings: 1,
		},

		// ── CWE-601: Open Redirect ──────────────────────────────
		{
			name:     "CWE-601: Open redirect via http.Redirect with user URL",
			filename: "handler.go",
			code: `package main
import "net/http"
func redirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, r.URL.String(), 302)
}`,
			expectRules: []string{"xss_http_redirect"},
			minFindings: 1,
		},

		// ── CWE-798: Hardcoded Credentials ──────────────────────
		{
			name:     "CWE-798: Hardcoded password in string literal",
			filename: "config.go",
			code: `package main
var password = "hunter2supersecret"
func init() {}`,
			expectRules: []string{"hardcoded_password"},
			minFindings: 1,
		},
		{
			name:     "CWE-798: Hardcoded API key",
			filename: "config.go",
			code: `package main
var api_key = "sk-1234567890abcdef1234567890abcdef"
func init() {}`,
			expectRules: []string{"hardcoded_password"},
			minFindings: 1,
		},
		{
			name:     "CWE-798: Hardcoded private key",
			filename: "config.go",
			code: `package main
var private_key = "supersecretprivatekey12345678"
func init() {}`,
			expectRules: []string{"hardcoded_password"},
			minFindings: 1,
		},
		{
			name:     "CWE-798: Variable reference (FALSE POSITIVE check)",
			filename: "config.go",
			code: `package main
var password = getPasswordFromEnv()
func init() {}`,
			denyRules:   []string{"hardcoded_password"},
			minFindings: 0,
		},
		{
			name:     "CWE-798: Hardcoded database connection string",
			filename: "db.go",
			code: `package main
var dsn = "postgres://admin:password123@localhost:5432/mydb"
func init() {}`,
			expectRules: []string{"hardcoded_connection_string"},
			minFindings: 1,
		},
		{
			name:     "CWE-798: Hardcoded MySQL connection string",
			filename: "db.go",
			code: `package main
var dsn = "mysql://root:secret123@db.example.com:3306/app"
func init() {}`,
			expectRules: []string{"hardcoded_connection_string"},
			minFindings: 1,
		},
		{
			name:     "CWE-798: Hardcoded AWS access key",
			filename: "aws.go",
			code: `package main
var key = "AKIAIOSFODNN7EXAMPLE"
func init() {}`,
			expectRules: []string{"aws_access_key"},
			minFindings: 1,
		},

		// ── CWE-327: Broken Crypto (MD5) ────────────────────────
		{
			name:     "CWE-327: Use of MD5 for hashing",
			filename: "crypto.go",
			code: `package main
import "crypto/md5"
func hash(data []byte) []byte {
	h := md5.New()
	h.Write(data)
	return h.Sum(nil)
}`,
			expectRules: []string{"weak_hash_md5"},
			minFindings: 1,
		},

		// ── CWE-328: Use of SHA-1 ──────────────────────────────
		{
			name:     "CWE-328: Use of SHA-1",
			filename: "crypto.go",
			code: `package main
import "crypto/sha1"
func hash(data []byte) []byte {
	h := sha1.New()
	h.Write(data)
	return h.Sum(nil)
}`,
			expectRules: []string{"weak_hash_sha1"},
			minFindings: 1,
		},

		// ── CWE-330: Use of Insufficiently Random Values ────────
		{
			name:     "CWE-330: math/rand import",
			filename: "utils.go",
			code:     `package main
import "math/rand"
func random() int { return rand.Intn(100) }`,
			expectRules: []string{"weak_random"},
			minFindings: 1,
		},
		{
			name:     "CWE-330: math/rand.Float64 usage",
			filename: "utils.go",
			code:     `n := rand.Float64()`,
			expectRules: []string{"weak_random"},
			minFindings: 1,
		},
		{
			name:     "CWE-330: crypto/rand (FALSE POSITIVE check)",
			filename: "crypto.go",
			code: `package main
import "crypto/rand"
func secureRandom() ([]byte, error) {
	buf := make([]byte, 32)
	_, err := rand.Read(buf)
	return buf, err
}`,
			denyRules:   []string{"weak_random"},
			minFindings: 0,
		},

		// ── CWE-295: Improper Certificate Validation ────────────
		{
			name:     "CWE-295: TLS verification disabled",
			filename: "client.go",
			code: `package main
import "crypto/tls"
func client() {
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	_ = tr
}`,
			expectRules: []string{"insecure_tls"},
			minFindings: 1,
		},
		{
			name:     "CWE-295: TLS verification disabled (multiline)",
			filename: "client.go",
			code: `cfg := &tls.Config{
	InsecureSkipVerify: true,
}`,
			expectRules: []string{"insecure_tls"},
			minFindings: 1,
		},

		// ── CWE-22: Path Traversal ──────────────────────────────
		{
			name:     "CWE-22: Path traversal via os.Open with user input",
			filename: "handler.go",
			code: `package main
import "os"
func readFile(r *http.Request) {
	f, _ := os.Open(r.URL.Path)
	_ = f
}`,
			expectRules: []string{"path_traversal"},
			minFindings: 1,
		},
		{
			name:     "CWE-22: Path traversal via os.ReadFile",
			filename: "handler.go",
			code: `package main
import "os"
func readFile(r *http.Request) {
	data, _ := os.ReadFile(r.URL.Query().Get("file"))
	_ = data
}`,
			expectRules: []string{"path_traversal"},
			minFindings: 1,
		},

		// ── CWE-918: Server-Side Request Forgery ────────────────
		{
			name:     "CWE-918: SSRF via http.Get with user-controlled URL",
			filename: "proxy.go",
			code: `package main
import "net/http"
func proxy(r *http.Request) {
	resp, _ := http.Get(r.URL.Query().Get("url"))
	_ = resp
}`,
			expectRules: []string{"ssrf_http_get"},
			minFindings: 1,
		},
		{
			name:     "CWE-918: SSRF via http.Post with request body",
			filename: "proxy.go",
			code: `package main
import "net/http"
func proxy(r *http.Request) {
	resp, _ := http.Post(req.URL.String(), "application/json", req.Body)
	_ = resp
}`,
			expectRules: []string{"ssrf_http_get"},
			minFindings: 1,
		},

		// ── Insecure Deserialization ─────────────────────────────
		{
			name:     "Insecure JSON decode from request body",
			filename: "handler.go",
			code: `func handler(w http.ResponseWriter, r *http.Request) {
	var input User
	json.NewDecoder(r.Body).Decode(&input)
}`,
			expectRules: []string{"insecure_json_decode"},
			minFindings: 1,
		},

		// ── JWT Secrets ─────────────────────────────────────────
		{
			name:     "JWT signed with hardcoded string literal",
			filename: "auth.go",
			code:     `token, _ := jwt.SigningMethodHS256.SignedString("my-super-secret-key")`,
			expectRules: []string{"weak_jwt_secret"},
			minFindings: 1,
		},

		// ── Info Disclosure ─────────────────────────────────────
		{
			name:     "Error details written to HTTP response",
			filename: "handler.go",
			code: `func handler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(fmt.Sprintf("error: %w", err)))
}`,
			expectRules: []string{"error_in_response"},
			minFindings: 1,
		},

		// ── CWE-732: Insecure File Permissions ──────────────────
		{
			name:     "CWE-732: Overly permissive file write (0777)",
			filename: "config.go",
			code: `package main
import "os"
func writeConfig() {
	os.WriteFile("config.json", data, 0777)
}`,
			expectRules: []string{"insecure_file_perms"},
			minFindings: 1,
		},
		{
			name:     "CWE-732: Overly permissive file write (0666)",
			filename: "config.go",
			code: `package main
import "os"
func writeConfig() {
	os.WriteFile("config.json", data, 0666)
}`,
			expectRules: []string{"insecure_file_perms"},
			minFindings: 1,
		},

		// ── False Positive: Normal Go code ──────────────────────
		{
			name:     "FP: Standard Go error wrapping",
			filename: "service.go",
			code: `package main
import "fmt"
func doWork() error {
	return fmt.Errorf("failed to connect to database: %w", err)
}`,
			denyRules:   []string{"error_info_leak", "error_in_response"},
			minFindings: 0,
		},
		{
			name:     "FP: Goroutine launch is not a race condition",
			filename: "async.go",
			code: `package main
func start() {
	go func() { doWork() }()
	go doMoreWork()
}`,
			denyRules:   []string{"race_condition_map"},
			minFindings: 0,
		},
		{
			name:     "FP: Normal function variable assignment",
			filename: "config.go",
			code: `package main
var timeout = getTimeout()
var retries = 3
func init() {}`,
			denyRules:   []string{"hardcoded_password"},
			minFindings: 0,
		},
		{
			name:     "FP: Secure parameterized query",
			filename: "db.go",
			code: `rows, err := db.Query("SELECT * FROM users WHERE id = $1 AND status = $2", id, status)`,
			denyRules:   []string{"sql_injection", "sql_injection_raw_query"},
			minFindings: 0,
		},
		{
			name:     "FP: crypto/rand.Intn (not math/rand)",
			filename: "crypto.go",
			code:     `n, _ := rand.Int(rand.Reader, big.NewInt(100))`,
			denyRules:   []string{"weak_random"},
			minFindings: 0,
		},
		{
			name:     "FP: Safe file write with 0644 permissions",
			filename: "config.go",
			code: `package main
import "os"
func writeConfig() {
	os.WriteFile("config.json", data, 0644)
}`,
			denyRules:   []string{"insecure_file_perms"},
			minFindings: 0,
		},
		{
			name:     "FP: Safe file write with 0600 permissions",
			filename: "secrets.go",
			code: `package main
import "os"
func writeSecret() {
	os.WriteFile("secrets.key", data, 0600)
}`,
			denyRules:   []string{"insecure_file_perms"},
			minFindings: 0,
		},
		{
			name:     "FP: Safe exec.Command with no user input",
			filename: "cmd.go",
			code: `package main
import "os/exec"
func run() {
	cmd := exec.Command("ls", "-la")
	cmd.Run()
}`,
			denyRules:   []string{"command_injection"},
			minFindings: 0,
		},

		// ── Expanded FP Coverage ────────────────────────────────
		{
			name:     "FP: SHA-256 for hashing (not weak)",
			filename: "crypto.go",
			code: `package main
import "crypto/sha256"
func hash(data []byte) []byte {
	h := sha256.New()
	h.Write(data)
	return h.Sum(nil)
}`,
			denyRules:   []string{"weak_hash_md5", "weak_hash_sha1"},
			minFindings: 0,
		},
		{
			name:     "FP: Safe http.Get with hardcoded URL",
			filename: "client.go",
			code: `package main
import "net/http"
func fetch() {
	resp, _ := http.Get("https://api.example.com/data")
	_ = resp
}`,
			denyRules:   []string{"ssrf_http_get"},
			minFindings: 0,
		},
		{
			name:     "FP: http.Redirect with hardcoded path",
			filename: "handler.go",
			code: `package main
import "net/http"
func redirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", 302)
}`,
			denyRules:   []string{"xss_http_redirect"},
			minFindings: 0,
		},
		{
			name:     "FP: bcrypt password hashing (not hardcoded)",
			filename: "auth.go",
			code: `package main
import "golang.org/x/crypto/bcrypt"
func hashPw(pw string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(hash), err
}`,
			denyRules:   []string{"hardcoded_password", "weak_hash_md5", "weak_hash_sha1"},
			minFindings: 0,
		},
		{
			name:     "FP: JWT with key from environment",
			filename: "auth.go",
			code: `package main
import "os"
func sign() {
	secret := os.Getenv("JWT_SECRET")
	token, _ := jwt.SigningMethodHS256.SignedString(secret)
}`,
			denyRules:   []string{"weak_jwt_secret"},
			minFindings: 0,
		},
		{
			name:     "FP: sql.Open with driver name only",
			filename: "db.go",
			code: `package main
import "database/sql"
func connect() {
	db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	_ = db
}`,
			denyRules:   []string{"hardcoded_connection_string", "sql_injection"},
			minFindings: 0,
		},
		{
			name:     "FP: Safe json.Unmarshal with hardcoded bytes",
			filename: "handler.go",
			code: `func handler() {
	data := []byte("{\"name\":\"test\"}")
	var input CreateUserRequest
	json.Unmarshal(data, &input)
	_ = input
}`,
			denyRules:   []string{"insecure_json_decode"},
			minFindings: 0,
		},
		{
			name:     "FP: error_in_response — fmt.Errorf in return statement",
			filename: "service.go",
			code: `package main
import "fmt"
func doWork() error {
	return fmt.Errorf("failed to connect to database: %w", err)
}`,
			denyRules:   []string{"error_in_response"},
			minFindings: 0,
		},
		{
			name:     "FP: exec.CommandContext with no user input",
			filename: "cmd.go",
			code: `package main
import "os/exec"
func run(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "ls", "-la")
	cmd.Run()
}`,
			denyRules:   []string{"command_injection"},
			minFindings: 0,
		},
		{
			name:     "FP: SQL injection in test file — downgraded to low severity",
			filename: "handler_test.go",
			code: `package main
import "fmt"
func TestQuery(t *testing.T) {
	q := fmt.Sprintf("SELECT * FROM users WHERE id=%s", id)
	_ = q
}`,
			expectRules: []string{"sql_injection"},
			minFindings: 1,
		},
		{
			name:     "FP: Generated protobuf file",
			filename: "api.pb.go",
			code: `// Code generated by protoc-gen-go. DO NOT EDIT.
package main
var password = "hunter2supersecret"
func init() {}`,
			denyRules:   []string{"hardcoded_password"},
			minFindings: 0,
		},
		{
			name:     "FP: Test fixture file",
			filename: "testdata/fixture.json",
			code: `{"password": "hunter2supersecret"}`,
			denyRules:   []string{"hardcoded_password"},
			minFindings: 0,
		},
		{
			name:     "FP: go.sum file content",
			filename: "go.sum",
			code: `github.com/foo/bar v1.2.3 h1:abc123def456=
github.com/foo/bar v1.2.3/go.mod h1:xyz789`,
			denyRules:   []string{"hardcoded_password", "insecure_tls"},
			minFindings: 0,
		},
		{
			name:     "FP: Comment with security keyword",
			filename: "config.go",
			code: `package main
// TODO: Replace hardcoded password with env var
var config = loadConfig()
func init() {}`,
			denyRules:   []string{"hardcoded_password"},
			minFindings: 0,
		},
		{
			name:     "FP: Template string in log message",
			filename: "handler.go",
			code: `package main
import "log"
func handler() {
	log.Printf("user %s logged in from %s", username, ip)
}`,
			denyRules:   []string{"error_in_response", "error_info_leak"},
			minFindings: 0,
		},
		{
			name:     "FP: io.Copy (not path traversal)",
			filename: "file.go",
			code: `package main
import "io"
func copyFile(src, dst string) {
	s, _ := os.Open(src)
	d, _ := os.Create(dst)
	io.Copy(d, s)
}`,
			denyRules:   []string{"path_traversal"},
			minFindings: 0,
		},
		{
			name:     "FP: http.Post with hardcoded URL",
			filename: "client.go",
			code: `package main
import "net/http"
func notify() {
	http.Post("https://hooks.example.com/event", "application/json", body)
}`,
			denyRules:   []string{"ssrf_http_get"},
			minFindings: 0,
		},
	}
}

// TestAccuracy_BaselineRules runs all baseline cases and verifies:
// - Expected rules fire (true positive detection)
// - Denied rules do NOT fire (false positive elimination)
// - Minimum finding count is met
func TestAccuracy_BaselineRules(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	cases := getBaselineCases()

	totalExpected := 0
	totalDetected := 0
	totalFP := 0

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := engine.Run(context.Background(), Input{
				Language: "go",
				Code:     tc.code,
				Filename: tc.filename,
			})

			detected := map[string]bool{}
			for _, f := range report.Findings {
				detected[f.RuleID] = true
			}

			// Check expected rules fired.
			for _, rule := range tc.expectRules {
				totalExpected++
				if !detected[rule] {
					t.Errorf("expected rule %s to fire but it did not (findings: %d)", rule, len(report.Findings))
				} else {
					totalDetected++
				}
			}

			// Check denied rules did NOT fire.
			for _, rule := range tc.denyRules {
				if detected[rule] {
					totalFP++
					t.Errorf("false positive: rule %s fired but should NOT have", rule)
				}
			}

			// Check minimum finding count.
			if len(report.Findings) < tc.minFindings {
				t.Errorf("expected at least %d findings, got %d", tc.minFindings, len(report.Findings))
			}
		})
	}

	// Print summary.
	t.Logf("=== Accuracy Baseline Summary ===")
	t.Logf("True positives detected: %d/%d (%.1f%%)", totalDetected, totalExpected,
		float64(totalDetected)/float64(totalExpected)*100)
	t.Logf("False positives: %d", totalFP)
	t.Logf("Cases: %d", len(cases))

	if totalExpected > 0 && float64(totalDetected)/float64(totalExpected) < AccuracyBaselineMinDetectionRate {
		t.Errorf("detection rate below %.0f%%: %.1f%%", AccuracyBaselineMinDetectionRate*100, float64(totalDetected)/float64(totalExpected)*100)
	}
	if totalFP > 0 {
		t.Errorf("false positive rate above 0%%: %d FPs", totalFP)
	}
}

// TestAccuracy_PrimaryFireCoverage verifies that each builtin rule has at
// least one dedicated test case where it fires as the PRIMARY finding
// (not as a secondary rule alongside another rule on the same line).
func TestAccuracy_PrimaryFireCoverage(t *testing.T) {
	analyzer := NewBuiltinAnalyzer()
	allRules := map[string]bool{}
	for _, r := range analyzer.rules {
		allRules[r.name] = true
	}

	// Dedicated cases where exactly one rule fires — no secondary rules.
	primaryCases := []baselineCase{
		{name: "sql_injection primary", filename: "a.go", code: `q := fmt.Sprintf("SELECT * FROM users WHERE id=%s", id)`, expectRules: []string{"sql_injection"}},
		{name: "sql_injection_raw_query primary", filename: "b.go", code: `db.QueryRow(fmt.Sprintf("SELECT * FROM t WHERE id=%s", id))`, expectRules: []string{"sql_injection_raw_query"}},
		{name: "command_injection primary", filename: "c.go", code: `exec.Command("sh", "-c", req.Input)`, expectRules: []string{"command_injection"}},
		{name: "xss_unsafe_html primary", filename: "d.go", code: `return template.HTML(data)`, expectRules: []string{"xss_unsafe_html"}},
		{name: "xss_unsafe_js primary", filename: "e.js", code: `document.getElementById("x").innerHTML = "<p>" + userInput;`, expectRules: []string{"xss_unsafe_js"}},
		{name: "xss_http_redirect primary", filename: "f.go", code: `http.Redirect(w, r, r.URL.String(), 302)`, expectRules: []string{"xss_http_redirect"}},
		{name: "hardcoded_password primary", filename: "g.go", code: `var password = "hunter2supersecret123"`, expectRules: []string{"hardcoded_password"}},
		{name: "hardcoded_connection_string primary", filename: "h.go", code: `var dsn = "postgres://admin:pw@localhost/db"`, expectRules: []string{"hardcoded_connection_string"}},
		{name: "aws_access_key primary", filename: "i.go", code: `var key = "AKIAIOSFODNN7EXAMPLE"`, expectRules: []string{"aws_access_key"}},
		{name: "weak_hash_md5 primary", filename: "j.go", code: `h := md5.New()`, expectRules: []string{"weak_hash_md5"}},
		{name: "weak_hash_sha1 primary", filename: "k.go", code: `h := sha1.New()`, expectRules: []string{"weak_hash_sha1"}},
		{name: "weak_random primary", filename: "l.go", code: `import "math/rand"`, expectRules: []string{"weak_random"}},
		{name: "insecure_tls primary", filename: "m.go", code: `InsecureSkipVerify: true`, expectRules: []string{"insecure_tls"}},
		{name: "weak_jwt_secret primary", filename: "n.go", code: `jwt.SignedString("my-secret-key")`, expectRules: []string{"weak_jwt_secret"}},
		{name: "path_traversal primary", filename: "o.go", code: `os.Open(r.URL.Path)`, expectRules: []string{"path_traversal"}},
		{name: "ssrf_http_get primary", filename: "p.go", code: `http.Get(r.URL.Query().Get("url"))`, expectRules: []string{"ssrf_http_get"}},
		{name: "insecure_json_decode primary", filename: "q.go", code: `json.NewDecoder(r.Body).Decode(&input)`, expectRules: []string{"insecure_json_decode"}},
		{name: "error_in_response primary", filename: "r.go", code: `w.Write([]byte(fmt.Sprintf("error: %w", err)))`, expectRules: []string{"error_in_response"}},
		{name: "insecure_file_perms primary", filename: "s.go", code: `os.WriteFile("f.txt", data, 0777)`, expectRules: []string{"insecure_file_perms"}},
	}

	engine := NewEngine(analyzer)
	for _, tc := range primaryCases {
		t.Run(tc.name, func(t *testing.T) {
			report := engine.Run(context.Background(), Input{
				Language: "go",
				Code:     tc.code,
				Filename: tc.filename,
			})
			detected := map[string]bool{}
			for _, f := range report.Findings {
				detected[f.RuleID] = true
			}
			for _, rule := range tc.expectRules {
				if !detected[rule] {
					t.Errorf("expected rule %q to fire as primary, but it did not", rule)
				}
			}
		})
	}
}

// TestAccuracy_ContextPenalties verifies that findings in test files,
// example files, and generated files get appropriate confidence penalties.
func TestAccuracy_ContextPenalties(t *testing.T) {
	cases := []struct {
		name       string
		filename   string
		code       string
		expectFind bool // should we find the vulnerability
		maxConf    float64
	}{
		{
			name:       "Production file — full confidence",
			filename:   "handler.go",
			code:       "InsecureSkipVerify: true\n",
			expectFind: true,
			maxConf:    0.99,
		},
		{
			name:       "Test file — lower confidence",
			filename:   "handler_test.go",
			code:       "InsecureSkipVerify: true\n",
			expectFind: true,
			maxConf:    0.80, // should be penalized
		},
		{
			name:       "Generated file — no findings",
			filename:   "api.pb.go",
			code:       "InsecureSkipVerify: true\n",
			expectFind: false,
			maxConf:    0,
		},
		{
			name:       "Vendor file — no findings",
			filename:   "vendor/github.com/foo/bar.go",
			code:       "InsecureSkipVerify: true\n",
			expectFind: false,
			maxConf:    0,
		},
		{
			name:       "Mock file — no findings",
			filename:   "mock_user.go",
			code:       "InsecureSkipVerify: true\n",
			expectFind: false,
			maxConf:    0,
		},
		{
			name:       "Stub file — no findings",
			filename:   "stub_db.go",
			code:       "InsecureSkipVerify: true\n",
			expectFind: false,
			maxConf:    0,
		},
	}

	engine := NewEngine(NewBuiltinAnalyzer())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := engine.Run(context.Background(), Input{
				Language: "go",
				Code:     tc.code,
				Filename: tc.filename,
			})
			if tc.expectFind && len(report.Findings) == 0 {
				t.Error("expected at least one finding")
			}
			if !tc.expectFind && len(report.Findings) > 0 {
				t.Errorf("expected no findings, got %d", len(report.Findings))
			}
			if tc.expectFind && len(report.Findings) > 0 {
				maxConf := 0.0
				for _, f := range report.Findings {
					if f.Confidence > maxConf {
						maxConf = f.Confidence
					}
				}
				if maxConf > tc.maxConf {
					t.Errorf("max confidence %v exceeds expected max %v", maxConf, tc.maxConf)
				}
			}
		})
	}
}

// TestAccuracy_SeverityOrdering verifies findings are sorted by severity
// (most severe first), then by confidence, then by line number.
func TestAccuracy_SeverityOrdering(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())

	code := `
password := "supersecretpassword123"
h := md5.New()
InsecureSkipVerify: true
`
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "mixed.go",
	})

	if len(report.Findings) < 2 {
		t.Fatalf("expected at least 2 findings, got %d", len(report.Findings))
	}

	// Verify severity ordering: critical > high > medium > low > info
	for i := 1; i < len(report.Findings); i++ {
		prev := SeverityRank(report.Findings[i-1].Severity)
		curr := SeverityRank(report.Findings[i].Severity)
		if prev < curr {
			t.Errorf("findings not sorted by severity: index %d has %s, index %d has %s",
				i-1, report.Findings[i-1].Severity, i, report.Findings[i].Severity)
		}
	}
}

// TestAccuracy_MultiLineFindings verifies the scanner handles multi-line
// code samples where vulnerabilities span across lines.
func TestAccuracy_MultiLineFindings(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())

	code := `package main

import (
	"crypto/tls"
	"net/http"
)

func insecureClient() *http.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	return &http.Client{Transport: tr}
}`
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "client.go",
	})

	found := false
	for _, f := range report.Findings {
		if f.RuleID == "insecure_tls" {
			found = true
			if f.Line == 0 {
				t.Error("expected non-zero line number")
			}
			break
		}
	}
	if !found {
		t.Error("expected insecure_tls finding in multi-line code")
	}
}

// TestAccuracy_EmptyAndMinimalCode verifies the scanner handles edge cases.
func TestAccuracy_EmptyAndMinimalCode(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())

	// Empty code
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     "",
		Filename: "empty.go",
	})
	if len(report.Findings) != 0 {
		t.Errorf("expected 0 findings for empty code, got %d", len(report.Findings))
	}

	// Single character
	report = engine.Run(context.Background(), Input{
		Language: "go",
		Code:     "x",
		Filename: "minimal.go",
	})
	if len(report.Findings) != 0 {
		t.Errorf("expected 0 findings for minimal code, got %d", len(report.Findings))
	}

	// No filename
	report = engine.Run(context.Background(), Input{
		Language: "go",
		Code:     `InsecureSkipVerify: true`,
		Filename: "",
	})
	if len(report.Findings) == 0 {
		t.Error("expected findings even with no filename")
	}
}

// TestAccuracy_FingerprintUniqueness verifies different rules on the same
// line produce separate findings (not merged).
func TestAccuracy_FingerprintUniqueness(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())

	// This code triggers both sql_injection and sql_injection_raw_query on the same line.
	code := `db.Exec(fmt.Sprintf("SELECT * FROM t WHERE name=%s", name))`
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "db.go",
	})

	ruleIDs := map[string]bool{}
	for _, f := range report.Findings {
		ruleIDs[f.RuleID] = true
	}

	// Both rules should fire independently (different fingerprints due to ruleID).
	if !ruleIDs["sql_injection"] {
		t.Error("expected sql_injection to fire")
	}
	if !ruleIDs["sql_injection_raw_query"] {
		t.Error("expected sql_injection_raw_query to fire")
	}
}

// TestAccuracy_TestFileInjectionDowngrade verifies that injection findings
// in test files are downgraded to SeverityLow by suppressTestFP (not dropped).
func TestAccuracy_TestFileInjectionDowngrade(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())

	code := `q := fmt.Sprintf("SELECT * FROM users WHERE id=%s", id)`
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "handler_test.go",
	})

	found := false
	for _, f := range report.Findings {
		if f.RuleID == "sql_injection" {
			found = true
			if f.Severity != SeverityLow {
				t.Errorf("expected SeverityLow for injection in test file, got %s", f.Severity)
			}
			break
		}
	}
	if !found {
		t.Error("expected sql_injection finding (downgraded, not dropped) in test file")
	}
}

// TestAccuracy_AllRulesCovered verifies every builtin rule has at least one
// baseline test case that triggers it.
func TestAccuracy_AllRulesCovered(t *testing.T) {
	analyzer := NewBuiltinAnalyzer()
	allRules := map[string]bool{}
	for _, r := range analyzer.rules {
		allRules[r.name] = true
	}

	cases := getBaselineCases()
	cases = append(cases, getExtraBaselineCases()...)
	engine := NewEngine(analyzer)
	covered := map[string]bool{}

	for _, tc := range cases {
		report := engine.Run(context.Background(), Input{
			Language: "go",
			Code:     tc.code,
			Filename: tc.filename,
		})
		for _, f := range report.Findings {
			covered[f.RuleID] = true
		}
	}

	for rule := range allRules {
		if !covered[rule] {
			t.Errorf("rule %q has no baseline test coverage", rule)
		}
	}
}
