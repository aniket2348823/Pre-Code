package middleware

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// PlanTier represents a subscription plan with rate limits.
type PlanTier string

const (
	PlanFree       PlanTier = "free"
	PlanPro        PlanTier = "pro"
	PlanTeam       PlanTier = "team"
	PlanEnterprise PlanTier = "enterprise"
)

// PlanLimits defines rate limits and quotas for a plan tier.
type PlanLimits struct {
	// Rate limiting
	RequestsPerMinute   int `json:"requests_per_minute"`
	RequestsPerDay      int `json:"requests_per_day"`
	RequestsPerMonth    int `json:"requests_per_month"`
	BurstSize           int `json:"burst_size"`

	// Usage quotas
	TokensPerMonth    int     `json:"tokens_per_month"`
	TasksPerMonth     int     `json:"tasks_per_month"`
	ScansPerMonth     int     `json:"scans_per_month"`
	MonthlyBudgetUsd  float64 `json:"monthly_budget_usd"`

	// Features
	MaxConcurrentTasks  int    `json:"max_concurrent_tasks"`
	MaxProjectMembers   int    `json:"max_project_members"`
	MaxProjects         int    `json:"max_projects"`
	MaxAgentsPerProject int    `json:"max_agents_per_project"`
	PriorityQueue       bool   `json:"priority_queue"`
	SupportSLA          string `json:"support_sla"`
}

// DefaultLimits returns the default limits for each plan tier.
func DefaultLimits() map[PlanTier]PlanLimits {
	return map[PlanTier]PlanLimits{
		PlanFree: {
			RequestsPerMinute:   30,
			RequestsPerDay:      500,
			RequestsPerMonth:    10000,
			BurstSize:           10,
			TokensPerMonth:      500000,
			TasksPerMonth:       50,
			ScansPerMonth:       100,
			MonthlyBudgetUsd:    5.0,
			MaxConcurrentTasks:  1,
			MaxProjectMembers:   3,
			MaxProjects:         1,
			MaxAgentsPerProject: 2,
			PriorityQueue:       false,
			SupportSLA:          "community",
		},
		PlanPro: {
			RequestsPerMinute:   120,
			RequestsPerDay:      5000,
			RequestsPerMonth:    100000,
			BurstSize:           30,
			TokensPerMonth:      5000000,
			TasksPerMonth:       500,
			ScansPerMonth:       1000,
			MonthlyBudgetUsd:    50.0,
			MaxConcurrentTasks:  5,
			MaxProjectMembers:   10,
			MaxProjects:         5,
			MaxAgentsPerProject: 10,
			PriorityQueue:       true,
			SupportSLA:          "email_48h",
		},
		PlanTeam: {
			RequestsPerMinute:   300,
			RequestsPerDay:      20000,
			RequestsPerMonth:    500000,
			BurstSize:           100,
			TokensPerMonth:      20000000,
			TasksPerMonth:       2000,
			ScansPerMonth:       5000,
			MonthlyBudgetUsd:    200.0,
			MaxConcurrentTasks:  15,
			MaxProjectMembers:   50,
			MaxProjects:         20,
			MaxAgentsPerProject: 25,
			PriorityQueue:       true,
			SupportSLA:          "priority_24h",
		},
		PlanEnterprise: {
			RequestsPerMinute:   1000,
			RequestsPerDay:      100000,
			RequestsPerMonth:    0, // unlimited
			BurstSize:           500,
			TokensPerMonth:      0, // unlimited
			TasksPerMonth:       0, // unlimited
			ScansPerMonth:       0, // unlimited
			MonthlyBudgetUsd:    0, // unlimited
			MaxConcurrentTasks:  50,
			MaxProjectMembers:   0, // unlimited
			MaxProjects:         0, // unlimited
			MaxAgentsPerProject: 0, // unlimited
			PriorityQueue:       true,
			SupportSLA:          "dedicated",
		},
	}
}

// UsageTracker tracks API usage per org per billing cycle.
type UsageTracker struct {
	client *redis.Client
}

// NewUsageTracker creates a new usage tracker backed by Redis.
func NewUsageTracker(client *redis.Client) *UsageTracker {
	return &UsageTracker{client: client}
}

// UsageKey generates a Redis key for tracking usage.
func (ut *UsageTracker) UsageKey(orgID string, metric string, period string) string {
	return fmt.Sprintf("usage:%s:%s:%s", orgID, metric, period)
}

// CurrentPeriod returns the current billing period string (YYYY-MM).
func CurrentPeriod() string {
	return time.Now().Format("2006-01")
}

// IncrementUsage atomically increments a usage counter and returns the new value.
func (ut *UsageTracker) IncrementUsage(ctx context.Context, orgID string, metric string, amount int64) (int64, error) {
	key := ut.UsageKey(orgID, metric, CurrentPeriod())
	val, err := ut.client.IncrBy(ctx, key, amount).Result()
	if err != nil {
		return 0, err
	}
	// Set expiry to 45 days (covers current month + buffer)
	ut.client.Expire(ctx, key, 45*24*time.Hour)
	return val, nil
}

