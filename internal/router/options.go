package router

import (
	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/agent"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/config"
	"github.com/vigilagent/vigilagent/internal/cost"
	"github.com/vigilagent/vigilagent/internal/database"
	"github.com/vigilagent/vigilagent/internal/llm"
	ratelimit "github.com/vigilagent/vigilagent/internal/middleware"
	"github.com/vigilagent/vigilagent/internal/memory"
	"github.com/vigilagent/vigilagent/internal/queue"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/scanner"
)

// Options holds all dependencies for the Router.
// Instead of 20 positional parameters, this struct groups related dependencies.
type Options struct {
	// Config
	Config *config.Config

	// Infrastructure
	DB      *database.Postgres
	Redis   *database.Redis
	NATS    *queue.NATS
	RedisDS interface{} // *redis.Client for rate limiter

	// Auth
	JWT       *auth.JWT
	APIKeys   *auth.APIKeyService
	APIAuth   *ratelimit.APIKeyAuth
	RateLimit *ratelimit.RateLimiter

	// Repositories
	Users      *repository.UserRepository
	Orgs       *repository.OrganizationRepository
	Projects   *repository.ProjectRepository
	Agents     *repository.AgentRepository
	Sessions   *repository.SessionRepository
	Events     *repository.EventRepository
	APIKeyRepo *repository.APIKeyRepository
	Tasks      *repository.TaskRepository
	Skills     *repository.SkillRepository
	Alerts     *repository.AlertRepository

	// Engine
	AgentExec *agent.Agent
	LLMRouter *llm.ModelRouter
	Memory    *memory.Manager
	Budget    *cost.BudgetManager
	Worker    *queue.TaskWorker

	// Deterministic engine (static analysis)
	Engine *scanner.Engine
}

// Router holds all HTTP handlers and dependencies.
type Router struct {
	*chi.Mux
	cfg        *config.Config
	db         *database.Postgres
	rds        *database.Redis
	nats       *queue.NATS
	auth       *auth.JWT
	apiKM      *auth.APIKeyService
	apiKeyAuth *ratelimit.APIKeyAuth
	rl         *ratelimit.RateLimiter

	// Repositories
	users    *repository.UserRepository
	orgs     *repository.OrganizationRepository
	projects *repository.ProjectRepository
	agents   *repository.AgentRepository
	sessions *repository.SessionRepository
	events   *repository.EventRepository
	apiKeys  *repository.APIKeyRepository
	tasks    *repository.TaskRepository
	skills   *repository.SkillRepository
	alerts   *repository.AlertRepository

	// Engine
	agentExec *agent.Agent
	llmRouter *llm.ModelRouter
	memory    *memory.Manager
	budget    *cost.BudgetManager
	worker    *queue.TaskWorker
	engine    *scanner.Engine
}

// New creates a Router from an Options struct.
func New(opts Options) *Router {
	r := &Router{
		Mux:        chi.NewMux(),
		cfg:        opts.Config,
		db:         opts.DB,
		rds:        opts.Redis,
		nats:       opts.NATS,
		auth:       opts.JWT,
		apiKM:      opts.APIKeys,
		apiKeyAuth: opts.APIAuth,
		rl:         opts.RateLimit,
		users:      opts.Users,
		orgs:       opts.Orgs,
		projects:   opts.Projects,
		agents:     opts.Agents,
		sessions:   opts.Sessions,
		events:     opts.Events,
		apiKeys:    opts.APIKeyRepo,
		tasks:      opts.Tasks,
		skills:     opts.Skills,
		alerts:     opts.Alerts,
		agentExec:  opts.AgentExec,
		llmRouter:  opts.LLMRouter,
		memory:     opts.Memory,
		budget:    opts.Budget,
		worker:    opts.Worker,
		engine:    opts.Engine,
	}
	r.setupMiddleware()
	r.setupRoutes()
	return r
}
