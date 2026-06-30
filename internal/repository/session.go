package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Session represents a session record in the database.
type Session struct {
	ID        string      `json:"id"`
	ProjectID string      `json:"project_id"`
	AgentID   string      `json:"agent_id,omitempty"`
	UserID    string      `json:"user_id,omitempty"`
	Status    string      `json:"status"`
	StartedAt time.Time   `json:"started_at"`
	EndedAt   *time.Time  `json:"ended_at,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// SessionRepository handles database operations for sessions.
type SessionRepository struct {
	pool *pgxpool.Pool
}

// NewSessionRepository creates a new session repository.
func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

// Create inserts a new session.
func (r *SessionRepository) Create(ctx context.Context, session *Session) error {
	query := `
		INSERT INTO sessions (project_id, agent_id, user_id, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id, started_at, created_at, updated_at
	`
	return r.pool.QueryRow(ctx, query,
		session.ProjectID, session.AgentID, session.UserID, session.Status,
	).Scan(&session.ID, &session.StartedAt, &session.CreatedAt, &session.UpdatedAt)
}

// FindByID retrieves a session by ID.
func (r *SessionRepository) FindByID(ctx context.Context, id string) (*Session, error) {
	query := `
		SELECT id, project_id, agent_id, user_id, status, started_at, ended_at, created_at, updated_at
		FROM sessions WHERE id = $1
	`
	session := &Session{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&session.ID, &session.ProjectID, &session.AgentID,
		&session.UserID, &session.Status,
		&session.StartedAt, &session.EndedAt,
		&session.CreatedAt, &session.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("session not found")
		}
		return nil, fmt.Errorf("failed to find session: %w", err)
	}
	return session, nil
}

// Update updates session status and optionally sets ended_at.
func (r *SessionRepository) Update(ctx context.Context, id, status string) error {
	query := `
		UPDATE sessions
		SET status = $2, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, id, status)
	return err
}

// EndSession sets the ended_at timestamp and status to completed.
func (r *SessionRepository) EndSession(ctx context.Context, id string) error {
	query := `
		UPDATE sessions
		SET status = 'completed', ended_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

// ListByAgent returns all sessions for an agent.
func (r *SessionRepository) ListByAgent(ctx context.Context, agentID string) ([]Session, error) {
	query := `
		SELECT id, project_id, agent_id, user_id, status, started_at, ended_at, created_at, updated_at
		FROM sessions WHERE agent_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(
			&s.ID, &s.ProjectID, &s.AgentID,
			&s.UserID, &s.Status,
			&s.StartedAt, &s.EndedAt,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}
