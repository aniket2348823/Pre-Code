package repository

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vigilagent/vigilagent/internal/database"
)

// APIKey represents an API key record in the database.
type APIKey struct {
	ID                string     `json:"id"`
	UserID            string     `json:"user_id"`
	Name              string     `json:"name"`
	KeyHash           string     `json:"-"` // never expose
	Prefix            string     `json:"prefix"`
	Scopes            []string   `json:"scopes,omitempty"`
	IsActive          bool       `json:"is_active"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	RotationTokenHash string     `json:"-"` // HMAC of rotation token, never expose
	RotatedAt         *time.Time `json:"rotated_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
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
		FROM api_keys WHERE key_hash = $1 AND is_active = TRUE AND (expires_at IS NULL OR expires_at > NOW())
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
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to update last used: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("api key not found")
	}
	return nil
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

// RotateAPIKey marks the old key as expiring in 24h and creates a new key with the same metadata.
// Returns the new key record (caller must populate KeyHash before calling Create, or use the returned struct directly).
func (r *APIKeyRepository) RotateAPIKey(ctx context.Context, oldKeyID, userID, newKeyHash, newPrefix, rotationTokenHash string, gracePeriod time.Duration) (*APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Fetch old key metadata
	var name string
	var scopes []string
	err = tx.QueryRow(ctx,
		`SELECT name, scopes FROM api_keys WHERE id = $1 AND user_id = $2`,
		oldKeyID, userID,
	).Scan(&name, &scopes)
	if err != nil {
		return nil, fmt.Errorf("api key not found or access denied")
	}

	// Mark old key: set expiry to now+gracePeriod, store rotation_token_hash, mark rotated_at
	_, err = tx.Exec(ctx,
		`UPDATE api_keys
		 SET expires_at = NOW() + $1::INTERVAL,
		     rotation_token_hash = $2,
		     rotated_at = NOW()
		 WHERE id = $3`,
		fmt.Sprintf("%d seconds", int(gracePeriod.Seconds())),
		rotationTokenHash,
		oldKeyID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to mark old key as expiring: %w", err)
	}

	// Create new key with same name, scopes, user
	newKey := &APIKey{
		UserID:   userID,
		Name:     name,
		KeyHash:  newKeyHash,
		Prefix:   newPrefix,
		Scopes:   scopes,
		IsActive: true,
	}
	err = tx.QueryRow(ctx,
		`INSERT INTO api_keys (user_id, name, key_hash, prefix, scopes, is_active)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at`,
		newKey.UserID, newKey.Name, newKey.KeyHash, newKey.Prefix, newKey.Scopes, newKey.IsActive,
	).Scan(&newKey.ID, &newKey.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create rotated key: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit rotation: %w", err)
	}

	return newKey, nil
}

// FindByRotationToken retrieves an API key by its rotation token hash (for validating old keys during transition).
func (r *APIKeyRepository) FindByRotationToken(ctx context.Context, rotationTokenHash string) (*APIKey, error) {
	query := `
		SELECT id, user_id, name, key_hash, prefix, scopes, is_active, last_used_at, expires_at, created_at
		FROM api_keys WHERE rotation_token_hash = $1 AND is_active = TRUE AND rotated_at IS NOT NULL
	`
	key := &APIKey{}
	err := r.pool.QueryRow(ctx, query, rotationTokenHash).Scan(
		&key.ID, &key.UserID, &key.Name, &key.KeyHash, &key.Prefix,
		&key.Scopes, &key.IsActive, &key.LastUsedAt, &key.ExpiresAt, &key.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("api key not found")
		}
		return nil, fmt.Errorf("failed to find api key by rotation token: %w", err)
	}
	return key, nil
}

// GetExpiredKeys returns all API keys that have expired (expires_at < NOW).
func (r *APIKeyRepository) GetExpiredKeys(ctx context.Context, limit int) ([]APIKey, error) {
	query := `
		SELECT id, user_id, name, prefix, is_active, expires_at, created_at
		FROM api_keys
		WHERE expires_at IS NOT NULL AND expires_at < NOW() AND is_active = TRUE
		ORDER BY expires_at ASC
		LIMIT $1
	`
	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query expired keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.IsActive, &k.ExpiresAt, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan expired key: %w", err)
		}
		keys = append(keys, k)
	}
	if keys == nil {
		keys = []APIKey{}
	}
	return keys, rows.Err()
}

// CleanupExpiredKeys deactivates and deletes expired API keys.
// Returns the number of keys cleaned up.
func (r *APIKeyRepository) CleanupExpiredKeys(ctx context.Context) (int, error) {
	// First deactivate (soft delete) keys past expiry
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET is_active = FALSE
		 WHERE expires_at IS NOT NULL AND expires_at < NOW() AND is_active = TRUE`)
	if err != nil {
		return 0, fmt.Errorf("failed to deactivate expired keys: %w", err)
	}
	deactivated := int(tag.RowsAffected())

	// Then hard-delete keys that have been inactive for more than 7 days past expiry
	tag, err = r.pool.Exec(ctx,
		`DELETE FROM api_keys
		 WHERE expires_at IS NOT NULL AND expires_at < NOW() - INTERVAL '7 days'
		   AND is_active = FALSE`)
	if err != nil {
		return deactivated, fmt.Errorf("failed to delete expired keys: %w", err)
	}
	deleted := int(tag.RowsAffected())

	return deactivated + deleted, nil
}

// UpdateLastUsedBatch updates last_used_at for multiple keys in one query (for batch operations).
func (r *APIKeyRepository) UpdateLastUsedBatch(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = NOW() WHERE id = ANY($1)`, ids)
	if err != nil {
		return fmt.Errorf("failed to batch update last used: %w", err)
	}
	return nil
}

// StartExpiredKeyCleanup runs expired key cleanup on the given interval.
// Returns a stop function to cancel the goroutine.
func (r *APIKeyRepository) StartExpiredKeyCleanup(ctx context.Context, interval time.Duration) context.CancelFunc {
	cleanupCtx, cancel := context.WithCancel(ctx)
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				n, err := r.CleanupExpiredKeys(cleanupCtx)
				if err != nil {
					log.Printf("apikey cleanup error: %v", err)
				} else if n > 0 {
					log.Printf("apikey cleanup: removed %d expired keys", n)
				}
			}
		}
	}()
	return cancel
}
