package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SearchCodeTool searches code using ripgrep.
type SearchCodeTool struct{}

func (t *SearchCodeTool) Name() string        { return "search_code" }
func (t *SearchCodeTool) Description() string  { return "Search for patterns in the codebase" }
func (t *SearchCodeTool) RequiresHITL(params map[string]interface{}) bool { return false }

func (t *SearchCodeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "The search pattern (regex)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory to search in (default: current directory)",
			},
			"glob": map[string]interface{}{
				"type":        "string",
				"description": "File glob pattern to filter (e.g., *.go)",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *SearchCodeTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	start := time.Now()
	pattern, _ := params["pattern"].(string)
	if pattern == "" {
		return &ToolResult{Success: false, Error: "pattern is required"}, nil
	}

	searchPath, _ := params["path"].(string)
	if searchPath == "" {
		searchPath = "."
	}

	glob, _ := params["glob"].(string)

	args := []string{"-n", "--color=never", pattern, searchPath}
	if glob != "" {
		args = []string{"-n", "--color=never", "-g", glob, pattern, searchPath}
	}

	cmd := exec.CommandContext(ctx, "rg", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// rg returns exit code 1 when no matches found
		if strings.Contains(string(output), "no matches") || strings.Contains(string(output), "0 matches") {
			return &ToolResult{
				Output:   "No matches found",
				Success:  true,
				Duration: time.Since(start),
			}, nil
		}
		return &ToolResult{
			Success:  false,
			Error:    fmt.Sprintf("search failed: %v", err),
			Duration: time.Since(start),
		}, nil
	}

	return &ToolResult{
		Output:   string(output),
		Success:  true,
		Duration: time.Since(start),
		Metadata: map[string]interface{}{"pattern": pattern, "path": searchPath, "glob": glob},
	}, nil
}
