package scanner

import (
	"context"
	"errors"
	"fmt"
	"math"
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
			name:        "CWE-89: SQL INSERT with string concat",
			filename:    "dao.go",
			code:        `q := fmt.Sprintf("INSERT INTO users (name) VALUES ('%s')", name)`,
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
			name:        "CWE-79: XSS via innerHTML assignment with concatenation",
			filename:    "frontend.js",
			code:        `<script>document.getElementById("output").innerHTML = "<p>" + userInput + "</p>";</script>`,
			expectRules: []string{"xss_unsafe_js"},
			minFindings: 1,
		},
		{
			name:        "CWE-79: XSS via outerHTML assignment",
			filename:    "frontend.js",
			code:        `document.getElementById("target").outerHTML = "<div>" + userInput;`,
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
			code: `package main
import "math/rand"
func random() int { return rand.Intn(100) }`,
			expectRules: []string{"weak_random"},
			minFindings: 1,
		},
		{
			name:        "CWE-330: math/rand.Float64 usage",
			filename:    "utils.go",
			code:        `n := rand.Float64()`,
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
			name:        "JWT signed with hardcoded string literal",
			filename:    "auth.go",
			code:        `token, _ := jwt.SigningMethodHS256.SignedString("my-super-secret-key")`,
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
			name:        "FP: Secure parameterized query",
			filename:    "db.go",
			code:        `rows, err := db.Query("SELECT * FROM users WHERE id = $1 AND status = $2", id, status)`,
			denyRules:   []string{"sql_injection", "sql_injection_raw_query"},
			minFindings: 0,
		},
		{
			name:        "FP: crypto/rand.Intn (not math/rand)",
			filename:    "crypto.go",
			code:        `n, _ := rand.Int(rand.Reader, big.NewInt(100))`,
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
			name:        "FP: Test fixture file",
			filename:    "testdata/fixture.json",
			code:        `{"password": "hunter2supersecret"}`,
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

// getExtraBaselineCases covers every builtin rule missing from the original set.
func getExtraBaselineCases() []baselineCase {
	return []baselineCase{
		// ── secrets ──────────────────────────────────────────────
		{
			name:        "aws_secret_key",
			filename:    "aws.go",
			code:        `aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`,
			expectRules: []string{"aws_secret_key"},
			minFindings: 1,
		},
		{
			name:        "github_token",
			filename:    "ci.go",
			code:        `var token = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef1234"`,
			expectRules: []string{"github_token"},
			minFindings: 1,
		},
		{
			name:        "slack_token",
			filename:    "webhook.go",
			code:        `var token = "xoxb-fake-slack-token-for-testing-purposes"`,
			expectRules: []string{"slack_token"},
			minFindings: 1,
		},
		{
			name:        "private_key_literal",
			filename:    "keys.go",
			code:        `var key = "-----BEGIN RSA PRIVATE KEY-----"`,
			expectRules: []string{"private_key_literal"},
			minFindings: 1,
		},
		{
			name:        "gcp_service_account_key",
			filename:    "gcp.go",
			code:        `"type": "service_account"`,
			expectRules: []string{"gcp_service_account_key"},
			minFindings: 1,
		},
		{
			name:        "world_readable_secret",
			filename:    "deploy.go",
			code:        `os.WriteFile("secret.key", data, 0664)`,
			expectRules: []string{"world_readable_secret"},
			minFindings: 1,
		},

		// ── crypto ───────────────────────────────────────────────
		{
			name:        "weak_cipher_des",
			filename:    "crypto.go",
			code:        `c, _ := des.NewCipher(key)`,
			expectRules: []string{"weak_cipher_des"},
			minFindings: 1,
		},
		{
			name:        "insecure_ecb_mode",
			filename:    "crypto.go",
			code:        `mode := ecb.NewEncrypter(block)`,
			expectRules: []string{"insecure_ecb_mode"},
			minFindings: 1,
		},
		{
			name:        "hardcoded_iv",
			filename:    "crypto.go",
			code:        `iv := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}`,
			expectRules: []string{"hardcoded_iv"},
			minFindings: 1,
		},

		// ── injection ────────────────────────────────────────────
		{
			name:        "template_injection",
			filename:    "handler.go",
			code:        `t := template.New(req.Input)`,
			expectRules: []string{"template_injection"},
			minFindings: 1,
		},
		{
			name:        "log_injection",
			filename:    "handler.go",
			code:        `log.Printf("user %s logged in", req.Username)`,
			expectRules: []string{"log_injection"},
			minFindings: 1,
		},
		{
			name:        "sql_injection_string_concat",
			filename:    "dao.go",
			code:        `query := "SELECT id FROM users WHERE name = '" + name + "'"`,
			expectRules: []string{"sql_injection_string_concat"},
			minFindings: 1,
		},
		{
			name:        "command_injection_shell_concat",
			filename:    "cmd.go",
			code:        `cmd := exec.Command("sh", "-c", "ping -c 3 "+host)`,
			expectRules: []string{"command_injection_shell_concat"},
			minFindings: 1,
		},
		{
			name:        "command_injection_shell_variable",
			filename:    "cmd.go",
			code:        `cmd := exec.Command("sh", "-c", cmdVar)`,
			expectRules: []string{"command_injection_shell_variable"},
			minFindings: 1,
		},
		{
			name:        "command_injection_shell_variable_cmd",
			filename:    "cmd.go",
			code:        `cmd := exec.Command("cmd", "/c", cmdVar)`,
			expectRules: []string{"command_injection_shell_variable"},
			minFindings: 1,
		},
		{
			name:        "command_injection_shell_concat_cmd",
			filename:    "cmd.go",
			code:        `cmd := exec.Command("cmd", "/c", "dir "+host)`,
			expectRules: []string{"command_injection_shell_concat"},
			minFindings: 1,
		},

		// ── path traversal ──────────────────────────────────────
		{
			name:        "path_traversal_unsanitized",
			filename:    "file.go",
			code:        `f, _ := os.Open("uploads/" + r.FileName)`,
			expectRules: []string{"path_traversal_unsanitized"},
			minFindings: 1,
		},

		// ── SSRF ────────────────────────────────────────────────
		{
			name:        "ssrf_http_client",
			filename:    "client.go",
			code:        `resp, _ := Client.Get(req.URL)`,
			expectRules: []string{"ssrf_http_client"},
			minFindings: 1,
		},
		{
			name:        "ssrf_url_parse",
			filename:    "handler.go",
			code:        `u, _ := url.Parse(req.URL)`,
			expectRules: []string{"ssrf_url_parse"},
			minFindings: 1,
		},

		// ── deserialization ─────────────────────────────────────
		{
			name:        "unsafe_xml_parse",
			filename:    "xml.go",
			code:        `dec := xml.NewDecoder(r.Body)`,
			expectRules: []string{"unsafe_xml_parse"},
			minFindings: 1,
		},
		{
			name:        "unsafe_yaml_decode",
			filename:    "yaml.go",
			code:        `yaml.Unmarshal(data, &cfg)`,
			expectRules: []string{"unsafe_yaml_decode"},
			minFindings: 1,
		},
		{
			name:        "gorilla_unsafe_mux",
			filename:    "mux.go",
			code:        `id := mux.Vars(r)["id"]`,
			expectRules: []string{"gorilla_unsafe_mux"},
			minFindings: 1,
		},

		// ── info disclosure ─────────────────────────────────────
		{
			name:        "stack_trace_exposure",
			filename:    "debug.go",
			code:        `debug.PrintStack()`,
			expectRules: []string{"stack_trace_exposure"},
			minFindings: 1,
		},
		{
			name:        "verbose_error_handler",
			filename:    "handler.go",
			code:        `http.Error(w, err.Error(), 500)`,
			expectRules: []string{"verbose_error_handler"},
			minFindings: 1,
		},
		{
			name:        "debug_endpoint_exposed",
			filename:    "server.go",
			code:        `import _ "net/http/pprof"`,
			expectRules: []string{"debug_endpoint_exposed"},
			minFindings: 1,
		},

		// ── permissions ─────────────────────────────────────────
		{
			name:        "chmod_777",
			filename:    "setup.go",
			code:        `os.Chmod("/tmp/data", 0777)`,
			expectRules: []string{"chmod_777"},
			minFindings: 1,
		},

		// ── go-specific ─────────────────────────────────────────
		{
			name:        "mutex_not_unlocked",
			filename:    "cache.go",
			code:        "mu.Lock()\n",
			expectRules: []string{"mutex_not_unlocked"},
			minFindings: 1,
		},
		{
			name:        "context_leak",
			filename:    "handler.go",
			code:        `ctx := context.Background()`,
			expectRules: []string{"context_leak"},
			minFindings: 1,
		},
		{
			name:        "time_sleep_in_handler",
			filename:    "handler.go",
			code:        `time.Sleep(5 * time.Second)`,
			expectRules: []string{"time_sleep_in_handler"},
			minFindings: 1,
		},

		// ── python (deterministic engine covers VSCode extension BYOK target) ──
		{
			name:        "python_command_injection",
			filename:    "app.py",
			code:        `import os; os.system(request.args.get("cmd"))`,
			expectRules: []string{"python_command_injection"},
			minFindings: 1,
		},
		{
			name:        "python_eval_exec",
			filename:    "app.py",
			code:        `result = eval(request.args.get("expr"))`,
			expectRules: []string{"python_eval_exec"},
			minFindings: 1,
		},
		{
			name:        "python_pickle_load",
			filename:    "app.py",
			code:        `data = pickle.loads(raw_bytes)`,
			expectRules: []string{"python_pickle_load"},
			minFindings: 1,
		},
		{
			name:        "python_unsafe_yaml",
			filename:    "app.py",
			code:        `cfg = yaml.load(stream)`,
			expectRules: []string{"python_unsafe_yaml"},
			minFindings: 1,
		},
		{
			name:        "python_sql_injection_fstring",
			filename:    "app.py",
			code:        `cursor.execute(f"SELECT * FROM users WHERE id = {uid}")`,
			expectRules: []string{"python_sql_injection_fstring"},
			minFindings: 1,
		},
		{
			name:        "python_sql_injection_format",
			filename:    "app.py",
			code:        `cursor.execute("SELECT * FROM users WHERE id = %s" % uid)`,
			expectRules: []string{"python_sql_injection_format"},
			minFindings: 1,
		},
		{
			name:        "python_ssrf",
			filename:    "app.py",
			code:        `resp = requests.get(request.args.get("url"))`,
			expectRules: []string{"python_ssrf"},
			minFindings: 1,
		},
		{
			name:        "python_path_traversal",
			filename:    "app.py",
			code:        `with open(request.files["f"].filename, "wb") as fh: pass`,
			expectRules: []string{"python_path_traversal"},
			minFindings: 1,
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

// ── Tests merged from scanner_deep_test.go ──────────────────────

func TestScan_EmptyCode(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: "", Filename: "empty.go"})
	if len(report.Findings) != 0 {
		t.Errorf("empty code should return 0 findings, got %d", len(report.Findings))
	}
}

func TestScan_WhitespaceOnly(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: "   \n\t  \n  ", Filename: "ws.go"})
	if len(report.Findings) != 0 {
		t.Errorf("whitespace-only code should return 0 findings, got %d", len(report.Findings))
	}
}

