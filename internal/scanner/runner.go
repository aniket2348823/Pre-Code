package scanner

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// Runner executes an external command. Injectable so adapters test without the
// real tool installed.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin string) (stdout, stderr string, err error)
}

// ExecRunner runs commands for real via os/exec.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string, stdin string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.String(), errBuf.String(), err
}

// toolExists reports whether a binary is resolvable on PATH.
func toolExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
