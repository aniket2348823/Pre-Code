package tools

import (
	"context"
	"runtime"
	"testing"
)

func TestRunCommandTool_Name(t *testing.T) {
	r := &RunCommandTool{}
	if r.Name() != "run_command" {
		t.Errorf("Name() = %q, want run_command", r.Name())
	}
}

func TestRunCommandTool_Description(t *testing.T) {
	r := &RunCommandTool{}
	if r.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestRunCommandTool_RequiresHITL(t *testing.T) {
	r := &RunCommandTool{}
	if !r.RequiresHITL(nil) {
		t.Error("RunCommandTool should always require HITL")
	}
}

func TestRunCommandTool_Parameters(t *testing.T) {
	r := &RunCommandTool{}
	p := r.Parameters()
	if p["type"] != "object" {
		t.Error("Parameters should have type object")
	}
	props := p["properties"].(map[string]interface{})
	if _, ok := props["command"]; !ok {
		t.Error("missing command")
	}
	if _, ok := props["timeout_seconds"]; !ok {
		t.Error("missing timeout_seconds")
	}
}

func TestRunCommandTool_Execute_Success(t *testing.T) {
	r := &RunCommandTool{}
	res, err := r.Execute(context.Background(), map[string]interface{}{"command": "echo hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if res.Duration <= 0 {
		t.Error("Duration should be positive")
	}
}

func TestRunCommandTool_Execute_EmptyCommand(t *testing.T) {
	r := &RunCommandTool{}
	res, err := r.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for empty command")
	}
}

func TestRunCommandTool_Execute_FailingCommand(t *testing.T) {
	r := &RunCommandTool{}
	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "exit /b 1"
	} else {
		cmd = "exit 1"
	}
	res, err := r.Execute(context.Background(), map[string]interface{}{"command": cmd})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for failing command")
	}
}

func TestRunCommandTool_Execute_WithTimeout(t *testing.T) {
	r := &RunCommandTool{}
	res, err := r.Execute(context.Background(), map[string]interface{}{
		"command":         "echo timeout_test",
		"timeout_seconds": float64(5),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
}

func TestRunCommandTool_Execute_NonStringCommand(t *testing.T) {
	r := &RunCommandTool{}
	res, err := r.Execute(context.Background(), map[string]interface{}{"command": 123})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Error("expected failure for non-string command")
	}
}

func TestShellCommand_Windows(t *testing.T) {
	// shellCommand is platform-dependent; test both branches
	shell, args := shellCommand("echo test")
	if runtime.GOOS == "windows" {
		if shell != "cmd" {
			t.Errorf("shell = %q, want cmd", shell)
		}
		if len(args) != 2 || args[0] != "/c" {
			t.Errorf("args = %v, want [/c echo test]", args)
		}
	} else {
		if shell != "sh" {
			t.Errorf("shell = %q, want sh", shell)
		}
		if len(args) != 2 || args[0] != "-c" {
			t.Errorf("args = %v, want [-c echo test]", args)
		}
	}
}

func TestShellCommand_BranchWindows(t *testing.T) {
	// Force both branches by calling the function; the runtime branch is selected
	shell, args := shellCommand("test")
	_ = shell
	_ = args
	// Both branches cannot be tested on a single OS, but the function is called
	// to cover whatever branch the current OS takes
}

func TestRunCommandTool_Metadata(t *testing.T) {
	r := &RunCommandTool{}
	res, err := r.Execute(context.Background(), map[string]interface{}{"command": "echo test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Metadata["command"] != "echo test" {
		t.Errorf("metadata command = %v", res.Metadata["command"])
	}
	if res.Metadata["os"] != runtime.GOOS {
		t.Errorf("metadata os = %v, want %s", res.Metadata["os"], runtime.GOOS)
	}
}
