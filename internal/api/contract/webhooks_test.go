package contract

import (
	"encoding/json"
	"testing"
)

func TestWebhookEvent_AllValid(t *testing.T) {
	all := AllWebhookEvents()
	if len(all) != 15 {
		t.Errorf("AllWebhookEvents() has %d entries, want 15", len(all))
	}
	for _, e := range all {
		if !e.Valid() {
			t.Errorf("WebhookEvent(%q).Valid() = false", e)
		}
	}
}

func TestWebhookEvent_InvalidRejected(t *testing.T) {
	if WebhookEvent("task.paused").Valid() {
		t.Error("task.paused should be invalid")
	}
	if WebhookEvent("").Valid() {
		t.Error("empty string should be invalid")
	}
}

func TestVerifyWebhookSignature_Valid(t *testing.T) {
	payload := []byte(`{"event":"task.completed","data":{}}`)
	secret := "whsec_test_secret_key"

	signature := ComputeSignature(payload, secret)
	if !VerifyWebhookSignature(payload, signature, secret) {
		t.Error("VerifyWebhookSignature should return true for valid signature")
	}
}

func TestVerifyWebhookSignature_Invalid(t *testing.T) {
	payload := []byte(`{"event":"task.completed","data":{}}`)
	secret := "whsec_test_secret_key"

	if VerifyWebhookSignature(payload, "invalid_hex_signature", secret) {
		t.Error("VerifyWebhookSignature should return false for invalid signature")
	}
}

func TestVerifyWebhookSignature_WrongSecret(t *testing.T) {
	payload := []byte(`{"event":"task.completed","data":{}}`)
	signature := ComputeSignature(payload, "correct_secret")

	if VerifyWebhookSignature(payload, signature, "wrong_secret") {
		t.Error("VerifyWebhookSignature should return false for wrong secret")
	}
}

func TestVerifyWebhookSignature_TamperedPayload(t *testing.T) {
	original := []byte(`{"event":"task.completed","data":{}}`)
	tampered := []byte(`{"event":"task.completed","data":{"malicious":true}}`)
	secret := "whsec_test_secret_key"

	signature := ComputeSignature(original, secret)
	if VerifyWebhookSignature(tampered, signature, secret) {
		t.Error("VerifyWebhookSignature should return false for tampered payload")
	}
}

func TestComputeSignature_Deterministic(t *testing.T) {
	payload := []byte(`{"test":true}`)
	secret := "key"

	sig1 := ComputeSignature(payload, secret)
	sig2 := ComputeSignature(payload, secret)

	if sig1 != sig2 {
		t.Errorf("signatures differ: %q != %q", sig1, sig2)
	}
}

func TestComputeSignature_DifferentPayloads(t *testing.T) {
	secret := "key"
	sig1 := ComputeSignature([]byte(`{"a":1}`), secret)
	sig2 := ComputeSignature([]byte(`{"a":2}`), secret)

	if sig1 == sig2 {
		t.Error("different payloads should produce different signatures")
	}
}

func TestWebhookPayload_JSONRoundTrip(t *testing.T) {
	now := Now()
	original := WebhookPayload{
		Event:     WebhookTaskCompleted,
		Timestamp: now,
		Data:      json.RawMessage(`{"task_id":"t-1","status":"completed"}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded WebhookPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Event != WebhookTaskCompleted {
		t.Errorf("Event = %q, want task.completed", decoded.Event)
	}
	if len(decoded.Data) == 0 {
		t.Error("Data should not be empty")
	}
}

func TestWebhookConfig_Validate(t *testing.T) {
	tests := []struct {
		name     string
		config   WebhookConfig
		hasErr   bool
		errField string
	}{
		{
			name: "valid config",
			config: WebhookConfig{
				URL:    "https://example.com/webhook",
				Secret: "whsec_abc123",
				Events: []WebhookEvent{WebhookTaskCompleted, WebhookTaskFailed},
				Active: true,
			},
			hasErr: false,
		},
		{
			name: "missing url",
			config: WebhookConfig{
				Secret: "whsec_abc123",
				Events: []WebhookEvent{WebhookTaskCompleted},
			},
			hasErr:   true,
			errField: "url",
		},
		{
			name: "missing secret",
			config: WebhookConfig{
				URL:    "https://example.com/webhook",
				Events: []WebhookEvent{WebhookTaskCompleted},
			},
			hasErr:   true,
			errField: "secret",
		},
		{
			name: "no events",
			config: WebhookConfig{
				URL:    "https://example.com/webhook",
				Secret: "whsec_abc123",
				Events: []WebhookEvent{},
			},
			hasErr:   true,
			errField: "events",
		},
		{
			name: "invalid event in list",
			config: WebhookConfig{
				URL:    "https://example.com/webhook",
				Secret: "whsec_abc123",
				Events: []WebhookEvent{WebhookTaskCompleted, "task.paused"},
			},
			hasErr:   true,
			errField: "events",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.config.Validate()
			if tt.hasErr && !errs.HasErrors() {
				t.Errorf("expected validation error on field %q", tt.errField)
			}
			if !tt.hasErr && errs.HasErrors() {
				t.Errorf("unexpected errors: %v", errs.ToMap())
			}
		})
	}
}
