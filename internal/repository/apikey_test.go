package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vigilagent/vigilagent/internal/database"
)

func TestNewAPIKeyRepository(t *testing.T) {
	t.Run("creates repo with pool", func(t *testing.T) {
		conn := database.NewConn(nil)
		r := NewAPIKeyRepository(conn)
		require.NotNil(t, r)
		assert.Equal(t, conn, r.pool)
	})

	t.Run("creates repo with nil conn", func(t *testing.T) {
		r := NewAPIKeyRepository(nil)
		require.NotNil(t, r)
		assert.Nil(t, r.pool)
	})
}

func TestAPIKey_Struct(t *testing.T) {
	now := time.Now()
	lastUsed := now.Add(-time.Hour)
	expires := now.Add(24 * time.Hour)
	k := APIKey{
		ID:         "key-1",
		UserID:     "user-1",
		Name:       "Production Key",
		KeyHash:    "sha256hash",
		Prefix:     "va_abc",
		Scopes:     []string{"read", "write"},
		IsActive:   true,
		LastUsedAt: &lastUsed,
		ExpiresAt:  &expires,
		CreatedAt:  now,
	}

	assert.Equal(t, "key-1", k.ID)
	assert.Equal(t, "user-1", k.UserID)
	assert.Equal(t, "Production Key", k.Name)
	assert.Equal(t, "sha256hash", k.KeyHash)
	assert.Equal(t, "va_abc", k.Prefix)
	assert.Equal(t, []string{"read", "write"}, k.Scopes)
	assert.True(t, k.IsActive)
	assert.NotNil(t, k.LastUsedAt)
	assert.NotNil(t, k.ExpiresAt)
}

func TestAPIKey_Struct_ZeroValues(t *testing.T) {
	k := APIKey{}
	assert.Empty(t, k.ID)
	assert.Empty(t, k.UserID)
	assert.Empty(t, k.Name)
	assert.Empty(t, k.KeyHash)
	assert.Empty(t, k.Prefix)
	assert.Nil(t, k.Scopes)
	assert.False(t, k.IsActive)
	assert.Nil(t, k.LastUsedAt)
	assert.Nil(t, k.ExpiresAt)
	assert.True(t, k.CreatedAt.IsZero())
}

func TestAPIKeyRepository_Create_NilPool(t *testing.T) {
	r := NewAPIKeyRepository(nil)
	assert.Panics(t, func() {
		_ = r.Create(nil, &APIKey{})
	})
}

func TestAPIKeyRepository_FindByHash_NilPool(t *testing.T) {
	r := NewAPIKeyRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.FindByHash(nil, "hash")
	})
}

func TestAPIKeyRepository_ListByUser_NilPool(t *testing.T) {
	r := NewAPIKeyRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.ListByUser(nil, "user-1")
	})
}

func TestAPIKeyRepository_Delete_NilPool(t *testing.T) {
	r := NewAPIKeyRepository(nil)
	assert.Panics(t, func() {
		_ = r.Delete(nil, "key-1", "user-1")
	})
}

func TestAPIKeyRepository_UpdateLastUsed_NilPool(t *testing.T) {
	r := NewAPIKeyRepository(nil)
	assert.Panics(t, func() {
		_ = r.UpdateLastUsed(nil, "key-1")
	})
}

func TestAPIKey_WithNilOptionalTimes(t *testing.T) {
	k := APIKey{
		ID:         "k1",
		LastUsedAt: nil,
		ExpiresAt:  nil,
	}
	assert.Nil(t, k.LastUsedAt)
	assert.Nil(t, k.ExpiresAt)
}

func TestAPIKey_WithEmptyScopes(t *testing.T) {
	k := APIKey{
		ID:     "k1",
		Scopes: []string{},
	}
	assert.Empty(t, k.Scopes)
}

func TestAPIKey_KeyHashNeverExposed(t *testing.T) {
	k := APIKey{
		KeyHash: "secret-hash-value",
	}
	// KeyHash has json:"-" tag, so JSON marshal should exclude it
	data, err := json.Marshal(k)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "secret-hash-value")
	assert.NotContains(t, string(data), "key_hash")
}
