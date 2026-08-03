package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/vigilagent/vigilagent/internal/database"
)

// Event represents an event record in the database.
type Event struct {
	ID         string                 `json:"id"`
	SessionID  string                 `json:"session_id"`
	EventType  string                 `json:"event_type"`
	Source     string                 `json:"source,omitempty"`
	Payload    map[string]interface{} `json:"payload,omitempty"`
	TokensUsed int                    `json:"tokens_used"`
	CostUsd    float64                `json:"cost_usd"`
	LatencyMs  int                    `json:"latency_ms,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
}

// CostSummary represents aggregated cost data.
type CostSummary struct {
	TotalCost  float64 `json:"total_cost"`
	EventCount int     `json:"event_count"`
	AvgCost    float64 `json:"avg_cost"`
}

// TokenSummary represents aggregated token data.
type TokenSummary struct {
	TotalTokens int     `json:"total_tokens"`
	EventCount  int     `json:"event_count"`
	AvgTokens   float64 `json:"avg_tokens"`
}

// SessionStats represents aggregated session data.
type SessionStats struct {
	TotalSessions  int     `json:"total_sessions"`
	ActiveSessions int     `json:"active_sessions"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	TotalEvents    int     `json:"total_events"`
}

// TopAgent represents an agent ranked by usage.
type TopAgent struct {
	AgentID      string  `json:"agent_id"`
	AgentName    string  `json:"agent_name"`
	ProjectID    string  `json:"project_id"`
	SessionCount int     `json:"session_count"`
	TotalEvents  int     `json:"total_events"`
	TotalTokens  int     `json:"total_tokens"`
	TotalCost    float64 `json:"total_cost"`
}

// DashboardActivity represents a recent activity item.
type DashboardActivity struct {
	EventType string    `json:"event_type"`
	Source    string    `json:"source"`
	Tokens    int       `json:"tokens"`
	Cost      float64   `json:"cost"`
	Timestamp time.Time `json:"timestamp"`
}

// EventRepository handles database operations for events.
type EventRepository struct {
	pool *database.Conn
}

// NewEventRepository creates a new event repository.
func NewEventRepository(pool *database.Conn) *EventRepository {
	return &EventRepository{pool: pool}
}

// Create inserts a new event.
func (r *EventRepository) Create(ctx context.Context, event *Event) error {
	query := `
		INSERT INTO events (session_id, event_type, source, payload, tokens_used, cost_usd, latency_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`
	return r.pool.QueryRow(ctx, query,
		event.SessionID, event.EventType, event.Source, event.Payload,
		event.TokensUsed, event.CostUsd, event.LatencyMs,
	).Scan(&event.ID, &event.CreatedAt)
}

// BatchCreate inserts multiple events in a single transaction.
// Uses ON CONFLICT to handle idempotent inserts (retry safety).
func (r *EventRepository) BatchCreate(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(context.Background())

	// Use ON CONFLICT DO NOTHING for idempotent batch inserts.
	// Do NOT use RETURNING here — ON CONFLICT DO NOTHING returns zero rows,
	// causing Scan to fail with ErrNoRows for deduplicated inserts.
	query := `
		INSERT INTO events (session_id, event_type, source, payload, tokens_used, cost_usd, latency_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT DO NOTHING
	`
	for i := range events {
		_, err := tx.Exec(ctx, query,
			events[i].SessionID, events[i].EventType, events[i].Source, events[i].Payload,
			events[i].TokensUsed, events[i].CostUsd, events[i].LatencyMs,
		)
		if err != nil {
			return fmt.Errorf("failed to insert event %d: %w", i, err)
		}
	}

	return tx.Commit(ctx)
}

// GetCostByOrg returns cost summary for an organization within a time range.
func (r *EventRepository) GetCostByOrg(ctx context.Context, orgID string, from, to time.Time) (*CostSummary, error) {
	query := `
		SELECT COALESCE(SUM(e.cost_usd), 0), COUNT(*), COALESCE(AVG(e.cost_usd), 0)
		FROM events e
		JOIN sessions s ON e.session_id = s.id
		JOIN projects p ON s.project_id = p.id
		WHERE p.org_id = $1
		AND e.created_at BETWEEN $2 AND $3
	`
	summary := &CostSummary{}
	err := r.pool.QueryRow(ctx, query, orgID, from, to).Scan(
		&summary.TotalCost, &summary.EventCount, &summary.AvgCost,
	)
	return summary, err
}

