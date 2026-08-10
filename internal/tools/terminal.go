package tools

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// validateCommand checks a command against security policy.
func validateCommand(command string, security *CommandSecurityConfig) error {
	if security == nil {
		return fmt.Errorf("command validation: no security config configured")
	}

	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("command is required")
	}

	// Strip leading sudo and env var assignments
	for {
		if strings.HasPrefix(command, "sudo ") {
			command = strings.TrimPrefix(command, "sudo ")
			continue
		}
		if eqIdx := strings.Index(command, "="); eqIdx >= 0 && strings.IndexByte(command[:eqIdx], ' ') < 0 {
			parts := strings.SplitN(command, " ", 2)
			if len(parts) > 1 {
				command = parts[1]
				continue
			}
		}
		break
	}

	fields := strings.Fields(command)
	if len(fields) == 0 {
		return fmt.Errorf("empty command after parsing")
	}

	cmd := filepath.Base(fields[0])

	if len(security.Blocklist) > 0 {
		for _, blocked := range security.Blocklist {
			if cmd == blocked {
				return fmt.Errorf("command %q is blocked by security policy", cmd)
			}
		}
	}

	if len(security.Allowlist) > 0 {
		for _, allowed := range security.Allowlist {
			if cmd == allowed {
				// Strict mode: the allowlist only gates the first token, but the
				// full string executes in a shell. Reject shell metacharacters so an
				// allowlisted binary cannot be chained into arbitrary commands
				// (e.g. "git; rm -rf /").
				if err := rejectShellInjection(command); err != nil {
					return err
				}
				return nil
			}
		}
		return fmt.Errorf("command %q is not in the allowlist", cmd)
	}

	return nil
}

// rejectShellInjection rejects commands that use shell metacharacters to chain
// or substitute arbitrary commands.
func rejectShellInjection(command string) error {
	for _, tok := range []string{";", "&&", "||", "|", ">", "<", "`", "$(", "${"} {
		if strings.Contains(command, tok) {
			return fmt.Errorf("command contains shell metacharacter %q which is not allowed in strict (allowlist) mode", tok)
		}
	}
	return nil
}

// RunCommandTool executes terminal commands.
type RunCommandTool struct {
	Security *CommandSecurityConfig
}

func (t *RunCommandTool) Name() string                                    { return "run_command" }
func (t *RunCommandTool) Description() string                             { return "Execute a terminal command and return output" }
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

	if err := validateCommand(command, t.Security); err != nil {
		return &ToolResult{Success: false, Error: err.Error(), Duration: time.Since(start)}, nil
	}

	timeout := 30
	if ts, ok := params["timeout_seconds"].(float64); ok && ts > 0 {
		timeout = int(ts)
	}
	// Hard cap so a runaway command cannot consume resources indefinitely.
	if timeout > 300 {
		timeout = 300
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