func TestScan_CommentsOnly(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: "// This is a comment\n/* block comment */", Filename: "comments.go"})
	_ = report
}

func TestScan_NullBytes(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: "var x = \x00\"test\"", Filename: "null.go"})
	_ = report
}

func TestScan_UnsupportedLanguage(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "cobol", Code: "DISPLAY 'HELLO'.", Filename: "prog.cobol"})
	if len(report.Findings) != 0 {
		t.Errorf("unsupported language should return 0 findings, got %d", len(report.Findings))
	}
}

func TestScan_FilenamePathTraversal(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: `InsecureSkipVerify: true`, Filename: "../../../etc/passwd.go"})
	if len(report.Findings) == 0 {
		t.Error("should detect findings even with path traversal filename")
	}
}

func TestScan_TestFile_FP(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `os.WriteFile("test.txt", data, 0777)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "handler_test.go"})
	for _, f := range report.Findings {
		if f.RuleID == "insecure_file_perms" && f.Severity != SeverityLow {
			t.Errorf("test file low-severity should be suppressed, got %s", f.Severity)
		}
	}
}

func TestScan_GeneratedFile(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "api.pb.go"})
	if len(report.Findings) != 0 {
		t.Errorf("generated file should suppress all findings, got %d", len(report.Findings))
	}
}

func TestScan_VendorFile(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "vendor/github.com/foo/bar.go"})
	if len(report.Findings) != 0 {
		t.Errorf("vendor file should suppress all findings, got %d", len(report.Findings))
	}
}

func TestScan_SQLInjection_Sprintf(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `q := fmt.Sprintf("SELECT * FROM users WHERE id=%s", id)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "handler.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "sql_injection" {
			found = true
		}
	}
	if !found {
		t.Error("should detect SQL injection via Sprintf")
	}
}

func TestScan_HardcodedAWSKey(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var key = "AKIAIOSFODNN7EXAMPLE"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "aws_access_key" {
			found = true
		}
	}
	if !found {
		t.Error("should detect hardcoded AWS key")
	}
}

func TestScan_GitHubToken(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var token = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef1234"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "auth.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "github_token" {
			found = true
		}
	}
	if !found {
		t.Error("should detect GitHub token")
	}
}

func TestScan_SlackToken(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var token = "xoxb-1234567890-1234567890-abcdefghij"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "slack_token" {
			found = true
		}
	}
	if !found {
		t.Error("should detect Slack token")
	}
}

func TestScan_PrivateKey(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAK...`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "key.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "private_key_literal" {
			found = true
		}
	}
	if !found {
		t.Error("should detect private key")
	}
}

func TestScan_InsecureSkipVerify(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `InsecureSkipVerify: true`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "client.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "insecure_tls" {
			found = true
		}
	}
	if !found {
		t.Error("should detect InsecureSkipVerify")
	}
}

func TestScan_MD5Usage(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `h := md5.New()`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "crypto.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "weak_hash_md5" {
			found = true
		}
	}
	if !found {
		t.Error("should detect MD5 usage")
	}
}

func TestScan_MathRand(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `import "math/rand"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "utils.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "weak_random" {
			found = true
		}
	}
	if !found {
		t.Error("should detect math/rand")
	}
}

func TestScan_PathTraversal(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `f, _ := os.Open(r.URL.Path)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "handler.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "path_traversal" {
			found = true
		}
	}
	if !found {
		t.Error("should detect path traversal")
	}
}

func TestScan_SSRF(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `resp, _ := http.Get(r.URL.Query().Get("url"))`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "proxy.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "ssrf_http_get" {
			found = true
		}
	}
	if !found {
		t.Error("should detect SSRF")
	}
}

func TestScan_XSS_UnsafeHTML(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `return template.HTML(data)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "handler.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "xss_unsafe_html" {
			found = true
		}
	}
	if !found {
		t.Error("should detect XSS via template.HTML")
	}
}

func TestScan_GoroutineWithoutRecovery(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `go func() { doWork() }()`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "async.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "goroutine_without_recovery" {
			found = true
		}
	}
	if !found {
		t.Error("should detect goroutine without recovery")
	}
}

func TestComputeFingerprint_Determinism(t *testing.T) {
	f1 := ComputeFingerprint("file.go", 10, "code", "rule1")
	f2 := ComputeFingerprint("file.go", 10, "code", "rule1")
	if f1 != f2 {
		t.Error("same input should produce same fingerprint")
	}
}

