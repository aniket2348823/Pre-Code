package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReadFileTool reads file contents.
type ReadFileTool struct{}

func (t *ReadFileTool) Name() string        { return "read_file" }
func (t *ReadFileTool) Description() string  { return "Read the contents of a file" }
func (t *ReadFileTool) RequiresHITL(params map[string]interface{}) bool { return false }

func (t *ReadFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to read",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	start := time.Now()
	path, _ := params["path"].(string)
	if path == "" {
		return &ToolResult{Success: false, Error: "path is required"}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("failed to read file: %v", err), Duration: time.Since(start)}, nil
	}

	return &ToolResult{
		Output:   string(data),
		Success:  true,
		Duration: time.Since(start),
		Metadata: map[string]interface{}{"path": path, "size": len(data)},
	}, nil
}

// WriteFileTool writes content to a file.
type WriteFileTool struct{}

func (t *WriteFileTool) Name() string        { return "write_file" }
func (t *WriteFileTool) Description() string  { return "Write content to a file (creates or overwrites)" }
func (t *WriteFileTool) RequiresHITL(params map[string]interface{}) bool {
	// Requires HITL if file already exists
	path, _ := params["path"].(string)
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			return true // File exists, needs approval to overwrite
		}
	}
	return false
}

func (t *WriteFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to write",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content to write to the file",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	start := time.Now()
	path, _ := params["path"].(string)
	content, _ := params["content"].(string)

	if path == "" {
		return &ToolResult{Success: false, Error: "path is required"}, nil
	}

	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("failed to create directory: %v", err)}, nil
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("failed to write file: %v", err)}, nil
	}

	return &ToolResult{
		Output:   fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path),
		Success:  true,
		Duration: time.Since(start),
		Metadata: map[string]interface{}{"path": path, "bytes_written": len(content)},
	}, nil
}

// EditFileTool performs targeted string replacement in a file.
type EditFileTool struct{}

func (t *EditFileTool) Name() string        { return "edit_file" }
func (t *EditFileTool) Description() string  { return "Edit a file by replacing a specific string with new content" }
func (t *EditFileTool) RequiresHITL(params map[string]interface{}) bool { return true }

func (t *EditFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to edit",
			},
			"old_string": map[string]interface{}{
				"type":        "string",
				"description": "The exact string to find and replace",
			},
			"new_string": map[string]interface{}{
				"type":        "string",
				"description": "The replacement string",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

func (t *EditFileTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	start := time.Now()
	path, _ := params["path"].(string)
	oldStr, _ := params["old_string"].(string)
	newStr, _ := params["new_string"].(string)

	if path == "" || oldStr == "" {
		return &ToolResult{Success: false, Error: "path and old_string are required"}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("failed to read file: %v", err)}, nil
	}

	content := string(data)
	if !strings.Contains(content, oldStr) {
		return &ToolResult{Success: false, Error: "old_string not found in file"}, nil
	}

	newContent := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("failed to write file: %v", err)}, nil
	}

	return &ToolResult{
		Output:   fmt.Sprintf("Successfully edited %s", path),
		Success:  true,
		Duration: time.Since(start),
		Metadata: map[string]interface{}{"path": path, "old_len": len(oldStr), "new_len": len(newStr)},
	}, nil
}

// ListDirectoryTool lists directory contents.
type ListDirectoryTool struct{}

func (t *ListDirectoryTool) Name() string        { return "list_directory" }
func (t *ListDirectoryTool) Description() string  { return "List files and directories in a path" }
func (t *ListDirectoryTool) RequiresHITL(params map[string]interface{}) bool { return false }

func (t *ListDirectoryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory path to list",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ListDirectoryTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	start := time.Now()
	path, _ := params["path"].(string)
	if path == "" {
		path = "."
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("failed to list directory: %v", err)}, nil
	}

	var files, dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name()+"/")
		} else {
			files = append(files, entry.Name())
		}
	}

	output := fmt.Sprintf("Directories: %s\nFiles: %s",
		strings.Join(dirs, ", "),
		strings.Join(files, ", "))

	return &ToolResult{
		Output:   output,
		Success:  true,
		Duration: time.Since(start),
		Metadata: map[string]interface{}{"path": path, "dirs": len(dirs), "files": len(files)},
	}, nil
}
