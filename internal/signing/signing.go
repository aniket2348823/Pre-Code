// Package signing provides request signing and verification for API security.
package signing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Signer creates HMAC signatures for HTTP requests.
type Signer struct {
	secret    []byte
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

// ─────────────────────────────────────────────────────────────────────────────
// PROVENANCE RECORDS
//
// A provenance record is the signed audit trail for a scan decision. It is
// explicit, verifiable metadata — never a hidden watermark in generated
// content. The record states where the content came from (provider/model from
// transaction metadata), what was scanned, and what the policy decided.
// ─────────────────────────────────────────────────────────────────────────────

// ProvenanceStatus describes how much is known about the content's origin.
const (
	// ProvenanceVerified: content was generated through the approved gateway;
	// provider/model/request metadata is known and recorded.
	ProvenanceVerified = "verified"
	// ProvenanceUnverified: source is unknown (e.g. pasted code). It is scanned
	// but never attributed to a model by stylistic guessing.
	ProvenanceUnverified = "unverified"
	// ProvenanceBypassed: detected outside the approved route, if policy or
	// network telemetry can establish this.
	ProvenanceBypassed = "bypassed"
)

// ProvenanceRecord is the signed record of one scan decision.
type ProvenanceRecord struct {
	ScanID           string    `json:"scan_id"`
	RequestID        string    `json:"request_id,omitempty"`
	Provider         string    `json:"provider,omitempty"`
	Model            string    `json:"model,omitempty"`
	TenantID         string    `json:"tenant_id,omitempty"`
	UserID           string    `json:"user_id,omitempty"`
	ProjectID        string    `json:"project_id,omitempty"`
	ClientType       string    `json:"client_type,omitempty"`
	ClientVersion    string    `json:"client_version,omitempty"`
	PolicyVersion    string    `json:"policy_version,omitempty"`
	ScannerVersion   string    `json:"scanner_version,omitempty"`
	ProvenanceStatus string    `json:"provenance_status"`
	ResponseHash     string    `json:"response_hash"`
	Decision         string    `json:"decision"`
	Mode             string    `json:"mode,omitempty"`
	Timestamp        time.Time `json:"timestamp"`
}

// HashContent returns the SHA-256 hex digest of a content payload. It anchors
// the record to the exact output that was scanned, so a later tamper of the
// content invalidates the record.
func HashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// canonicalProvenance marshals the record into a stable canonical string used
// for signing. Only semantic fields are signed (not the timestamp's monotonic
// clock component, which would otherwise differ between sign and verify).
func canonicalProvenance(rec ProvenanceRecord) ([]byte, error) {
	return json.Marshal(struct {
		ScanID           string `json:"scan_id"`
		RequestID        string `json:"request_id"`
		Provider         string `json:"provider"`
		Model            string `json:"model"`
		TenantID         string `json:"tenant_id"`
		UserID           string `json:"user_id"`
		ProjectID        string `json:"project_id"`
		ClientType       string `json:"client_type"`
		ClientVersion    string `json:"client_version"`
		PolicyVersion    string `json:"policy_version"`
		ScannerVersion   string `json:"scanner_version"`
		ProvenanceStatus string `json:"provenance_status"`
		ResponseHash     string `json:"response_hash"`
		Decision         string `json:"decision"`
		Mode             string `json:"mode"`
		Timestamp        string `json:"timestamp"`
	}{
		ScanID:           rec.ScanID,
		RequestID:        rec.RequestID,
		Provider:         rec.Provider,
		Model:            rec.Model,
		TenantID:         rec.TenantID,
		UserID:           rec.UserID,
		ProjectID:        rec.ProjectID,
		ClientType:       rec.ClientType,
		ClientVersion:    rec.ClientVersion,
		PolicyVersion:    rec.PolicyVersion,
		ScannerVersion:   rec.ScannerVersion,
		ProvenanceStatus: rec.ProvenanceStatus,
		ResponseHash:     rec.ResponseHash,
		Decision:         rec.Decision,
		Mode:             rec.Mode,
		Timestamp:        rec.Timestamp.UTC().Format(time.RFC3339Nano),
	})
}

// SignProvenance signs a provenance record with the server-managed secret and
// returns the HMAC-SHA256 signature (hex).
func SignProvenance(secret string, rec ProvenanceRecord) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("provenance signing secret is not configured")
	}
	canon, err := canonicalProvenance(rec)
	if err != nil {
		return "", err
	}
	return HMACSign([]byte(secret), string(canon)), nil
}

// VerifyProvenance checks that the record's signature matches the canonical
// form under the given secret.
func VerifyProvenance(secret string, rec ProvenanceRecord, signature string) error {
	expected, err := SignProvenance(secret, rec)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return fmt.Errorf("provenance signature mismatch")
	}
	return nil
}
