package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/config"
	"github.com/vigilagent/vigilagent/internal/database"
	"github.com/vigilagent/vigilagent/internal/queue"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/router"
	"github.com/vigilagent/vigilagent/internal/telemetry"
)

type Server struct {
	cfg     *config.Config
	router  *router.Router
	db      *database.Postgres
	redis   *database.Redis
	nats    *queue.NATS
	cleanup func()
}

func New(cfg *config.Config) (*Server, error) {
	srv := &Server{cfg: cfg}

	// Initialize database connection
	db, err := database.NewPostgres(context.Background(), &cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("database initialization failed: %w", err)
	}
	srv.db = db

	// Run database migrations
	if err := srv.runMigrations(); err != nil {
		db.Close()
		return nil, fmt.Errorf("database migration failed: %w", err)
	}

	// Initialize Redis connection
	rds, err := database.NewRedis(context.Background(), &cfg.Redis)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("redis initialization failed: %w", err)
	}
	srv.redis = rds

	// Initialize NATS connection
	natsConn, err := queue.NewNATS(&cfg.NATS)
	if err != nil {
		db.Close()
		rds.Close()
		return nil, fmt.Errorf("nats initialization failed: %w", err)
	}
	srv.nats = natsConn

	// Initialize auth services
	jwtSvc := auth.NewJWT(&cfg.Auth)
	apiKeySvc := auth.NewAPIKeyService(cfg.Auth.APIKeyPrefix)

	// Initialize OpenTelemetry
	cleanup, err := telemetry.Setup(context.Background(), "vigilagent", "0.1.0")
	if err != nil {
		slog.Warn("opentelemetry setup failed, continuing without tracing", "error", err)
	} else {
		srv.cleanup = cleanup
	}

	// Initialize repositories
	userRepository := repository.NewUserRepository(db.Pool)
	orgRepository := repository.NewOrganizationRepository(db.Pool)
	projectRepository := repository.NewProjectRepository(db.Pool)
	agentRepository := repository.NewAgentRepository(db.Pool)
	sessionRepository := repository.NewSessionRepository(db.Pool)
	eventRepository := repository.NewEventRepository(db.Pool)

	// Initialize router with dependencies
	r := router.New(cfg, db, rds, natsConn, jwtSvc, apiKeySvc, rds.Client, userRepository, orgRepository, projectRepository, agentRepository, sessionRepository, eventRepository)
	srv.router = r

	slog.Info("server initialized successfully")
	return srv, nil
}

func (s *Server) Router() *router.Router {
	return s.router
}

func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("cleaning up server resources")

	// Run telemetry cleanup
	if s.cleanup != nil {
		s.cleanup()
	}

	// Close NATS connection
	if s.nats != nil {
		s.nats.Close()
	}

	// Close Redis connection
	if s.redis != nil {
		s.redis.Close()
	}

	// Close database connection pool
	if s.db != nil {
		s.db.Close()
	}

	slog.Info("server resources cleaned up")
	return nil
}

func (s *Server) runMigrations() error {
	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}

	return database.Migrate(context.Background(), s.db.Pool, migrationsDir)
}
