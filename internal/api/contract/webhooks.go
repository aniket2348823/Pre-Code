package contract

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ---------------------------------------------------------------------------
// Webhook types — API contract §4
// ---------------------------------------------------------------------------

// WebhookEvent identifies the kind of webhook notification.
type WebhookEvent string

const (
	WebhookTaskCompleted  WebhookEvent = "task.completed"
	WebhookTaskFailed     WebhookEvent = "task.failed"
	WebhookHITLRequired   WebhookEvent = "hitl.required"
	WebhookBudgetExceeded WebhookEvent = "budget.exceeded"
	WebhookSkillInstalled WebhookEvent = "skill.installed"
)

// AllWebhookEvents returns every defined webhook event.
func AllWebhookEvents() []WebhookEvent {
	return []WebhookEvent{
		WebhookTaskCompleted, WebhookTaskFailed, WebhookHITLRequired,
		WebhookBudgetExceeded, WebhookSkillInstalled,
	}
}

// Valid returns true when the event is one of the known values.
func (e WebhookEvent) Valid() bool {
	for _, v := range AllWebhookEvents() {
		if e == v {
			return true
		}
	}
	return false
}

// WebhookPayload is the envelope sent to webhook endpoints.
type WebhookPayload struct {
	Event     WebhookEvent    `json:"event"`
	Timestamp Timestamp       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// WebhookConfig defines a user's webhook registration.
type WebhookConfig struct {
	ID     string         `json:"id,omitempty"`
	URL    string         `json:"url"`
	Secret string         `json:"secret"`
	Events []WebhookEvent `json:"events"`
	Active bool           `json:"active"`
}

// Validate checks required fields.
func (c *WebhookConfig) Validate() ValidationErrors {
	var errs ValidationErrors
	if c.URL == "" {
		errs.Add("url", "url is required")
	}
	if c.Secret == "" {
		errs.Add("secret", "secret is required")
	}
	if len(c.Events) == 0 {
		errs.Add("events", "at least one event is required")
	}
	for i, e := range c.Events {
		if !e.Valid() {
			errs.Add("events", "invalid event at index "+itoa(i))
		}
	}
	return errs
}

// ---------------------------------------------------------------------------
// HMAC-SHA256 Webhook Signature — API contract §4.2
// ---------------------------------------------------------------------------

// SignatureHeader is the HTTP header containing the HMAC-SHA256 signature.
const SignatureHeader = "X-VigilAgent-Signature"

// ComputeSignature calculates the HMAC-SHA256 hex digest of payload using secret.
func ComputeSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyWebhookSignature validates that the given signature matches the
// HMAC-SHA256 of payload signed with secret. Comparison is constant-time.
func VerifyWebhookSignature(payload []byte, signature, secret string) bool {
	expected := ComputeSignature(payload, secret)
	return hmac.Equal([]byte(signature), []byte(expected))
}
