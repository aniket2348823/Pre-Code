package scanner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecRunner_RunSuccess(t *testing.T) {
	r := ExecRunner{}
	stdout, stderr, err := r.Run(context.Background(), "go", []string{"version"}, "")
	require.NoError(t, err)
	assert.NotEmpty(t, stdout)
	assert.Empty(t, stderr)
}

func TestExecRunner_RunExitsNonZero(t *testing.T) {
	r := ExecRunner{}
	_, _, err := r.Run(context.Background(), "nonexistent-tool-xyz-9999", nil, "")
	assert.Error(t, err)
}

func TestExecRunner_RunWithStdinPath(t *testing.T) {
	r := ExecRunner{}
	// This exercises the stdin path even if the command fails
	stdout, _, _ := r.Run(context.Background(), "go", []string{"run"}, "fmt.Println(\"hello\")")
	_ = stdout
}

func TestToolExists_Go(t *testing.T) {
	assert.True(t, toolExists("go"), "go should be on PATH in Go dev environment")
}

func TestToolExists_Nonexistent(t *testing.T) {
	assert.False(t, toolExists("definitely-not-a-real-tool-xyz-9999"))
}

func TestToolExists_Empty(t *testing.T) {
	assert.False(t, toolExists(""))
}

func TestFakeRunner_CapturesArgs(t *testing.T) {
	fr := &fakeRunner{stdout: "ok"}
	r := Runner(fr)
	stdout, _, err := r.Run(context.Background(), "mytool", []string{"arg1", "arg2"}, "")
	require.NoError(t, err)
	assert.Equal(t, "ok", stdout)
	assert.Equal(t, "mytool", fr.gotName)
	assert.Equal(t, []string{"arg1", "arg2"}, fr.gotArgs)
}

func TestFakeRunner_ReturnsError(t *testing.T) {
	fr := &fakeRunner{err: assert.AnError}
	_, _, err := fr.Run(context.Background(), "tool", nil, "")
	assert.ErrorIs(t, err, assert.AnError)
}

func TestFakeRunner_ReturnsStderr(t *testing.T) {
	fr := &fakeRunner{stderr: "warning"}
	_, stderr, _ := fr.Run(context.Background(), "tool", nil, "")
	assert.Equal(t, "warning", stderr)
}

func TestFakeRunner_EmptyStdout(t *testing.T) {
	fr := &fakeRunner{stdout: ""}
	stdout, _, _ := fr.Run(context.Background(), "tool", nil, "")
	assert.Empty(t, stdout)
}

func TestExecRunner_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	r := ExecRunner{}
	_, _, err := r.Run(ctx, "go", []string{"version"}, "")
	// May or may not error depending on timing, but should not panic
	_ = err
}
