package contract

import (
	"encoding/json"
	"testing"
)

func TestRateLimitTier_DefaultValues(t *testing.T) {
	tests := []struct {
		tier          Tier
		reqPerMin     int
		monthlyTasks  int
		monthlyTokens int64
	}{
		{TierFree, 60, 100, 500_000},
		{TierPro, 300, 2_000, 5_000_000},
		{TierTeam, 600, 5_000, 10_000_000},
		{TierEnterprise, 1200, -1, -1},
	}
	for _, tt := range tests {
		t.Run(string(tt.tier), func(t *testing.T) {
			rl := RateLimitForTier(tt.tier)
			if rl == nil {
				t.Fatalf("RateLimitForTier(%q) returned nil", tt.tier)
			}
			if rl.RequestsPerMin != tt.reqPerMin {
				t.Errorf("RequestsPerMin = %d, want %d", rl.RequestsPerMin, tt.reqPerMin)
			}
			if rl.MonthlyTasks != tt.monthlyTasks {
				t.Errorf("MonthlyTasks = %d, want %d", rl.MonthlyTasks, tt.monthlyTasks)
			}
			if rl.MonthlyTokens != tt.monthlyTokens {
				t.Errorf("MonthlyTokens = %d, want %d", rl.MonthlyTokens, tt.monthlyTokens)
			}
		})
	}
}

func TestRateLimitForTier_UnknownTier(t *testing.T) {
	if rl := RateLimitForTier(Tier("platinum")); rl != nil {
		t.Errorf("expected nil for unknown tier, got %+v", rl)
	}
}

func TestDefaultRateLimits_Count(t *testing.T) {
	limits := DefaultRateLimits()
	if len(limits) != 4 {
		t.Errorf("DefaultRateLimits() has %d entries, want 4", len(limits))
	}
}

func TestRateLimitHeaders_Constants(t *testing.T) {
	// Verify the header names match the API contract spec.
	headers := map[string]string{
		"limit":       HeaderRateLimitLimit,
		"remaining":   HeaderRateLimitRemaining,
		"reset":       HeaderRateLimitReset,
		"retry_after": HeaderRetryAfter,
	}
	expected := map[string]string{
		"limit":       "X-RateLimit-Limit",
		"remaining":   "X-RateLimit-Remaining",
		"reset":       "X-RateLimit-Reset",
		"retry_after": "Retry-After",
	}
	for key, got := range headers {
		want := expected[key]
		if got != want {
			t.Errorf("Header %s = %q, want %q", key, got, want)
		}
	}
}

func TestRateLimitInfo_JSONRoundTrip(t *testing.T) {
	original := RateLimitInfo{
		Limit:     300,
		Remaining: 142,
		ResetUnix: 1719792000,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded RateLimitInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Limit != 300 {
		t.Errorf("Limit = %d, want 300", decoded.Limit)
	}
	if decoded.Remaining != 142 {
		t.Errorf("Remaining = %d, want 142", decoded.Remaining)
	}
	if decoded.ResetUnix != 1719792000 {
		t.Errorf("ResetUnix = %d, want 1719792000", decoded.ResetUnix)
	}
}

func TestRateLimitTier_AlignWithTierMonthlyTaskLimit(t *testing.T) {
	// Cross-validate that RateLimitTier.MonthlyTasks matches Tier.MonthlyTaskLimit().
	for _, rl := range DefaultRateLimits() {
		tierLimit := rl.Tier.MonthlyTaskLimit()
		if rl.MonthlyTasks != tierLimit {
			t.Errorf("Tier %q: RateLimitTier.MonthlyTasks=%d != Tier.MonthlyTaskLimit()=%d",
				rl.Tier, rl.MonthlyTasks, tierLimit)
		}
	}
}