// GetTokensByOrg returns token summary for an organization within a time range.
func (r *EventRepository) GetTokensByOrg(ctx context.Context, orgID string, from, to time.Time) (*TokenSummary, error) {
	query := `
		SELECT COALESCE(SUM(e.tokens_used), 0), COUNT(*), COALESCE(AVG(e.tokens_used), 0)
		FROM events e
		JOIN sessions s ON e.session_id = s.id
		JOIN projects p ON s.project_id = p.id
		WHERE p.org_id = $1
		AND e.created_at BETWEEN $2 AND $3
	`
	summary := &TokenSummary{}
	err := r.pool.QueryRow(ctx, query, orgID, from, to).Scan(
		&summary.TotalTokens, &summary.EventCount, &summary.AvgTokens,
	)
	return summary, err
}

// GetSessionStatsByOrg returns session stats for an organization.
func (r *EventRepository) GetSessionStatsByOrg(ctx context.Context, orgID string) (*SessionStats, error) {
	query := `
		SELECT
			COUNT(DISTINCT s.id) as total_sessions,
			COUNT(DISTINCT s.id) FILTER (WHERE s.status = 'active') as active_sessions,
			COALESCE(AVG(e.latency_ms), 0) as avg_latency,
			COUNT(DISTINCT e.id) as total_events
		FROM sessions s
		JOIN projects p ON s.project_id = p.id
		LEFT JOIN events e ON e.session_id = s.id
		WHERE p.org_id = $1
	`
	stats := &SessionStats{}
	err := r.pool.QueryRow(ctx, query, orgID).Scan(
		&stats.TotalSessions, &stats.ActiveSessions, &stats.AvgLatencyMs, &stats.TotalEvents,
	)
	return stats, err
}

// GetTopAgentsByOrg returns top agents ranked by event count for an organization.
func (r *EventRepository) GetTopAgentsByOrg(ctx context.Context, orgID string, limit int) ([]TopAgent, error) {
	if limit <= 0 {
		limit = 10
	}
	query := `
		SELECT a.id, a.name, a.project_id,
			COUNT(DISTINCT s.id) as session_count,
			COUNT(e.id) as total_events,
			COALESCE(SUM(e.tokens_used), 0) as total_tokens,
			COALESCE(SUM(e.cost_usd), 0) as total_cost
		FROM agents a
		JOIN projects p ON a.project_id = p.id
		LEFT JOIN sessions s ON s.agent_id = a.id
		LEFT JOIN events e ON e.session_id = s.id
		WHERE p.org_id = $1
		GROUP BY a.id, a.name, a.project_id
		ORDER BY total_events DESC
		LIMIT $2
	`
	rows, err := r.pool.Query(ctx, query, orgID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query top agents: %w", err)
	}
	defer rows.Close()

	var agents []TopAgent
	for rows.Next() {
		var a TopAgent
		if err := rows.Scan(
			&a.AgentID, &a.AgentName, &a.ProjectID,
			&a.SessionCount, &a.TotalEvents, &a.TotalTokens, &a.TotalCost,
		); err != nil {
			return nil, fmt.Errorf("failed to scan top agent: %w", err)
		}
		agents = append(agents, a)
	}
	if agents == nil {
		agents = []TopAgent{}
	}
	return agents, rows.Err()
}

// GetRecentActivity returns recent events for an organization.
func (r *EventRepository) GetRecentActivity(ctx context.Context, orgID string, limit int) ([]DashboardActivity, error) {
	if limit <= 0 {
		limit = 20
	}
	query := `
		SELECT e.event_type, e.source, e.tokens_used, e.cost_usd, e.created_at
		FROM events e
		JOIN sessions s ON e.session_id = s.id
		JOIN projects p ON s.project_id = p.id
		WHERE p.org_id = $1
		ORDER BY e.created_at DESC
		LIMIT $2
	`
	rows, err := r.pool.Query(ctx, query, orgID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query activity: %w", err)
	}
	defer rows.Close()

	var activities []DashboardActivity
	for rows.Next() {
		var a DashboardActivity
		if err := rows.Scan(&a.EventType, &a.Source, &a.Tokens, &a.Cost, &a.Timestamp); err != nil {
			return nil, fmt.Errorf("failed to scan activity: %w", err)
		}
		activities = append(activities, a)
	}
	if activities == nil {
		activities = []DashboardActivity{}
	}
	return activities, rows.Err()
}
