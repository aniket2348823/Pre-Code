package llm

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeReadBody_Normal(t *testing.T) {
	body, err := safeReadBody(bytes.NewReader([]byte("hello world")))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(body))
}

func TestSafeReadBody_EmptyReader(t *testing.T) {
	body, err := safeReadBody(bytes.NewReader(nil))
	require.NoError(t, err)
	assert.Empty(t, body)
}

func TestSafeReadBody_LargeBody(t *testing.T) {
	data := strings.Repeat("x", 1024*100) // 100KB
	body, err := safeReadBody(bytes.NewReader([]byte(data)))
	require.NoError(t, err)
	assert.Equal(t, data, string(body))
}

func TestSafeReadBody_ExactLimit(t *testing.T) {
	data := strings.Repeat("a", maxResponseBodySize)
	body, err := safeReadBody(bytes.NewReader([]byte(data)))
	require.NoError(t, err)
	assert.Equal(t, data, string(body))
}

func TestSafeReadBody_OverLimit(t *testing.T) {
	data := strings.Repeat("b", maxResponseBodySize+1)
	body, err := safeReadBody(bytes.NewReader([]byte(data)))
	require.NoError(t, err)
	// LimitReader truncates at maxResponseBodySize
	assert.Equal(t, maxResponseBodySize, len(body))
}

func TestSafeReadBody_JSON(t *testing.T) {
	json := `{"key": "value", "number": 42}`
	body, err := safeReadBody(bytes.NewReader([]byte(json)))
	require.NoError(t, err)
	assert.Equal(t, json, string(body))
}

func TestSafeReadBody_BinaryData(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
	body, err := safeReadBody(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, data, body)
}

func TestMaxResponseBodySize(t *testing.T) {
	assert.Equal(t, 10*1024*1024, maxResponseBodySize)
}

func TestSafeReadBody_ReadCloser(t *testing.T) {
	data := []byte("test data")
	r := io.NopCloser(bytes.NewReader(data))
	body, err := safeReadBody(r)
	require.NoError(t, err)
	assert.Equal(t, "test data", string(body))
}

func TestSafeReadBody_Unicode(t *testing.T) {
	data := "Hello 世界 🌍 مرحبا"
	body, err := safeReadBody(bytes.NewReader([]byte(data)))
	require.NoError(t, err)
	assert.Equal(t, data, string(body))
}

func TestSafeReadBody_Newlines(t *testing.T) {
	data := "line1\nline2\nline3\n"
	body, err := safeReadBody(bytes.NewReader([]byte(data)))
	require.NoError(t, err)
	assert.Equal(t, data, string(body))
}
