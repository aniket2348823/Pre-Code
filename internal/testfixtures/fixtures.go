// Package testfixtures provides the shared vulnerability fixture corpus used by
// Go tests (scanner accuracy, proxy verdicts) and by the VS Code extension's
// manual-testing workflow. The fixtures live in <repo>/testdata/fixtures and
// each vulnerable snippet carries golden expectations in manifest.json.
package testfixtures

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Expectation is a golden assertion: a rule-ID substring that must appear on a
// finding of the given severity.
type Expectation struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
}

// FixtureEntry describes one fixture file and its golden expectations.
type FixtureEntry struct {
	File     string        `json:"file"`
	Language string        `json:"language"`
	Expect   []Expectation `json:"expect,omitempty"`
}

// Manifest lists the vulnerable fixtures (with golden expectations) and the
// clean fixtures (which must not produce serious findings).
type Manifest struct {
	Vulnerable []FixtureEntry `json:"vulnerable"`
	Clean      []FixtureEntry `json:"clean"`
}

// Root returns the absolute path to the fixture corpus directory.
func Root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "fixtures")
}

// LoadManifest reads and parses manifest.json from the corpus root.
func LoadManifest() (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(Root(), "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("read fixture manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse fixture manifest: %w", err)
	}
	return &m, nil
}

// Read returns the code for a fixture file (path relative to the corpus root).
func Read(file string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(Root(), file))
	if err != nil {
		return "", fmt.Errorf("read fixture %s: %w", file, err)
	}
	return string(raw), nil
}
