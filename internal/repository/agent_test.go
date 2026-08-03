package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vigilagent/vigilagent/internal/database"
)

func TestAgentRepositoryInterface(t *testing.T) {
	var _ AgentRepositoryInterface = &AgentRepository{}
}

func TestNewAgentRepository(t *testing.T) {
	t.Run("creates repo with pool", func(t *testing.T) {
		conn := database.NewConn(nil)
		r := NewAgentRepository(conn)
		require.NotNil(t, r)
		assert.Equal(t, conn, r.pool)
	})

	t.Run("creates repo with nil conn", func(t *testing.T) {
		r := NewAgentRepository(nil)
		require.NotNil(t, r)
		assert.Nil(t, r.pool)
	})
}

func TestAgent_Struct(t *testing.T) {
	a := Agent{
		ID:          "agent-1",
		ProjectID:   "proj-1",
		Name:        "test-agent",
		Description: "A test agent",
		Status:      "active",
		Config: map[string]interface{}{
			"model": "gpt-4",
		},
	}

	assert.Equal(t, "agent-1", a.ID)
	assert.Equal(t, "proj-1", a.ProjectID)
	assert.Equal(t, "test-agent", a.Name)
	assert.Equal(t, "A test agent", a.Description)
	assert.Equal(t, "active", a.Status)
	assert.Equal(t, "gpt-4", a.Config["model"])
}

func TestAgentRepository_Create_NilPool(t *testing.T) {
	r := NewAgentRepository(nil)
	assert.Panics(t, func() {
		_ = r.Create(nil, &Agent{})
	})
}

func TestAgentRepository_FindByID_NilPool(t *testing.T) {
	r := NewAgentRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.FindByID(nil, "nonexistent")
	})
}

func TestAgentRepository_Update_NilPool(t *testing.T) {
	r := NewAgentRepository(nil)
	assert.Panics(t, func() {
		_ = r.Update(nil, "id", "n", "d", "active", map[string]interface{}{})
	})
}

func TestAgentRepository_Delete_NilPool(t *testing.T) {
	r := NewAgentRepository(nil)
	assert.Panics(t, func() {
		_ = r.Delete(nil, "id")
	})
}

func TestAgentRepository_ListByProject_NilPool(t *testing.T) {
	r := NewAgentRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.ListByProject(nil, "proj-1")
	})
}

func TestAgent_Struct_ZeroValues(t *testing.T) {
	a := Agent{}
	assert.Empty(t, a.ID)
	assert.Empty(t, a.ProjectID)
	assert.Empty(t, a.Name)
	assert.Empty(t, a.Description)
	assert.Nil(t, a.Config)
	assert.Empty(t, a.Status)
	assert.True(t, a.CreatedAt.IsZero())
	assert.True(t, a.UpdatedAt.IsZero())
}

func TestAgent_Struct_JSONTags(t *testing.T) {
	a := Agent{
		ID:          "a1",
		ProjectID:   "p1",
		Name:        "name",
		Description: "desc",
		Status:      "active",
	}
	assert.Equal(t, "a1", a.ID)
	assert.Equal(t, "p1", a.ProjectID)
	assert.Equal(t, "name", a.Name)
	assert.Equal(t, "desc", a.Description)
	assert.Equal(t, "active", a.Status)
}

func TestAgent_Struct_NilConfig(t *testing.T) {
	a := Agent{Config: nil}
	assert.Nil(t, a.Config)
}
