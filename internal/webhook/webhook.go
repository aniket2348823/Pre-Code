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
	"sync"
	"sync/atomic"
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
	UserID    string    `json:"user_id"`
	URL       string    `json:"url"`
	Secret    string    `json:"-"`
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
	validator   *SSRFValidator
	mu          sync.RWMutex
	// slots bounds how many deliveries can be in flight at once. A single event
	// fanned out to many endpoints must not spawn an unbounded number of
	// goroutines (amplification vector when many endpoints are registered).
	slots chan struct{}
	// lastDropLog throttles the saturated-pool warning to once per second so a
	// sustained overload does not spam the log with one line per dropped delivery.
	lastDropLog atomic.Int64
}

// maxConcurrentDeliveries caps concurrent webhook deliveries per engine.
const maxConcurrentDeliveries = 64

// NewEngine creates a DB-backed webhook engine.
func NewEngine(pool *pgxpool.Pool) *Engine {
	return &Engine{
		pool: pool,
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Block redirects so a webhook URL cannot bounce us to an internal
			// address after SSRF validation (redirect targets are never re-checked).
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return fmt.Errorf("webhook redirects not allowed")
			},
		},
		maxRetry:  3,
		validator: NewSSRFValidator(),
		slots:     make(chan struct{}, maxConcurrentDeliveries),
	}
}

// Register inserts a new webhook endpoint into the database.
func (e *Engine) Register(ctx context.Context, ep *Endpoint) error {
	if err := e.ValidateEndpoint(ctx, ep.URL); err != nil {
		return fmt.Errorf("SSRF validation failed: %w", err)
	}
	query := `
		INSERT INTO webhook_endpoints (user_id, url, secret, events, is_active)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`
	eventsJSON, _ := json.Marshal(ep.Events)
	return e.pool.QueryRow(ctx, query,
		ep.UserID, ep.URL, ep.Secret, eventsJSON, ep.Active,
	).Scan(&ep.ID, &ep.CreatedAt)
}

// Unregister deletes a webhook endpoint by ID for a specific user.
func (e *Engine) Unregister(ctx context.Context, userID, id string) error {
	_, err := e.pool.Exec(ctx, `DELETE FROM webhook_endpoints WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// GetEndpoint returns a webhook endpoint by ID for a specific user.
func (e *Engine) GetEndpoint(ctx context.Context, userID, id string) (*Endpoint, error) {
	query := `
		SELECT id, user_id, url, secret, events, is_active, created_at
		FROM webhook_endpoints WHERE id = $1 AND user_id = $2
	`
	ep := &Endpoint{}
	var eventsJSON []byte
	err := e.pool.QueryRow(ctx, query, id, userID).Scan(
		&ep.ID, &ep.UserID, &ep.URL, &ep.Secret, &eventsJSON, &ep.Active, &ep.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(eventsJSON, &ep.Events)
	return ep, nil
}

// ListEndpoints returns all webhook endpoints for a specific user.
func (e *Engine) ListEndpoints(ctx context.Context, userID string) ([]Endpoint, error) {
	query := `
		SELECT id, user_id, url, secret, events, is_active, created_at
		FROM webhook_endpoints WHERE user_id = $1 ORDER BY created_at DESC
	`
	rows, err := e.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []Endpoint
	for rows.Next() {
		var ep Endpoint
		var eventsJSON []byte
		if err := rows.Scan(
			&ep.ID, &ep.UserID, &ep.URL, &ep.Secret, &eventsJSON, &ep.Active, &ep.CreatedAt,
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

// ListAllEndpoints returns all webhook endpoints (for dispatch).
func (e *Engine) ListAllEndpoints(ctx context.Context) ([]Endpoint, error) {
	query := `
		SELECT id, user_id, url, secret, events, is_active, created_at
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
			&ep.ID, &ep.UserID, &ep.URL, &ep.Secret, &eventsJSON, &ep.Active, &ep.CreatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(eventsJSON, &ep.Events)
		endpoints = append(endpoints, ep)
	}
	return endpoints, rows.Err()
}

// cachedEndpoints returns endpoints from a 30-second in-memory cache to avoid
// hitting the DB on every Dispatch call.
func (e *Engine) cachedEndpoints(ctx context.Context) ([]Endpoint, error) {
	e.mu.RLock()
	if time.Now().Before(e.cacheExpiry) && e.cache != nil {
		cached := e.cache
		e.mu.RUnlock()
		return cached, nil
	}
	e.mu.RUnlock()

	endpoints, err := e.ListAllEndpoints(ctx)
	if err != nil {
		e.mu.RLock()
		stale := e.cache
		e.mu.RUnlock()
		if stale != nil {
			slog.Warn("webhook: using stale endpoint cache", "error", err)
			return stale, nil
		}
		return nil, err
	}

	e.mu.Lock()
	e.cache = endpoints
	e.cacheExpiry = time.Now().Add(30 * time.Second)
	e.mu.Unlock()
	return endpoints, nil
}

