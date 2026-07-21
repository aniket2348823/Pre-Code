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

// Sandbox wraps command execution with safety constraints.
type Sandbox struct {
	config *SandboxConfig
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

	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("command timed out after %s", timeout)
	}

	if err != nil {
		return string(output), fmt.Errorf("command failed: %w (output: %s)", err, string(output))
	}

	return string(output), nil
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
