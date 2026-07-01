// Command vigil-migrate applies (or reports) VigilAgent database migrations.
//
// Usage:
//
//	vigil-migrate up        # apply all pending .up.sql migrations (default)
//	vigil-migrate version   # print the highest applied migration version
//
// The database connection is read from the same configuration/env vars as the
// API server (VIGILAGENT_DATABASE_*). Migrations live in ./migrations by
// default, or wherever MIGRATIONS_DIR points.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/vigilagent/vigilagent/internal/config"
	"github.com/vigilagent/vigilagent/internal/database"
)

func main() {
	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pg, err := database.NewPostgres(ctx, &cfg.Database)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pg.Close()

	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}

	switch command {
	case "up":
		if err := database.Migrate(ctx, pg.Pool, migrationsDir); err != nil {
			slog.Error("migration failed", "error", err)
			os.Exit(1)
		}
		slog.Info("migrations applied successfully")
	case "version":
		version, err := database.CurrentVersion(ctx, pg.Pool)
		if err != nil {
			slog.Error("failed to read migration version", "error", err)
			os.Exit(1)
		}
		fmt.Printf("current migration version: %d\n", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (expected: up, version)\n", command)
		os.Exit(2)
	}
}