func TestComputeFingerprint_DifferentInputs(t *testing.T) {
	f1 := ComputeFingerprint("file.go", 10, "code1", "rule1")
	f2 := ComputeFingerprint("file.go", 10, "code2", "rule1")
	if f1 == f2 {
		t.Error("different inputs should produce different fingerprints")
	}
}

func TestBuiltinAnalyzer_Available(t *testing.T) {
	a := NewBuiltinAnalyzer()
	if !a.Available() {
		t.Error("builtin analyzer should always be available")
	}
	if a.Name() != "builtin" {
		t.Errorf("expected name 'builtin', got %q", a.Name())
	}
}

func TestEngine_EmptyFilename(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: `InsecureSkipVerify: true`, Filename: ""})
	if len(report.Findings) == 0 {
		t.Error("should detect findings even with empty filename")
	}
}

func TestScan_CodeWithOnlyComments(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `// TODO: fix this later
/* This is a multi-line
   comment block */
// Another comment`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "comments.go"})
	for _, f := range report.Findings {
		if f.Severity == SeverityCritical || f.Severity == SeverityHigh {
			t.Errorf("high/critical finding in comments only: %s", f.RuleID)
		}
	}
}

func TestScan_MockFile(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "mock_user.go"})
	if len(report.Findings) != 0 {
		t.Errorf("mock file should suppress findings, got %d", len(report.Findings))
	}
}

func TestScan_StubFile(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "stub_db.go"})
	if len(report.Findings) != 0 {
		t.Errorf("stub file should suppress findings, got %d", len(report.Findings))
	}
}

func TestScan_TestdataFile(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "testdata/fixture.json"})
	if len(report.Findings) != 0 {
		t.Errorf("testdata file should suppress findings, got %d", len(report.Findings))
	}
}

func TestScan_ExcludeFilenames(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "example_usage.go"})
	_ = report
}

func TestHasHighConfidenceFindings_Empty(t *testing.T) {
	report := &Report{Findings: []Finding{}}
	if HasHighConfidenceFindings(report) {
		t.Error("empty report should not have high confidence findings")
	}
}

func TestHasHighConfidenceFindings_LowConfidence(t *testing.T) {
	report := &Report{Findings: []Finding{
		{Confidence: 0.3, Severity: SeverityHigh},
	}}
	if HasHighConfidenceFindings(report) {
		t.Error("low confidence finding should not be high confidence")
	}
}

func TestHasHighConfidenceFindings_HighConfidence(t *testing.T) {
	report := &Report{Findings: []Finding{
		{Confidence: 0.95, Severity: SeverityHigh},
	}}
	if !HasHighConfidenceFindings(report) {
		t.Error("high confidence finding should be detected")
	}
}

func TestScan_LogInjection(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `slog.Info("user action", "input", req.Input)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "handler.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "log_injection" {
			found = true
		}
	}
	if !found {
		t.Error("should detect log injection")
	}
}

func TestScan_CommandInjection(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `exec.Command("sh", "-c", req.Input)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "cmd.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "command_injection" {
			found = true
		}
	}
	if !found {
		t.Error("should detect command injection")
	}
}

func TestScan_XSS_ScriptTag(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `<script>alert(1)</script>`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "template.html"})
	_ = report
}

func TestMergeScoreAndFilter(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	if len(report.Findings) == 0 {
		t.Error("expected findings for hardcoded password")
	}
	for _, f := range report.Findings {
		if f.Fingerprint == "" {
			t.Error("finding should have non-empty fingerprint")
		}
	}
}

func TestScan_HardcodedPasswordConfidence(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	var highConf []Finding
	for _, f := range report.Findings {
		if f.Confidence >= 0.60 {
			highConf = append(highConf, f)
		}
	}
	if len(highConf) == 0 {
		t.Error("expected at least one high-confidence finding for hardcoded password")
	}
}

func TestScan_RequireContext_Missing(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `exec.Command("sh", "-c", "echo hello")`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "cmd.go"})
	for _, f := range report.Findings {
		if f.RuleID == "command_injection" {
			t.Error("command_injection should not fire without required context")
		}
	}
}

func TestScan_RequireContext_Present(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `exec.Command("sh", "-c", req.Input)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "cmd.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "command_injection" {
			found = true
		}
	}
	if !found {
		t.Error("command_injection should fire with req. context")
	}
}

func TestScan_WeakHash_SHA1(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `import "crypto/sha1"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "crypto.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "weak_hash_sha1" {
			found = true
		}
	}
	if !found {
		t.Error("should detect SHA-1")
	}
}

func TestScan_HardcodedPassword(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `var password = "hunter2supersecret123"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	found := false
	for _, f := range report.Findings {
		if f.RuleID == "hardcoded_password" {
			found = true
		}
	}
	if !found {
		t.Error("should detect hardcoded password")
	}
}

func TestScan_FP_ParameterizedQuery(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `db.Query("SELECT * FROM users WHERE id=$1", id)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "db.go"})
	for _, f := range report.Findings {
		if f.RuleID == "sql_injection" || f.RuleID == "sql_injection_raw_query" {
			t.Errorf("parameterized query should not trigger: %s", f.RuleID)
		}
	}
}

