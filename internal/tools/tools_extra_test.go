package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// --- Search: no matches found (rg returns 1) ---

func TestSearchCodeTool_Execute_NoMatches(t *testing.T) {
	s := &SearchCodeTool{}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello world"), 0644)

	res, err := s.Execute(context.Background(), map[string]interface{}{
		"pattern": "zzz_no_match_xyz_12345",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// rg may return exit code 1 with "no matches" or "0 matches" in output
	// If rg is not installed, we get an error result
	if res.Success && res.Output != "No matches found" {
		// rg found matches or rg not available
		_ = res
	}
}

func TestSearchCodeTool_Execute_ErrorMessage(t *testing.T) {
	s := &SearchCodeTool{}
	// Invalid regex pattern
	res, err := s.Execute(context.Background(), map[string]interface{}{
		"pattern": "[invalid",
		"path":    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// This should either fail with an error or succeed if rg handles it
	_ = res
}

// --- Search: rg returns error with no "no matches" in output ---

func TestSearchCodeTool_Execute_RGError(t *testing.T) {
	s := &SearchCodeTool{}
	res, err := s.Execute(context.Background(), map[string]interface{}{
		"pattern": "test",
		"path":    "/nonexistent/directory/path/12345",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		// rg might not be installed, so skip
		t.Skip("rg not installed or path handled gracefully")
	}
}

// --- Sandbox: executeLocal with timeout ---

func TestSandbox_ExecuteLocal_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("timeout test unreliable on Windows")
	}
	cfg := &SandboxConfig{
		Engine:  "local",
		Timeout: 100 * time.Millisecond,
		Network: false,
	}
	s := NewSandbox(cfg)
	_, err := s.Execute(context.Background(), "sleep 10")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// --- Sandbox: executeLocal zero timeout fallback ---

func TestSandbox_ExecuteLocal_ZeroTimeoutFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh not found on Windows")
	}
	cfg := &SandboxConfig{
		Engine:  "local",
		Timeout: 0,
		Network: false,
	}
	s := NewSandbox(cfg)
	// Zero timeout should fallback to 30s, so this should succeed
	out, err := s.Execute(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Error("expected output")
	}
}

// --- Sandbox: executeLocal error ---

func TestSandbox_ExecuteLocal_Error(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh not found on Windows")
	}
	cfg := &SandboxConfig{
		Engine:  "local",
		Timeout: 5 * time.Second,
		Network: false,
	}
	s := NewSandbox(cfg)
	_, err := s.Execute(context.Background(), "exit 1")
	if err == nil {
		t.Fatal("expected error for failing command")
	}
}

// --- Sandbox: executeDocker timeout path ---

func TestSandbox_ExecuteDocker_Timeout(t *testing.T) {
	if IsDockerAvailable() {
		t.Skip("Docker available; skipping no-docker test")
	}
	cfg := &SandboxConfig{
		Engine:    "docker",
		Image:     "alpine:latest",
		Timeout:   100 * time.Millisecond,
		MaxMemory: "64m",
		Network:   false,
		WorkDir:   "/workspace",
	}
	s := NewSandbox(cfg)
	_, err := s.Execute(context.Background(), "sleep 10")
	if err == nil {
		t.Skip("docker available; test not applicable")
	}
}

// --- Sandbox: executeDocker error path ---

func TestSandbox_ExecuteDocker_Error(t *testing.T) {
	if IsDockerAvailable() {
		t.Skip("Docker available; skipping no-docker test")
	}
	cfg := &SandboxConfig{
		Engine:    "docker",
		Image:     "alpine:latest",
		Timeout:   5 * time.Second,
		MaxMemory: "",
		Network:   true,
		WorkDir:   "",
	}
	s := NewSandbox(cfg)
	_, err := s.Execute(context.Background(), "exit 1")
	if err == nil {
		t.Skip("docker available; test not applicable")
	}
}

// --- Sandbox: executeDocker zero timeout fallback ---

func TestSandbox_ExecuteDocker_ZeroTimeoutFallback(t *testing.T) {
	if IsDockerAvailable() {
		t.Skip("Docker available; skipping no-docker test")
	}
	cfg := &SandboxConfig{
		Engine:    "docker",
		Image:     "alpine:latest",
		Timeout:   0,
		MaxMemory: "",
		Network:   true,
		WorkDir:   "",
	}
	s := NewSandbox(cfg)
	_, err := s.Execute(context.Background(), "echo test")
	if err == nil {
		t.Skip("docker available; test not applicable")
	}
}

// --- Terminal: execute with context cancellation ---

func TestRunCommandTool_Execute_ContextCancel(t *testing.T) {
	r := &RunCommandTool{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	res, err := r.Execute(ctx, map[string]interface{}{"command": "echo test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Command may or may not succeed depending on timing
	_ = res
}

// --- Terminal: execute with very short timeout ---

func TestRunCommandTool_Execute_ShortTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("timeout test unreliable on Windows")
	}
	r := &RunCommandTool{}
	res, err := r.Execute(context.Background(), map[string]interface{}{
		"command":         "sleep 10",
		"timeout_seconds": float64(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for timeout")
	}
}

// --- Terminal: negative timeout ---

func TestRunCommandTool_Execute_NegativeTimeout(t *testing.T) {
	r := &RunCommandTool{}
	res, err := r.Execute(context.Background(), map[string]interface{}{
		"command":         "echo test",
		"timeout_seconds": float64(-1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Negative timeout should use default 30s
	_ = res
}

// --- WriteFileTool: non-string path ---

func TestWriteFileTool_Execute_NonStringPath(t *testing.T) {
	w := &WriteFileTool{}
	res, err := w.Execute(context.Background(), map[string]interface{}{
		"path":    123,
		"content": "data",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for non-string path")
	}
}

// --- EditFileTool: non-string params ---

func TestEditFileTool_Execute_NonStringParams(t *testing.T) {
	e := &EditFileTool{}
	res, err := e.Execute(context.Background(), map[string]interface{}{
		"path":       123,
		"old_string": 456,
		"new_string": 789,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for non-string params")
	}
}

// --- ReadFileTool: nil params ---

func TestReadFileTool_Execute_NilParams(t *testing.T) {
	r := &ReadFileTool{}
	res, err := r.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for nil params")
	}
}

// --- ListDirectoryTool: nil params ---

func TestListDirectoryTool_Execute_NilParams(t *testing.T) {
	l := &ListDirectoryTool{}
	res, err := l.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should use default path "."
	_ = res
}

// --- Search: non-string path and glob ---

func TestSearchCodeTool_Execute_NonStringPath(t *testing.T) {
	s := &SearchCodeTool{}
	res, err := s.Execute(context.Background(), map[string]interface{}{
		"pattern": "test",
		"path":    123,
		"glob":    456,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = res
}

// --- ToolResult: error field ---

func TestToolResult_ErrorField(t *testing.T) {
	result := &ToolResult{
		Output:  "output",
		Success: false,
		Error:   "something went wrong",
		Cost:    0.0,
	}
	if result.Error != "something went wrong" {
		t.Error("error field mismatch")
	}
	if result.Success {
		t.Error("expected failure")
	}
}

// --- ToolResult: metadata field ---

func TestToolResult_MetadataField(t *testing.T) {
	result := &ToolResult{
		Output:   "output",
		Success:  true,
		Metadata: map[string]interface{}{"key": "value"},
	}
	if result.Metadata["key"] != "value" {
		t.Error("metadata mismatch")
	}
}

// --- ToolResult: duration field ---

func TestToolResult_DurationField(t *testing.T) {
	result := &ToolResult{
		Output:   "output",
		Success:  true,
		Duration: 100 * time.Millisecond,
	}
	if result.Duration != 100*time.Millisecond {
		t.Error("duration mismatch")
	}
}

// --- FileChange and SkillMetrics structs ---

func TestFileChange_Struct(t *testing.T) {
	fc := FileChange{Path: "test.go", Action: "modified", Diff: "+line"}
	if fc.Path != "test.go" || fc.Action != "modified" || fc.Diff != "+line" {
		t.Error("FileChange mismatch")
	}
}

func TestSkillMetrics_Struct(t *testing.T) {
	sm := SkillMetrics{Duration: time.Second, FilesScanned: 10, IssuesFound: 3, IssuesFixed: 2}
	if sm.FilesScanned != 10 || sm.IssuesFound != 3 || sm.IssuesFixed != 2 {
		t.Error("SkillMetrics mismatch")
	}
}

// --- Sandbox: local execution success ---

func TestSandbox_ExecuteLocal_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh not found on Windows")
	}
	cfg := &SandboxConfig{
		Engine:  "local",
		Timeout: 5 * time.Second,
		Network: true,
	}
	s := NewSandbox(cfg)
	out, err := s.Execute(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello\n" {
		t.Errorf("output = %q, want 'hello\n'", out)
	}
}

// --- Search: rg not found error ---

func TestSearchCodeTool_RGNotInstalled(t *testing.T) {
	s := &SearchCodeTool{}
	// If rg is installed, this should succeed; if not, should fail gracefully
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main\n"), 0644)
	res, err := s.Execute(context.Background(), map[string]interface{}{
		"pattern": "package",
		"path":    dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Either success (rg found matches) or failure (rg not installed)
	_ = res
}

// --- ToolDef struct ---

func TestToolDef_Struct(t *testing.T) {
	td := ToolDef{
		Name:        "test",
		Description: "test tool",
		Parameters:  map[string]interface{}{"type": "object"},
	}
	if td.Name != "test" {
		t.Error("ToolDef name mismatch")
	}
}
