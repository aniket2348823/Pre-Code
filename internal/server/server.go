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
	"github.com/vigilagent/vigilagent/internal/costintel"
	"github.com/vigilagent/vigilagent/internal/database"
	"github.com/vigilagent/vigilagent/internal/llm"
	mw "github.com/vigilagent/vigilagent/internal/middleware"
	"github.com/vigilagent/vigilagent/internal/memory"
	"github.com/vigilagent/vigilagent/internal/queue"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/router"
	"github.com/vigilagent/vigilagent/internal/attackgraph"
	"github.com/vigilagent/vigilagent/internal/audit"
	"github.com/vigilagent/vigilagent/internal/compliance"
	"github.com/vigilagent/vigilagent/internal/confidence"
	"github.com/vigilagent/vigilagent/internal/email"
	"github.com/vigilagent/vigilagent/internal/featureflags"
	"github.com/vigilagent/vigilagent/internal/knowledge"
	"github.com/vigilagent/vigilagent/internal/requirements"
	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/internal/schema"
	"github.com/vigilagent/vigilagent/internal/skillengine"
	"github.com/vigilagent/vigilagent/internal/skills"
	"github.com/vigilagent/vigilagent/internal/cors"
	"github.com/vigilagent/vigilagent/internal/telemetry"
	"github.com/vigilagent/vigilagent/internal/tools"
)

type Server struct {
	cfg       *config.Config
	router    *router.Router
	db        *database.Postgres
	redis     *database.Redis
	nats      *queue.NATS
	cleanup   func()
	hotReload *config.HotReloader
}

