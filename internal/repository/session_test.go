package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vigilagent/vigilagent/internal/database"
)

func TestNewSessionRepository(t *testing.T) {
	t.Run("creates repo with pool", func(t *testing.T) {
		conn := database.NewConn(nil)
		r := NewSessionRepository(conn)
		require.NotNil(t, r)
		assert.Equal(t, conn, r.pool)
	})

	t.Run("creates repo with nil conn", func(t *testing.T) {
		r := NewSessionRepository(nil)
		require.NotNil(t, r)
		assert.Nil(t, r.pool)
	})
}

func TestSession_Struct(t *testing.T) {
	now := time.Now()
	ended := now.Add(time.Hour)
	s := Session{
		ID:        "sess-1",
		ProjectID: "proj-1",
		AgentID:   "agent-1",
		UserID:    "user-1",
		Status:    "active",
		StartedAt: now,
		EndedAt:   &ended,
		CreatedAt: now,
		UpdatedAt: now,
	}

	assert.Equal(t, "sess-1", s.ID)
	assert.Equal(t, "proj-1", s.ProjectID)
	assert.Equal(t, "agent-1", s.AgentID)
	assert.Equal(t, "user-1", s.UserID)
	assert.Equal(t, "active", s.Status)
	assert.False(t, s.StartedAt.IsZero())
	assert.NotNil(t, s.EndedAt)
}

func TestSession_Struct_ZeroValues(t *testing.T) {
	s := Session{}
	assert.Empty(t, s.ID)
	assert.Empty(t, s.ProjectID)
	assert.Empty(t, s.AgentID)
	assert.Empty(t, s.UserID)
	assert.Empty(t, s.Status)
	assert.True(t, s.StartedAt.IsZero())
	assert.Nil(t, s.EndedAt)
	assert.True(t, s.CreatedAt.IsZero())
	assert.True(t, s.UpdatedAt.IsZero())
}

func TestSessionRepository_Create_NilPool(t *testing.T) {
	r := NewSessionRepository(nil)
	assert.Panics(t, func() {
		_ = r.Create(nil, &Session{})
	})
}

func TestSessionRepository_FindByID_NilPool(t *testing.T) {
	r := NewSessionRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.FindByID(nil, "nonexistent")
	})
}

func TestSessionRepository_Update_NilPool(t *testing.T) {
	r := NewSessionRepository(nil)
	assert.Panics(t, func() {
		_ = r.Update(nil, "id", "completed")
	})
}

func TestSessionRepository_EndSession_NilPool(t *testing.T) {
	r := NewSessionRepository(nil)
	assert.Panics(t, func() {
		_ = r.EndSession(nil, "id")
	})
}

func TestSessionRepository_CleanupStaleSessions_NilPool(t *testing.T) {
	r := NewSessionRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.CleanupStaleSessions(nil, 30*time.Minute)
	})
}

func TestSessionRepository_ListByAgent_NilPool(t *testing.T) {
	r := NewSessionRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.ListByAgent(nil, "agent-1")
	})
}

func TestSessionRepository_StartStaleSessionCleanup_ReturnsCancelFunc(t *testing.T) {
	conn := database.NewConn(nil)
	r := NewSessionRepository(conn)
	ctx := t.Context()

	// StartStaleSessionCleanup should return a cancel function.
	// The goroutine will fail to connect (nil pool) but that's OK —
	// we're testing that the function returns a working cancel func.
	cancel := r.StartStaleSessionCleanup(ctx, 30*time.Minute, 5*time.Second)
	require.NotNil(t, cancel)

	// Cancel should not panic
	cancel()
}

func TestSession_Struct_NilEndedAt(t *testing.T) {
	s := Session{EndedAt: nil}
	assert.Nil(t, s.EndedAt)
}

func TestSession_Struct_AllFieldsPopulated(t *testing.T) {
	now := time.Now()
	s := Session{
		ID:        "s1",
		ProjectID: "p1",
		AgentID:   "a1",
		UserID:    "u1",
		Status:    "completed",
		StartedAt: now,
		CreatedAt: now,
		UpdatedAt: now,
	}
	assert.Equal(t, "s1", s.ID)
	assert.Equal(t, "p1", s.ProjectID)
	assert.Equal(t, "a1", s.AgentID)
	assert.Equal(t, "u1", s.UserID)
	assert.Equal(t, "completed", s.Status)
}
