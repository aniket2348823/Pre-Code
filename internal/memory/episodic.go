package memory

import (
	"context"
	"fmt"
	"time"
	"github.com/pgvector/pgvector-go"
	"github.com/vigilagent/vigilagent/internal/database"
)

// EpisodicStore manages episodic memory in PostgreSQL.
type EpisodicStore struct {
	pool *database.Conn
}

// NewEpisodicStore creates a new episodic memory store.
func NewEpisodicStore(pool *database.Conn) *EpisodicStore {
	return &EpisodicStore{pool: pool}
}

// EpisodicMemory represents a past interaction or decision.
type EpisodicMemory struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	ProjectID  string    `json:"project_id,omitempty"`
	EpisodeType string  `json:"episode_type"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	Summary    string    `json:"summary,omitempty"`
	TaskID     string    `json:"task_id,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	Importance float64   `json:"importance"`
	AccessCount int      `json:"access_count"`
	Tags       []string  `json:"tags,omitempty"`
	Embedding  pgvector.Vector `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Store saves a new episodic memory.
func (s *EpisodicStore) Store(ctx context.Context, mem *EpisodicMemory) error {
	query := `
		INSERT INTO memory_episodes (user_id, project_id, episode_type, title, content, summary, task_id, session_id, importance, tags, embedding, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
		RETURNING id, created_at
	`
	return s.pool.QueryRow(ctx, query,
		mem.UserID, mem.ProjectID, mem.EpisodeType, mem.Title, mem.Content,
		mem.Summary, mem.TaskID, mem.SessionID, mem.Importance, mem.Tags, mem.Embedding,
	).Scan(&mem.ID, &mem.CreatedAt)
}

// Search finds episodic memories by semantic similarity.
func (s *EpisodicStore) Search(ctx context.Context, userID string, embedding pgvector.Vector, limit int) ([]EpisodicMemory, error) {
	query := `
		SELECT id, user_id, project_id, episode_type, title, content, summary,
		       importance, access_count, tags, created_at,
		       1 - (embedding <=> $1) as similarity
		FROM memory_episodes
		WHERE user_id = $2 AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY embedding <=> $1
		LIMIT $3
	`
	rows, err := s.pool.Query(ctx, query, embedding, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("episodic search failed: %w", err)
	}
	defer rows.Close()

	var results []EpisodicMemory
	for rows.Next() {
		var mem EpisodicMemory
		if err := rows.Scan(&mem.ID, &mem.UserID, &mem.ProjectID, &mem.EpisodeType,
			&mem.Title, &mem.Content, &mem.Summary, &mem.Importance,
			&mem.AccessCount, &mem.Tags, &mem.CreatedAt); err != nil {
			continue
		}
		results = append(results, mem)
	}
	return results, nil
}

// ListByUser returns all episodic memories for a user.
func (s *EpisodicStore) ListByUser(ctx context.Context, userID string, limit int) ([]EpisodicMemory, error) {
	query := `
		SELECT id, user_id, episode_type, title, content, importance, access_count, created_at
		FROM memory_episodes
		WHERE user_id = $1 AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY importance DESC, created_at DESC
		LIMIT $2
	`
	rows, err := s.pool.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []EpisodicMemory
	for rows.Next() {
		var mem EpisodicMemory
		if err := rows.Scan(&mem.ID, &mem.UserID, &mem.EpisodeType, &mem.Title,
			&mem.Content, &mem.Importance, &mem.AccessCount, &mem.CreatedAt); err != nil {
			continue
		}
		results = append(results, mem)
	}
	return results, nil
}
