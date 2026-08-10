package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vigilagent/vigilagent/internal/database"
)


func TestNewAlertRepository(t *testing.T) {
	t.Run("creates repo with pool", func(t *testing.T) {
		conn := database.NewConn(nil)
		r := NewAlertRepository(conn)
		require.NotNil(t, r)
		assert.Equal(t, conn, r.pool)
	})

	t.Run("creates repo with nil conn", func(t *testing.T) {
		r := NewAlertRepository(nil)
		require.NotNil(t, r)
		assert.Nil(t, r.pool)
	})
}

func TestAlert_Struct(t *testing.T) {
	now := time.Now()
	lastFired := now.Add(-time.Hour)
	a := Alert{
		ID:        "alert-1",
		UserID:    "user-1",
		OrgID:     "org-1",
		Name:      "CPU Alert",
		Type:      "threshold",
		Condition: map[string]interface{}{"metric": "cpu", "threshold": 90},
		Channel:   "email",
		IsActive:  true,
		LastFired: &lastFired,
	}

	assert.Equal(t, "alert-1", a.ID)
	assert.Equal(t, "user-1", a.UserID)
	assert.Equal(t, "org-1", a.OrgID)
	assert.Equal(t, "CPU Alert", a.Name)
	assert.Equal(t, "threshold", a.Type)
	assert.Equal(t, "email", a.Channel)
	assert.True(t, a.IsActive)
	assert.NotNil(t, a.LastFired)
	assert.Equal(t, "cpu", a.Condition["metric"])
}

func TestAlert_Struct_ZeroValues(t *testing.T) {
	a := Alert{}
	assert.Empty(t, a.ID)
	assert.Empty(t, a.UserID)
	assert.Empty(t, a.OrgID)
	assert.Empty(t, a.Name)
	assert.Empty(t, a.Type)
	assert.Nil(t, a.Condition)
	assert.Empty(t, a.Channel)
	assert.False(t, a.IsActive)
	assert.Nil(t, a.LastFired)
	assert.True(t, a.CreatedAt.IsZero())
	assert.True(t, a.UpdatedAt.IsZero())
}

func TestAlertRepository_Create_NilPool(t *testing.T) {
	r := NewAlertRepository(nil)
	assert.Panics(t, func() {
		_ = r.Create(nil, &Alert{})
	})
}

func TestAlertRepository_FindByID_NilPool(t *testing.T) {
	r := NewAlertRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.FindByID(nil, "nonexistent")
	})
}

func TestAlertRepository_ListByUser_NilPool(t *testing.T) {
	r := NewAlertRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.ListByUser(nil, "user-1")
	})
}

func TestAlertRepository_Update_NilPool(t *testing.T) {
	r := NewAlertRepository(nil)
	assert.Panics(t, func() {
		_ = r.Update(nil, "id", "n", "ch", true)
	})
}

func TestAlertRepository_Delete_NilPool(t *testing.T) {
	r := NewAlertRepository(nil)
	assert.Panics(t, func() {
		_ = r.Delete(nil, "id")
	})
}

func TestAlert_WithNilLastFired(t *testing.T) {
	a := Alert{
		ID:        "a1",
		LastFired: nil,
	}
	assert.Nil(t, a.LastFired)
}

func TestAlert_WithEmptyCondition(t *testing.T) {
	a := Alert{
		ID:        "a1",
		Condition: map[string]interface{}{},
	}
	assert.Empty(t, a.Condition)
}
