// Package webhook provides outbound webhook notifications for events
// like scan completion, alert triggers, and budget threshold breaches.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Event represents a webhook event payload.
type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"` // scan.completed, alert.triggered, budget.threshold
	Payload   map[string]interface{} `json:"payload"`
	CreatedAt time.Time              `json:"created_at"`
}

// Endpoint represents a registered webhook endpoint.
type Endpoint struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Secret    string    `json:"secret,omitempty"` // HMAC secret for signature verification
	Events    []string  `json:"events"`           // event types to subscribe to
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// DeliveryResult tracks webhook delivery success/failure.
type DeliveryResult struct {
	EndpointID string    `json:"endpoint_id"`
	EventID    string    `json:"event_id"`
	StatusCode int       `json:"status_code"`
	DurationMs int64     `json:"duration_ms"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	RetryCount int       `json:"retry_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// Engine manages webhook registrations and deliveries.
type Engine struct {
	mu        sync.RWMutex
	endpoints map[string]*Endpoint
	results   []DeliveryResult
	client    *http.Client
	maxRetry  int
}

// NewEngine creates a webhook engine.
func NewEngine() *Engine {
	return &Engine{
		endpoints: make(map[string]*Endpoint),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		maxRetry: 3,
	}
}

// Register adds a new webhook endpoint.
func (e *Engine) Register(ep *Endpoint) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ep.CreatedAt.IsZero() {
		ep.CreatedAt = time.Now()
	}
	e.endpoints[ep.ID] = ep
}

// Unregister removes a webhook endpoint.
func (e *Engine) Unregister(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.endpoints[id]; !ok {
		return false
	}
	delete(e.endpoints, id)
	return true
}

// GetEndpoint returns a webhook endpoint by ID.
func (e *Engine) GetEndpoint(id string) *Endpoint {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ep, ok := e.endpoints[id]
	if !ok {
		return nil
	}
	cp := *ep
	return &cp
}

// ListEndpoints returns all registered endpoints.
func (e *Engine) ListEndpoints() []Endpoint {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Endpoint, 0, len(e.endpoints))
	for _, ep := range e.endpoints {
		out = append(out, *ep)
	}
	return out
}

// ComputeSignature creates an HMAC-SHA256 signature of the payload.
func ComputeSignature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature verifies an HMAC-SHA256 signature.
func VerifySignature(secret, payload []byte, signature string) bool {
	expected := ComputeSignature(string(secret), payload)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// Dispatch sends an event to all matching endpoints asynchronously.
func (e *Engine) Dispatch(ctx context.Context, event Event) {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}

	e.mu.RLock()
	var targets []*Endpoint
	for _, ep := range e.endpoints {
		if !ep.Active {
			continue
		}
		for _, sub := range ep.Events {
			if sub == event.Type || sub == "*" {
				targets = append(targets, ep)
				break
			}
		}
	}
	e.mu.RUnlock()

	for _, ep := range targets {
		go e.deliver(ctx, ep, event, 0)
	}
}

// deliver sends a single webhook with retries.
func (e *Engine) deliver(ctx context.Context, ep *Endpoint, event Event, retryCount int) {
	payload, err := json.Marshal(event)
	if err != nil {
		slog.Error("webhook: failed to marshal event", "error", err, "endpoint_id", ep.ID)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(payload))
	if err != nil {
		slog.Error("webhook: failed to create request", "error", err, "endpoint_id", ep.ID)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Event", event.Type)
	req.Header.Set("X-Webhook-ID", event.ID)

	if ep.Secret != "" {
		sig := ComputeSignature(ep.Secret, payload)
		req.Header.Set("X-Webhook-Signature", "sha256="+sig)
	}

	start := time.Now()
	resp, err := e.client.Do(req)
	duration := time.Since(start).Milliseconds()

	result := DeliveryResult{
		EndpointID: ep.ID,
		EventID:    event.ID,
		DurationMs: duration,
		RetryCount: retryCount,
		CreatedAt:  time.Now(),
	}

	if err != nil {
		result.Error = err.Error()
		result.Success = false
		e.recordResult(result)

		if retryCount < e.maxRetry {
			delay := time.Duration(1<<uint(retryCount)) * time.Second
			time.AfterFunc(delay, func() {
				e.deliver(ctx, ep, event, retryCount+1)
			})
		}
		return
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300

	if !result.Success {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		result.Error = fmt.Sprintf("status %d: %s", resp.StatusCode, string(body))

		if retryCount < e.maxRetry {
			delay := time.Duration(1<<uint(retryCount)) * time.Second
			time.AfterFunc(delay, func() {
				e.deliver(ctx, ep, event, retryCount+1)
			})
		}
	}

	e.recordResult(result)
}

func (e *Engine) recordResult(r DeliveryResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.results = append(e.results, r)
}

// GetResults returns recent delivery results.
func (e *Engine) GetResults(n int) []DeliveryResult {
	e.mu.RLock()
	defer e.mu.RUnlock()
	total := len(e.results)
	if n > total {
		n = total
	}
	start := total - n
	out := make([]DeliveryResult, n)
	copy(out, e.results[start:])
	return out
}

// Stats returns delivery statistics.
func (e *Engine) Stats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var total, success, fail int
	for _, r := range e.results {
		total++
		if r.Success {
			success++
		} else {
			fail++
		}
	}
	return map[string]interface{}{
		"total_deliveries": total,
		"successful":       success,
		"failed":           fail,
		"endpoints":        len(e.endpoints),
	}
}
