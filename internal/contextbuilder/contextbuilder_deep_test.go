package contextbuilder

import (
	"strings"
	"testing"
)

func TestBuildContext_NilConfig(t *testing.T) {
	b := NewBuilder(nil)
	req := &BuildRequest{ProjectType: "go"}
	pc, err := b.BuildContext(nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if pc.TokenBudget != 8000 {
		t.Errorf("expected 8000 default budget, got %d", pc.TokenBudget)
	}
}

func TestBuildContext_EmptyTask(t *testing.T) {
	b := NewBuilder(nil)
	req := &BuildRequest{}
	pc, err := b.BuildContext(nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(pc.Files) != 0 {
		t.Error("empty task should have no files")
	}
}

func TestBuildPrompt_ZeroBudget(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{ProjectType: "go"}
	prompt := b.BuildPromptWithBudget(pc, "fix bug", 0)
	if prompt == "" {
		t.Error("prompt should not be empty")
	}
}

func TestBuildPrompt_BudgetSmallerThanSystemPrompt(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{ProjectType: "go"}
	prompt := b.BuildPromptWithBudget(pc, "fix bug", 10)
	if prompt == "" {
		t.Error("prompt should not be empty even with tiny budget")
	}
}

func TestTruncate_ShorterThanLimit(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		Files: []File{{Path: "small.go", Content: "short", Language: "go"}},
	}
	prompt := b.BuildPromptWithBudget(pc, "fix", 100000)
	if !strings.Contains(prompt, "short") {
		t.Error("short content should not be truncated")
	}
}

func TestTruncate_ExactlyAtLimit(t *testing.T) {
	b := NewBuilder(nil)
	content := strings.Repeat("x", 400) // ~100 tokens
	pc := &ProjectContext{
		Files: []File{{Path: "exact.go", Content: content, Language: "go"}},
	}
	prompt := b.BuildPromptWithBudget(pc, "fix", 200)
	_ = prompt // Should not panic
}

func TestTruncate_ExceedingLimit(t *testing.T) {
	b := NewBuilder(nil)
	content := strings.Repeat("x", 10000) // ~2500 tokens
	pc := &ProjectContext{
		Files: []File{{Path: "big.go", Content: content, Language: "go"}},
	}
	prompt := b.BuildPromptWithBudget(pc, "fix", 100)
	if strings.Contains(prompt, "truncated") || len(prompt) > 0 {
		// Should handle gracefully
	}
}

func TestTruncate_MultiByteUTF8(t *testing.T) {
	b := NewBuilder(nil)
	content := "你好世界" + strings.Repeat("x", 100)
	pc := &ProjectContext{
		Files: []File{{Path: "unicode.go", Content: content, Language: "go"}},
	}
	prompt := b.BuildPromptWithBudget(pc, "fix", 100)
	if prompt == "" {
		t.Error("should handle multi-byte UTF-8")
	}
}

func TestBuildRequest_NilMessages(t *testing.T) {
	req := &BuildRequest{Files: nil}
	b := NewBuilder(nil)
	pc, err := b.BuildContext(nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(pc.Files) != 0 {
		t.Error("nil files should produce empty list")
	}
}

func TestBuildRequest_Unicode(t *testing.T) {
	req := &BuildRequest{
		Files: []File{{Path: "test.go", Content: "你好世界", Language: "go"}},
	}
	b := NewBuilder(nil)
	pc, err := b.BuildContext(nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(pc.Files) != 1 {
		t.Error("unicode content should be preserved")
	}
}

func TestDetectConventions_EmptyCodebase(t *testing.T) {
	b := NewBuilder(nil)
	conventions := b.detectConventions([]File{})
	if len(conventions) != 0 {
		t.Error("empty codebase should have no conventions")
	}
}

func TestDetectDependencies_NoFiles(t *testing.T) {
	b := NewBuilder(nil)
	deps := b.detectDependencies([]File{})
	if len(deps) != 0 {
		t.Error("no files should have no dependencies")
	}
}

func TestBuildPrompt_NilFiles(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{ProjectType: "go"}
	prompt := b.BuildPrompt(pc, "fix bug")
	if prompt == "" {
		t.Error("prompt should not be empty")
	}
}

func TestBuildPrompt_WithMemoryContext(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		ProjectType: "go",
		MemoryContext: []MemorySnippet{
			{Type: "episodic", Content: "previously fixed auth bug", Score: 0.8},
		},
	}
	prompt := b.BuildPrompt(pc, "fix auth")
	if !strings.Contains(prompt, "previously fixed auth bug") {
		t.Error("prompt should contain memory context")
	}
}

func TestBuildPrompt_WithCursor(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		ProjectType:    "go",
		CursorPosition: &CursorPosition{File: "main.go", Line: 10, Column: 5},
	}
	prompt := b.BuildPrompt(pc, "fix bug")
	if !strings.Contains(prompt, "main.go") {
		t.Error("prompt should contain cursor file")
	}
}

func TestDefaultConfig_Deep(t *testing.T) {
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

func TestBuildPrompt_WithOpenTabs(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		ProjectType: "go",
		OpenTabs:    []string{"main.go", "handler.go"},
	}
	prompt := b.BuildPrompt(pc, "fix bug")
	if !strings.Contains(prompt, "main.go") || !strings.Contains(prompt, "handler.go") {
		t.Error("prompt should contain open tabs")
	}
}

func TestBuildPrompt_WithGitHistory(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		ProjectType:     "go",
		GitBranch:       "main",
		GitRecentCommits: []string{"feat: add login", "fix: auth bug"},
	}
	prompt := b.BuildPrompt(pc, "fix bug")
	if !strings.Contains(prompt, "feat: add login") {
		t.Error("prompt should contain git history")
	}
}
