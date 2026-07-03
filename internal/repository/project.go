package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Project represents a project record in the database.
type Project struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectRepository handles database operations for projects.
type ProjectRepository struct {
	pool *pgxpool.Pool
}

// NewProjectRepository creates a new project repository.
func NewProjectRepository(pool *pgxpool.Pool) *ProjectRepository {
	return &ProjectRepository{pool: pool}
}

// Create inserts a new project.
func (r *ProjectRepository) Create(ctx context.Context, project *Project) error {
	query := `
		INSERT INTO projects (org_id, name, description, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`
	return r.pool.QueryRow(ctx, query,
		project.OrgID, project.Name, project.Description, project.Status,
	).Scan(&project.ID, &project.CreatedAt, &project.UpdatedAt)
}

// FindByID retrieves a project by ID.
func (r *ProjectRepository) FindByID(ctx context.Context, id string) (*Project, error) {
	query := `
		SELECT id, org_id, name, description, status, created_at, updated_at
		FROM projects WHERE id = $1
	`
	project := &Project{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&project.ID, &project.OrgID, &project.Name,
		&project.Description, &project.Status,
		&project.CreatedAt, &project.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("project not found")
		}
		return nil, fmt.Errorf("failed to find project: %w", err)
	}
	return project, nil
}

// Update updates project fields.
func (r *ProjectRepository) Update(ctx context.Context, id, name, description, status string) error {
	query := `
		UPDATE projects
		SET name = $2, description = $3, status = $4, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, id, name, description, status)
	return err
}

// Delete removes a project by ID.
func (r *ProjectRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	return err
}

// Count returns the total number of projects.
func (r *ProjectRepository) Count(ctx context.Context, count *int) error {
	return r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM projects`).Scan(count)
}

// ListByOrg returns all projects for an organization.
func (r *ProjectRepository) ListByOrg(ctx context.Context, orgID string) ([]Project, error) {
	query := `
		SELECT id, org_id, name, description, status, created_at, updated_at
		FROM projects WHERE org_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(
			&p.ID, &p.OrgID, &p.Name,
			&p.Description, &p.Status,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan project: %w", err)
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}