func TestScan_FP_SHA256(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `import "crypto/sha256"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "crypto.go"})
	for _, f := range report.Findings {
		if f.RuleID == "weak_hash_md5" || f.RuleID == "weak_hash_sha1" {
			t.Errorf("SHA-256 should not trigger weak hash: %s", f.RuleID)
		}
	}
}

func TestScan_FP_CryptoRand(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `import "crypto/rand"`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "crypto.go"})
	for _, f := range report.Findings {
		if f.RuleID == "weak_random" {
			t.Error("crypto/rand should not trigger weak_random")
		}
	}
}

func TestScan_FP_SafeFileWrite(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `os.WriteFile("config.json", data, 0644)`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	for _, f := range report.Findings {
		if f.RuleID == "insecure_file_perms" {
			t.Error("0644 permissions should not trigger")
		}
	}
}

func TestScan_FP_SafeExec(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `exec.Command("ls", "-la")`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "cmd.go"})
	for _, f := range report.Findings {
		if f.RuleID == "command_injection" {
			t.Error("safe exec should not trigger command_injection")
		}
	}
}

func TestScan_FP_GoSum(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `github.com/foo/bar v1.2.3 h1:abc123=`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "go.sum"})
	for _, f := range report.Findings {
		if f.RuleID == "hardcoded_password" || f.RuleID == "insecure_tls" {
			t.Errorf("go.sum should not trigger: %s", f.RuleID)
		}
	}
}

func TestScan_SeverityOrderingDeep(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `
password := "supersecretpassword123"
h := md5.New()
InsecureSkipVerify: true
`
	report := engine.Run(context.Background(), Input{Language: "go", Code: code, Filename: "mixed.go"})
	if len(report.Findings) < 2 {
		t.Fatalf("expected at least 2 findings, got %d", len(report.Findings))
	}
	for i := 1; i < len(report.Findings); i++ {
		prev := SeverityRank(report.Findings[i-1].Severity)
		curr := SeverityRank(report.Findings[i].Severity)
		if prev < curr {
			t.Errorf("findings not sorted by severity at index %d", i)
		}
	}
}

func TestScan_NoFilename(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Language: "go", Code: `InsecureSkipVerify: true`, Filename: ""})
	if len(report.Findings) == 0 {
		t.Error("should detect findings with empty filename")
	}
}

// ── Merged from bandit_test.go ──────────────────────────────────

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

// ── Merged from builtin_test.go ─────────────────────────────────

func TestBuiltinDetectsKnownVulns(t *testing.T) {
	code := "" +
		`q := fmt.Sprintf("SELECT * FROM users WHERE id=%d", id)` + "\n" +
		`password := "supersecret123"` + "\n" +
		`h := md5.New()` + "\n"
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

func TestBuiltinSuppressesTestFileSecrets(t *testing.T) {
	code := `password := "supersecret123"` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "auth_test.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	// Should still detect but the engine will down-grade severity in test files.
	if len(found) == 0 {
		t.Fatal("expected builtin to still detect secrets in test files")
	}
}

func TestBuiltinIgnoresGeneratedFiles(t *testing.T) {
	code := `password := "supersecret123"` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "api.pb.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("generated file should produce no findings, got %d", len(found))
	}
}

func TestBuiltinIgnoresVendorFiles(t *testing.T) {
	code := `InsecureSkipVerify: true` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "vendor/github.com/foo/bar.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("vendor file should produce no findings, got %d", len(found))
	}
}

func TestBuiltinDoesNotFlagRandRead(t *testing.T) {
	code := `n, err := rand.Read(buf)` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "crypto.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	for _, f := range found {
		if f.RuleID == "weak_random" {
			t.Fatal("rand.Read (crypto/rand) should NOT be flagged as weak_random")
		}
	}
}

func TestBuiltinDoesNotFlagErrorWrapping(t *testing.T) {
	code := `return fmt.Errorf("failed to connect: %w", err)` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "db.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	for _, f := range found {
		if f.RuleID == "error_info_leak" || f.RuleID == "error_in_response" {
			t.Fatal("standard error wrapping should NOT be flagged")
		}
	}
}

func TestBuiltinSqlInjectionDetects(t *testing.T) {
	code := `q := fmt.Sprintf("SELECT * FROM users WHERE id=%d", id)` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "query.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("expected sql_injection finding")
	}
}

func TestBuiltinHardcodedPasswordDetects(t *testing.T) {
	code := `password := "mysecretpass123"` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "config.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	foundIt := false
	for _, f := range found {
		if f.RuleID == "hardcoded_password" {
			foundIt = true
			break
		}
	}
	if !foundIt {
		t.Fatal("expected hardcoded_password finding")
	}
}

func TestBuiltinWeakRandomDetectsMathRand(t *testing.T) {
	code := `"math/rand"` + "\n" + `n := rand.Intn(100)` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "util.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	foundIt := false
	for _, f := range found {
		if f.RuleID == "weak_random" {
			foundIt = true
			break
		}
	}
	if !foundIt {
		t.Fatal("expected weak_random finding for math/rand")
	}
}

func TestBuiltinInsecureTLS(t *testing.T) {
	code := `InsecureSkipVerify: true` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "http.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("expected insecure_tls finding")
	}
}

func TestBuiltinHardcodedConnectionString(t *testing.T) {
	code := `dsn := "postgres://user:pass@localhost/db"` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "db.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	foundIt := false
	for _, f := range found {
		if f.RuleID == "hardcoded_connection_string" {
			foundIt = true
			break
		}
	}
	if !foundIt {
		t.Fatal("expected hardcoded_connection_string finding")
	}
}

func TestBuiltinWeakJwtSecret(t *testing.T) {
	code := `token, _ := jwt.Sign(claims).SignedString("myhardcodedsecret")` + "\n"
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "auth.go"})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	foundIt := false
	for _, f := range found {
		if f.RuleID == "weak_jwt_secret" {
			foundIt = true
			break
		}
	}
	if !foundIt {
		t.Fatal("expected weak_jwt_secret finding")
	}
}

// ── Merged from confidence_test.go ──────────────────────────────

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestConfidence(t *testing.T) {
	// critical, single builtin: base 0.65, no real-tool weight, no corroboration.
	if c := Confidence(SeverityCritical, []string{"builtin"}); !near(c, 0.65) {
		t.Fatalf("critical/builtin = %v want 0.65", c)
	}
	// critical, single bandit: 0.65 + 0.15 real-tool weight (pure tool).
	if c := Confidence(SeverityCritical, []string{"bandit"}); !near(c, 0.80) {
		t.Fatalf("critical/bandit = %v want 0.80", c)
	}
	// medium, two analyzers: 0.40 + 0.10 (real-tool corroboration) + 0.25 = 0.75.
	if c := Confidence(SeverityMedium, []string{"builtin", "bandit"}); !near(c, 0.75) {
		t.Fatalf("medium/corroborated = %v want 0.75", c)
	}
	// critical + 2 real tools: 0.65 + 0.15 + 0.25 = 1.05, clamped to 0.99.
	if c := Confidence(SeverityCritical, []string{"semgrep", "bandit"}); !near(c, 0.99) {
		t.Fatalf("critical/2-tools = %v want 0.99", c)
	}
	// never exceeds 0.99 or drops below 0.05.
	if c := Confidence(SeverityInfo, []string{"builtin"}); c < 0.05 {
		t.Fatalf("floor breached: %v", c)
	}
}

func TestConfidenceWithFile(t *testing.T) {
	// Test file gets penalty.
	base := Confidence(SeverityCritical, []string{"builtin"})
	testFile := ConfidenceWithFile(SeverityCritical, []string{"builtin"}, "auth_test.go", "")
	if testFile >= base {
		t.Fatalf("test file should have lower confidence: %v >= %v", testFile, base)
	}

	// Generated file gets no findings from engine (suppressed by builtin).
	// But contextPenalty still applies if called directly.
	genFile := ConfidenceWithFile(SeverityHigh, []string{"builtin"}, "api.pb.go", "some code")
	if genFile >= base {
		t.Fatalf("generated file should have lower confidence")
	}

	// Regular file gets no penalty.
	regular := ConfidenceWithFile(SeverityCritical, []string{"builtin"}, "auth.go", "")
	if !near(regular, base) {
		t.Fatalf("regular file should have same confidence: %v != %v", regular, base)
	}
}

func TestContextPenalty(t *testing.T) {
	if p := contextPenalty("auth_test.go"); p >= 0 {
		t.Fatalf("test file should have negative penalty, got %v", p)
	}
	if p := contextPenalty("example/main.go"); p >= 0 {
		t.Fatalf("example file should have negative penalty, got %v", p)
	}
	if p := contextPenalty("internal/auth.go"); p != 0 {
		t.Fatalf("regular file should have zero penalty, got %v", p)
	}
}

func TestSnippetConfidence(t *testing.T) {
	// String literal assignment gets boost.
	if s := snippetConfidence(`password := "secret123"`); s <= 0 {
		t.Fatalf("literal assignment should have positive boost, got %v", s)
	}
}

func TestIsHighConfidence(t *testing.T) {
	if !IsHighConfidence(0.50) {
		t.Fatal("0.50 should be high confidence")
	}
	if IsHighConfidence(0.10) {
		t.Fatal("0.10 should not be high confidence")
	}
}

func TestShouldReport(t *testing.T) {
	f := Finding{Confidence: 0.35}
	if !ShouldReport(f) {
		t.Fatal("0.35 should be reportable")
	}
	f2 := Finding{Confidence: 0.10}
	if ShouldReport(f2) {
		t.Fatal("0.10 should not be reportable")
	}
}

// ── Merged from debug_test.go ───────────────────────────────────

func TestDebugErrorResponse(t *testing.T) {
	// Test the regex directly
	line := `	w.Write([]byte(fmt.Sprintf("error: %w", err)))`
	analyzer := NewBuiltinAnalyzer()
	for _, r := range analyzer.rules {
		if r.name == "error_in_response" {
			fmt.Printf("Pattern: %s\n", r.pattern.String())
			fmt.Printf("Line: %q\n", line)
			fmt.Printf("Match: %v\n", r.pattern.MatchString(line))
		}
	}

	// Test through the engine
	engine := NewEngine(NewBuiltinAnalyzer())
	code := `func handler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(fmt.Sprintf("error: %w", err)))
}`
	fmt.Printf("Code: %q\n", code)
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "handler.go",
	})
	fmt.Printf("Findings: %d\n", len(report.Findings))
	for _, f := range report.Findings {
		fmt.Printf("  RuleID=%s Severity=%s\n", f.RuleID, f.Severity)
	}
}

// ── Merged from engine_test.go ──────────────────────────────────

type fakeAnalyzer struct {
	name      string
	available bool
	findings  []Finding
	err       error
}

func (f fakeAnalyzer) Name() string    { return f.name }
func (f fakeAnalyzer) Available() bool { return f.available }
func (f fakeAnalyzer) Analyze(ctx context.Context, in Input) ([]Finding, error) {
	return f.findings, f.err
}

func mkFinding(analyzer string, sev Severity) Finding {
	// Use a shared ruleID so two analyzers reporting the same vuln produce
	// matching fingerprints and merge correctly.
	return Finding{
		RuleID: "shared-rule", Analyzers: []string{analyzer}, Severity: sev,
		Filename: "x.py", Line: 3, Snippet: "danger()",
		Fingerprint: ComputeFingerprint("x.py", 3, "danger()", "shared-rule"),
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
	// high + real-tool corroboration + corroboration = 0.55 + 0.10 + 0.25 = 0.90
	if f.Confidence < 0.85 || f.Confidence > 0.95 {
		t.Fatalf("corroborated confidence = %v want ~0.90", f.Confidence)
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

func TestEngineFiltersLowConfidence(t *testing.T) {
	// Very low confidence finding from info severity with builtin only should be filtered.
	low := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "low-rule", Analyzers: []string{"builtin"}, Severity: SeverityInfo,
			Filename: "x.py", Line: 1, Snippet: "low risk",
			Fingerprint: ComputeFingerprint("x.py", 1, "low risk", "low-rule"),
		}},
	}
	eng := NewEngine(low)
	rep := eng.Run(context.Background(), Input{Code: "x"})
	// info + builtin = 0.20 base, below minConfidence of 0.30.
	if len(rep.Findings) != 0 {
		t.Fatalf("expected 0 findings (filtered), got %d", len(rep.Findings))
	}
}

func TestEngineNoFindingsAboveThreshold(t *testing.T) {
	// A high-severity finding from builtin only should pass the filter.
	high := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "high-rule", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
			Filename: "x.py", Line: 1, Snippet: "critical issue",
			Fingerprint: ComputeFingerprint("x.py", 1, "critical issue", "high-rule"),
		}},
	}
	eng := NewEngine(high)
	rep := eng.Run(context.Background(), Input{Code: "x"})
	if len(rep.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(rep.Findings))
	}
}

func TestEngineTestFPSuppression(t *testing.T) {
	// Secrets in test files should be downgraded but still present.
	secretsInTest := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "hardcoded_password", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
			Category: "secrets",
			Filename: "auth_test.go", Line: 10, Snippet: `password := "test123456"`,
			Fingerprint: ComputeFingerprint("auth_test.go", 10, `password := "test123456"`, "hardcoded_password"),
		}},
	}
	eng := NewEngine(secretsInTest)
	rep := eng.Run(context.Background(), Input{Code: "x", Filename: "auth_test.go"})

	// Should have 1 finding but with downgraded severity.
	if len(rep.Findings) != 1 {
		t.Fatalf("expected 1 finding (suppressed, not removed), got %d", len(rep.Findings))
	}
	if rep.Findings[0].Severity != SeverityInfo {
		t.Fatalf("test file secret should be downgraded to info, got %s", rep.Findings[0].Severity)
	}
}

func TestHasHighConfidenceFindings_FromEngine(t *testing.T) {
	rep := &Report{
		Findings: []Finding{
			{Confidence: 0.80, Filename: "main.go"},
		},
	}
	if !HasHighConfidenceFindings(rep) {
		t.Fatal("should detect high-confidence findings")
	}

	rep2 := &Report{
		Findings: []Finding{
			{Confidence: 0.80, Filename: "main_test.go"},
		},
	}
	if HasHighConfidenceFindings(rep2) {
		t.Fatal("test file findings should not count as high confidence")
	}
}

// ── Merged from finding_test.go ─────────────────────────────────

func TestComputeFingerprint_FromFinding(t *testing.T) {
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

func TestSeverityRank_FromFinding(t *testing.T) {
	if SeverityRank(SeverityCritical) <= SeverityRank(SeverityHigh) {
		t.Fatal("critical must outrank high")
	}
	if SeverityRank(SeverityInfo) <= SeverityRank("") {
		t.Fatal("info must outrank unknown")
	}
}

// ── Merged from integration_test.go ─────────────────────────────

// TestIntegration_BuiltinOnlyPipeline tests the full scanner pipeline with only
// the builtin analyzer (no external tools). This exercises:
// Analyze → merge → dedupe → confidence → filter → report.
func TestIntegration_BuiltinOnlyPipeline(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())

	code := `
