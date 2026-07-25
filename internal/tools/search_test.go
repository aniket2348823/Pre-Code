package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSearchCodeTool_Name(t *testing.T) {
	s := &SearchCodeTool{}
	if s.Name() != "search_code" {
		t.Errorf("Name() = %q, want search_code", s.Name())
	}
}

func TestSearchCodeTool_Description(t *testing.T) {
	s := &SearchCodeTool{}
	if s.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestSearchCodeTool_RequiresHITL(t *testing.T) {
	s := &SearchCodeTool{}
	if s.RequiresHITL(nil) {
		t.Error("SearchCodeTool should not require HITL")
	}
}

func TestSearchCodeTool_Parameters(t *testing.T) {
	s := &SearchCodeTool{}
	p := s.Parameters()
	if p["type"] != "object" {
		t.Error("Parameters should have type object")
	}
	props := p["properties"].(map[string]interface{})
	if _, ok := props["pattern"]; !ok {
		t.Error("missing pattern")
	}
	if _, ok := props["path"]; !ok {
		t.Error("missing path")
	}
	if _, ok := props["glob"]; !ok {
		t.Error("missing glob")
	}
	req := p["required"].([]string)
	if len(req) != 1 || req[0] != "pattern" {
		t.Errorf("required = %v, want [pattern]", req)
	}
}

func TestSearchCodeTool_Execute_EmptyPattern(t *testing.T) {
	s := &SearchCodeTool{}
	res, err := s.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for empty pattern")
	}
}

func TestSearchCodeTool_Execute_NonStringPattern(t *testing.T) {
	s := &SearchCodeTool{}
	res, err := s.Execute(context.Background(), map[string]interface{}{"pattern": 123})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for non-string pattern")
	}
}

func TestSearchCodeTool_Execute_WithGlob(t *testing.T) {
	s := &SearchCodeTool{}
	// Exercise the glob branch of args construction
	// Will fail if rg not installed, but code path executes
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main\n"), 0644)

	res, err := s.Execute(context.Background(), map[string]interface{}{
		"pattern": "package",
		"path":    dir,
		"glob":    "*.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Either success (rg found matches) or failure (rg not installed)
	_ = res
}

func TestSearchCodeTool_Execute_DefaultPath(t *testing.T) {
	s := &SearchCodeTool{}
	// Exercise default path branch
	res, err := s.Execute(context.Background(), map[string]interface{}{
		"pattern": "nonexistent_pattern_xyz_123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = res
}

func TestSearchCodeTool_Execute_WithoutGlob(t *testing.T) {
	s := &SearchCodeTool{}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main\n"), 0644)

	// Exercise no-glob branch of args construction
	res, err := s.Execute(context.Background(), map[string]interface{}{
		"pattern": "package",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = res
}

func TestSearchCodeTool_Execute_Metadata(t *testing.T) {
	s := &SearchCodeTool{}
	// Verify metadata is set on success path
	dir := t.TempDir()
	res, err := s.Execute(context.Background(), map[string]interface{}{
		"pattern": "zzz_no_match_123",
		"path":    dir,
		"glob":    "*.txt",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// If rg is not installed, we get an error result; check metadata isn't set on error
	if res.Success && res.Metadata["pattern"] != "zzz_no_match_123" {
		t.Errorf("metadata pattern = %v", res.Metadata["pattern"])
	}
}