// GetUsage returns the current usage for a metric.
func (ut *UsageTracker) GetUsage(ctx context.Context, orgID string, metric string) (int64, error) {
	key := ut.UsageKey(orgID, metric, CurrentPeriod())
	val, err := ut.client.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}

// CheckQuota checks if an org has exceeded its quota for a given metric.
// Returns (allowed, remaining, error). If limit <= 0, it's unlimited.
func (ut *UsageTracker) CheckQuota(ctx context.Context, orgID string, metric string, limit int) (bool, int, error) {
	if limit <= 0 {
		return true, -1, nil // unlimited
	}
	usage, err := ut.GetUsage(ctx, orgID, metric)
	if err != nil {
		return true, 0, err // fail-open on Redis errors
	}
	rem := limit - int(usage)
	return rem > 0, rem, nil
}

// --- Plan-Aware Rate Limiter ---

// OrgPlanFunc extracts the org ID and plan from a request.
type OrgPlanFunc func(r *http.Request) (orgID string, plan PlanTier)

// PlanAwareRateLimiter provides plan-based rate limiting with usage metering.
type PlanAwareRateLimiter struct {
	client  *redis.Client
	limits  map[PlanTier]PlanLimits
	tracker *UsageTracker
	mu      sync.RWMutex
}

// NewPlanAwareRateLimiter creates a new plan-aware rate limiter.
func NewPlanAwareRateLimiter(client *redis.Client) *PlanAwareRateLimiter {
	return &PlanAwareRateLimiter{
		client:  client,
		limits:  DefaultLimits(),
		tracker: NewUsageTracker(client),
	}
}

// SetLimits overrides the default limits for a specific plan.
func (p *PlanAwareRateLimiter) SetLimits(tier PlanTier, limits PlanLimits) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.limits[tier] = limits
}

// GetLimits returns the limits for a plan tier.
func (p *PlanAwareRateLimiter) GetLimits(tier PlanTier) PlanLimits {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if l, ok := p.limits[tier]; ok {
		return l
	}
	return p.limits[PlanFree]
}

// Middleware returns a chi-compatible middleware that enforces plan-based rate limits.
func (p *PlanAwareRateLimiter) Middleware(orgPlanFn OrgPlanFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			orgID, plan := orgPlanFn(r)
			if orgID == "" {
				orgID = "anonymous"
				plan = PlanFree
			}

			limits := p.GetLimits(plan)

			// 1. Check per-minute rate limit using simple INCR + EXPIRE
			minuteKey := fmt.Sprintf("ratelimit:org:%s:%d", orgID, time.Now().Unix()/60)
			allowed, count := p.checkMinuteLimit(r.Context(), minuteKey, int64(limits.RequestsPerMinute))
			if !allowed {
				retryAfter := time.Until(time.Now().Truncate(time.Minute).Add(time.Minute))
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limits.RequestsPerMinute))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(retryAfter).Unix(), 10))
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
				w.Header().Set("X-RateLimit-Plan", string(plan))
				response.JSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"code":        "RATE_001",
					"error":       "rate limit exceeded",
					"plan":        plan,
					"retry_after": int(retryAfter.Seconds()) + 1,
				})
				return
			}

			// 2. Check daily quota
			if limits.RequestsPerDay > 0 {
				dayKey := fmt.Sprintf("ratelimit:org:%s:day:%s", orgID, time.Now().Format("2006-01-02"))
				current, _ := p.client.Get(r.Context(), dayKey).Int64()
				if int(current) >= limits.RequestsPerDay {
					response.JSON(w, http.StatusTooManyRequests, map[string]interface{}{
						"code":    "RATE_002",
						"error":   "daily quota exceeded",
						"plan":    plan,
						"message": "Upgrade your plan for more daily requests",
					})
					return
				}
				// Use Lua script for atomic check-and-increment to avoid race condition
				// Only increment if the new count would not exceed the limit
				luaScript := redis.NewScript(`
					local current = redis.call('GET', KEYS[1])
					if current == false then
						current = 0
					else
						current = tonumber(current)
					end
					if current >= tonumber(ARGV[1]) then
						return current
					end
					local newval = redis.call('INCR', KEYS[1])
					if newval == 1 then
						redis.call('EXPIRE', KEYS[1], ARGV[2])
					end
					return newval
				`)
				count, err := luaScript.Run(r.Context(), p.client, []string{dayKey}, limits.RequestsPerDay, int(25*time.Hour/time.Second)).Int64()
				if err != nil {
					slog.Warn("daily quota check failed, allowing request", "error", err)
				} else if int(count) > limits.RequestsPerDay {
					response.JSON(w, http.StatusTooManyRequests, map[string]interface{}{
						"code":    "RATE_002",
						"error":   "daily quota exceeded",
						"plan":    plan,
						"message": "Upgrade your plan for more daily requests",
					})
					return
				}
			}

			// 3. Set rate limit headers
			resetTime := time.Now().Truncate(time.Minute).Add(time.Minute).Unix()
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limits.RequestsPerMinute))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(max(0, int64(limits.RequestsPerMinute)-count), 10)) // count is from checkMinuteLimit above
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime, 10))
			w.Header().Set("X-RateLimit-Plan", string(plan))

			next.ServeHTTP(w, r)
		})
	}
}

