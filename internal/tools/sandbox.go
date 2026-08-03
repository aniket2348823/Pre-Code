package tools

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// SandboxConfig configures the Docker sandbox for command execution.
type SandboxConfig struct {
	Engine    string        `json:"engine"`              // "docker", "local"
	Image     string        `json:"image,omitempty"`      // Docker image
	Timeout   time.Duration `json:"timeout"`              // Max execution time
	MaxMemory string        `json:"max_memory,omitempty"` // e.g. "256m"
	Network   bool          `json:"network"`              // Enable network access
	WorkDir   string        `json:"work_dir,omitempty"`   // Working directory inside container
}

// DefaultSandboxConfig returns sensible defaults for local execution.
func DefaultSandboxConfig() *SandboxConfig {
	return &SandboxConfig{
		Engine:  "local",
		Timeout: 30 * time.Second,
		Network: true,
	}
}

// DockerSandboxConfig returns defaults for Docker-based sandbox.
func DockerSandboxConfig() *SandboxConfig {
	return &SandboxConfig{
		Engine:    "docker",
		Image:     "golang:1.22-alpine",
		Timeout:   60 * time.Second,
		MaxMemory: "256m",
		Network:   false,
		WorkDir:   "/workspace",
	}
}

// WARNING: When using Engine="local", the Sandbox provides NO isolation.
// Commands execute directly on the host system with full access to the
// filesystem, network, and all system resources. This is NOT a security
// boundary. Never use the "local" engine with untrusted input or in
// production environments. Use Engine="docker" for actual sandboxing.
type Sandbox struct {
	config *SandboxConfig
}

// LocalWarning returns a warning about the lack of isolation in local mode.
// Returns empty string for non-local engines.
func (s *Sandbox) LocalWarning() string {
	if s.config.Engine == "local" {
		return "SECURITY WARNING: Local sandbox provides NO isolation. " +
			"Commands execute with full host access. " +
			"Do not use with untrusted input."
	}
	return ""
}

// NewSandbox creates a new sandbox with the given configuration.
func NewSandbox(cfg *SandboxConfig) *Sandbox {
	if cfg == nil {
		cfg = DefaultSandboxConfig()
	}
	return &Sandbox{config: cfg}
}

// Execute runs a command within the sandbox constraints.
func (s *Sandbox) Execute(ctx context.Context, command string) (string, error) {
	if s.config.Engine == "docker" {
		return s.executeDocker(ctx, command)
	}
	return s.executeLocal(ctx, command)
}

func (s *Sandbox) executeLocal(ctx context.Context, command string) (string, error) {
	timeout := s.config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()

	warning := s.LocalWarning()
	result := string(output)
	if warning != "" {
		result = warning + "\n" + result
	}

	if ctx.Err() == context.DeadlineExceeded {
		return result, fmt.Errorf("command timed out after %s", timeout)
	}

	if err != nil {
		return result, fmt.Errorf("command failed: %w (output: %s)", err, result)
	}

	return result, nil
}

func (s *Sandbox) executeDocker(ctx context.Context, command string) (string, error) {
	timeout := s.config.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"run", "--rm"}

	if s.config.MaxMemory != "" {
		args = append(args, "--memory", s.config.MaxMemory)
	}
	if !s.config.Network {
		args = append(args, "--network", "none")
	}
	if s.config.WorkDir != "" {
		args = append(args, "-w", s.config.WorkDir)
	}

	args = append(args, s.config.Image, "sh", "-c", command)

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("docker command timed out after %s", timeout)
	}

	if err != nil {
		return string(output), fmt.Errorf("docker command failed: %w", err)
	}

	return string(output), nil
}

// IsDockerAvailable checks if Docker is installed and accessible.
func IsDockerAvailable() bool {
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	return cmd.Run() == nil
}
