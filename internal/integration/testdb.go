package integration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDB wraps a real PostgreSQL connection with schema isolation for testing.
type TestDB struct {
	Pool    *pgxpool.Pool
	Schema  string
	cleanup func()
}

// SetupTestDB creates a fresh test database, runs migrations, and returns a
// TestDB with a cleanup function. Skips if INTEGRATION_TEST != "1".
//
// Usage:
//
//	func TestSomething(t *testing.T) {
//	    db := SetupTestDB(t)
//	    defer db.cleanup()
//	    // ... use db.Pool
//	}
func SetupTestDB(t *testing.T) *TestDB {
	t.Helper()

	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("INTEGRATION_TEST not set to \"1\", skipping integration test")
	}

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = defaultTestDSN()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	schemaName := fmt.Sprintf("test_%d", time.Now().UnixNano()%1000000000)

	// Connect to default postgres DB to create/drop test databases.
	adminDSN := switchDatabaseName(dsn, "postgres")
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("failed to connect to admin database: %v", err)
	}

	testDBName := fmt.Sprintf("vigilagent_test_%d", time.Now().UnixNano()%1000000000)

	// Terminate existing connections before dropping.
	adminPool.Exec(ctx, fmt.Sprintf(
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid != pg_backend_pid()",
		testDBName,
	))

	_, err = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", testDBName))
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to drop old test database: %v", err)
	}

	_, err = adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", testDBName))
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to create test database: %v", err)
	}
	adminPool.Close()

	// Connect to the new test database.
	testDSN := switchDatabaseName(dsn, testDBName)
	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("failed to connect to test database %s: %v", testDBName, err)
	}

	// Create isolation schema.
	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaName)); err != nil {
		pool.Close()
		dropTestDB(testDBName, adminDSN)
		t.Fatalf("failed to create test schema: %v", err)
	}

	// Set search_path so queries default to the test schema.
	if _, err := pool.Exec(ctx, fmt.Sprintf("SET search_path TO %s, public", schemaName)); err != nil {
		pool.Close()
		dropTestDB(testDBName, adminDSN)
		t.Fatalf("failed to set search_path: %v", err)
	}

	// Run migrations.
	migrationsDir := filepath.Join(projectRoot(), "migrations")
	if err := runMigrations(ctx, pool, migrationsDir); err != nil {
		pool.Close()
		dropTestDB(testDBName, adminDSN)
		t.Fatalf("failed to run migrations: %v", err)
	}

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaName))
		pool.Close()
		dropTestDB(testDBName, adminDSN)
		slog.Debug("integration test cleanup complete", "schema", schemaName, "db", testDBName)
	}

	return &TestDB{Pool: pool, Schema: schemaName, cleanup: cleanup}
}

// Close calls the cleanup function.
func (db *TestDB) Close() {
	if db.cleanup != nil {
		db.cleanup()
	}
}

// ResetSchema drops all objects in the test schema for test isolation.
func (db *TestDB) ResetSchema(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.Pool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", db.Schema)); err != nil {
		t.Fatalf("failed to reset schema: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", db.Schema)); err != nil {
		t.Fatalf("failed to recreate schema: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, fmt.Sprintf("SET search_path TO %s, public", db.Schema)); err != nil {
		t.Fatalf("failed to set search_path: %v", err)
	}
}

// SeedTestData loads SQL seed files from testdata/ directory.
// Files are executed in lexical order. Use for per-test setup.
func SeedTestData(t *testing.T, db *TestDB, seedFiles ...string) {
	t.Helper()
	if len(seedFiles) == 0 {
		dir := filepath.Join(projectRoot(), "internal", "integration", "testdata")
		matches, err := filepath.Glob(filepath.Join(dir, "*.sql"))
		if err != nil {
			t.Fatalf("failed to glob seed files: %v", err)
		}
		sort.Strings(matches)
		seedFiles = matches
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, file := range seedFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("failed to read seed file %s: %v", file, err)
		}
		trimmed := strings.TrimSpace(string(content))
		if trimmed == "" {
			continue
		}
		if _, err := db.Pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("failed to execute seed file %s: %v", file, err)
		}
		slog.Debug("loaded seed file", "file", filepath.Base(file))
	}
}

// runMigrations executes all .up.sql migration files in order.
func runMigrations(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT PRIMARY KEY,
			applied_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	pattern := filepath.Join(migrationsDir, "*.up.sql")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob migrations: %w", err)
	}
	sort.Strings(matches)

	for _, file := range matches {
		base := filepath.Base(file)
		version, ok := migrationVersion(base)
		if !ok {
			continue
		}

		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if exists {
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("apply migration %d (%s): %w", version, base, err)
		}

		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT (version) DO NOTHING", version); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("record migration %d: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %d: %w", version, err)
		}

		slog.Debug("applied migration", "version", version, "file", base)
	}
	return nil
}

// migrationVersion extracts the numeric version from a migration filename.
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

// switchDatabaseName replaces the database name in a key-value DSN.
func switchDatabaseName(dsn, newDB string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		if idx := strings.LastIndex(dsn, "/"); idx != -1 {
			params := ""
			if qIdx := strings.Index(dsn[idx:], "?"); qIdx != -1 {
				params = dsn[idx+qIdx:]
				return dsn[:idx+1] + newDB + params
			}
			return dsn[:idx+1] + newDB
		}
		return dsn
	}
	fields := strings.Fields(dsn)
	for i, f := range fields {
		if strings.HasPrefix(f, "dbname=") {
			fields[i] = "dbname=" + newDB
			return strings.Join(fields, " ")
		}
	}
	return dsn + " dbname=" + newDB
}

// defaultTestDSN returns a fallback DSN for local PostgreSQL.
func defaultTestDSN() string {
	return "host=localhost port=5432 user=postgres password=postgres dbname=postgres sslmode=disable"
}

// projectRoot returns the project root directory.
func projectRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(wd)))
}

// dropTestDB drops a database via the admin pool.
func dropTestDB(dbName, adminDSN string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		return
	}
	defer pool.Close()

	pool.Exec(ctx, fmt.Sprintf(
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid != pg_backend_pid()",
		dbName,
	))
	pool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
}
