package router

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/agent"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/config"
	"github.com/vigilagent/vigilagent/internal/cost"
	"github.com/vigilagent/vigilagent/internal/costintel"
	"github.com/vigilagent/vigilagent/internal/database"
	"github.com/vigilagent/vigilagent/internal/email"
	"github.com/vigilagent/vigilagent/internal/featureflags"
	"github.com/vigilagent/vigilagent/internal/llm"
	mw "github.com/vigilagent/vigilagent/internal/middleware"
	"github.com/vigilagent/vigilagent/internal/memory"
	"github.com/vigilagent/vigilagent/internal/queue"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/skills"
	"github.com/vigilagent/vigilagent/internal/knowledge"
	"github.com/vigilagent/vigilagent/internal/skillengine"
	"github.com/vigilagent/vigilagent/internal/attackgraph"
	"github.com/vigilagent/vigilagent/internal/audit"
	"github.com/vigilagent/vigilagent/internal/confidence"
	"github.com/vigilagent/vigilagent/internal/compliance"
	"github.com/vigilagent/vigilagent/internal/pipeline"
	"github.com/vigilagent/vigilagent/internal/requirements"
	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/schema"
	"github.com/vigilagent/vigilagent/internal/webhook"
)

// Options holds all dependencies for the Router.
type Options struct {
	// Config
	Config *config.Config

	// Infrastructure
	DB      *database.Postgres
	Redis   *database.Redis
	NATS    *queue.NATS
	RedisDS interface{} // *redis.Client for rate limiter

	// Auth
	JWT           *auth.JWT
	APIKeys       *auth.APIKeyService
	APIAuth       *mw.APIKeyAuth
	RateLimit     *mw.RateLimiter
	AuthRateLimit *mw.RateLimiter

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

	// Deterministic engine (security requirements)
	Requirements *requirements.Resolver

	// Deterministic engine (schema validation)
	Validator *schema.Validator

	// Deterministic engine (compliance)
	Compliance *compliance.Checker

	// Unified validation pipeline
	Pipeline *pipeline.Pipeline

	// Knowledge graph
	Knowledge *knowledge.Graph

	// Skill engine
	SkillEngine *skillengine.Engine

	// Confidence engine
	Confidence *confidence.Engine

	// Attack graph
	AttackGraph *attackgraph.Engine

	// Audit layer
	Audit *audit.Engine

	// Webhook engine for event notifications
	Webhook *webhook.Engine

	// Cost intelligence engine
	CostIntel *costintel.Engine

	// Email service
	Email *email.VerificationService

	// Feature flags
	FeatureFlags *featureflags.Manager

	// Skill marketplace RAG engine
	SkillRAG *skills.RAGEngine
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
	apiKeyAuth *mw.APIKeyAuth
	rl         *mw.RateLimiter
	authRL     *mw.RateLimiter
	authSessionMiddleware *mw.AuthSessionMiddleware
	webhookEngine        *webhook.Engine
	wsManager            *WebSocketManager

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
	engine              *scanner.Engine
	requirements        *requirements.Resolver
	validator           *schema.Validator
	complianceChecker   *compliance.Checker
	pipeline            *pipeline.Pipeline
	knowledge           *knowledge.Graph
	skillEng            *skillengine.Engine
	confidence          *confidence.Engine
	attackGraph         *attackgraph.Engine
	audit               *audit.Engine
	costIntel           *costintel.Engine
	lockout             mw.Lockout
	lockoutCancel       context.CancelFunc

	// Email + Feature Flags + RAG
	email        *email.VerificationService
	featureFlags *featureflags.Manager
	skillRAG     *skills.RAGEngine

	requirementsHandlerFn http.HandlerFunc
	validateHandlerFn     http.HandlerFunc
	schemaHandlerFn       http.HandlerFunc
	complianceHandlerFn   http.HandlerFunc
	pipelineHandlerFn     http.HandlerFunc
	knowledgeHandlerFn    http.HandlerFunc
	skillEngineHandlerFn  http.HandlerFunc
	confidenceHandlerFn   http.HandlerFunc
	attackGraphHandlerFn  http.HandlerFunc
	auditHandlerFn        http.HandlerFunc
}

