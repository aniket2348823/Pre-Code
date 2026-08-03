package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultLimits_AllTiersPresent(t *testing.T) {
	limits := DefaultLimits()
	assert.Contains(t, limits, PlanFree)
	assert.Contains(t, limits, PlanPro)
	assert.Contains(t, limits, PlanTeam)
	assert.Contains(t, limits, PlanEnterprise)
}

func TestDefaultLimits_FreePlanValues(t *testing.T) {
	limits := DefaultLimits()
	free := limits[PlanFree]
	assert.Equal(t, 30, free.RequestsPerMinute)
	assert.Equal(t, 500, free.RequestsPerDay)
	assert.Equal(t, 10000, free.RequestsPerMonth)
	assert.Equal(t, 10, free.BurstSize)
	assert.Equal(t, 500000, free.TokensPerMonth)
	assert.Equal(t, 50, free.TasksPerMonth)
	assert.Equal(t, 1, free.MaxConcurrentTasks)
	assert.False(t, free.PriorityQueue)
	assert.Equal(t, "community", free.SupportSLA)
}

func TestDefaultLimits_ProPlanValues(t *testing.T) {
	limits := DefaultLimits()
	pro := limits[PlanPro]
	assert.Equal(t, 120, pro.RequestsPerMinute)
	assert.Equal(t, 5000, pro.RequestsPerDay)
	assert.True(t, pro.PriorityQueue)
	assert.Equal(t, "email_48h", pro.SupportSLA)
}

func TestDefaultLimits_EnterpriseUnlimited(t *testing.T) {
	limits := DefaultLimits()
	ent := limits[PlanEnterprise]
	assert.Equal(t, 0, ent.RequestsPerMonth, "0 means unlimited")
	assert.Equal(t, 0, ent.TokensPerMonth, "0 means unlimited")
	assert.Equal(t, 0, ent.TasksPerMonth, "0 means unlimited")
	assert.True(t, ent.PriorityQueue)
	assert.Equal(t, "dedicated", ent.SupportSLA)
}

func TestDefaultLimits_TeamPlanValues(t *testing.T) {
	limits := DefaultLimits()
	team := limits[PlanTeam]
	assert.Equal(t, 300, team.RequestsPerMinute)
	assert.Equal(t, 20000, team.RequestsPerDay)
	assert.Equal(t, 2000, team.TasksPerMonth)
	assert.Equal(t, "priority_24h", team.SupportSLA)
}

func TestCurrentPeriod_Format(t *testing.T) {
	period := CurrentPeriod()
	now := time.Now()
	expected := now.Format("2006-01")
	assert.Equal(t, expected, period)
}

func TestUsageTracker_UsageKey(t *testing.T) {
	ut := &UsageTracker{}
	key := ut.UsageKey("org-123", "tasks", "2026-01")
	assert.Equal(t, "usage:org-123:tasks:2026-01", key)
}

func TestNewUsageTracker(t *testing.T) {
	ut := NewUsageTracker(nil)
	assert.NotNil(t, ut)
}

func TestPlanAwareRateLimiter_GetLimits(t *testing.T) {
	parl := NewPlanAwareRateLimiter(nil)

	limits := parl.GetLimits(PlanFree)
	assert.Equal(t, 30, limits.RequestsPerMinute)

	limits = parl.GetLimits(PlanPro)
	assert.Equal(t, 120, limits.RequestsPerMinute)
}

func TestPlanAwareRateLimiter_SetLimits(t *testing.T) {
	parl := NewPlanAwareRateLimiter(nil)

	custom := PlanLimits{RequestsPerMinute: 999}
	parl.SetLimits(PlanFree, custom)

	limits := parl.GetLimits(PlanFree)
	assert.Equal(t, 999, limits.RequestsPerMinute)
}

func TestPlanAwareRateLimiter_GetLimits_FallbackToFree(t *testing.T) {
	parl := NewPlanAwareRateLimiter(nil)

	limits := parl.GetLimits("nonexistent_plan")
	free := parl.GetLimits(PlanFree)
	assert.Equal(t, free.RequestsPerMinute, limits.RequestsPerMinute)
}

func TestNewPlanAwareRateLimiter(t *testing.T) {
	parl := NewPlanAwareRateLimiter(nil)
	assert.NotNil(t, parl)
	assert.NotNil(t, parl.limits)
	assert.NotNil(t, parl.tracker)
}

func TestQuotaEnforcer_SetLimits(t *testing.T) {
	qe := NewQuotaEnforcer(nil)
	custom := PlanLimits{TasksPerMonth: 10}
	qe.SetLimits(PlanFree, custom)

	qe.mu.RLock()
	assert.Equal(t, 10, qe.limits[PlanFree].TasksPerMonth)
	qe.mu.RUnlock()
}

func TestNewQuotaEnforcer(t *testing.T) {
	qe := NewQuotaEnforcer(nil)
	assert.NotNil(t, qe)
	assert.NotNil(t, qe.tracker)
}

func TestPlanTier_Constants(t *testing.T) {
	assert.Equal(t, PlanTier("free"), PlanFree)
	assert.Equal(t, PlanTier("pro"), PlanPro)
	assert.Equal(t, PlanTier("team"), PlanTeam)
	assert.Equal(t, PlanTier("enterprise"), PlanEnterprise)
}

func TestPlanLimits_ZeroValuesAreUnlimited(t *testing.T) {
	limits := DefaultLimits()
	ent := limits[PlanEnterprise]
	assert.Equal(t, 0, ent.RequestsPerMonth)
	assert.Equal(t, 0, ent.TokensPerMonth)
	assert.Equal(t, 0, ent.TasksPerMonth)
	assert.Equal(t, 0, ent.ScansPerMonth)
	assert.Equal(t, 0.0, ent.MonthlyBudgetUsd)
}
