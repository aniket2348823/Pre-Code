// Package contextbuilder assembles workspace-aware prompts that give LLMs
// better context. It gathers open files, project structure, git history,
// memory recall, and conventions to build enhanced prompts. This is what
// makes VigilAgent's LLM calls produce better output than raw API usage.
package contextbuilder

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/util"
)

// File represents a workspace file with its content.
type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Lines   int    `json:"lines"`
	Language string `json:"language"`
}

// ProjectContext holds all gathered context about the workspace.
type ProjectContext struct {
	Files           []File           `json:"files"`
	OpenTabs        []string         `json:"open_tabs"`        // currently open file paths
	GitBranch       string           `json:"git_branch"`
	GitRecentCommits []string        `json:"git_recent_commits"` // last N commit messages
	ProjectType     string           `json:"project_type"`     // go, node, python, etc.
	Dependencies    []string         `json:"dependencies"`     // from go.mod, package.json, etc.
	Conventions     []Convention     `json:"conventions"`      // detected coding conventions
	MemoryContext   []MemorySnippet  `json:"memory_context"`   // recalled from memory
	CursorPosition  *CursorPosition  `json:"cursor_position,omitempty"`
	TokenBudget     int              `json:"token_budget"`     // max tokens for context
	Timestamp       time.Time        `json:"timestamp"`
}

// Convention represents a detected coding convention.
type Convention struct {
	Category string `json:"category"` // naming, imports, error_handling, etc.
	Pattern  string `json:"pattern"`
	Example  string `json:"example"`
}

