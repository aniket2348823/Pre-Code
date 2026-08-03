package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vigilagent/vigilagent/internal/database"
)

func TestEventRepositoryInterface(t *testing.T) {
	var _ EventRepositoryInterface = &EventRepository{}
}

func TestNewEventRepository(t *testing.T) {
	t.Run("creates repo with pool", func(t *testing.T) {
		conn := database.NewConn(nil)
		r := NewEventRepository(conn)
		require.NotNil(t, r)
		assert.Equal(t, conn, r.pool)
	})

	t.Run("creates repo with nil conn", func(t *testing.T) {
		r := NewEventRepository(nil)
		require.NotNil(t, r)
		assert.Nil(t, r.pool)
	})
}

func TestEvent_Struct(t *testing.T) {
	now := time.Now()
	e := Event{
		ID:         "evt-1",
		SessionID:  "sess-1",
		EventType:  "llm_call",
		Source:     "openai",
		Payload:    map[string]interface{}{"model": "gpt-4"},
		TokensUsed: 1500,
		CostUsd:    0.05,
		LatencyMs:  200,
		CreatedAt:  now,
	}

	assert.Equal(t, "evt-1", e.ID)
	assert.Equal(t, "sess-1", e.SessionID)
	assert.Equal(t, "llm_call", e.EventType)
	assert.Equal(t, "openai", e.Source)
	assert.Equal(t, 1500, e.TokensUsed)
	assert.Equal(t, 0.05, e.CostUsd)
	assert.Equal(t, 200, e.LatencyMs)
	assert.Equal(t, "gpt-4", e.Payload["model"])
}

func TestEvent_Struct_ZeroValues(t *testing.T) {
	e := Event{}
	assert.Empty(t, e.ID)
	assert.Empty(t, e.SessionID)
	assert.Empty(t, e.EventType)
	assert.Empty(t, e.Source)
	assert.Nil(t, e.Payload)
	assert.Equal(t, 0, e.TokensUsed)
	assert.Equal(t, 0.0, e.CostUsd)
	assert.Equal(t, 0, e.LatencyMs)
	assert.True(t, e.CreatedAt.IsZero())
}

func TestCostSummary_Struct(t *testing.T) {
	cs := CostSummary{TotalCost: 1.50, EventCount: 10, AvgCost: 0.15}
	assert.Equal(t, 1.50, cs.TotalCost)
	assert.Equal(t, 10, cs.EventCount)
	assert.Equal(t, 0.15, cs.AvgCost)
}

func TestTokenSummary_Struct(t *testing.T) {
	ts := TokenSummary{TotalTokens: 50000, EventCount: 100, AvgTokens: 500}
	assert.Equal(t, 50000, ts.TotalTokens)
	assert.Equal(t, 100, ts.EventCount)
	assert.Equal(t, 500.0, ts.AvgTokens)
}

func TestSessionStats_Struct(t *testing.T) {
	ss := SessionStats{
		TotalSessions:  50,
		ActiveSessions: 5,
		AvgLatencyMs:   120.5,
		TotalEvents:    500,
	}
	assert.Equal(t, 50, ss.TotalSessions)
	assert.Equal(t, 5, ss.ActiveSessions)
	assert.Equal(t, 120.5, ss.AvgLatencyMs)
	assert.Equal(t, 500, ss.TotalEvents)
}

func TestTopAgent_Struct(t *testing.T) {
	ta := TopAgent{
		AgentID:      "agent-1",
		AgentName:    "code-reviewer",
		ProjectID:    "proj-1",
		SessionCount: 25,
		TotalEvents:  200,
		TotalTokens:  10000,
		TotalCost:    0.50,
	}
	assert.Equal(t, "agent-1", ta.AgentID)
	assert.Equal(t, "code-reviewer", ta.AgentName)
	assert.Equal(t, "proj-1", ta.ProjectID)
	assert.Equal(t, 25, ta.SessionCount)
	assert.Equal(t, 200, ta.TotalEvents)
	assert.Equal(t, 10000, ta.TotalTokens)
	assert.Equal(t, 0.50, ta.TotalCost)
}

