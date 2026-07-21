package contract

// ---------------------------------------------------------------------------
// Rate Limiting types — API contract §5
// ---------------------------------------------------------------------------

// Rate limit HTTP headers per API contract §5.1.
const (
	HeaderRateLimitLimit     = "X-RateLimit-Limit"
	HeaderRateLimitRemaining = "X-RateLimit-Remaining"
	HeaderRateLimitReset     = "X-RateLimit-Reset"
	HeaderRetryAfter         = "Retry-After"
)

// RateLimitInfo represents the current rate-limit state for a request.
type RateLimitInfo struct {
	Limit     int   `json:"limit"`
	Remaining int   `json:"remaining"`
	ResetUnix int64 `json:"reset"` // Unix timestamp
}

// RateLimitTier holds the per-tier request and task limits.
// Monthly limits per reconciliation report C2 resolution.
type RateLimitTier struct {
	Tier             Tier  `json:"tier"`
	RequestsPerMin   int   `json:"requests_per_min"`
	MonthlyTasks     int   `json:"monthly_tasks"`
	MonthlyTokens    int64 `json:"monthly_tokens"`
}

// DefaultRateLimits returns the rate-limit configuration for each pricing tier.
func DefaultRateLimits() []RateLimitTier {
	return []RateLimitTier{
		{
			Tier:           TierFree,
			RequestsPerMin: 60,
			MonthlyTasks:   100,
			MonthlyTokens:  500_000,
		},
		{
			Tier:           TierPro,
			RequestsPerMin: 300,
			MonthlyTasks:   2_000,
			MonthlyTokens:  5_000_000,
		},
		{
			Tier:           TierTeam,
			RequestsPerMin: 600,
			MonthlyTasks:   5_000,
			MonthlyTokens:  10_000_000,
		},
		{
			Tier:           TierEnterprise,
			RequestsPerMin: 1200,
			MonthlyTasks:   -1, // custom / unlimited
			MonthlyTokens:  -1,
		},
	}
}

// RateLimitForTier returns the rate-limit configuration for the given tier.
// Returns nil if the tier is not found.
func RateLimitForTier(tier Tier) *RateLimitTier {
	for _, rl := range DefaultRateLimits() {
		if rl.Tier == tier {
			return &rl
		}
	}
	return nil
}