// MemorySnippet is a recalled memory item.
type MemorySnippet struct {
	Type    string  `json:"type"`    // working, episodic, semantic
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// CursorPosition represents where the user's cursor is.
type CursorPosition struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Config holds context builder configuration.
type Config struct {
	MaxFiles       int    `json:"max_files"`
	MaxFileLines   int    `json:"max_file_lines"`
	MaxCommits     int    `json:"max_commits"`
	MaxTokenBudget int    `json:"max_token_budget"`
	SystemPrompt   string `json:"system_prompt"`
}

// DefaultConfig returns a production-ready context builder configuration.
func DefaultConfig() *Config {
	return &Config{
		MaxFiles:       20,
		MaxFileLines:   500,
		MaxCommits:     10,
		MaxTokenBudget: 8000,
		SystemPrompt:   defaultSystemPrompt,
	}
}

// Builder assembles workspace context into enhanced prompts.
type Builder struct {
	config *Config
	mu     sync.RWMutex
}

// NewBuilder creates a context builder with the given config.
func NewBuilder(cfg *Config) *Builder {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Builder{config: cfg}
}

// BuildContext creates a ProjectContext from the provided inputs.
func (b *Builder) BuildContext(ctx context.Context, req *BuildRequest) (*ProjectContext, error) {
	pc := &ProjectContext{
		OpenTabs:   req.OpenTabs,
		GitBranch:  req.GitBranch,
		ProjectType: req.ProjectType,
		CursorPosition: req.Cursor,
		TokenBudget: b.config.MaxTokenBudget,
		Timestamp:   time.Now(),
	}

	// Process files (truncate long ones)
	for _, f := range req.Files {
		lines := strings.Split(f.Content, "\n")
		if len(lines) > b.config.MaxFileLines {
			f.Content = strings.Join(lines[:b.config.MaxFileLines], "\n") + "\n// ... truncated"
			f.Lines = b.config.MaxFileLines
		}
		f.Lines = len(strings.Split(f.Content, "\n"))
		pc.Files = append(pc.Files, f)
	}

	// Limit total files
	if len(pc.Files) > b.config.MaxFiles {
		pc.Files = pc.Files[:b.config.MaxFiles]
	}

	// Process commits
	pc.GitRecentCommits = req.RecentCommits
	if len(pc.GitRecentCommits) > b.config.MaxCommits {
		pc.GitRecentCommits = pc.GitRecentCommits[:b.config.MaxCommits]
	}

	// Detect conventions from file contents
	pc.Conventions = b.detectConventions(pc.Files)

	// Store memory context
	pc.MemoryContext = req.MemoryContext

	// Detect dependencies from files
	pc.Dependencies = b.detectDependencies(pc.Files)

	return pc, nil
}

// BuildPrompt creates an enhanced system prompt from the project context.
func (b *Builder) BuildPrompt(pc *ProjectContext, taskDescription string) string {
	var sb strings.Builder

	// System prompt
	sb.WriteString(b.config.SystemPrompt)
	sb.WriteString("\n\n")

	// Project context
	sb.WriteString("## Project Context\n\n")
	if pc.ProjectType != "" {
		sb.WriteString(fmt.Sprintf("- **Language/Framework:** %s\n", pc.ProjectType))
	}
	if pc.GitBranch != "" {
		sb.WriteString(fmt.Sprintf("- **Branch:** %s\n", pc.GitBranch))
	}
	if len(pc.Dependencies) > 0 {
		sb.WriteString(fmt.Sprintf("- **Dependencies:** %s\n", strings.Join(pc.Dependencies, ", ")))
	}

	// Conventions
	if len(pc.Conventions) > 0 {
		sb.WriteString("\n## Detected Conventions\n\n")
		for _, c := range pc.Conventions {
			sb.WriteString(fmt.Sprintf("- **%s:** %s\n", c.Category, c.Pattern))
			if c.Example != "" {
				sb.WriteString(fmt.Sprintf("  Example: `%s`\n", c.Example))
			}
		}
	}

	// Open tabs
	if len(pc.OpenTabs) > 0 {
		sb.WriteString("\n## Currently Open Files\n\n")
		for _, tab := range pc.OpenTabs {
			sb.WriteString(fmt.Sprintf("- `%s`\n", tab))
		}
	}

	// File contents
	if len(pc.Files) > 0 {
		sb.WriteString("\n## Relevant Files\n\n")
		for _, f := range pc.Files {
			sb.WriteString(fmt.Sprintf("### %s\n```%s\n%s\n```\n\n", f.Path, f.Language, f.Content))
		}
	}

	// Git history
	if len(pc.GitRecentCommits) > 0 {
		sb.WriteString("\n## Recent Git History\n\n")
		for _, commit := range pc.GitRecentCommits {
			sb.WriteString(fmt.Sprintf("- %s\n", commit))
		}
	}

	// Memory context
	if len(pc.MemoryContext) > 0 {
		sb.WriteString("\n## Relevant Past Context\n\n")
		for _, m := range pc.MemoryContext {
			sb.WriteString(fmt.Sprintf("- [%s] %s (relevance: %.0f%%)\n", m.Type, util.Truncate(m.Content, 200), m.Score*100))
		}
	}

	// Cursor position
	if pc.CursorPosition != nil {
		sb.WriteString(fmt.Sprintf("\n## Cursor Position\n\nFile: `%s`, Line: %d, Column: %d\n",
			pc.CursorPosition.File, pc.CursorPosition.Line, pc.CursorPosition.Column))
	}

	// Task
	sb.WriteString("\n## Task\n\n")
	sb.WriteString(taskDescription)

	return sb.String()
}

// BuildPromptWithBudget creates a prompt that respects the token budget.
func (b *Builder) BuildPromptWithBudget(pc *ProjectContext, taskDescription string, maxTokens int) string {
	prompt := b.BuildPrompt(pc, taskDescription)

	// Rough token estimation: ~4 chars per token
	estimatedTokens := len(prompt) / 4
	if estimatedTokens <= maxTokens {
		return prompt
	}

	// Truncate to fit budget: remove least important sections
	// Priority: task > cursor > memory > git > files > conventions > open tabs
	var sb strings.Builder
	sb.WriteString(b.config.SystemPrompt)
	sb.WriteString("\n\n")
	sb.WriteString("## Task\n\n")
	sb.WriteString(taskDescription)

	remaining := maxTokens - (sb.Len() / 4)

	// Add files up to budget
	if remaining > 0 && len(pc.Files) > 0 {
		sb.WriteString("\n\n## Relevant Files\n\n")
		for _, f := range pc.Files {
			fileContent := fmt.Sprintf("### %s\n```%s\n%s\n```\n\n", f.Path, f.Language, f.Content)
			fileTokens := len(fileContent) / 4
			if fileTokens < remaining {
				sb.WriteString(fileContent)
				remaining -= fileTokens
			} else {
				// Truncate this file
				lines := strings.Split(f.Content, "\n")
				truncated := ""
				for _, line := range lines {
					test := truncated + line + "\n"
					if len(test)/4 >= remaining {
						break
					}
					truncated = test
				}
				if truncated != "" {
					sb.WriteString(fmt.Sprintf("### %s\n```%s\n%s// ... truncated\n```\n\n", f.Path, f.Language, truncated))
				}
				break
			}
		}
	}

	return sb.String()
}

// detectConventions analyzes files to detect coding conventions.
func (b *Builder) detectConventions(files []File) []Convention {
	var conventions []Convention

	// Analyze naming patterns
	namingStyle := detectNamingStyle(files)
	if namingStyle != "" {
		conventions = append(conventions, Convention{
			Category: "naming",
			Pattern:  namingStyle,
		})
	}

	// Analyze import style
	importStyle := detectImportStyle(files)
	if importStyle != "" {
		conventions = append(conventions, Convention{
			Category: "imports",
			Pattern:  importStyle,
		})
	}

	// Analyze error handling
	errorStyle := detectErrorHandling(files)
	if errorStyle != "" {
		conventions = append(conventions, Convention{
			Category: "error_handling",
			Pattern:  errorStyle,
		})
	}

	return conventions
}

// detectNamingStyle determines the naming convention used.
func detectNamingStyle(files []File) string {
	for _, f := range files {
		if f.Language == "go" {
			return "Go conventions (camelCase for private, PascalCase for exported)"
		}
		if f.Language == "typescript" || f.Language == "javascript" {
			return "JavaScript/TypeScript conventions (camelCase for variables/functions, PascalCase for classes)"
		}
	}
	return ""
}

// detectImportStyle determines how imports are organized.
func detectImportStyle(files []File) string {
	for _, f := range files {
		if f.Language == "go" && strings.Contains(f.Content, "import (") {
			return "Go grouped imports (stdlib, external, internal)"
		}
	}
	return ""
}

// detectErrorHandling determines the error handling pattern.
func detectErrorHandling(files []File) string {
	for _, f := range files {
		if f.Language == "go" && strings.Contains(f.Content, "if err != nil") {
			return "Go early-return error handling"
		}
	}
	return ""
}

// detectDependencies extracts dependencies from project files.
func (b *Builder) detectDependencies(files []File) []string {
	var deps []string
	for _, f := range files {
		if strings.HasSuffix(f.Path, "go.mod") {
			// Extract require lines
			lines := strings.Split(f.Content, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "github.com/") || strings.HasPrefix(line, "golang.org/") {
					parts := strings.Fields(line)
					if len(parts) > 0 {
						deps = append(deps, parts[0])
					}
				}
			}
		}
		if strings.HasSuffix(f.Path, "package.json") {
			// Simple extraction - in production use JSON parser
			if strings.Contains(f.Content, "\"dependencies\"") {
				deps = append(deps, "npm dependencies detected")
			}
		}
	}
	return deps
}



// BuildRequest is the input to BuildContext.
type BuildRequest struct {
	Files          []File           `json:"files"`
	OpenTabs       []string         `json:"open_tabs"`
	GitBranch      string           `json:"git_branch"`
	RecentCommits  []string         `json:"recent_commits"`
	ProjectType    string           `json:"project_type"`
	MemoryContext  []MemorySnippet  `json:"memory_context"`
	Cursor         *CursorPosition  `json:"cursor,omitempty"`
}

const defaultSystemPrompt = `You are VigilAgent, an expert AI coding assistant.

## Core Rules
1. Always read relevant files before making changes
2. Follow existing code conventions and patterns
3. Make minimal, focused changes
4. Include all necessary imports
5. Remove unused code
6. Handle errors explicitly
7. Add tests for new functionality

## Output Format
- Use str_replace for targeted edits
- Use write_file only for new files
- Always explain your changes briefly

## Safety Rules
- Never delete files without explicit permission
- Never run destructive commands
- Always verify assumptions before acting
- Ask for clarification if the task is ambiguous`
