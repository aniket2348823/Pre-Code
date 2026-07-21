package tools

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// RunCommandTool executes terminal commands.
type RunCommandTool struct{}

func (t *RunCommandTool) Name() string        { return "run_command" }
func (t *RunCommandTool) Description() string  { return "Execute a terminal command and return output" }
func (t *RunCommandTool) RequiresHITL(params map[string]interface{}) bool { return true }

func (t *RunCommandTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"timeout_seconds": map[string]interface{}{
				"type":        "number",
				"description": "Maximum execution time in seconds (default: 30)",
			},
		},
		"required": []string{"command"},
	}
}

// shellCommand returns the appropriate shell binary and arguments for the current OS.
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", command}
	}
	return "sh", []string{"-c", command}
}

func (t *RunCommandTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	start := time.Now()
	command, _ := params["command"].(string)
	if command == "" {
		return &ToolResult{Success: false, Error: "command is required"}, nil
	}

	timeout := 30
	if ts, ok := params["timeout_seconds"].(float64); ok && ts > 0 {
		timeout = int(ts)
	}

	cancelCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	shell, args := shellCommand(command)
	cmd := exec.CommandContext(cancelCtx, shell, args...)
	output, err := cmd.CombinedOutput()

	result := &ToolResult{
		Output:   string(output),
		Duration: time.Since(start),
		Metadata: map[string]interface{}{"command": command, "timeout": timeout, "os": runtime.GOOS},
	}

	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("command failed: %v", err)
	} else {
		result.Success = true
	}

	return result, nil
}
