// Package util provides shared helper functions used across the
// deterministic engine packages.
package util

import (
	"strings"
)

// Itoa converts an integer to its string representation without
// allocating from the fmt pool. Safe for non-negative values.
func Itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	// Reverse
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

// Join joins a slice of strings with a separator.
func Join(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(parts[0])
	for _, p := range parts[1:] {
		b.WriteString(sep)
		b.WriteString(p)
	}
	return b.String()
}

// ContainsWord checks if text contains keyword. For keywords ≤3 chars,
// it requires word boundaries to prevent false positives from substring
// matches (e.g., "api" should not match inside "captcha").
// For longer keywords, substring match is sufficient.
func ContainsWord(text, keyword string) bool {
	if !strings.Contains(text, keyword) {
		return false
	}
	if len(keyword) <= 3 {
		return HasWordBoundary(text, keyword)
	}
	return true
}

// HasWordBoundary checks if keyword appears at least once in text starting
// at a word boundary (left boundary). This means the character before the
// keyword (if present) is not alphanumeric or underscore. This is the correct
// semantic for security keyword matching — we want keywords that START words
// (e.g., "auth" matches "authentication" but not "captcha").
func HasWordBoundary(text, keyword string) bool {
	start := 0
	for {
		idx := strings.Index(text[start:], keyword)
		if idx == -1 {
			return false
		}
		absIdx := start + idx

		// Check character before keyword — must be non-word or start of string
		if absIdx > 0 && IsWordChar(text[absIdx-1]) {
			start = absIdx + 1
			continue
		}

		return true
	}
}

// IsWordChar returns true if c is an alphanumeric or underscore character.
func IsWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// Truncate shortens a string to maxLen characters, appending "..." if truncated.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ComputeMatchScore returns a score 0.0–1.0 based on how many keywords match,
// zeroed out if any exclusion keyword matches. Uses word-boundary detection
// for short keywords.
func ComputeMatchScore(text string, keywords, excludes []string) float64 {
	// Check exclusions first — any match kills the rule
	for _, ex := range excludes {
		if ContainsWord(text, ex) {
			return 0.0
		}
	}

	// Count positive keyword matches
	matched := 0
	for _, kw := range keywords {
		if ContainsWord(text, kw) {
			matched++
		}
	}

	if len(keywords) == 0 {
		return 0.0
	}

	return float64(matched) / float64(len(keywords))
}
