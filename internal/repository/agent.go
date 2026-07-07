package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vigilagent/vigilagent/internal/database"
)

// Agent represents an agent record in the database.
type Agent struct {
	ID          string                 `json:"id"`
	ProjectID   string                 `json:"project_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Config      map[string]interface{} `json:"config,omitempty"`
	Status      string                 `json:"status"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// AgentRepository handles database operations for agents.
type AgentRepository struct {
	pool *database.Conn
}

// NewAgentRepository creates a new agent repository.
func NewAgentRepository(pool *database.Conn) *AgentRepository {
	return &AgentRepository{pool: pool}
}

// Create inserts a new agent.
func (r *AgentRepository) Create(ctx context.Context, agent *Agent) error {
	query := `
		INSERT INTO agents (project_id, name, description, config, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`
	return r.pool.QueryRow(ctx, query,
		agent.ProjectID, agent.Name, agent.Description, agent.Config, agent.Status,
	).Scan(&agent.ID, &agent.CreatedAt, &agent.UpdatedAt)
}

// FindByID retrieves an agent by ID.
func (r *AgentRepository) FindByID(ctx context.Context, id string) (*Agent, error) {
	query := `
		SELECT id, project_id, name, description, config, status, created_at, updated_at
		FROM agents WHERE id = $1
	`
	agent := &Agent{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&agent.ID, &agent.ProjectID, &agent.Name,
		&agent.Description, &agent.Config, &agent.Status,
		&agent.CreatedAt, &agent.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("agent not found")
		}
		return nil, fmt.Errorf("failed to find agent: %w", err)
	}
	return agent, nil
}

// Update updates agent fields.
func (r *AgentRepository) Update(ctx context.Context, id, name, description, status string, config map[string]interface{}) error {
	query := `
		UPDATE agents
		SET name = $2, description = $3, status = $4, config = $5, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, id, name, description, status, config)
	return err
}

// Delete removes an agent by ID.
func (r *AgentRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM agents WHERE id = $1`, id)
	return err
}

// ListByProject returns all agents for a project.
func (r *AgentRepository) ListByProject(ctx context.Context, projectID string) ([]Agent, error) {
	query := `
		SELECT id, project_id, name, description, config, status, created_at, updated_at
		FROM agents WHERE project_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var a Agent
		if err := rows.Scan(
			&a.ID, &a.ProjectID, &a.Name,
			&a.Description, &a.Config, &a.Status,
			&a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}
