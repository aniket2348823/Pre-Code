package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/vigilagent/vigilagent/internal/agent"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/config"
	"github.com/vigilagent/vigilagent/internal/cost"
	"github.com/vigilagent/vigilagent/internal/database"
	"github.com/vigilagent/vigilagent/internal/llm"
	mw "github.com/vigilagent/vigilagent/internal/middleware"
	"github.com/vigilagent/vigilagent/internal/memory"
	"github.com/vigilagent/vigilagent/internal/queue"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/router"
	"github.com/vigilagent/vigilagent/internal/telemetry"
	"github.com/vigilagent/vigilagent/internal/tools"
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
	// Validate config before proceeding
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	srv := &Server{cfg: cfg}

	db, err := database.NewPostgres(context.Background(), &cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("database initialization failed: %w", err)
	}
	srv.db = db

	if err := srv.runMigrations(); err != nil {
		db.Close()
		return nil, fmt.Errorf("database migration failed: %w", err)
	}

	rds, err := database.NewRedis(context.Background(), &cfg.Redis)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("redis initialization failed: %w", err)
	}
	srv.redis = rds

	natsConn, err := queue.NewNATS(&cfg.NATS)
	if err != nil {
		db.Close()
		rds.Close()
		return nil, fmt.Errorf("nats initialization failed: %w", err)
	}
	srv.nats = natsConn

	jwtSvc := auth.NewJWT(&cfg.Auth)
	apiKeySvc := auth.NewAPIKeyService(cfg.Auth.APIKeyPrefix)

	cleanup, err := telemetry.Setup(context.Background(), "vigilagent", version)
	if err != nil {
		slog.Warn("opentelemetry setup failed, continuing without tracing", "error", err)
	} else {
		srv.cleanup = cleanup
	}

	userRepo := repository.NewUserRepository(db.Pool)
	orgRepo := repository.NewOrganizationRepository(db.Pool)
	projectRepo := repository.NewProjectRepository(db.Pool)
	agentRepo := repository.NewAgentRepository(db.Pool)
	sessionRepo := repository.NewSessionRepository(db.Pool)
	eventRepo := repository.NewEventRepository(db.Pool)
	apiKeyRepo := repository.NewAPIKeyRepository(db.Pool)
	taskRepo := repository.NewTaskRepository(db.Pool)
	skillRepo := repository.NewSkillRepository(db.Pool)
	alertRepo := repository.NewAlertRepository(db.Pool)

	apiKeyAuth := mw.NewAPIKeyAuth(db.Pool)
	rl := mw.NewRateLimiter(rds.Client, 100, time.Minute)

	// Initialize LLM providers based on config
	modelRouter := llm.NewModelRouter(&llm.RouterConfig{
		DefaultModel:  cfg.LLM.DefaultModel,
		BudgetPerTask: cfg.LLM.BudgetPerTask,
	})

	if cfg.LLM.OpenAIKey != "" {
		oaiProvider := llm.NewOpenAI(cfg.LLM.OpenAIKey)
		modelRouter.RegisterProvider("openai", oaiProvider)
		slog.Info("registered OpenAI provider")
	}
	if cfg.LLM.AnthropicKey != "" {
		anthropicProvider := llm.NewAnthropic(cfg.LLM.AnthropicKey)
		modelRouter.RegisterProvider("anthropic", anthropicProvider)
		slog.Info("registered Anthropic provider")
	}

	// Enforce a hard per-task budget (risk R31) with restart-safe persistence,
	// and cache identical responses to cut spend (docs 06/07 core cost levers).
	budgetMgr := cost.NewBudgetManager(db.Pool, 0, cfg.LLM.BudgetPerTask)
	modelRouter.SetBudgetGuard(budgetMgr)
	modelRouter.SetCache(llm.NewInMemoryCache(time.Hour))
	slog.Info("router budget enforcement + response cache enabled", "budget_per_task", cfg.LLM.BudgetPerTask)

	// Initialize tool registry with all built-in tools
	toolRegistry := tools.NewToolRegistry()
	toolRegistry.Register(&tools.ReadFileTool{})
	toolRegistry.Register(&tools.WriteFileTool{})
	toolRegistry.Register(&tools.EditFileTool{})
	toolRegistry.Register(&tools.ListDirectoryTool{})
	toolRegistry.Register(&tools.RunCommandTool{})
	toolRegistry.Register(&tools.SearchCodeTool{})

	// Initialize agent engine
	agentExec := agent.NewAgent(modelRouter, toolRegistry)

	// Initialize memory manager. When an OpenAI key is configured, use real
	// embeddings so semantic recall works; otherwise fall back to zero vectors.
	var memMgr *memory.Manager
	if cfg.LLM.OpenAIKey != "" {
		memMgr = memory.NewManagerWithEmbedder(db.Pool, memory.NewOpenAIEmbedder(cfg.LLM.OpenAIKey))
		slog.Info("memory manager using OpenAI embeddings")
	} else {
		memMgr = memory.NewManager(db.Pool)
		slog.Warn("no OpenAI key; memory recall running without embeddings (zero vectors)")
	}

	// Start periodic health checks for LLM providers
	go modelRouter.StartHealthChecks(context.Background(), 2*time.Minute)

	r := router.New(router.Options{
		Config:     cfg,
		DB:         db,
		Redis:      rds,
		NATS:       natsConn,
		JWT:        jwtSvc,
		APIKeys:    apiKeySvc,
		APIAuth:    apiKeyAuth,
		RateLimit:  rl,
		Users:      userRepo,
		Orgs:       orgRepo,
		Projects:   projectRepo,
		Agents:     agentRepo,
		Sessions:   sessionRepo,
		Events:     eventRepo,
		APIKeyRepo: apiKeyRepo,
		Tasks:      taskRepo,
		Skills:     skillRepo,
		Alerts:     alertRepo,
		AgentExec:  agentExec,
		LLMRouter:  modelRouter,
		Memory:     memMgr,
	})
	srv.router = r

	slog.Info("server initialized successfully")
	return srv, nil
}

func (s *Server) Router() *router.Router {
	return s.router
}// version is set via ldflags at build time.
var version = "dev"

func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("cleaning up server resources")
	if s.cleanup != nil {
		s.cleanup()
	}
	if s.nats != nil {
		s.nats.Close()
	}
	if s.redis != nil {
		s.redis.Close()
	}
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
