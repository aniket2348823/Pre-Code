package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vigilagent/vigilagent/internal/database"
)

// APIKey represents an API key record in the database.
type APIKey struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	Name        string     `json:"name"`
	KeyHash     string     `json:"-"` // never expose
	Prefix      string     `json:"prefix"`
	Scopes      []string   `json:"scopes,omitempty"`
	IsActive    bool       `json:"is_active"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// APIKeyRepository handles database operations for API keys.
type APIKeyRepository struct {
	pool *database.Conn
}

// NewAPIKeyRepository creates a new API key repository.
func NewAPIKeyRepository(pool *database.Conn) *APIKeyRepository {
	return &APIKeyRepository{pool: pool}
}

// Create inserts a new API key.
func (r *APIKeyRepository) Create(ctx context.Context, key *APIKey) error {
	query := `
		INSERT INTO api_keys (user_id, name, key_hash, prefix, scopes, is_active, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`
	return r.pool.QueryRow(ctx, query,
		key.UserID, key.Name, key.KeyHash, key.Prefix, key.Scopes, key.IsActive, key.ExpiresAt,
	).Scan(&key.ID, &key.CreatedAt)
}

// FindByHash retrieves an API key by its SHA-256 hash (O(1) indexed lookup).
func (r *APIKeyRepository) FindByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	query := `
		SELECT id, user_id, name, key_hash, prefix, scopes, is_active, last_used_at, expires_at, created_at
		FROM api_keys WHERE key_hash = $1 AND is_active = TRUE
	`
	key := &APIKey{}
	err := r.pool.QueryRow(ctx, query, keyHash).Scan(
		&key.ID, &key.UserID, &key.Name, &key.KeyHash, &key.Prefix,
		&key.Scopes, &key.IsActive, &key.LastUsedAt, &key.ExpiresAt, &key.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("api key not found")
		}
		return nil, fmt.Errorf("failed to find api key: %w", err)
	}
	return key, nil
}

// UpdateLastUsed updates the last_used_at timestamp.
func (r *APIKeyRepository) UpdateLastUsed(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, id)
	return err
}

// ListByUser returns all API keys for a user (never returns key_hash).
func (r *APIKeyRepository) ListByUser(ctx context.Context, userID string) ([]APIKey, error) {
	query := `
		SELECT id, user_id, name, prefix, scopes, is_active, last_used_at, expires_at, created_at
		FROM api_keys WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list api keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(
			&k.ID, &k.UserID, &k.Name, &k.Prefix,
			&k.Scopes, &k.IsActive, &k.LastUsedAt, &k.ExpiresAt, &k.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan api key: %w", err)
		}
		keys = append(keys, k)
	}
	if keys == nil {
		keys = []APIKey{}
	}
	return keys, rows.Err()
}

// Delete removes an API key by ID and user ownership.
func (r *APIKeyRepository) Delete(ctx context.Context, id, userID string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM api_keys WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("failed to delete api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("api key not found or access denied")
	}
	return nil
}
