// Package signing provides request signing and verification for API security.
package signing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Signer creates HMAC signatures for HTTP requests.
type Signer struct {
	secret   []byte
	clockSkew time.Duration
}

// NewSigner creates a new request signer with the given secret.
func NewSigner(secret string) *Signer {
	return &Signer{
		secret:    []byte(secret),
		clockSkew: 5 * time.Minute,
	}
}

// SignRequest adds an HMAC signature header to an HTTP request.
func (s *Signer) SignRequest(r *http.Request, body []byte) error {
	timestamp := time.Now().Unix()
	r.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))

	canonical := buildCanonical(r.Method, r.URL.Path, r.URL.RawQuery, timestamp, r.Header, body)
	sig := HMACSign(s.secret, canonical)
	r.Header.Set("X-Signature", sig)
	return nil
}

// VerifyRequest checks the HMAC signature on an HTTP request.
func (s *Signer) VerifyRequest(r *http.Request, body []byte) error {
	sig := r.Header.Get("X-Signature")
	if sig == "" {
		return fmt.Errorf("missing X-Signature header")
	}

	tsStr := r.Header.Get("X-Timestamp")
	if tsStr == "" {
		return fmt.Errorf("missing X-Timestamp header")
	}

	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid X-Timestamp: %w", err)
	}

	// Check clock skew
	now := time.Now().Unix()
	if now-ts > int64(s.clockSkew.Seconds()) || ts-now > int64(s.clockSkew.Seconds()) {
		return fmt.Errorf("request timestamp outside allowed window")
	}

	canonical := buildCanonical(r.Method, r.URL.Path, r.URL.RawQuery, ts, r.Header, body)
	expected := HMACSign(s.secret, canonical)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}

// HMACSign computes an HMAC-SHA256 signature.
func HMACSign(secret []byte, data string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// buildCanonical constructs a canonical string for signing.
// The body parameter is hashed into the signature to prevent body tampering.
func buildCanonical(method, path, query string, timestamp int64, headers http.Header, body []byte) string {
	var sb strings.Builder
	sb.WriteString(strings.ToUpper(method))
	sb.WriteString("\n")
	sb.WriteString(path)
	sb.WriteString("\n")
	sb.WriteString(query)
	sb.WriteString("\n")
	sb.WriteString(strconv.FormatInt(timestamp, 10))
	sb.WriteString("\n")

	// Include body hash in canonical form (truncated for safety)
	if len(body) > 0 {
		bodyHash := sha256.Sum256(body)
		sb.WriteString(hex.EncodeToString(bodyHash[:]))
	} else {
		sb.WriteString("empty")
	}
	sb.WriteString("\n")

	// Include specific headers in canonical form
	headerKeys := make([]string, 0)
	for k := range headers {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-") {
			headerKeys = append(headerKeys, lk)
		}
	}
	sort.Strings(headerKeys)
	for _, k := range headerKeys {
		// Skip the signature header itself to avoid chicken-and-egg problem
		if k == "x-signature" {
			continue
		}
		sb.WriteString(k)
		sb.WriteString(":")
		sb.WriteString(strings.TrimSpace(headers.Get(k)))
		sb.WriteString("\n")
	}

	return sb.String()
}

// SetClockSkew overrides the default clock skew tolerance.
func (s *Signer) SetClockSkew(d time.Duration) {
	s.clockSkew = d
}