func New(cfg *config.Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	srv := &Server{cfg: cfg}

	// Connect to PostgreSQL with retry (services may not be ready yet in Docker)
	var err error
	var db *database.Postgres
	for attempt := 1; attempt <= 10; attempt++ {
		db, err = database.NewPostgres(context.Background(), &cfg.Database)
		if err == nil {
			break
		}
		slog.Warn("database connection failed, retrying...", "attempt", attempt, "max", 10, "error", err)
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("database initialization failed after 10 attempts: %w", err)
	}
	srv.db = db

	if err := srv.runMigrations(); err != nil {
		db.Close()
		return nil, fmt.Errorf("database migration failed: %w", err)
	}

	// Connect to Redis with retry
	var rds *database.Redis
	for attempt := 1; attempt <= 5; attempt++ {
		rds, err = database.NewRedis(context.Background(), &cfg.Redis)
		if err == nil {
			break
		}
		slog.Warn("redis connection failed, retrying...", "attempt", attempt, "max", 5, "error", err)
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("redis initialization failed after 5 attempts: %w", err)
	}
	srv.redis = rds

	// Connect to NATS with retry
	var natsConn *queue.NATS
	for attempt := 1; attempt <= 5; attempt++ {
		natsConn, err = queue.NewNATS(&cfg.NATS)
		if err == nil {
			break
		}
		slog.Warn("nats connection failed, retrying...", "attempt", attempt, "max", 5, "error", err)
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	if err != nil {
		db.Close()
		rds.Close()
		return nil, fmt.Errorf("nats initialization failed after 5 attempts: %w", err)
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

	conn := db.Conn()
	userRepo := repository.NewUserRepository(conn)
	orgRepo := repository.NewOrganizationRepository(conn)
	projectRepo := repository.NewProjectRepository(conn)
	agentRepo := repository.NewAgentRepository(conn)
	sessionRepo := repository.NewSessionRepository(conn)
	eventRepo := repository.NewEventRepository(conn)
	apiKeyRepo := repository.NewAPIKeyRepository(conn)
	taskRepo := repository.NewTaskRepository(conn)
	skillRepo := repository.NewSkillRepository(conn)
	alertRepo := repository.NewAlertRepository(conn)

	apiKeyAuth := mw.NewAPIKeyAuth(conn)
	rl := mw.NewRateLimiter(rds.Client, 100, time.Minute)
	authRL := mw.NewRateLimiter(rds.Client, 10, time.Minute)

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
	if cfg.LLM.GeminiKey != "" {
		geminiProvider, err := llm.NewGemini(cfg.LLM.GeminiKey)
		if err != nil {
			slog.Warn("failed to create Gemini provider", "error", err)
		} else {
			modelRouter.RegisterProvider("gemini", geminiProvider)
			slog.Info("registered Gemini provider")
		}
	}
	if cfg.LLM.OpenRouterKey != "" {
		openrouterProvider := llm.NewOpenRouter(cfg.LLM.OpenRouterKey)
		modelRouter.RegisterProvider("openrouter", openrouterProvider)
		slog.Info("registered OpenRouter provider")
	}
	if cfg.LLM.MistralKey != "" {
		mistralProvider := llm.NewMistral(cfg.LLM.MistralKey)
		modelRouter.RegisterProvider("mistral", mistralProvider)
		slog.Info("registered Mistral provider")
	}
	if cfg.LLM.GroqKey != "" {
		groqProvider := llm.NewGroq(cfg.LLM.GroqKey)
		modelRouter.RegisterProvider("groq", groqProvider)
		slog.Info("registered Groq provider")
	}
	if cfg.LLM.NVIDIANIMKey != "" {
		nimProvider := llm.NewNVIDIANIM(cfg.LLM.NVIDIANIMKey)
		modelRouter.RegisterProvider("nvidia_nim", nimProvider)
		slog.Info("registered NVIDIA NIM provider")
	}
	if cfg.LLM.CohereKey != "" {
		cohereProvider := llm.NewCohere(cfg.LLM.CohereKey)
		modelRouter.RegisterProvider("cohere", cohereProvider)
		slog.Info("registered Cohere provider")
	}

	budgetMgr := cost.NewBudgetManager(conn, 0, cfg.LLM.BudgetPerTask)
	webhookEngine := webhook.NewEngine(db.Pool)
	budgetMgr.OnExceeded(func(ctx context.Context, err *cost.BudgetExceededError) {
		webhookEngine.Dispatch(ctx, webhook.Event{
			Type: "budget.exceeded",
			Payload: map[string]interface{}{
				"type":     err.Type,
				"id":       err.ID,
				"usage":    err.Usage,
				"budget":   err.Budget,
				"proposed": err.Proposed,
			},
		})
	})
	modelRouter.SetBudgetGuard(budgetMgr)
	modelRouter.SetCache(llm.NewInMemoryCache(time.Hour))
	slog.Info("router budget enforcement + response cache enabled", "budget_per_task", cfg.LLM.BudgetPerTask)

	toolRegistry := tools.NewToolRegistry()
	toolRegistry.Register(&tools.ReadFileTool{})
	toolRegistry.Register(&tools.WriteFileTool{})
	toolRegistry.Register(&tools.EditFileTool{})
	toolRegistry.Register(&tools.ListDirectoryTool{})
	toolRegistry.Register(&tools.RunCommandTool{})
	toolRegistry.Register(&tools.SearchCodeTool{})

	// Initialize agent engine
	agentExec := agent.NewAgent(modelRouter, toolRegistry)

	// Initialize memory manager with optional OpenAI embeddings
	var memMgr *memory.Manager
	if cfg.LLM.OpenAIKey != "" {
		memMgr = memory.NewManagerWithEmbedder(conn, memory.NewOpenAIEmbedder(cfg.LLM.OpenAIKey))
		slog.Info("memory manager using OpenAI embeddings")
	} else {
		memMgr = memory.NewManager(conn)
		slog.Warn("no OpenAI key; memory recall running without embeddings (zero vectors)")
	}

	// Wire memory system into agent for episodic recall during task execution
	agentExec.SetMemory(&memoryAdapter{mgr: memMgr})
	slog.Info("agent memory wired", "layers", "working+episodic+semantic")

	go modelRouter.StartHealthChecks(context.Background(), 2*time.Minute)

	// Wire email service
	var emailSender email.Sender
	if cfg.SMTP.Host != "" {
		emailSender = email.NewSMTPSender(email.SMTPConfig{
			Host:     cfg.SMTP.Host,
			Port:     cfg.SMTP.Port,
			Username: cfg.SMTP.Username,
			Password: cfg.SMTP.Password,
			From:     cfg.SMTP.From,
			FromName: cfg.SMTP.FromName,
		})
		slog.Info("email sender configured", "host", cfg.SMTP.Host)
	} else {
		emailSender = &email.NoOpSender{}
		slog.Info("email sender: no-op (SMTP not configured)")
	}
	// Use Redis-backed token store for email verification tokens (survives restarts)
	var verificationSvc *email.VerificationService
	if rds != nil && rds.Client != nil {
		redisTokenStore := email.NewRedisTokenStore(rds.Client, 24*time.Hour)
		verificationSvc = email.NewVerificationServiceWithRedis(emailSender, redisTokenStore)
		slog.Info("email verification service: Redis-backed token store")
	} else {
		verificationSvc = email.NewVerificationService(emailSender)
		slog.Warn("email verification service: in-memory token store (tokens lost on restart)")
	}
	go verificationSvc.Cleanup(context.Background(), 1*time.Hour)

	// Wire feature flags
	featureFlagMgr := featureflags.NewManager(conn)
	featureFlagMgr.StartRefresh(context.Background(), 5*time.Minute)
	_ = featureflags.EnsureTable(context.Background(), conn)

	// Wire RAG engine for skill marketplace search
	skillRAG := skills.NewRAGEngine(conn, memory.NewNoOpEmbedder(1536))
	_ = skills.EnsureRequiredTables(context.Background(), conn)

	opts := router.Options{
		Config:     cfg,
		DB:         db,
		Redis:      rds,
		NATS:       natsConn,
		JWT:        jwtSvc,
		APIKeys:    apiKeySvc,
		APIAuth:    apiKeyAuth,
		RateLimit:     rl,
		AuthRateLimit: authRL,
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
		Engine:       scanner.NewEngine(scanner.NewBuiltinAnalyzer()),
		Requirements: requirements.NewResolver(),
		Validator:    schema.NewValidator(),
		Compliance:   compliance.NewChecker(),
		Knowledge:    knowledge.NewGraph(),
		SkillEngine:  skillengine.NewEngine(),
		Confidence:   confidence.NewEngine(),
		AttackGraph:  attackgraph.NewEngine(),
		Audit:        audit.NewEngine(audit.NewMemoryStore()),
		Budget:       budgetMgr,
		Webhook:      webhookEngine,
		CostIntel:    costintel.NewEngine(),
		Email:        verificationSvc,
		FeatureFlags: featureFlagMgr,
		SkillRAG:     skillRAG,
	}

	var r *router.Router
	if len(cfg.CORS.AllowedOrigins) > 0 {
		r = router.NewWithMiddleware(opts, &router.MiddlewareConfig{
			RequestID: true,
			Timeout:   30 * time.Second,
			CORS: &cors.Config{
				AllowOrigins: cfg.CORS.AllowedOrigins,
				AllowMethods: cfg.CORS.AllowedMethods,
				AllowHeaders: cfg.CORS.AllowedHeaders,
				MaxAge:       3600,
			},
		})
	} else {
		r = router.New(opts)
	}
	srv.router = r

	// Start config hot reload watcher
	hotReload := config.NewHotReloader(cfg)
	hotReload.OnChange(func(newCfg *config.Config) {
		slog.Info("hot reload: updating model router config")
		modelRouter.SetPrices(llm.AllPrices())
		if newCfg.LLM.DefaultModel != "" {
			slog.Info("hot reload: new default model", "model", newCfg.LLM.DefaultModel)
		}
	})
	go hotReload.Start(context.Background())
	srv.hotReload = hotReload

	// NOTE: pg_trgm extension should be created via migrations or manually.
	// CREATE EXTENSION requires superuser and hangs through connection poolers.

	slog.Info("server initialized successfully")
	return srv, nil
}

func (s *Server) Router() *router.Router {
	return s.router
}

var version = "dev"

func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("cleaning up server resources")
	if s.router != nil {
		s.router.Shutdown()
	}
	if s.hotReload != nil {
		s.hotReload.Stop()
	}
	if s.cleanup != nil {
		s.cleanup()
	}
	if s.nats != nil {
		// Drain in-flight messages before closing
		drainCtx, drainCancel := context.WithTimeout(ctx, 10*time.Second)
		defer drainCancel()
		if err := s.nats.Drain(drainCtx); err != nil {
			slog.Warn("NATS drain failed, forcing close", "error", err)
			s.nats.Close()
		}
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

// memoryAdapter bridges memory.Manager to agent.MemoryStore.
type memoryAdapter struct {
	mgr *memory.Manager
}

func (a *memoryAdapter) Recall(ctx context.Context, query string, limit int) ([]agent.MemoryResult, error) {
	results, err := a.mgr.Recall(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]agent.MemoryResult, len(results))
	for i, r := range results {
		out[i] = agent.MemoryResult{
			Type:    r.Type,
			Content: r.Content,
			Score:   r.Score,
		}
	}
	return out, nil
}

func (a *memoryAdapter) StoreEpisode(ctx context.Context, userID, episodeType, title, content string, importance float64) error {
	return a.mgr.StoreEpisode(ctx, userID, episodeType, title, content, importance)
}

