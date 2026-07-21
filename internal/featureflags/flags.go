package featureflags

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vigilagent/vigilagent/internal/database"
)

// Flag represents a feature flag.
type Flag struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Enabled     bool                   `json:"enabled"`
	Rules       map[string]interface{} `json:"rules,omitempty"` // targeting rules
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// Manager manages feature flags backed by PostgreSQL with in-memory caching.
type Manager struct {
	pool   *database.Conn
	cache  map[string]*Flag
	mu     sync.RWMutex
	ttl    time.Duration
	lastFetch time.Time
}

// NewManager creates a new feature flag manager.
func NewManager(pool *database.Conn) *Manager {
	return &Manager{
		pool:  pool,
		cache: make(map[string]*Flag),
		ttl:   5 * time.Minute,
	}
}

// IsEnabled checks if a feature flag is enabled.
// Falls back to false if the flag doesn't exist or DB is unavailable.
func (m *Manager) IsEnabled(ctx context.Context, name string) bool {
	flag, err := m.Get(ctx, name)
	if err != nil || flag == nil {
		return false
	}
	return flag.Enabled
}

// Get retrieves a feature flag by name.
func (m *Manager) Get(ctx context.Context, name string) (*Flag, error) {
	// Check cache first
	m.mu.RLock()
	if flag, ok := m.cache[name]; ok && time.Since(m.lastFetch) < m.ttl {
		m.mu.RUnlock()
		return flag, nil
	}
	m.mu.RUnlock()

	// Fetch from DB
	query := `
		SELECT name, description, enabled, rules, created_at, updated_at
		FROM feature_flags WHERE name = $1
	`
	flag := &Flag{}
	var rulesJSON []byte
	err := m.pool.QueryRow(ctx, query, name).Scan(
		&flag.Name, &flag.Description, &flag.Enabled,
		&rulesJSON, &flag.CreatedAt, &flag.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if rulesJSON != nil {
		_ = json.Unmarshal(rulesJSON, &flag.Rules)
	}

	// Update cache
	m.mu.Lock()
	m.cache[name] = flag
	m.mu.Unlock()

	return flag, nil
}

// GetAll returns all feature flags.
func (m *Manager) GetAll(ctx context.Context) ([]Flag, error) {
	query := `
		SELECT name, description, enabled, rules, created_at, updated_at
		FROM feature_flags ORDER BY name
	`
	rows, err := m.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flags []Flag
	for rows.Next() {
		var f Flag
		var rulesJSON []byte
		if err := rows.Scan(
			&f.Name, &f.Description, &f.Enabled,
			&rulesJSON, &f.CreatedAt, &f.UpdatedAt,
		); err != nil {
			continue
		}
		if rulesJSON != nil {
			_ = json.Unmarshal(rulesJSON, &f.Rules)
		}
		flags = append(flags, f)
	}
	return flags, rows.Err()
}

// Set creates or updates a feature flag.
func (m *Manager) Set(ctx context.Context, flag *Flag) error {
	rulesJSON, _ := json.Marshal(flag.Rules)
	query := `
		INSERT INTO feature_flags (name, description, enabled, rules, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (name) DO UPDATE SET
			description = EXCLUDED.description,
			enabled = EXCLUDED.enabled,
			rules = EXCLUDED.rules,
			updated_at = NOW()
	`
	_, err := m.pool.Exec(ctx, query, flag.Name, flag.Description, flag.Enabled, rulesJSON)
	if err != nil {
		return err
	}

	// Invalidate cache
	m.mu.Lock()
	delete(m.cache, flag.Name)
	m.mu.Unlock()

	slog.Info("feature flag updated", "name", flag.Name, "enabled", flag.Enabled)
	return nil
}

// Delete removes a feature flag.
func (m *Manager) Delete(ctx context.Context, name string) error {
	_, err := m.pool.Exec(ctx, `DELETE FROM feature_flags WHERE name = $1`, name)
	if err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.cache, name)
	m.mu.Unlock()

	return nil
}

// EnsureTable creates the feature_flags table if it doesn't exist.
func EnsureTable(ctx context.Context, pool *database.Conn) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS feature_flags (
			name VARCHAR(100) PRIMARY KEY,
			description TEXT,
			enabled BOOLEAN DEFAULT false,
			rules JSONB DEFAULT '{}',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	return err
}

// StartRefresh starts a background goroutine that refreshes the cache periodically.
func (m *Manager) StartRefresh(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				flags, err := m.GetAll(ctx)
				if err != nil {
					slog.Warn("feature flag refresh failed", "error", err)
					continue
				}
				m.mu.Lock()
				for i := range flags {
					m.cache[flags[i].Name] = &flags[i]
				}
				m.lastFetch = time.Now()
				m.mu.Unlock()
			}
		}
	}()
}
