// Package cachekeys provides cache key generation utilities for HTTP responses.
package cachekeys

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Generate creates a deterministic cache key from method, path, and headers.
func Generate(method, path string, headers map[string]string) string {
	var sb strings.Builder
	sb.WriteString(strings.ToUpper(method))
	sb.WriteString("|")
	sb.WriteString(path)

	if len(headers) > 0 {
		keys := make([]string, 0, len(headers))
		for k := range headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString("|")
			sb.WriteString(strings.ToLower(k))
			sb.WriteString("=")
			sb.WriteString(headers[k])
		}
	}

	h := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(h[:])[:16]
}

// GenerateWithBody creates a cache key that includes a body hash.
func GenerateWithBody(method, path, body string, headers map[string]string) string {
	bodyHash := sha256.Sum256([]byte(body))
	key := Generate(method, path, headers)
	return fmt.Sprintf("%s:%s", key, hex.EncodeToString(bodyHash[:])[:8])
}
