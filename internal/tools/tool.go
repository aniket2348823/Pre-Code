package tools

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Tool defines the interface for agent tools.
type Tool interface {
	// Name returns the tool's unique identifier.
	Name() string
	// Description returns a human-readable description.
	Description() string
	// Parameters returns the JSON Schema for parameters.
	Parameters() map[string]interface{}
	// Execute runs the tool with given parameters.
	Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error)
	// RequiresHITL returns whether this action needs human approval.
	RequiresHITL(params map[string]interface{}) bool
}

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	Output   string                 `json:"output"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Cost     float64               `json:"cost"`
	Duration time.Duration         `json:"duration"`
	Success  bool                  `json:"success"`
	Error    string                `json:"error,omitempty"`
}

// ToolRegistry manages available tools.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

// Get retrieves a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

// List returns all registered tools.
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

// ListDefs returns tool definitions for LLM function calling.
func (r *ToolRegistry) ListDefs() []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDef, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, ToolDef{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		})
	}
	return defs
}

// ToolDef represents a tool definition for LLM function calling.
type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// FileChange represents a file modification.
type FileChange struct {
	Path   string `json:"path"`
	Action string `json:"action"` // "created", "modified", "deleted"
	Diff   string `json:"diff,omitempty"`
}

// SkillMetrics contains execution metrics.
type SkillMetrics struct {
	Duration     time.Duration `json:"duration"`
	FilesScanned int           `json:"files_scanned"`
	IssuesFound  int           `json:"issues_found"`
	IssuesFixed  int           `json:"issues_fixed"`
}

func init() {
	// Ensure fmt is used
	_ = fmt.Sprintf
}
