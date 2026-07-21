package scanner

import (
	"context"
	"testing"
)

// fakeRunner is a shared test double for shell-out adapters. Later tasks reuse it.
type fakeRunner struct {
	stdout, stderr string
	err            error
	gotName        string
	gotArgs        []string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, stdin string) (string, string, error) {
	f.gotName = name
	f.gotArgs = args
	return f.stdout, f.stderr, f.err
}

func TestToolExists(t *testing.T) {
	if !toolExists("go") {
		t.Fatal("expected 'go' to be on PATH in a Go dev environment")
	}
	if toolExists("definitely-not-a-real-tool-xyz-9999") {
		t.Fatal("expected a nonsense binary to be absent")
	}
}
