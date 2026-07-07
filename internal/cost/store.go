package cost

import (
	"context"
	"github.com/vigilagent/vigilagent/internal/database"
)

// UsageStore persists cumulative spend so budget enforcement survives restarts.
// Keys are namespaced as "org:<id>" and "task:<id>".
type UsageStore interface {
	// LoadUsage returns all persisted usage counters keyed by namespace key.
	LoadUsage(ctx context.Context) (map[string]float64, error)
	// AddUsage atomically increments the stored amount for a key.
	AddUsage(ctx context.Context, key string, delta float64) error
}

// PostgresUsageStore persists usage in the budget_usage table.
type PostgresUsageStore struct {
	pool *database.Conn
}

// NewPostgresUsageStore creates a Postgres-backed usage store.
func NewPostgresUsageStore(pool *database.Conn) *PostgresUsageStore {
	return &PostgresUsageStore{pool: pool}
}

// LoadUsage reads all usage counters.
func (s *PostgresUsageStore) LoadUsage(ctx context.Context) (map[string]float64, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, amount FROM budget_usage`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	usage := make(map[string]float64)
	for rows.Next() {
		var key string
		var amount float64
		if err := rows.Scan(&key, &amount); err != nil {
			return nil, err
		}
		usage[key] = amount
	}
	return usage, rows.Err()
}

// AddUsage increments a key's amount atomically, inserting it if absent.
func (s *PostgresUsageStore) AddUsage(ctx context.Context, key string, delta float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO budget_usage (key, amount, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE
		SET amount = budget_usage.amount + EXCLUDED.amount,
		    updated_at = now()`,
		key, delta)
	return err
}
