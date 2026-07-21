package llm

import (
	"io"
)

// maxResponseBodySize is the maximum size (in bytes) we'll read from an LLM
// provider response body. This prevents OOM from a misbehaving or malicious
// provider returning an enormous payload.
const maxResponseBodySize = 10 * 1024 * 1024 // 10 MB

// safeReadBody reads from r up to maxResponseBodySize bytes.
// If the body exceeds the limit, it returns an error rather than consuming
// unbounded memory.
func safeReadBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxResponseBodySize))
}