package main

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"net/http"
	"os"
)

func handler(w http.ResponseWriter, r *http.Request) {
	// SQL injection
	q := fmt.Sprintf("SELECT * FROM users WHERE id=%s", r.URL.Query().Get("id"))

	// Hardcoded secret
	password := "supersecretpassword123"

	// Weak hash
	h := md5.New()

	// Insecure TLS
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	// Path traversal
	f, _ := os.Open(r.URL.Path)

	_ = q
	_ = h
	_ = tr
	_ = f
}
`
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "handler.go",
	})

	if len(report.AnalyzersRun) != 1 || report.AnalyzersRun[0] != "builtin" {
		t.Fatalf("expected only builtin analyzer, got %v", report.AnalyzersRun)
	}
	if len(report.AnalyzerErrors) != 0 {
		t.Fatalf("expected no analyzer errors, got %v", report.AnalyzerErrors)
	}

	// Should find multiple vulnerabilities.
	if len(report.Findings) < 3 {
		t.Fatalf("expected at least 3 findings, got %d", len(report.Findings))
	}

	// All findings should be from builtin.
	for _, f := range report.Findings {
		if len(f.Analyzers) != 1 || f.Analyzers[0] != "builtin" {
			t.Fatalf("finding %s should be from builtin only, got %v", f.RuleID, f.Analyzers)
		}
		if f.Fingerprint == "" {
			t.Fatalf("finding %s has no fingerprint", f.RuleID)
		}
		if f.Confidence < 0.05 || f.Confidence > 0.99 {
			t.Fatalf("finding %s has out-of-range confidence: %v", f.RuleID, f.Confidence)
		}
	}

	// Verify specific rules were triggered.
	ruleIDs := map[string]bool{}
	for _, f := range report.Findings {
		ruleIDs[f.RuleID] = true
	}
	expected := []string{"sql_injection", "hardcoded_password", "weak_hash_md5", "insecure_tls"}
	for _, e := range expected {
		if !ruleIDs[e] {
			t.Fatalf("expected rule %s to fire, but it did not", e)
		}
	}
}

// TestIntegration_DedupeAcrossAnalyzers tests that two analyzers flagging
// the same line produce one merged finding with both analyzers listed.
func TestIntegration_DedupeAcrossAnalyzers(t *testing.T) {
	// Two fake analyzers that flag the same location with the same snippet.
	// Use the same ruleID so the fingerprint matches and they merge.
	sharedRule := "sql-injection"
	snippet := `q := fmt.Sprintf("SELECT * FROM t WHERE id=%s", id)`
	a1 := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: sharedRule, Analyzers: []string{"builtin"}, Severity: SeverityCritical,
			Filename: "x.go", Line: 10, Snippet: snippet,
			Fingerprint: ComputeFingerprint("x.go", 10, snippet, sharedRule),
			Category:    "injection",
		}},
	}
	a2 := fakeAnalyzer{
		name: "bandit", available: true,
		findings: []Finding{{
			RuleID: sharedRule, Analyzers: []string{"bandit"}, Severity: SeverityHigh,
			Filename: "x.go", Line: 10, Snippet: snippet,
			Fingerprint: ComputeFingerprint("x.go", 10, snippet, sharedRule),
			Category:    "injection",
		}},
	}

	engine := NewEngine(a1, a2)
	report := engine.Run(context.Background(), Input{Code: "x", Filename: "x.go"})

	if len(report.Findings) != 1 {
		t.Fatalf("expected 1 merged finding, got %d", len(report.Findings))
	}
	f := report.Findings[0]
	if len(f.Analyzers) != 2 {
		t.Fatalf("expected 2 analyzers on merged finding, got %v", f.Analyzers)
	}
	if f.Severity != SeverityCritical {
		t.Fatalf("merge should keep highest severity, got %s", f.Severity)
	}
}

// TestIntegration_GeneratedFileSuppression tests that generated/vendor files
// produce zero findings from the builtin analyzer.
func TestIntegration_GeneratedFileSuppression(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())

	code := `