// newRouter creates a Router from Options without wiring middleware or routes.
func newRouter(opts Options) *Router {
	// Use Redis-backed lockout if Redis is available, otherwise in-memory
	var lockout mw.Lockout
	if opts.Redis != nil && opts.Redis.Client != nil {
		lockout = mw.NewLockout(opts.Redis.Client, 5, 15*time.Minute)
	} else {
		lockout = mw.NewAccountLockout(5, 15*time.Minute)
	}

	return &Router{
		Mux:         chi.NewMux(),
		cfg:         opts.Config,
		db:          opts.DB,
		rds:         opts.Redis,
		nats:        opts.NATS,
		auth:        opts.JWT,
		apiKM:       opts.APIKeys,
		apiKeyAuth:  opts.APIAuth,
		rl:          opts.RateLimit,
		authRL:      opts.AuthRateLimit,
		users:       opts.Users,
		orgs:        opts.Orgs,
		projects:    opts.Projects,
		agents:      opts.Agents,
		sessions:    opts.Sessions,
		events:      opts.Events,
		apiKeys:     opts.APIKeyRepo,
		tasks:       opts.Tasks,
		skills:      opts.Skills,
		alerts:      opts.Alerts,
		agentExec:   opts.AgentExec,
		llmRouter:   opts.LLMRouter,
		memory:      opts.Memory,
		budget:      opts.Budget,
		worker:      opts.Worker,
		engine:      opts.Engine,
		requirements: opts.Requirements,
		validator:   opts.Validator,
		complianceChecker: opts.Compliance,
		pipeline:    opts.Pipeline,
		knowledge:   opts.Knowledge,
		skillEng:    opts.SkillEngine,
		confidence:  opts.Confidence,
		attackGraph: opts.AttackGraph,
		audit:       opts.Audit,
		webhookEngine: opts.Webhook,
		wsManager:     NewWebSocketManager(DefaultWebSocketManagerConfig()),
		lockout:       lockout,
		costIntel:     opts.CostIntel,
		email:       opts.Email,
		featureFlags: opts.FeatureFlags,
		skillRAG:    opts.SkillRAG,
	}
}

// New creates a Router from an Options struct with the default middleware stack.
func New(opts Options) *Router {
	r := newRouter(opts)
	if r.db != nil && r.db.Pool != nil {
		r.authSessionMiddleware = mw.NewAuthSessionMiddleware(r.db.Conn())
	}
	r.initHandlers()
	r.setupMiddleware()
	r.setupRoutes()
	return r
}

// initHandlers builds deterministic engine handlers at construction time.
func (r *Router) initHandlers() {
	// Start lockout cleanup goroutine for in-memory implementation
	if al, ok := r.lockout.(*mw.AccountLockout); ok {
		ctx, cancel := context.WithCancel(context.Background())
		go al.Cleanup(ctx, 5*time.Minute)
		r.lockoutCancel = cancel
	}

	if r.engine == nil {
		r.engine = scanner.NewEngine(scanner.NewBuiltinAnalyzer())
	}

	reqResolver := r.requirements
	if reqResolver == nil {
		reqResolver = requirements.NewResolver()
	}
	r.requirementsHandlerFn = requirements.NewHTTPHandler(reqResolver)
	r.validateHandlerFn = requirements.NewValidateHTTPHandler(reqResolver)

	validator := r.validator
	if validator == nil {
		validator = schema.NewValidator()
	}
	r.schemaHandlerFn = schema.NewHTTPHandler(validator)

	checker := r.complianceChecker
	if checker == nil {
		checker = compliance.NewChecker()
	}
	r.complianceHandlerFn = compliance.NewHTTPHandler(checker)

	if r.pipeline == nil {
		r.pipeline = pipeline.NewPipeline(validator, reqResolver, checker, r.engine)
	}
	r.pipelineHandlerFn = pipeline.NewHTTPHandler(r.pipeline)

	kg := r.knowledge
	if kg == nil {
		kg = knowledge.NewGraph()
	}
	r.knowledgeHandlerFn = knowledge.NewHTTPHandler(kg)

	se := r.skillEng
	if se == nil {
		se = skillengine.NewEngine()
	}
	r.skillEngineHandlerFn = skillengine.NewHTTPHandler(se)

	ce := r.confidence
	if ce == nil {
		ce = confidence.NewEngine()
	}
	r.confidenceHandlerFn = confidence.NewHTTPHandler(ce)

	ag := r.attackGraph
	if ag == nil {
		ag = attackgraph.NewEngine()
	}
	r.attackGraphHandlerFn = attackgraph.NewHTTPHandler(ag)

	au := r.audit
	if au == nil {
		au = audit.NewEngine(audit.NewMemoryStore())
	}
	r.auditHandlerFn = audit.NewHTTPHandler(au)
}

// Shutdown cancels background goroutines started by the router.
func (r *Router) Shutdown() {
	if r.lockoutCancel != nil {
		r.lockoutCancel()
	}
}