func TestDashboardActivity_Struct(t *testing.T) {
	now := time.Now()
	da := DashboardActivity{
		EventType: "task_completed",
		Source:    "agent",
		Tokens:    500,
		Cost:      0.02,
		Timestamp: now,
	}
	assert.Equal(t, "task_completed", da.EventType)
	assert.Equal(t, "agent", da.Source)
	assert.Equal(t, 500, da.Tokens)
	assert.Equal(t, 0.02, da.Cost)
	assert.Equal(t, now, da.Timestamp)
}

func TestEventRepository_Create_NilPool(t *testing.T) {
	r := NewEventRepository(nil)
	assert.Panics(t, func() {
		_ = r.Create(nil, &Event{})
	})
}

func TestEventRepository_BatchCreate_EmptySlice(t *testing.T) {
	r := NewEventRepository(nil)
	err := r.BatchCreate(nil, []Event{})
	assert.NoError(t, err, "empty batch should return nil without touching DB")
}

func TestEventRepository_BatchCreate_NilSlice(t *testing.T) {
	r := NewEventRepository(nil)
	err := r.BatchCreate(nil, nil)
	assert.NoError(t, err, "nil batch should return nil without touching DB")
}

func TestEventRepository_BatchCreate_NilPool(t *testing.T) {
	r := NewEventRepository(nil)
	events := []Event{{SessionID: "s1", EventType: "test"}}
	assert.Panics(t, func() {
		_ = r.BatchCreate(nil, events)
	})
}

func TestEventRepository_GetCostByOrg_NilPool(t *testing.T) {
	r := NewEventRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.GetCostByOrg(nil, "org-1", time.Now().Add(-24*time.Hour), time.Now())
	})
}

func TestEventRepository_GetTokensByOrg_NilPool(t *testing.T) {
	r := NewEventRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.GetTokensByOrg(nil, "org-1", time.Now().Add(-24*time.Hour), time.Now())
	})
}

func TestEventRepository_GetSessionStatsByOrg_NilPool(t *testing.T) {
	r := NewEventRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.GetSessionStatsByOrg(nil, "org-1")
	})
}

func TestEventRepository_GetTopAgentsByOrg_NilPool(t *testing.T) {
	r := NewEventRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.GetTopAgentsByOrg(nil, "org-1", 10)
	})
}

func TestEventRepository_GetRecentActivity_NilPool(t *testing.T) {
	r := NewEventRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.GetRecentActivity(nil, "org-1", 20)
	})
}

func TestCostSummary_ZeroValues(t *testing.T) {
	cs := CostSummary{}
	assert.Equal(t, 0.0, cs.TotalCost)
	assert.Equal(t, 0, cs.EventCount)
	assert.Equal(t, 0.0, cs.AvgCost)
}

func TestTokenSummary_ZeroValues(t *testing.T) {
	ts := TokenSummary{}
	assert.Equal(t, 0, ts.TotalTokens)
	assert.Equal(t, 0, ts.EventCount)
	assert.Equal(t, 0.0, ts.AvgTokens)
}

func TestSessionStats_ZeroValues(t *testing.T) {
	ss := SessionStats{}
	assert.Equal(t, 0, ss.TotalSessions)
	assert.Equal(t, 0, ss.ActiveSessions)
	assert.Equal(t, 0.0, ss.AvgLatencyMs)
	assert.Equal(t, 0, ss.TotalEvents)
}

func TestTopAgent_ZeroValues(t *testing.T) {
	ta := TopAgent{}
	assert.Empty(t, ta.AgentID)
	assert.Empty(t, ta.AgentName)
	assert.Empty(t, ta.ProjectID)
	assert.Equal(t, 0, ta.SessionCount)
	assert.Equal(t, 0, ta.TotalEvents)
	assert.Equal(t, 0, ta.TotalTokens)
	assert.Equal(t, 0.0, ta.TotalCost)
}

func TestDashboardActivity_ZeroValues(t *testing.T) {
	da := DashboardActivity{}
	assert.Empty(t, da.EventType)
	assert.Empty(t, da.Source)
	assert.Equal(t, 0, da.Tokens)
	assert.Equal(t, 0.0, da.Cost)
	assert.True(t, da.Timestamp.IsZero())
}

func TestEvent_WithNilPayload(t *testing.T) {
	e := Event{Payload: nil}
	assert.Nil(t, e.Payload)
}