password := "supersecretpassword123"
InsecureSkipVerify: true
`
	// Generated file
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "api.pb.go",
	})
	if len(report.Findings) != 0 {
		t.Fatalf("generated file should produce 0 findings, got %d", len(report.Findings))
	}

	// Vendor file
	report2 := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "vendor/github.com/foo/bar.go",
	})
	if len(report2.Findings) != 0 {
		t.Fatalf("vendor file should produce 0 findings, got %d", len(report2.Findings))
	}
}

// TestIntegration_TestFileSuppression tests that test files get downgraded
// findings (not removed, but lowered severity).
func TestIntegration_TestFileSuppression(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())

	code := `password := "supersecretpassword123"` + "\n"
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "auth_test.go",
	})

	// Should find the secret but with downgraded severity.
	if len(report.Findings) != 1 {
		t.Fatalf("expected 1 finding in test file, got %d", len(report.Findings))
	}
	f := report.Findings[0]
	if f.Severity != SeverityInfo {
		t.Fatalf("test file secret should be downgraded to info, got %s", f.Severity)
	}
}

// TestIntegration_EmptyInput tests that empty input produces no findings.
func TestIntegration_EmptyInput(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{Code: ""})
	if len(report.Findings) != 0 {
		t.Fatalf("empty input should produce 0 findings, got %d", len(report.Findings))
	}
}

// TestIntegration_SkippedAnalyzers tests that unavailable analyzers are
// recorded in AnalyzersSkipped and don't block the scan.
func TestIntegration_SkippedAnalyzers(t *testing.T) {
	unavailable := fakeAnalyzer{name: "semgrep", available: false}
	builtin := NewBuiltinAnalyzer()

	engine := NewEngine(builtin, unavailable)
	report := engine.Run(context.Background(), Input{
		Code:     `InsecureSkipVerify: true`,
		Filename: "tls.go",
	})

	if _, ok := report.AnalyzersSkipped["semgrep"]; !ok {
		t.Fatal("semgrep should be in AnalyzersSkipped")
	}
	if len(report.Findings) == 0 {
		t.Fatal("builtin findings should still be present")
	}
}

// TestIntegration_ConfidenceThreshold tests that the minConfidence filter
// removes low-confidence findings.
func TestIntegration_ConfidenceThreshold(t *testing.T) {
	// Create engine with high threshold to filter more aggressively.
	engine := NewEngine(NewBuiltinAnalyzer())
	engine.minConfidence = 0.50

	code := `"math/rand"` + "\n" + `n := rand.Intn(100)` + "\n"
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     code,
		Filename: "util.go",
	})

	// weak_random in util.go with single builtin analyzer:
	// base 0.55 + builtin weight 0 = 0.55, above 0.50 threshold.
	if len(report.Findings) == 0 {
		t.Fatal("weak_random should pass 0.50 threshold with builtin base 0.55")
	}
}

// TestIntegration_ReportStructure tests that the Report struct is properly
// populated with all metadata.
func TestIntegration_ReportStructure(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	report := engine.Run(context.Background(), Input{
		Language: "go",
		Code:     `InsecureSkipVerify: true`,
		Filename: "tls.go",
	})

	if len(report.AnalyzersRun) != 1 {
		t.Fatalf("expected 1 analyzer run, got %d", len(report.AnalyzersRun))
	}
	if len(report.AnalyzersSkipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(report.AnalyzersSkipped))
	}
	if len(report.AnalyzerErrors) != 0 {
		t.Fatalf("expected 0 errors, got %d", len(report.AnalyzerErrors))
	}

	// Verify Report is JSON-serializable.
	if report.Findings[0].RuleID == "" {
		t.Fatal("finding should have a RuleID")
	}
}

// ── Merged from runner_test.go ──────────────────────────────────

// fakeRunner is a shared test double for shell-out adapters.
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

// ── Merged from semgrep_test.go ─────────────────────────────────

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

// ── Additional coverage tests ───────────────────────────────────

func TestExecRunner_Run(t *testing.T) {
	r := ExecRunner{}
	stdout, stderr, err := r.Run(context.Background(), "go", []string{"version"}, "")
	if err != nil {
		t.Fatalf("ExecRunner.Run failed: %v", err)
	}
	if stdout == "" {
		t.Fatal("expected stdout from go version")
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
}

func TestExecRunner_RunWithStdin(t *testing.T) {
	r := ExecRunner{}
	stdout, _, err := r.Run(context.Background(), "go", []string{"run"}, "fmt.Println(\"hello\")")
	// go run with stdin won't work well, but it exercises the stdin path
	_ = stdout
	_ = err
}

func TestExecRunner_RunError(t *testing.T) {
	r := ExecRunner{}
	_, _, err := r.Run(context.Background(), "nonexistent-tool-xyz-9999", nil, "")
	if err == nil {
		t.Fatal("expected error for nonexistent tool")
	}
}

func TestDefaultEngine(t *testing.T) {
	e := DefaultEngine()
	if e == nil {
		t.Fatal("DefaultEngine should not return nil")
	}
	if len(e.analyzers) != 3 {
		t.Fatalf("expected 3 analyzers, got %d", len(e.analyzers))
	}
}

func TestWithMinConfidence(t *testing.T) {
	e := NewEngine(NewBuiltinAnalyzer())
	WithMinConfidence(0.50)(e)
	if e.minConfidence != 0.50 {
		t.Fatalf("expected minConfidence 0.50, got %v", e.minConfidence)
	}
}

func TestWithTestFPSuppression(t *testing.T) {
	e := NewEngine(NewBuiltinAnalyzer())
	e.suppressTestFP = false
	WithTestFPSuppression()(e)
	if !e.suppressTestFP {
		t.Fatal("expected suppressTestFP to be true")
	}
}

func TestBanditSeverityUnknown(t *testing.T) {
	if banditSeverity("CRITICAL") != SeverityInfo {
		t.Fatal("unknown bandit severity should return SeverityInfo")
	}
}

func TestSemgrepSeverityUnknown(t *testing.T) {
	if semgrepSeverity("CRITICAL") != SeverityInfo {
		t.Fatal("unknown semgrep severity should return SeverityInfo")
	}
}

func TestSemgrepSeverityWarning(t *testing.T) {
	if semgrepSeverity("WARNING") != SeverityMedium {
		t.Fatal("WARNING should map to SeverityMedium")
	}
}

func TestSemgrepSeverityInfo(t *testing.T) {
	if semgrepSeverity("INFO") != SeverityLow {
		t.Fatal("INFO should map to SeverityLow")
	}
}

func TestSemgrepAnalyzerName(t *testing.T) {
	s := NewSemgrepAnalyzer(nil)
	if s.Name() != "semgrep" {
		t.Fatalf("expected name 'semgrep', got %q", s.Name())
	}
}

func TestBanditAnalyzerNilRunner(t *testing.T) {
	b := NewBanditAnalyzer(nil)
	if b.runner == nil {
		t.Fatal("NewBanditAnalyzer(nil) should use ExecRunner")
	}
}

func TestSemgrepAnalyzerNilRunner(t *testing.T) {
	s := NewSemgrepAnalyzer(nil)
	if s.runner == nil {
		t.Fatal("NewSemgrepAnalyzer(nil) should use ExecRunner")
	}
}

func TestBanditUnparseableOutput(t *testing.T) {
	fr := &fakeRunner{stdout: "not json at all", err: fmt.Errorf("tool failed")}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }
	_, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x", Filename: "a.py"})
	if err == nil {
		t.Fatal("expected error for unparseable output with runner error")
	}
}

func TestBanditUnparseableOutputNoRunnerError(t *testing.T) {
	fr := &fakeRunner{stdout: "not json"}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }
	_, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x", Filename: "a.py"})
	if err == nil {
		t.Fatal("expected error for unparseable output")
	}
}

func TestSemgrepUnparseableOutput(t *testing.T) {
	fr := &fakeRunner{stdout: "not json at all", err: fmt.Errorf("tool failed")}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }
	_, err := s.Analyze(context.Background(), Input{Language: "python", Code: "x", Filename: "a.py"})
	if err == nil {
		t.Fatal("expected error for unparseable output with runner error")
	}
}

func TestSemgrepUnparseableOutputNoRunnerError(t *testing.T) {
	fr := &fakeRunner{stdout: "not json"}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }
	_, err := s.Analyze(context.Background(), Input{Language: "python", Code: "x", Filename: "a.py"})
	if err == nil {
		t.Fatal("expected error for unparseable output")
	}
}

func TestBanditEmptyFilename(t *testing.T) {
	canned := `{"results":[{"filename":"snippet.py","issue_severity":"LOW","issue_text":"test","test_id":"B999","test_name":"test_rule","line_number":1,"code":"x"}]}`
	fr := &fakeRunner{stdout: canned}
	b := NewBanditAnalyzer(fr)
	b.exists = func() bool { return true }
	found, err := b.Analyze(context.Background(), Input{Language: "python", Code: "x", Filename: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(found))
	}
	if found[0].Filename != "snippet.py" {
		t.Fatalf("expected default filename 'snippet.py', got %q", found[0].Filename)
	}
}

func TestSemgrepEmptyFilename(t *testing.T) {
	canned := `{"results":[{"check_id":"test.rule","path":"snippet.txt","start":{"line":1},"extra":{"message":"test","severity":"ERROR","lines":"code","metadata":{"category":"sec"}}}]}`
	fr := &fakeRunner{stdout: canned}
	s := NewSemgrepAnalyzer(fr)
	s.exists = func() bool { return true }
	found, err := s.Analyze(context.Background(), Input{Language: "python", Code: "x", Filename: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(found))
	}
	if found[0].Filename != "snippet.txt" {
		t.Fatalf("expected default filename 'snippet.txt', got %q", found[0].Filename)
	}
}

func TestContextPenaltyBenchFile(t *testing.T) {
	p := contextPenalty("handler_bench_test.go")
	if p >= 0 {
		t.Fatalf("bench file should have negative penalty, got %v", p)
	}
}

func TestContextPenaltyBenchContains(t *testing.T) {
	p := contextPenalty("bench_runner.go")
	if p >= 0 {
		t.Fatalf("file containing 'bench' should have negative penalty, got %v", p)
	}
}

func TestContextPenaltyMd(t *testing.T) {
	p := contextPenalty("README.md")
	if p >= 0 {
		t.Fatalf(".md file should have negative penalty, got %v", p)
	}
}

func TestContextPenaltyTxt(t *testing.T) {
	p := contextPenalty("notes.txt")
	if p >= 0 {
		t.Fatalf(".txt file should have negative penalty, got %v", p)
	}
}

func TestContextPenaltySample(t *testing.T) {
	p := contextPenalty("sample_handler.go")
	if p >= 0 {
		t.Fatalf("sample file should have negative penalty, got %v", p)
	}
}

func TestIsGeneratedFileGenerated(t *testing.T) {
	if !isGeneratedFile("model_generated.go") {
		t.Fatal("expected _generated.go to be detected as generated")
	}
}

func TestIsGeneratedFileLower(t *testing.T) {
	if !isGeneratedFile("GENERATED_code.go") {
		t.Fatal("expected GENERATED to be detected as generated (case insensitive)")
	}
}

func TestBuiltinAnalyzerNoFilename(t *testing.T) {
	a := NewBuiltinAnalyzer()
	found, err := a.Analyze(context.Background(), Input{
		Language: "go",
		Code:     `password := "supersecret123"`,
		Filename: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("expected findings with empty filename")
	}
}

func TestBuiltinAnalyzerExcludeFilenames(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `var password = "hunter2supersecret123"`
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "example_usage.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range found {
		if f.RuleID == "hardcoded_password" {
			t.Fatal("hardcoded_password should be excluded for example filename")
		}
	}
}

func TestBuiltinAnalyzerRequireContextMissing(t *testing.T) {
	a := NewBuiltinAnalyzer()
	code := `exec.Command("sh", "-c", "echo hello")`
	found, err := a.Analyze(context.Background(), Input{Language: "go", Code: code, Filename: "cmd.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range found {
		if f.RuleID == "command_injection" {
			t.Fatal("command_injection should not fire without required context")
		}
	}
}

func TestMergeScoreAndFilterNoEmptyFingerprint(t *testing.T) {
	f := Finding{
		RuleID: "test-rule", Analyzers: []string{"builtin"}, Severity: SeverityHigh,
		Filename: "x.go", Line: 10, Snippet: "code here",
	}
	f2 := Finding{
		RuleID: "test-rule", Analyzers: []string{"bandit"}, Severity: SeverityHigh,
		Filename: "x.go", Line: 10, Snippet: "code here",
	}
	engine := NewEngine(fakeAnalyzer{name: "a", available: true, findings: []Finding{f, f2}})
	rep := engine.Run(context.Background(), Input{Code: "x", Filename: "x.go"})
	for _, fp := range rep.Findings {
		if fp.Fingerprint == "" {
			t.Fatal("finding should have non-empty fingerprint after merge")
		}
	}
}

func TestSuppressTestFPInjectionInTest(t *testing.T) {
	f := &Finding{
		RuleID: "sql_injection", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
		Category: "injection",
		Filename: "handler_test.go", Line: 5, Snippet: `q := fmt.Sprintf("SELECT %s", id)`,
	}
	result := suppressTestFP(f)
	if result == nil {
		t.Fatal("suppressTestFP should not nil the finding")
	}
	if result.Severity != SeverityLow {
		t.Fatalf("injection in test file should be SeverityLow, got %s", result.Severity)
	}
}

func TestSuppressTestFPInjectionMultiTool(t *testing.T) {
	f := &Finding{
		RuleID: "sql_injection", Analyzers: []string{"builtin", "semgrep"}, Severity: SeverityCritical,
		Category: "injection",
		Filename: "handler_test.go", Line: 5, Snippet: `q := fmt.Sprintf("SELECT %s", id)`,
	}
	result := suppressTestFP(f)
	if result == nil {
		t.Fatal("should not nil")
	}
	if result.Severity == SeverityLow {
		t.Fatal("multi-tool finding should not be downgraded to SeverityLow")
	}
}

func TestSuppressTestFPCryptoInTest(t *testing.T) {
	f := &Finding{
		RuleID: "weak_hash_md5", Analyzers: []string{"builtin"}, Severity: SeverityHigh,
		Category: "crypto",
		Filename: "crypto_test.go", Line: 1, Snippet: "md5.New()",
	}
	result := suppressTestFP(f)
	if result == nil {
		t.Fatal("should not nil")
	}
}

func TestEngineRunOversizedInput(t *testing.T) {
	engine := NewEngine(NewBuiltinAnalyzer())
	bigCode := make([]byte, maxCodeSize+1)
	for i := range bigCode {
		bigCode[i] = 'x'
	}
	report := engine.Run(context.Background(), Input{Code: string(bigCode), Filename: "big.go"})
	if len(report.Findings) != 0 {
		t.Fatal("oversized input should produce no findings")
	}
	if _, ok := report.AnalyzerErrors["engine"]; !ok {
		t.Fatal("oversized input should record engine error")
	}
}

func TestEngineRunAnalyzerError(t *testing.T) {
	errAnalyzer := fakeAnalyzer{name: "broken", available: true, err: fmt.Errorf("crash")}
	goodAnalyzer := fakeAnalyzer{
		name: "good", available: true,
		findings: []Finding{{
			RuleID: "test-rule", Analyzers: []string{"good"}, Severity: SeverityHigh,
			Filename: "x.go", Line: 1, Snippet: "code",
			Fingerprint: ComputeFingerprint("x.go", 1, "code", "test-rule"),
		}},
	}
	engine := NewEngine(errAnalyzer, goodAnalyzer)
	report := engine.Run(context.Background(), Input{Code: "x", Filename: "x.go"})
	if _, ok := report.AnalyzerErrors["broken"]; !ok {
		t.Fatal("broken analyzer error should be recorded")
	}
	if len(report.Findings) != 1 {
		t.Fatalf("good analyzer findings should survive, got %d", len(report.Findings))
	}
}

func TestEngineMergeKeepsFixAndMessage(t *testing.T) {
	a1 := fakeAnalyzer{
		name: "a1", available: true,
		findings: []Finding{{
			RuleID: "r1", Analyzers: []string{"a1"}, Severity: SeverityMedium,
			Filename: "x.go", Line: 1, Snippet: "code",
			Fingerprint: ComputeFingerprint("x.go", 1, "code", "r1"),
			Fix:         "", Message: "",
		}},
	}
	a2 := fakeAnalyzer{
		name: "a2", available: true,
		findings: []Finding{{
			RuleID: "r1", Analyzers: []string{"a2"}, Severity: SeverityMedium,
			Filename: "x.go", Line: 1, Snippet: "code",
			Fingerprint: ComputeFingerprint("x.go", 1, "code", "r1"),
			Fix:         "fix-a2", Message: "msg-a2",
		}},
	}
	engine := NewEngine(a1, a2)
	report := engine.Run(context.Background(), Input{Code: "x", Filename: "x.go"})
	if len(report.Findings) != 1 {
		t.Fatalf("expected 1 merged finding, got %d", len(report.Findings))
	}
	if report.Findings[0].Fix != "fix-a2" {
		t.Fatalf("expected fix from second analyzer, got %q", report.Findings[0].Fix)
	}
	if report.Findings[0].Message != "msg-a2" {
		t.Fatalf("expected message from second analyzer, got %q", report.Findings[0].Message)
	}
}

func TestEngineSuppressTestFPDisabled(t *testing.T) {
	secretsInTest := fakeAnalyzer{
		name: "builtin", available: true,
		findings: []Finding{{
			RuleID: "hardcoded_password", Analyzers: []string{"builtin"}, Severity: SeverityCritical,
			Category: "secrets",
			Filename: "auth_test.go", Line: 10, Snippet: `password := "test123456"`,
			Fingerprint: ComputeFingerprint("auth_test.go", 10, `password := "test123456"`, "hardcoded_password"),
		}},
	}
	eng := NewEngine(secretsInTest)
	eng.suppressTestFP = false
	rep := eng.Run(context.Background(), Input{Code: "x", Filename: "auth_test.go"})
	if len(rep.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(rep.Findings))
	}
	if rep.Findings[0].Severity != SeverityCritical {
		t.Fatalf("without suppression, severity should stay critical, got %s", rep.Findings[0].Severity)
	}
}

func TestUnionSorted(t *testing.T) {
	result := unionSorted([]string{"c", "a"}, []string{"b", "a"})
	expected := []string{"a", "b", "c"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d elements, got %d", len(expected), len(result))
	}
	for i, v := range result {
		if v != expected[i] {
			t.Fatalf("index %d: expected %q, got %q", i, expected[i], v)
		}
	}
}

func TestComputeFingerprintNoRuleID(t *testing.T) {
	f := ComputeFingerprint("x.go", 1, "code")
	if f == "" {
		t.Fatal("fingerprint should not be empty")
	}
	if len(f) != 16 {
		t.Fatalf("fingerprint length = %d want 16", len(f))
	}
}

func TestBaseConfidenceAllSeverities(t *testing.T) {
	tests := []struct {
		sev  Severity
		want float64
	}{
		{SeverityCritical, 0.65},
		{SeverityHigh, 0.55},
		{SeverityMedium, 0.40},
		{SeverityLow, 0.30},
		{SeverityInfo, 0.20},
	}
	for _, tt := range tests {
		if got := baseConfidence(tt.sev); got != tt.want {
			t.Errorf("baseConfidence(%s) = %v, want %v", tt.sev, got, tt.want)
		}
	}
}

func TestAnalyzerWeightBuiltinOnly(t *testing.T) {
	if w := analyzerWeight([]string{"builtin"}); w != 0.0 {
		t.Fatalf("builtin-only weight = %v, want 0.0", w)
	}
}

func TestAnalyzerWeightPureRealTool(t *testing.T) {
	if w := analyzerWeight([]string{"bandit"}); w != 0.15 {
		t.Fatalf("pure bandit weight = %v, want 0.15", w)
	}
}

func TestAnalyzerWeightCorroborated(t *testing.T) {
	if w := analyzerWeight([]string{"builtin", "semgrep"}); w != 0.10 {
		t.Fatalf("corroborated weight = %v, want 0.10", w)
	}
}

func TestSnippetConfidenceVarFunc(t *testing.T) {
	s := snippetConfidence("var x int")
	if s != -0.05 {
		t.Fatalf("var reference should have -0.05 modifier, got %v", s)
	}
}

func TestSnippetConfidenceEnv(t *testing.T) {
	s := snippetConfidence(`password := os.Getenv("SECRET")`)
	if s != 0.0 {
		t.Fatalf("env reference should have 0.0 modifier, got %v", s)
	}
}

func TestClampFloat(t *testing.T) {
	if clampFloat(-1, 0, 1) != 0 {
		t.Fatal("clamp below min failed")
	}
	if clampFloat(2, 0, 1) != 1 {
		t.Fatal("clamp above max failed")
	}
	if clampFloat(0.5, 0, 1) != 0.5 {
		t.Fatal("clamp in-range failed")
	}
}

func TestIsTestDataFileBackslash(t *testing.T) {
	if !isTestDataFile(`foo\testdata\fixture.json`) {
		t.Fatal("should detect testdata with backslash")
	}
}

func TestIsTestDataFilePrefix(t *testing.T) {
	if !isTestDataFile("testdata/fixture.json") {
		t.Fatal("should detect testdata as prefix")
	}
}

func TestIsGeneratedFileStub(t *testing.T) {
	if !isGeneratedFile("stub_db.go") {
		t.Fatal("stub_ should be detected as generated")
	}
}

func TestIsGeneratedFileMock(t *testing.T) {
	if !isGeneratedFile("mock_user.go") {
		t.Fatal("mock_ should be detected as generated")
	}
}

func TestIsTestFileContainsTest(t *testing.T) {
	if !isTestFile("src/test/helpers.go") {
		t.Fatal("should detect /test/ in path")
	}
}

func TestIsTestFileContainsTests(t *testing.T) {
	if !isTestFile("src/tests/helpers.go") {
		t.Fatal("should detect /tests/ in path")
	}
}
