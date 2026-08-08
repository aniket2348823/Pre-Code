package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vigilagent/vigilagent/internal/scanner"
)

var (
	baseURL string
	token   string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "vigil",
		Short: "VigilAgent CLI — AI agent management platform",
		Long:  `VigilAgent is an AI agent management platform with real-time monitoring, analytics, and control.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if baseURL == "" {
				baseURL = os.Getenv("VIGIL_API_URL")
				if baseURL == "" {
					baseURL = "http://localhost:8080"
				}
			}
			if token == "" {
				token = os.Getenv("VIGIL_API_TOKEN")
			}
			return nil
		},
	}

	rootCmd.PersistentFlags().StringVar(&baseURL, "url", "", "API base URL (default: http://localhost:8080)")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "API token (or set VIGIL_API_TOKEN)")

	rootCmd.AddCommand(
		configCmd(),
		versionCmd(),
		scanCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ─── vigil scan — deterministic scan + SARIF + fail gate (CI enforcement) ──
// The CI layer's entry point: runs the same deterministic engine the gateway
// uses over a workspace, writes a SARIF 2.1.0 report, and fails the process
// when findings meet the --fail-on severity gate. This is what blocks a merge
// on protected branches when code slipped past the IDE/gateway.

// scanExtensions are the file types the CLI walker considers.
var scanExtensions = map[string]bool{
	".go": true, ".py": true, ".js": true, ".mjs": true, ".cjs": true,
	".jsx": true, ".ts": true, ".tsx": true, ".rs": true, ".java": true,
	".yaml": true, ".yml": true, ".json": true, ".sql": true, ".sh": true,
	".tf": true,
}

// isTestFile reports whether a path is a test file (Go _test.go, JS/TS
// .test/.spec, Python test_/conftest, etc.). The deterministic scanner's own
// accuracy fixtures embed intentionally vulnerable code — test files must be
// excluded from the CI fail-gate scan so they cannot block production merges.
func isTestFile(p string) bool {
	name := strings.ToLower(filepath.Base(p))
	ext := strings.ToLower(filepath.Ext(p))
	base := strings.TrimSuffix(name, ext)
	// Normalized path so directory checks work on Windows backslash paths.
	norm := strings.ReplaceAll(strings.ToLower(p), "\\", "/")
	switch {
	case strings.HasSuffix(name, "_test.go"):
		return true
	case strings.HasSuffix(name, "_fuzz.go"):
		// Fuzz harnesses are test-only (testing import, fake secrets by design).
		return true
	case strings.HasSuffix(name, "_bench.go"):
		// Benchmarks are test-only (testing.B import, fake configs by design).
		return true
	case base == "test":
		// Package-private test helper files (not _test.go) — by convention they
		// import `testing` and hold fixtures, never production logic.
		return true
	case strings.Contains(norm, "/integration/"):
		// Integration test infrastructure (testdb.go, testredis.go).
		return true
	case ext == ".go" && strings.HasSuffix(base, "_test"):
		return true
	case ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx":
		return strings.Contains(base, ".test") || strings.Contains(base, ".spec") || strings.HasSuffix(base, "_test")
	case ext == ".py":
		return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test") || strings.Contains(norm, "/test/")
	case ext == ".rs":
		return strings.HasSuffix(base, "_test") || strings.Contains(norm, "/tests/")
	}
	return false
}

// langForFile guesses the scanner language from a file extension.
func langForFile(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".sql":
		return "sql"
	case ".sh":
		return "shell"
	case ".tf":
		return "terraform"
	default:
		return ""
	}
}

func scanCmd() *cobra.Command {
	var (
		path     string
		sarifOut string
		failOn   string
		language string
	)
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan files with the deterministic engine (SARIF + fail gate)",
		Long: `Scan a file or directory with VigilAgent's deterministic engine.
Writes a SARIF 2.1.0 report (--sarif) and exits non-zero when findings meet
the --fail-on severity gate — the CI enforcement layer for protected branches.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			engine := scanner.DefaultEngine()

			var files []string
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("scan path: %w", err)
			}
			if !info.IsDir() {
				files = append(files, path)
			} else {
				err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if d.IsDir() {
						// Excluded dirs are skipped during recursive discovery, but an
						// explicitly-passed root (e.g. --path testdata/fixtures/...) is
						// always scanned — fixtures are only excluded as nested dirs.
						// testfixtures/ and *_test files hold INTENTIONAL vulnerable
						// code samples for the accuracy suite — they must never trip
						// the CI fail gate (the gate guards production code). scripts/
						// holds ops/deploy/load-test tooling (env-var-driven shell
						// scripts, k6 tests) — not production application code.
						if p != path && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "dist" || d.Name() == "vendor" || d.Name() == "bin" || d.Name() == "coverage" || d.Name() == "testdata" || d.Name() == "testfixtures" || d.Name() == "scripts") {
							return filepath.SkipDir
						}
						return nil
					}
					// Skip *_test source files: the scanner's own accuracy tests embed
					// deliberately vulnerable code to prove the rules fire. Scanning
					// them as if they were production code would fail every CI run.
					if isTestFile(p) {
						return nil
					}
					// Demo/demonstration files (sample_*) carry intentional vulns to
					// showcase the scanner — never production code, never a gate.
					if strings.HasPrefix(strings.ToLower(filepath.Base(p)), "sample_") {
						return nil
					}
					if scanExtensions[strings.ToLower(filepath.Ext(p))] {
						files = append(files, p)
					}
					return nil
				})
				if err != nil {
					return fmt.Errorf("walk path: %w", err)
				}
			}

			var findings []scanner.Finding
			seen := make(map[string]bool)
			for _, f := range files {
				code, err := os.ReadFile(f)
				if err != nil {
					fmt.Fprintf(os.Stderr, "⚠️ skip %s: %v\n", f, err)
					continue
				}
				lang := language
				if lang == "" || lang == "auto" {
					lang = langForFile(f)
				}
				report := engine.Run(ctx, scanner.Input{
					Language: lang,
					Code:     string(code),
					Filename: f,
				})
				for _, fd := range report.Findings {
					if !scanner.ShouldReport(fd) || seen[fd.Fingerprint] {
						continue
					}
					seen[fd.Fingerprint] = true
					findings = append(findings, fd)
				}
			}

			counts := map[scanner.Severity]int{}
			for _, f := range findings {
				counts[f.Severity]++
				fmt.Printf("[%s] %s:%d — %s (%s)\n",
					strings.ToUpper(string(f.Severity)), f.Filename, f.Line, f.Message, f.RuleID)
			}
			fmt.Printf("\nScanned %d file(s) — %d finding(s): %d critical, %d high, %d medium, %d low\n",
				len(files), len(findings),
				counts[scanner.SeverityCritical], counts[scanner.SeverityHigh],
				counts[scanner.SeverityMedium], counts[scanner.SeverityLow])

			if sarifOut != "" {
				out, err := scanner.ExportSARIF(findings)
				if err != nil {
					return fmt.Errorf("export sarif: %w", err)
				}
				if err := os.WriteFile(sarifOut, out, 0o644); err != nil {
					return fmt.Errorf("write sarif: %w", err)
				}
				fmt.Printf("📄 SARIF report written to %s\n", sarifOut)
			}

			if failOn != "" {
				gate := map[scanner.Severity]bool{}
				for _, s := range strings.Split(failOn, ",") {
					gate[scanner.Severity(strings.ToLower(strings.TrimSpace(s)))] = true
				}
				for _, f := range findings {
					if gate[f.Severity] {
						return fmt.Errorf("❌ %d finding(s) meet the fail gate (--fail-on %s) — blocked by VigilAgent policy",
							len(findings), failOn)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "file or directory to scan")
	cmd.Flags().StringVar(&sarifOut, "sarif", "", "write SARIF 2.1.0 report to this file")
	cmd.Flags().StringVar(&failOn, "fail-on", "high,critical", "comma-separated severities that fail the run (empty = never fail)")
	cmd.Flags().StringVar(&language, "language", "auto", "language hint (auto, go, python, javascript, ...)")
	return cmd
}
