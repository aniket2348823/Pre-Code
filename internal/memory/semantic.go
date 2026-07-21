package memory

import (
	"context"
	"fmt"
	"time"
	"github.com/pgvector/pgvector-go"
	"github.com/vigilagent/vigilagent/internal/database"
)

// SemanticStore manages semantic memory (codebase patterns) using pgvector.
type SemanticStore struct {
	pool *database.Conn
}

// NewSemanticStore creates a new semantic memory store.
func NewSemanticStore(pool *database.Conn) *SemanticStore {
	return &SemanticStore{pool: pool}
}

// Pattern represents a codebase pattern.
type Pattern struct {
	ID               string          `json:"id"`
	UserID           string          `json:"user_id"`
	ProjectID        string          `json:"project_id"`
	PatternType      string          `json:"pattern_type"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Examples         []string        `json:"examples,omitempty"`
	Confidence       float64         `json:"confidence"`
	ObservationCount int             `json:"observation_count"`
	Embedding        pgvector.Vector `json:"-"`
	FilePatterns     []string        `json:"file_patterns,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// Store saves a new pattern to semantic memory.
func (s *SemanticStore) Store(ctx context.Context, pattern *Pattern) error {
	query := `
		INSERT INTO memory_patterns (user_id, project_id, pattern_type, name, description, examples, confidence, observation_count, embedding, file_patterns, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		RETURNING id, created_at
	`
	return s.pool.QueryRow(ctx, query,
		pattern.UserID, pattern.ProjectID, pattern.PatternType, pattern.Name,
		pattern.Description, pattern.Examples, pattern.Confidence,
		pattern.ObservationCount, pattern.Embedding, pattern.FilePatterns,
	).Scan(&pattern.ID, &pattern.CreatedAt)
}

// Search finds patterns by semantic similarity.
func (s *SemanticStore) Search(ctx context.Context, projectID string, embedding pgvector.Vector, limit int) ([]Pattern, error) {
	query := `
		SELECT id, user_id, project_id, pattern_type, name, description, confidence,
		       observation_count, file_patterns, created_at,
		       1 - (embedding <=> $1) as similarity
		FROM memory_patterns
		WHERE project_id = $2
		ORDER BY embedding <=> $1
		LIMIT $3
	`
	rows, err := s.pool.Query(ctx, query, embedding, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("semantic search failed: %w", err)
	}
	defer rows.Close()

	var results []Pattern
	for rows.Next() {
		var p Pattern
		if err := rows.Scan(&p.ID, &p.UserID, &p.ProjectID, &p.PatternType, &p.Name,
			&p.Description, &p.Confidence, &p.ObservationCount, &p.FilePatterns,
			&p.CreatedAt); err != nil {
			continue
		}
		results = append(results, p)
	}
	return results, nil
}

// ListByProject returns all patterns for a project.
func (s *SemanticStore) ListByProject(ctx context.Context, projectID string, limit int) ([]Pattern, error) {
	query := `
		SELECT id, pattern_type, name, description, confidence, observation_count, created_at
		FROM memory_patterns
		WHERE project_id = $1
		ORDER BY confidence DESC, observation_count DESC
		LIMIT $2
	`
	rows, err := s.pool.Query(ctx, query, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Pattern
	for rows.Next() {
		var p Pattern
		if err := rows.Scan(&p.ID, &p.PatternType, &p.Name, &p.Description,
			&p.Confidence, &p.ObservationCount, &p.CreatedAt); err != nil {
			continue
		}
		results = append(results, p)
	}
	return results, nil
}
