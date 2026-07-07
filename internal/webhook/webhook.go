// Package webhook provides DB-backed outbound webhook notifications for events
// like scan completion, alert triggers, and budget threshold breaches.
// Endpoints and delivery results are stored in PostgreSQL (webhook_endpoints
// and webhook_deliveries tables) for persistence across restarts.
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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	Secret    string    `json:"secret,omitempty"`
	Events    []string  `json:"events"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// DeliveryResult tracks webhook delivery success/failure.
type DeliveryResult struct {
	EndpointID string    `json:"endpoint_id"`
	EventType  string    `json:"event_type"`
	StatusCode int       `json:"status_code"`
	DurationMs int64     `json:"duration_ms"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	RetryCount int       `json:"retry_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// Engine manages webhook registrations and deliveries backed by PostgreSQL.
type Engine struct {
	pool        *pgxpool.Pool
	client      *http.Client
	maxRetry    int
	cache       []Endpoint
	cacheExpiry time.Time
}

// NewEngine creates a DB-backed webhook engine.
func NewEngine(pool *pgxpool.Pool) *Engine {
	return &Engine{
		pool: pool,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		maxRetry: 3,
	}
}

// Register inserts a new webhook endpoint into the database.
func (e *Engine) Register(ctx context.Context, ep *Endpoint) error {
	query := `
		INSERT INTO webhook_endpoints (url, secret, events, is_active)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`
	eventsJSON, _ := json.Marshal(ep.Events)
	return e.pool.QueryRow(ctx, query,
		ep.URL, ep.Secret, eventsJSON, ep.Active,
	).Scan(&ep.ID, &ep.CreatedAt)
}

// Unregister deletes a webhook endpoint by ID.
func (e *Engine) Unregister(ctx context.Context, id string) error {
	_, err := e.pool.Exec(ctx, `DELETE FROM webhook_endpoints WHERE id = $1`, id)
	return err
}

// GetEndpoint returns a webhook endpoint by ID.
func (e *Engine) GetEndpoint(ctx context.Context, id string) (*Endpoint, error) {
	query := `
		SELECT id, url, secret, events, is_active, created_at
		FROM webhook_endpoints WHERE id = $1
	`
	ep := &Endpoint{}
	var eventsJSON []byte
	err := e.pool.QueryRow(ctx, query, id).Scan(
		&ep.ID, &ep.URL, &ep.Secret, &eventsJSON, &ep.Active, &ep.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(eventsJSON, &ep.Events)
	return ep, nil
}

// ListEndpoints returns all webhook endpoints.
func (e *Engine) ListEndpoints(ctx context.Context) ([]Endpoint, error) {
	query := `
		SELECT id, url, secret, events, is_active, created_at
		FROM webhook_endpoints ORDER BY created_at DESC
	`
	rows, err := e.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []Endpoint
	for rows.Next() {
		var ep Endpoint
		var eventsJSON []byte
		if err := rows.Scan(
			&ep.ID, &ep.URL, &ep.Secret, &eventsJSON, &ep.Active, &ep.CreatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(eventsJSON, &ep.Events)
		endpoints = append(endpoints, ep)
	}
	return endpoints, rows.Err()
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

// cachedEndpoints returns endpoints from a 30-second in-memory cache to avoid
// hitting the DB on every Dispatch call.
func (e *Engine) cachedEndpoints(ctx context.Context) ([]Endpoint, error) {
	if time.Now().Before(e.cacheExpiry) && e.cache != nil {
		return e.cache, nil
	}
	endpoints, err := e.ListEndpoints(ctx)
	if err != nil && e.cache != nil {
		slog.Warn("webhook: using stale endpoint cache", "error", err)
		return e.cache, nil
	}
	if err != nil {
		return nil, err
	}
	e.cache = endpoints
	e.cacheExpiry = time.Now().Add(30 * time.Second)
	return endpoints, nil
}

// Dispatch sends an event to all matching active endpoints asynchronously.
func (e *Engine) Dispatch(ctx context.Context, event Event) {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}

	endpoints, err := e.cachedEndpoints(ctx)
	if err != nil {
		slog.Error("webhook: failed to list endpoints", "error", err)
		return
	}

	for i := range endpoints {
		ep := &endpoints[i]
		if !ep.Active {
			continue
		}
		for _, sub := range ep.Events {
			if sub == event.Type || sub == "*" {
				go e.deliver(ctx, ep, event, 0)
				break
			}
		}
	}
}

// deliver sends a single webhook with retries and records the result in DB.
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
		EventType:  event.Type,
		DurationMs: duration,
		RetryCount: retryCount,
		CreatedAt:  time.Now(),
	}

	if err != nil {
		result.Error = err.Error()
		result.Success = false
		e.recordResult(ctx, result)

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

	e.recordResult(ctx, result)
	// Update last_triggered_at on the endpoint
	_, _ = e.pool.Exec(ctx, `UPDATE webhook_endpoints SET last_triggered_at = NOW() WHERE id = $1`, ep.ID)
}

// recordResult inserts a delivery result into the webhook_deliveries table.
func (e *Engine) recordResult(ctx context.Context, r DeliveryResult) {
	query := `
		INSERT INTO webhook_deliveries (endpoint_id, event_type, status_code, success, error, duration_ms)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := e.pool.Exec(ctx, query,
		r.EndpointID, r.EventType, r.StatusCode, r.Success, r.Error, r.DurationMs,
	)
	if err != nil {
		slog.Error("webhook: failed to record delivery", "error", err, "endpoint_id", r.EndpointID)
	}
}

// GetResults returns recent delivery results for an endpoint.
func (e *Engine) GetResults(ctx context.Context, endpointID string, limit int) ([]DeliveryResult, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT endpoint_id, event_type, status_code, duration_ms, success, error, created_at
		FROM webhook_deliveries
		WHERE endpoint_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := e.pool.Query(ctx, query, endpointID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []DeliveryResult
	for rows.Next() {
		var r DeliveryResult
		if err := rows.Scan(
			&r.EndpointID, &r.EventType, &r.StatusCode, &r.DurationMs,
			&r.Success, &r.Error, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// Stats returns delivery statistics from the database.
func (e *Engine) Stats(ctx context.Context) (map[string]interface{}, error) {
	var endpoints, total24h, success24h, fail24h int

	if err := e.pool.QueryRow(ctx, `SELECT COUNT(*) FROM webhook_endpoints`).Scan(&endpoints); err != nil {
		return nil, err
	}

	if err := e.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE success), COUNT(*) FILTER (WHERE NOT success)
		FROM webhook_deliveries
		WHERE created_at > NOW() - INTERVAL '24 hours'
	`).Scan(&total24h, &success24h, &fail24h); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"endpoints":            endpoints,
		"total_deliveries_24h": total24h,
		"successful_24h":       success24h,
		"failed_24h":           fail24h,
	}, nil
}
