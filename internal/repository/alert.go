package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Alert represents an alert record in the database.
type Alert struct {
	ID         string                 `json:"id"`
	UserID     string                 `json:"user_id"`
	OrgID      string                 `json:"org_id,omitempty"`
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Condition  map[string]interface{} `json:"condition,omitempty"`
	Channel    string                 `json:"channel"`
	IsActive   bool                   `json:"is_active"`
	LastFired  *time.Time             `json:"last_fired,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

// AlertRepository handles database operations for alerts.
type AlertRepository struct {
	pool *pgxpool.Pool
}

// NewAlertRepository creates a new alert repository.
func NewAlertRepository(pool *pgxpool.Pool) *AlertRepository {
	return &AlertRepository{pool: pool}
}

// Create inserts a new alert.
func (r *AlertRepository) Create(ctx context.Context, alert *Alert) error {
	query := `
		INSERT INTO alerts (user_id, org_id, name, type, condition, channel, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`
	return r.pool.QueryRow(ctx, query,
		alert.UserID, alert.OrgID, alert.Name, alert.Type,
		alert.Condition, alert.Channel, alert.IsActive,
	).Scan(&alert.ID, &alert.CreatedAt, &alert.UpdatedAt)
}

// FindByID retrieves an alert by ID.
func (r *AlertRepository) FindByID(ctx context.Context, id string) (*Alert, error) {
	query := `
		SELECT id, user_id, org_id, name, type, condition, channel, is_active, last_fired, created_at, updated_at
		FROM alerts WHERE id = $1
	`
	alert := &Alert{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&alert.ID, &alert.UserID, &alert.OrgID, &alert.Name, &alert.Type,
		&alert.Condition, &alert.Channel, &alert.IsActive,
		&alert.LastFired, &alert.CreatedAt, &alert.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("alert not found")
		}
		return nil, fmt.Errorf("failed to find alert: %w", err)
	}
	return alert, nil
}

// ListByUser lists alerts for a user.
func (r *AlertRepository) ListByUser(ctx context.Context, userID string) ([]Alert, error) {
	query := `
		SELECT id, user_id, org_id, name, type, condition, channel, is_active, last_fired, created_at, updated_at
		FROM alerts WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []Alert
	for rows.Next() {
		var a Alert
		if err := rows.Scan(
			&a.ID, &a.UserID, &a.OrgID, &a.Name, &a.Type,
			&a.Condition, &a.Channel, &a.IsActive,
			&a.LastFired, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, nil
}

// Update updates an alert.
func (r *AlertRepository) Update(ctx context.Context, id, name, channel string, isActive bool) error {
	query := `UPDATE alerts SET name=$2, channel=$3, is_active=$4, updated_at=NOW() WHERE id=$1`
	_, err := r.pool.Exec(ctx, query, id, name, channel, isActive)
	return err
}

// Delete removes an alert.
func (r *AlertRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM alerts WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}
