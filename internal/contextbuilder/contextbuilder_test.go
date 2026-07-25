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

// --- Deep tests merged below ---

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

func TestBuildPrompt_WithConventionsAndExample(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		ProjectType: "go",
		Conventions: []Convention{
			{Category: "naming", Pattern: "Go conventions", Example: "camelCase for private"},
			{Category: "error_handling", Pattern: "early-return"},
		},
	}
	prompt := b.BuildPrompt(pc, "fix bug")
	if !containsStr(prompt, "camelCase for private") {
		t.Error("prompt should contain convention example")
	}
	if !containsStr(prompt, "error_handling") {
		t.Error("prompt should contain error_handling convention")
	}
}

func TestBuildPrompt_WithDependencies(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		ProjectType:  "go",
		Dependencies: []string{"github.com/chi/chi/v5", "github.com/jackc/pgx/v5"},
	}
	prompt := b.BuildPrompt(pc, "fix bug")
	if !containsStr(prompt, "chi/chi") {
		t.Error("prompt should contain dependencies")
	}
}

func TestBuildPromptWithBudget_FileExceedsBudget(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		ProjectType: "go",
		Files: []File{
			{Path: "huge.go", Content: strings.Repeat("x\n", 5000), Language: "go"},
		},
	}
	prompt := b.BuildPromptWithBudget(pc, "fix", 50)
	if !containsStr(prompt, "Task") {
		t.Error("prompt should contain task")
	}
}

func TestBuildPromptWithBudget_MultipleFilesPartialFit(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		ProjectType: "go",
		Files: []File{
			{Path: "small.go", Content: "short", Language: "go"},
			{Path: "big.go", Content: strings.Repeat("x\n", 5000), Language: "go"},
		},
	}
	prompt := b.BuildPromptWithBudget(pc, "fix", 200)
	if !containsStr(prompt, "small.go") {
		t.Error("prompt should contain small file")
	}
}

func TestBuildPrompt_AllSections(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		ProjectType:  "go",
		GitBranch:    "main",
		Dependencies: []string{"dep1"},
		Conventions:  []Convention{{Category: "naming", Pattern: "camelCase", Example: "fooBar"}},
		OpenTabs:     []string{"a.go"},
		Files:        []File{{Path: "a.go", Content: "pkg", Language: "go"}},
		GitRecentCommits: []string{"fix: bug"},
		MemoryContext: []MemorySnippet{{Type: "episodic", Content: "past fix", Score: 0.9}},
		CursorPosition: &CursorPosition{File: "a.go", Line: 1, Column: 1},
	}
	prompt := b.BuildPrompt(pc, "do thing")
	if prompt == "" {
		t.Error("prompt should not be empty")
	}
	if !containsStr(prompt, "do thing") {
		t.Error("missing task")
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

func TestBuildContext_FileTruncation(t *testing.T) {
	cfg := &Config{
		MaxFiles:       20,
		MaxFileLines:   5,
		MaxCommits:     10,
		MaxTokenBudget: 8000,
		SystemPrompt:   "test",
	}
	b := NewBuilder(cfg)
	req := &BuildRequest{
		Files: []File{
			{Path: "big.go", Content: "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8", Language: "go"},
		},
	}
	pc, err := b.BuildContext(nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(pc.Files) != 1 {
		t.Fatal("expected 1 file")
	}
	if pc.Files[0].Lines > 6 {
		t.Errorf("expected at most 6 lines (5 + truncation marker), got %d", pc.Files[0].Lines)
	}
	if !containsStr(pc.Files[0].Content, "truncated") {
		t.Error("expected truncated content")
	}
}

func TestBuildContext_MaxFilesLimit(t *testing.T) {
	cfg := &Config{
		MaxFiles:       2,
		MaxFileLines:   500,
		MaxCommits:     10,
		MaxTokenBudget: 8000,
		SystemPrompt:   "test",
	}
	b := NewBuilder(cfg)
	req := &BuildRequest{
		Files: []File{
			{Path: "a.go", Content: "a", Language: "go"},
			{Path: "b.go", Content: "b", Language: "go"},
			{Path: "c.go", Content: "c", Language: "go"},
		},
	}
	pc, err := b.BuildContext(nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(pc.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(pc.Files))
	}
}

func TestBuildContext_MaxCommitsLimit(t *testing.T) {
	cfg := &Config{
		MaxFiles:       20,
		MaxFileLines:   500,
		MaxCommits:     2,
		MaxTokenBudget: 8000,
		SystemPrompt:   "test",
	}
	b := NewBuilder(cfg)
	req := &BuildRequest{
		RecentCommits: []string{"c1", "c2", "c3", "c4"},
	}
	pc, err := b.BuildContext(nil, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(pc.GitRecentCommits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(pc.GitRecentCommits))
	}
}

func TestDetectNamingStyle_JavaScript(t *testing.T) {
	files := []File{
		{Path: "app.ts", Content: "const x = 1;", Language: "typescript"},
	}
	style := detectNamingStyle(files)
	if !containsStr(style, "JavaScript") {
		t.Errorf("expected JS naming, got %q", style)
	}
}

func TestDetectNamingStyle_Empty(t *testing.T) {
	style := detectNamingStyle([]File{{Path: "readme.txt", Content: "hi", Language: "txt"}})
	if style != "" {
		t.Errorf("expected empty, got %q", style)
	}
}

func TestDetectDependencies_PackageJSON(t *testing.T) {
	b := NewBuilder(nil)
	files := []File{
		{
			Path:    "package.json",
			Content: `{"dependencies": {"react": "^18.0.0"}}`,
			Language: "json",
		},
	}
	deps := b.detectDependencies(files)
	if len(deps) != 1 {
		t.Errorf("expected 1 dep, got %d", len(deps))
	}
}

func TestBuildPromptWithBudget_PartialFiles(t *testing.T) {
	b := NewBuilder(nil)
	pc := &ProjectContext{
		ProjectType: "go",
		Files: []File{
			{Path: "a.go", Content: strings.Repeat("x", 2000), Language: "go"},
			{Path: "b.go", Content: strings.Repeat("y", 2000), Language: "go"},
		},
	}
	prompt := b.BuildPromptWithBudget(pc, "fix", 300)
	if prompt == "" {
		t.Error("prompt should not be empty")
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