// checkMinuteLimit uses simple INCR + EXPIRE for minute-based rate limiting.
func (p *PlanAwareRateLimiter) checkMinuteLimit(ctx context.Context, key string, limit int64) (bool, int64) {
	if p.client == nil {
		return true, 0 // fail-open when no Redis
	}
	count, err := p.client.Incr(ctx, key).Result()
	if err != nil {
		slog.Warn("rate limit check failed, allowing request", "error", err)
		return true, 0 // fail-open
	}
	if count == 1 {
		p.client.Expire(ctx, key, 65*time.Second) // slightly more than 1 minute
	}
	return count <= limit, count
}

// --- Usage Metering Middleware ---

// UsageMeteringMiddleware tracks API usage per org for billing.
type UsageMeteringMiddleware struct {
	tracker *UsageTracker
}

// NewUsageMeteringMiddleware creates a new usage metering middleware.
func NewUsageMeteringMiddleware(client *redis.Client) *UsageMeteringMiddleware {
	return &UsageMeteringMiddleware{
		tracker: NewUsageTracker(client),
	}
}

// Middleware returns middleware that tracks request count per org.
func (u *UsageMeteringMiddleware) Middleware(orgPlanFn OrgPlanFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract orgID BEFORE calling next (context may change)
			orgID, _ := orgPlanFn(r)

			// Wrap ResponseWriter to capture status code
			ww := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(ww, r)

			// Only count successful requests toward usage
			if ww.status < 400 && orgID != "" {
				if _, err := u.tracker.IncrementUsage(r.Context(), orgID, "api_requests", 1); err != nil {
					slog.Warn("usage metering failed", "org_id", orgID, "error", err)
				}
			}
		})
	}
}

// statusWriter wraps ResponseWriter to capture the status code.
// Implements http.Flusher and http.Hijacker to support SSE streaming and WebSocket.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Unwrap returns the underlying ResponseWriter for io.WriterTo support.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush implements http.Flusher for SSE streaming support.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker for WebSocket upgrade support.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

// --- Quota Enforcement Middleware ---

// QuotaEnforcer checks org-level usage quotas before processing requests.
type QuotaEnforcer struct {
	tracker *UsageTracker
	limits  map[PlanTier]PlanLimits
	mu      sync.RWMutex
}

// NewQuotaEnforcer creates a new quota enforcer.
func NewQuotaEnforcer(client *redis.Client) *QuotaEnforcer {
	return &QuotaEnforcer{
		tracker: NewUsageTracker(client),
		limits:  DefaultLimits(),
	}
}

// SetLimits overrides the default limits for a specific plan.
func (q *QuotaEnforcer) SetLimits(tier PlanTier, limits PlanLimits) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.limits[tier] = limits
}

// CheckTasksQuota checks if an org can create more tasks.
func (q *QuotaEnforcer) CheckTasksQuota(ctx context.Context, orgID string, plan PlanTier) (bool, int, error) {
	q.mu.RLock()
	limits := q.limits[plan]
	q.mu.RUnlock()
	return q.tracker.CheckQuota(ctx, orgID, "tasks", limits.TasksPerMonth)
}

// CheckTokensQuota checks if an org has token budget remaining.
func (q *QuotaEnforcer) CheckTokensQuota(ctx context.Context, orgID string, plan PlanTier) (bool, int, error) {
	q.mu.RLock()
	limits := q.limits[plan]
	q.mu.RUnlock()
	return q.tracker.CheckQuota(ctx, orgID, "tokens", limits.TokensPerMonth)
}

// CheckScansQuota checks if an org can run more scans.
func (q *QuotaEnforcer) CheckScansQuota(ctx context.Context, orgID string, plan PlanTier) (bool, int, error) {
	q.mu.RLock()
	limits := q.limits[plan]
	q.mu.RUnlock()
	return q.tracker.CheckQuota(ctx, orgID, "scans", limits.ScansPerMonth)
}

// Middleware returns middleware that checks task quotas only on task creation.
// Only enforces quotas on POST /tasks — reads (GET) are allowed through.
func (q *QuotaEnforcer) Middleware(orgPlanFn OrgPlanFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only check quotas on task creation (POST /tasks)
			if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/tasks") {
				next.ServeHTTP(w, r)
				return
			}

			orgID, plan := orgPlanFn(r)
			if orgID == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed, remaining, err := q.CheckTasksQuota(r.Context(), orgID, plan)
			if err != nil {
				slog.Warn("quota check failed, allowing request", "org_id", orgID, "error", err)
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-Quota-Tasks-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-Quota-Plan", string(plan))

			if !allowed {
				response.JSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"code":    "QUOTA_001",
					"error":   "monthly task quota exceeded",
					"plan":    plan,
					"message": "Upgrade your plan for more tasks per month",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
