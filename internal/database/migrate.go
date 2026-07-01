package database

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrate runs all pending SQL migration files against the database.
// Migration files are expected in the migrations/ directory with the naming
// convention NNNNNN_description.up.sql. Files are applied in lexical order.
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	slog.Info("running database migrations", "dir", migrationsDir)

	// Collect .up.sql migration files from the migrations directory
	pattern := filepath.Join(migrationsDir, "*.up.sql")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob migration files: %w", err)
	}

	if len(matches) == 0 {
		slog.Info("no migration files found")
		return nil
	}

	sort.Strings(matches)

	// Ensure the schema_migrations table exists
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT PRIMARY KEY,
			applied_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	for _, file := range matches {
		// Extract version number from filename (e.g., 000001_init.up.sql -> 1)
		base := filepath.Base(file)
		version, ok := migrationVersion(base)
		if !ok {
			continue
		}

		// Check if already applied
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists); err != nil {
			return fmt.Errorf("failed to check migration %d: %w", version, err)
		}
		if exists {
			slog.Debug("migration already applied, skipping", "version", version, "file", base)
			continue
		}

		// Read the migration file
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		// Execute the migration in a transaction
		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %d: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("failed to apply migration %d (%s): %w", version, base, err)
		}

		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("failed to record migration %d: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration %d: %w", version, err)
		}

		slog.Info("applied migration", "version", version, "file", base)
	}

	slog.Info("database migrations complete", "applied", len(matches))
	return nil
}

// migrationVersion extracts the leading numeric version from a migration file
// base name such as "000001_init_schema.up.sql" -> (1, true). It returns
// (0, false) when the name does not start with an underscore-delimited number.
func migrationVersion(base string) (int, bool) {
	parts := strings.SplitN(base, "_", 2)
	if len(parts) < 2 {
		return 0, false
	}
	var version int
	if _, err := fmt.Sscanf(parts[0], "%d", &version); err != nil {
		return 0, false
	}
	return version, true
}

// CurrentVersion returns the highest applied migration version, or 0 when no
// migrations have been applied yet (or the tracking table does not exist).
func CurrentVersion(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var version int
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(version), 0) FROM schema_migrations
	`).Scan(&version)
	if err != nil {
		// Missing table means nothing has been applied yet.
		if strings.Contains(err.Error(), "does not exist") {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read current migration version: %w", err)
	}
	return version, nil
}
