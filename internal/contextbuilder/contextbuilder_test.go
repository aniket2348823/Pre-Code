package contextbuilder

import (
	"strings"
	"testing"

	"github.com/vigilagent/vigilagent/internal/util"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxFiles <= 0 {
		t.Error("MaxFiles should be positive")
	}
	if cfg.MaxFileLines <= 0 {
		t.Error("MaxFileLines should be positive")
	}
	if cfg.MaxTokenBudget <= 0 {
		t.Error("MaxTokenBudget should be positive")
	}
	if cfg.SystemPrompt == "" {
		t.Error("SystemPrompt should not be empty")
	}
}

func TestNewBuilder(t *testing.T) {
	b := NewBuilder(nil)
	if b == nil {
		t.Fatal("NewBuilder should not return nil")
	}
	if b.config == nil {
		t.Error("config should be set")
	}
}

func TestBuildContext(t *testing.T) {
	b := NewBuilder(nil)
	req := &BuildRequest{
		Files: []File{
			{Path: "main.go", Content: "package main\n\nfunc main() {}", Language: "go"},
		},
		OpenTabs:      []string{"main.go"},
		GitBranch:     "main",
		ProjectType:   "go",
		RecentCommits: []string{"feat: add login"},
	}

	pc, err := b.BuildContext(nil, req)
	if err != nil {
		t.Fatalf("BuildContext failed: %v", err)
	}
	if len(pc.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(pc.Files))
	}
	if pc.GitBranch != "main" {
		t.Errorf("expected main, got %s", pc.GitBranch)
	}
	if pc.ProjectType != "go" {
		t.Errorf("expected go, got %s", pc.ProjectType)
	}
}

func TestBuildPrompt(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		Files: []File{
			{Path: "user.go", Content: "package main", Language: "go"},
		},
		ProjectType:  "go",
		GitBranch:    "feature/auth",
		Conventions: []Convention{
			{Category: "naming", Pattern: "Go conventions"},
		},
	}

	prompt := b.BuildPrompt(pc, "Fix the login bug")
	if prompt == "" {
		t.Error("prompt should not be empty")
	}
	if !containsStr(prompt, "Fix the login bug") {
		t.Error("prompt should contain task description")
	}
	if !containsStr(prompt, "feature/auth") {
		t.Error("prompt should contain git branch")
	}
	if !containsStr(prompt, "Go conventions") {
		t.Error("prompt should contain conventions")
	}
}

func TestBuildPromptWithBudget(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		Files: []File{
			{Path: "big.go", Content: "package main\n// lots of code\n" + strings.Repeat("// line\n", 1000), Language: "go"},
		},
		ProjectType: "go",
	}

	// Small budget should truncate
	prompt := b.BuildPromptWithBudget(pc, "Fix bug", 200)
	if prompt == "" {
		t.Error("prompt should not be empty")
	}
}

func TestDetectConventions(t *testing.T) {
	b := NewBuilder(nil)
	files := []File{
		{Path: "main.go", Content: "package main\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tif err != nil {\n\t\treturn\n\t}\n}", Language: "go"},
	}

	conventions := b.detectConventions(files)
	if len(conventions) == 0 {
		t.Error("should detect at least one convention")
	}

	// Check for naming convention
	found := false
	for _, c := range conventions {
		if c.Category == "naming" {
			found = true
			break
		}
	}
	if !found {
		t.Error("should detect naming convention for Go")
	}
}

func TestDetectDependencies(t *testing.T) {
	b := NewBuilder(nil)
	files := []File{
		{
			Path:    "go.mod",
			Content: "module example\n\nrequire (\n\tgithub.com/chi/chi/v5 v5.0.0\n\tgithub.com/jackc/pgx/v5 v5.0.0\n)",
			Language: "go",
		},
	}

	deps := b.detectDependencies(files)
	if len(deps) < 2 {
		t.Errorf("expected at least 2 dependencies, got %d", len(deps))
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		expect string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := util.Truncate(tt.input, tt.maxLen)
		if got != tt.expect {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.expect)
		}
	}
}

func TestBuildRequest(t *testing.T) {
	req := &BuildRequest{
		Files: []File{
			{Path: "test.go", Content: "package main", Language: "go"},
		},
		OpenTabs:   []string{"test.go"},
		GitBranch:  "main",
		ProjectType: "go",
		Cursor:     &CursorPosition{File: "test.go", Line: 10, Column: 5},
	}

	if len(req.Files) != 1 {
		t.Error("expected 1 file")
	}
	if req.Cursor.Line != 10 {
		t.Error("expected line 10")
	}
}

func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}
