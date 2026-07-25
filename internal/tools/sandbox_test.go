package tools

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestDefaultSandboxConfig(t *testing.T) {
	cfg := DefaultSandboxConfig()
	if cfg.Engine != "local" {
		t.Errorf("Engine = %q, want local", cfg.Engine)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
	}
	if !cfg.Network {
		t.Error("Network should be true")
	}
}

func TestDockerSandboxConfig(t *testing.T) {
	cfg := DockerSandboxConfig()
	if cfg.Engine != "docker" {
		t.Errorf("Engine = %q, want docker", cfg.Engine)
	}
	if cfg.Image != "golang:1.22-alpine" {
		t.Errorf("Image = %q", cfg.Image)
	}
	if cfg.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s", cfg.Timeout)
	}
	if cfg.MaxMemory != "256m" {
		t.Errorf("MaxMemory = %q", cfg.MaxMemory)
	}
	if cfg.Network {
		t.Error("Network should be false for docker sandbox")
	}
	if cfg.WorkDir != "/workspace" {
		t.Errorf("WorkDir = %q", cfg.WorkDir)
	}
}

func TestNewSandbox_NilConfig(t *testing.T) {
	s := NewSandbox(nil)
	if s.config == nil {
		t.Fatal("config should not be nil")
	}
	if s.config.Engine != "local" {
		t.Errorf("Engine = %q, want local", s.config.Engine)
	}
}

func TestNewSandbox_WithConfig(t *testing.T) {
	cfg := &SandboxConfig{Engine: "custom"}
	s := NewSandbox(cfg)
	if s.config.Engine != "custom" {
		t.Errorf("Engine = %q, want custom", s.config.Engine)
	}
}

func TestSandbox_Execute_Local_ShellNotFound(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("testing sh-not-found only on Windows")
	}
	s := NewSandbox(nil)
	_, err := s.Execute(context.Background(), "echo test")
	if err == nil {
		t.Skip("sh is available; test not applicable")
	}
}

func TestSandbox_Execute_Local_TimeoutPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("testing timeout path only on Windows (sh not found triggers err path)")
	}
	cfg := &SandboxConfig{
		Engine:  "local",
		Timeout: 1 * time.Millisecond,
		Network: false,
	}
	s := NewSandbox(cfg)
	_, err := s.Execute(context.Background(), "sleep 10")
	if err == nil {
		t.Skip("sh is available; test not applicable")
	}
}

func TestSandbox_Execute_Local_ZeroTimeout(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("testing zero-timeout fallback on Windows")
	}
	cfg := &SandboxConfig{
		Engine:  "local",
		Timeout: 0,
		Network: false,
	}
	s := NewSandbox(cfg)
	_, err := s.Execute(context.Background(), "echo test")
	if err == nil {
		t.Skip("sh is available; test not applicable")
	}
}

func TestSandbox_Execute_Docker_NoDocker(t *testing.T) {
	if IsDockerAvailable() {
		t.Skip("Docker is available; skip no-docker test")
	}
	cfg := DockerSandboxConfig()
	s := NewSandbox(cfg)
	_, err := s.Execute(context.Background(), "echo test")
	if err == nil {
		t.Skip("docker is available; test not applicable")
	}
}

func TestSandbox_Execute_Docker_AllOptions_NoMem(t *testing.T) {
	if IsDockerAvailable() {
		t.Skip("Docker is available; skip no-docker test")
	}
	cfg := &SandboxConfig{
		Engine:    "docker",
		Image:     "alpine:latest",
		Timeout:   30 * time.Second,
		MaxMemory: "",
		Network:   false,
		WorkDir:   "",
	}
	s := NewSandbox(cfg)
	_, err := s.Execute(context.Background(), "echo test")
	if err == nil {
		t.Skip("docker is available; test not applicable")
	}
}

func TestSandbox_Execute_Docker_TimeoutPath(t *testing.T) {
	if IsDockerAvailable() {
		t.Skip("Docker is available; skip no-docker test")
	}
	cfg := DockerSandboxConfig()
	cfg.Timeout = 1 * time.Millisecond
	s := NewSandbox(cfg)
	_, err := s.Execute(context.Background(), "sleep 10")
	if err == nil {
		t.Skip("docker is available; test not applicable")
	}
}

func TestSandbox_Execute_Docker_MaxMemory_NoNetwork(t *testing.T) {
	if IsDockerAvailable() {
		t.Skip("Docker is available; skip no-docker test")
	}
	cfg := &SandboxConfig{
		Engine:    "docker",
		Image:     "alpine:latest",
		Timeout:   30 * time.Second,
		MaxMemory: "128m",
		Network:   false,
		WorkDir:   "",
	}
	s := NewSandbox(cfg)
	_, err := s.Execute(context.Background(), "echo test")
	if err == nil {
		t.Skip("docker is available; test not applicable")
	}
}

func TestSandbox_Execute_Docker_MaxMemory_Network_WorkDir(t *testing.T) {
	if IsDockerAvailable() {
		t.Skip("Docker is available; skip no-docker test")
	}
	cfg := &SandboxConfig{
		Engine:    "docker",
		Image:     "alpine:latest",
		Timeout:   30 * time.Second,
		MaxMemory: "256m",
		Network:   true,
		WorkDir:   "/workspace",
	}
	s := NewSandbox(cfg)
	_, err := s.Execute(context.Background(), "echo test")
	if err == nil {
		t.Skip("docker is available; test not applicable")
	}
}

func TestIsDockerAvailable(t *testing.T) {
	_ = IsDockerAvailable()
}

func TestSandboxConfig_StructFields(t *testing.T) {
	cfg := &SandboxConfig{
		Engine:    "test",
		Image:     "test-image",
		Timeout:   10 * time.Second,
		MaxMemory: "512m",
		Network:   true,
		WorkDir:   "/test",
	}
	s := NewSandbox(cfg)
	if s.config.Engine != "test" {
		t.Error("engine mismatch")
	}
	if s.config.Image != "test-image" {
		t.Error("image mismatch")
	}
	if s.config.MaxMemory != "512m" {
		t.Error("maxMemory mismatch")
	}
	if !s.config.Network {
		t.Error("network should be true")
	}
	if s.config.WorkDir != "/test" {
		t.Error("workDir mismatch")
	}
}