// Dispatch sends an event to all matching active endpoints asynchronously.
func (e *Engine) Dispatch(ctx context.Context, event Event) {
	// Defensive: the engine is nil when the server runs without a database
	// (dev/mock mode), and many handlers call Dispatch without a nil check.
	if e == nil || e.pool == nil {
		return
	}
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
				// Detach from the caller's request context: async deliveries must
				// survive the originating HTTP request, or they get cancelled
				// mid-flight and results are never recorded. The retry path
				// already uses a fresh background context for the same reason.
				deliverCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				// Bound concurrent deliveries. If the pool is saturated, skip this
				// delivery with a log — webhook notification is best-effort and a
				// slow endpoint must not block the whole fan-out or grow the
				// goroutine count without limit.
				select {
				case e.slots <- struct{}{}:
					go func() {
						defer func() { <-e.slots }()
						defer cancel()
						e.deliver(deliverCtx, ep, event, 0)
					}()
				default:
					cancel()
					e.logSaturated("webhook: delivery pool saturated, skipping delivery", ep.ID, event.Type)
				}
				break
			}
		}
	}
}

// deliver sends a single webhook with retries and records the result in DB.
func (e *Engine) deliver(ctx context.Context, ep *Endpoint, event Event, retryCount int) {
	if err := e.ValidateEndpoint(ctx, ep.URL); err != nil {
		slog.Error("webhook: SSRF validation failed at delivery", "error", err, "endpoint_id", ep.ID)
		return
	}

	payload, err := json.Marshal(event)
	if err != nil {
		slog.Error("webhook: failed to marshal event", "error", err, "endpoint_id", ep.ID)
		return
	}

	// Limit webhook response body to prevent OOM from large payloads.
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

		// Retry with exponential backoff, capped at maxRetry.
		if retryCount < e.maxRetry {
			e.scheduleRetry(ep, event, retryCount)
		}
		return
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300

	if !result.Success {
		// Limit response body read to 1KB to prevent memory issues.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		result.Error = fmt.Sprintf("status %d: %s", resp.StatusCode, string(body))

		if retryCount < e.maxRetry {
			e.scheduleRetry(ep, event, retryCount)
		}
	}

	e.recordResult(ctx, result)
	// Update last_triggered_at on the endpoint (best-effort; failure is not fatal).
	if _, err := e.pool.Exec(ctx, `UPDATE webhook_endpoints SET last_triggered_at = NOW() WHERE id = $1`, ep.ID); err != nil {
		slog.Warn("webhook: failed to update last_triggered_at", "error", err, "endpoint_id", ep.ID)
	}
}

// scheduleRetry retries a delivery after an exponential backoff delay. The retry
// acquires a slot from the same concurrent-delivery pool as the initial attempt,
// so the fan-out bound also covers the retry path (a delivery whose initial run
// failed cannot bypass the pool via time.AfterFunc).
func (e *Engine) scheduleRetry(ep *Endpoint, event Event, retryCount int) {
	delay := time.Duration(1<<uint(retryCount)) * time.Second
	time.AfterFunc(delay, func() {
		select {
		case e.slots <- struct{}{}:
			defer func() { <-e.slots }()
			// #nosec context_leak: background context for long-running startup/worker/lifecycle code - no request context exists here
			retryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			e.deliver(retryCtx, ep, event, retryCount+1)
		default:
			e.logSaturated("webhook: delivery pool saturated, skipping retry", ep.ID, event.Type)
		}
	})
}

// logSaturated reports a saturated delivery pool at most once per second at
// Warn level; the remainder of the drops degrade to Debug to avoid log spam.
func (e *Engine) logSaturated(msg string, epID, eventType string) {
	sec := time.Now().Unix()
	if e.lastDropLog.Load() != sec && e.lastDropLog.CompareAndSwap(e.lastDropLog.Load(), sec) {
		slog.Warn(msg, "endpoint_id", epID, "event_type", eventType)
		return
	}
	slog.Debug(msg, "endpoint_id", epID, "event_type", eventType)
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
		// #nosec log_injection: structured key-value logging (the rule's own recommended safe pattern) - no format-string interpolation of user input
		slog.Error("webhook: failed to record delivery", "error", err, "endpoint_id", r.EndpointID)
	}
}

// GetResults returns recent delivery results for an endpoint owned by a specific user.
func (e *Engine) GetResults(ctx context.Context, userID, endpointID string, limit int) ([]DeliveryResult, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT d.endpoint_id, d.event_type, d.status_code, d.duration_ms, d.success, d.error, d.created_at
		FROM webhook_deliveries d
		JOIN webhook_endpoints e ON e.id = d.endpoint_id
		WHERE d.endpoint_id = $1 AND e.user_id = $2
		ORDER BY d.created_at DESC
		LIMIT $3
	`
	rows, err := e.pool.Query(ctx, query, endpointID, userID, limit)
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

// Stats returns delivery statistics from the database for a specific user.
func (e *Engine) Stats(ctx context.Context, userID string) (map[string]interface{}, error) {
	var endpoints, total24h, success24h, fail24h int

	if err := e.pool.QueryRow(ctx, `SELECT COUNT(*) FROM webhook_endpoints WHERE user_id = $1`, userID).Scan(&endpoints); err != nil {
		return nil, err
	}

	if err := e.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE success), COUNT(*) FILTER (WHERE NOT success)
		FROM webhook_deliveries d
		JOIN webhook_endpoints e ON e.id = d.endpoint_id
		WHERE e.user_id = $1 AND d.created_at > NOW() - INTERVAL '24 hours'
	`, userID).Scan(&total24h, &success24h, &fail24h); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"endpoints":            endpoints,
		"total_deliveries_24h": total24h,
		"successful_24h":       success24h,
		"failed_24h":           fail24h,
	}, nil
}
