package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func testFileSec(t *testing.T) *FileSecurityConfig {
	t.Helper()
	return &FileSecurityConfig{AllowedDirs: []string{t.TempDir()}}
}

func TestReadFileTool_Name(t *testing.T) {
	r := &ReadFileTool{}
	if r.Name() != "read_file" {
		t.Errorf("Name() = %q, want read_file", r.Name())
	}
}

func TestReadFileTool_Description(t *testing.T) {
	r := &ReadFileTool{}
	if r.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestReadFileTool_RequiresHITL(t *testing.T) {
	r := &ReadFileTool{}
	if r.RequiresHITL(nil) {
		t.Error("ReadFileTool should not require HITL")
	}
}

func TestReadFileTool_Parameters(t *testing.T) {
	r := &ReadFileTool{}
	p := r.Parameters()
	if p["type"] != "object" {
		t.Error("Parameters should have type object")
	}
	props := p["properties"].(map[string]interface{})
	if _, ok := props["path"]; !ok {
		t.Error("missing path property")
	}
	req, ok := p["required"].([]string)
	if !ok {
		t.Fatal("required not a []string")
	}
	if len(req) != 1 || req[0] != "path" {
		t.Errorf("required = %v, want [path]", req)
	}
}

func TestReadFileTool_Execute_Success(t *testing.T) {
	sec := testFileSec(t)
	dir := sec.AllowedDirs[0]
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	r := &ReadFileTool{Security: sec}
	res, err := r.Execute(context.Background(), map[string]interface{}{"path": path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if res.Output != "hello world" {
		t.Errorf("Output = %q, want hello world", res.Output)
	}
	if res.Duration <= 0 {
		t.Error("Duration should be positive")
	}
	if res.Metadata["path"] != path {
		t.Errorf("metadata path = %v", res.Metadata["path"])
	}
}

func TestReadFileTool_Execute_EmptyPath(t *testing.T) {
	r := &ReadFileTool{}
	res, err := r.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for empty path")
	}
}

func TestReadFileTool_Execute_Nonexistent(t *testing.T) {
	r := &ReadFileTool{}
	res, err := r.Execute(context.Background(), map[string]interface{}{"path": "/nonexistent/file.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for nonexistent file")
	}
}

func TestReadFileTool_Execute_NonStringPath(t *testing.T) {
	r := &ReadFileTool{}
	res, err := r.Execute(context.Background(), map[string]interface{}{"path": 123})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for non-string path")
	}
}

func TestWriteFileTool_Name(t *testing.T) {
	w := &WriteFileTool{}
	if w.Name() != "write_file" {
		t.Errorf("Name() = %q, want write_file", w.Name())
	}
}

func TestWriteFileTool_Description(t *testing.T) {
	w := &WriteFileTool{}
	if w.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestWriteFileTool_Parameters(t *testing.T) {
	w := &WriteFileTool{}
	p := w.Parameters()
	if p["type"] != "object" {
		t.Error("Parameters should have type object")
	}
	props := p["properties"].(map[string]interface{})
	if _, ok := props["path"]; !ok {
		t.Error("missing path")
	}
	if _, ok := props["content"]; !ok {
		t.Error("missing content")
	}
}

func TestWriteFileTool_RequiresHITL_NewFile(t *testing.T) {
	w := &WriteFileTool{}
	if w.RequiresHITL(map[string]interface{}{"path": "/nonexistent/path/file.txt"}) {
		t.Error("should not require HITL for new file")
	}
}

func TestWriteFileTool_RequiresHITL_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	os.WriteFile(path, []byte("data"), 0644)

	w := &WriteFileTool{}
	if !w.RequiresHITL(map[string]interface{}{"path": path}) {
		t.Error("should require HITL for existing file")
	}
}

func TestWriteFileTool_RequiresHITL_EmptyPath(t *testing.T) {
	w := &WriteFileTool{}
	if w.RequiresHITL(map[string]interface{}{}) {
		t.Error("should not require HITL for empty path")
	}
}

func TestWriteFileTool_Execute_Success(t *testing.T) {
	sec := testFileSec(t)
	dir := sec.AllowedDirs[0]
	path := filepath.Join(dir, "new.txt")

	w := &WriteFileTool{Security: sec}
	res, err := w.Execute(context.Background(), map[string]interface{}{
		"path":    path,
		"content": "test content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "test content" {
		t.Errorf("file content = %q, want test content", string(data))
	}
}

func TestWriteFileTool_Execute_CreatesDir(t *testing.T) {
	sec := testFileSec(t)
	dir := sec.AllowedDirs[0]
	path := filepath.Join(dir, "sub", "dir", "file.txt")

	w := &WriteFileTool{Security: sec}
	res, err := w.Execute(context.Background(), map[string]interface{}{
		"path":    path,
		"content": "nested",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
}

func TestWriteFileTool_Execute_EmptyPath(t *testing.T) {
	w := &WriteFileTool{}
	res, err := w.Execute(context.Background(), map[string]interface{}{"content": "data"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for empty path")
	}
}

func TestWriteFileTool_Execute_Overwrite(t *testing.T) {
	sec := testFileSec(t)
	dir := sec.AllowedDirs[0]
	path := filepath.Join(dir, "overwrite.txt")
	os.WriteFile(path, []byte("old"), 0644)

	w := &WriteFileTool{Security: sec}
	res, err := w.Execute(context.Background(), map[string]interface{}{
		"path":    path,
		"content": "new content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new content" {
		t.Errorf("file content = %q, want new content", string(data))
	}
}

func TestWriteFileTool_Execute_MkdirAllError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard to trigger MkdirAll failure on Windows")
	}
	w := &WriteFileTool{Security: testFileSec(t)}
	res, err := w.Execute(context.Background(), map[string]interface{}{
		"path":    "/dev/null/impossible/file.txt",
		"content": "data",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for MkdirAll error")
	}
}

func TestWriteFileTool_Execute_WriteFileError(t *testing.T) {
	sec := testFileSec(t)
	dir := sec.AllowedDirs[0]
	path := filepath.Join(dir, "readonly.txt")
	os.WriteFile(path, []byte("old"), 0644)
	os.Chmod(path, 0444)

	w := &WriteFileTool{}
	res, err := w.Execute(context.Background(), map[string]interface{}{
		"path":    path,
		"content": "new",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Skip("chmod did not prevent write (running as admin?)")
	}
}

func TestWriteFileTool_Execute_NilContent(t *testing.T) {
	sec := testFileSec(t)
	dir := sec.AllowedDirs[0]
	path := filepath.Join(dir, "nil_content.txt")

	w := &WriteFileTool{Security: sec}
	res, err := w.Execute(context.Background(), map[string]interface{}{
		"path": path,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	data, _ := os.ReadFile(path)
	if len(data) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(data))
	}
}

func TestEditFileTool_Name(t *testing.T) {
	e := &EditFileTool{}
	if e.Name() != "edit_file" {
		t.Errorf("Name() = %q, want edit_file", e.Name())
	}
}

func TestEditFileTool_Description(t *testing.T) {
	e := &EditFileTool{}
	if e.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestEditFileTool_RequiresHITL(t *testing.T) {
	e := &EditFileTool{}
	if !e.RequiresHITL(nil) {
		t.Error("EditFileTool should always require HITL")
	}
}

func TestEditFileTool_Parameters(t *testing.T) {
	e := &EditFileTool{}
	p := e.Parameters()
	if p["type"] != "object" {
		t.Error("Parameters should have type object")
	}
	props := p["properties"].(map[string]interface{})
	if _, ok := props["path"]; !ok {
		t.Error("missing path")
	}
	if _, ok := props["old_string"]; !ok {
		t.Error("missing old_string")
	}
	if _, ok := props["new_string"]; !ok {
		t.Error("missing new_string")
	}
}

func TestEditFileTool_Execute_Success(t *testing.T) {
	sec := testFileSec(t)
	dir := sec.AllowedDirs[0]
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	e := &EditFileTool{Security: sec}
	res, err := e.Execute(context.Background(), map[string]interface{}{
		"path":       path,
		"old_string": "world",
		"new_string": "Go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello Go" {
		t.Errorf("file content = %q, want hello Go", string(data))
	}
}

func TestEditFileTool_Execute_MissingParams(t *testing.T) {
	e := &EditFileTool{}
	res, err := e.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for missing params")
	}
}

func TestEditFileTool_Execute_EmptyPath(t *testing.T) {
	e := &EditFileTool{}
	res, err := e.Execute(context.Background(), map[string]interface{}{
		"old_string": "a",
		"new_string": "b",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for empty path")
	}
}

func TestEditFileTool_Execute_EmptyOldString(t *testing.T) {
	e := &EditFileTool{}
	res, err := e.Execute(context.Background(), map[string]interface{}{
		"path":       "/some/path",
		"old_string": "",
		"new_string": "b",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for empty old_string")
	}
}

func TestEditFileTool_Execute_NonexistentFile(t *testing.T) {
	e := &EditFileTool{}
	res, err := e.Execute(context.Background(), map[string]interface{}{
		"path":       "/nonexistent/file.txt",
		"old_string": "a",
		"new_string": "b",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for nonexistent file")
	}
}

func TestEditFileTool_Execute_NotFound(t *testing.T) {
	sec := testFileSec(t)
	dir := sec.AllowedDirs[0]
	path := filepath.Join(dir, "no_match.txt")
	os.WriteFile(path, []byte("content"), 0644)

	e := &EditFileTool{Security: sec}
	res, err := e.Execute(context.Background(), map[string]interface{}{
		"path":       path,
		"old_string": "nonexistent string",
		"new_string": "replacement",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure when old_string not found")
	}
}

func TestEditFileTool_Execute_WriteFileError(t *testing.T) {
	sec := testFileSec(t)
	dir := sec.AllowedDirs[0]
	path := filepath.Join(dir, "readonly.txt")
	os.WriteFile(path, []byte("hello world"), 0644)
	os.Chmod(path, 0444)

	e := &EditFileTool{Security: sec}
	res, err := e.Execute(context.Background(), map[string]interface{}{
		"path":       path,
		"old_string": "world",
		"new_string": "Go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Skip("chmod did not prevent write (running as admin?)")
	}
}

func TestListDirectoryTool_Name(t *testing.T) {
	l := &ListDirectoryTool{}
	if l.Name() != "list_directory" {
		t.Errorf("Name() = %q, want list_directory", l.Name())
	}
}

func TestListDirectoryTool_Description(t *testing.T) {
	l := &ListDirectoryTool{}
	if l.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestListDirectoryTool_RequiresHITL(t *testing.T) {
	l := &ListDirectoryTool{}
	if l.RequiresHITL(nil) {
		t.Error("ListDirectoryTool should not require HITL")
	}
}

func TestListDirectoryTool_Parameters(t *testing.T) {
	l := &ListDirectoryTool{}
	p := l.Parameters()
	if p["type"] != "object" {
		t.Error("Parameters should have type object")
	}
}

func TestListDirectoryTool_Execute_Success(t *testing.T) {
	sec := testFileSec(t)
	dir := sec.AllowedDirs[0]
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	l := &ListDirectoryTool{Security: sec}
	res, err := l.Execute(context.Background(), map[string]interface{}{"path": dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if res.Metadata["dirs"] != 1 {
		t.Errorf("dirs = %v, want 1", res.Metadata["dirs"])
	}
	if res.Metadata["files"] != 2 {
		t.Errorf("files = %v, want 2", res.Metadata["files"])
	}
}

func TestListDirectoryTool_Execute_EmptyPath(t *testing.T) {
	l := &ListDirectoryTool{}
	res, err := l.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success with default path, got error: %s", res.Error)
	}
}

func TestListDirectoryTool_Execute_Nonexistent(t *testing.T) {
	l := &ListDirectoryTool{}
	res, err := l.Execute(context.Background(), map[string]interface{}{"path": "/nonexistent/dir"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for nonexistent directory")
	}
}
