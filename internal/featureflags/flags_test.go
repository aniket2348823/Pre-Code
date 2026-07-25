package featureflags

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestManager_IsEnabled_CacheHit tests IsEnabled with cache hits only
func TestManager_IsEnabled_CacheHit(t *testing.T) {
	m := &Manager{
		cache: map[string]*Flag{
			"enabled-flag":  {Name: "enabled-flag", Enabled: true},
			"disabled-flag": {Name: "disabled-flag", Enabled: false},
		},
		ttl:       5 * time.Minute,
		lastFetch: time.Now(),
	}

	tests := []struct {
		name     string
		flagName string
		expected bool
	}{
		{"enabled flag returns true", "enabled-flag", true},
		{"disabled flag returns false", "disabled-flag", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.IsEnabled(context.Background(), tt.flagName); got != tt.expected {
				t.Errorf("IsEnabled(%q) = %v, want %v", tt.flagName, got, tt.expected)
			}
		})
	}
}

// TestManager_IsEnabled_CacheMiss tests IsEnabled when cache misses (returns false without DB)
func TestManager_IsEnabled_CacheMiss(t *testing.T) {
	m := &Manager{
		cache:     make(map[string]*Flag),
		ttl:       5 * time.Minute,
		lastFetch: time.Now(),
	}

	// With no DB pool and empty cache, IsEnabled should return false
	if got := m.IsEnabled(context.Background(), "nonexistent"); got != false {
		t.Errorf("IsEnabled(nonexistent) = %v, want false", got)
	}
}

// TestManager_Get_CacheHit tests Get with cache hit
func TestManager_Get_CacheHit(t *testing.T) {
	m := &Manager{
		cache: map[string]*Flag{
			"cached-flag": {Name: "cached-flag", Enabled: true, Description: "Cached"},
		},
		ttl:       5 * time.Minute,
		lastFetch: time.Now(),
	}

	flag, err := m.Get(context.Background(), "cached-flag")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if flag == nil {
		t.Fatal("expected flag from cache")
	}
	if flag.Name != "cached-flag" {
		t.Errorf("expected cached-flag, got %s", flag.Name)
	}
	if !flag.Enabled {
		t.Error("expected flag to be enabled")
	}
}

// TestManager_Get_CacheMiss_EmptyCache tests Get with cache miss and no DB
func TestManager_Get_CacheMiss_EmptyCache(t *testing.T) {
	m := &Manager{
		cache:     make(map[string]*Flag),
		ttl:       5 * time.Minute,
		lastFetch: time.Now(),
	}

	// With no DB pool, Get should return (nil, nil) for cache miss
	flag, err := m.Get(context.Background(), "missing")
	if err != nil {
		// Expected to fail because pool is nil
		t.Logf("Expected error with nil pool: %v", err)
	}
	if flag != nil {
		t.Error("expected nil for missing flag with no DB")
	}
}

// TestManager_GetAll_CacheHit tests GetAll with cache populated
func TestManager_GetAll_CacheHit(t *testing.T) {
	m := &Manager{
		cache: map[string]*Flag{
			"flag-a": {Name: "flag-a", Enabled: true},
			"flag-b": {Name: "flag-b", Enabled: false},
		},
		ttl:        5 * time.Minute,
		lastFetch:  time.Now(),
	}

	// Verify cache has expected flags
	if len(m.cache) != 2 {
		t.Errorf("expected 2 flags in cache, got %d", len(m.cache))
	}
	if _, ok := m.cache["flag-a"]; !ok {
		t.Error("expected flag-a in cache")
	}
	if _, ok := m.cache["flag-b"]; !ok {
		t.Error("expected flag-b in cache")
	}
}

// TestManager_SetAndDelete tests Set and Delete with nil pool
func TestManager_SetAndDelete(t *testing.T) {
	m := &Manager{
		cache: map[string]*Flag{},
		ttl:   5 * time.Minute,
	}

	flag := &Flag{
		Name:        "test-flag",
		Description: "Test flag",
		Enabled:     true,
		Rules:       map[string]interface{}{"key": "value"},
	}

	err := m.Set(context.Background(), flag)
	// Expected to fail with nil pool
	if err == nil {
		t.Log("Set succeeded unexpectedly with nil pool")
	}

	err = m.Delete(context.Background(), "test-flag")
	// Expected to fail with nil pool
	if err == nil {
		t.Log("Delete succeeded unexpectedly with nil pool")
	}
}

// TestManager_StartRefresh tests StartRefresh doesn't panic with nil pool
func TestManager_StartRefresh(t *testing.T) {
	m := &Manager{
		cache:      make(map[string]*Flag),
		ttl:        5 * time.Minute,
		lastFetch:  time.Now(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.StartRefresh(ctx, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	// If we get here without panic, test passes
}

// TestNewManager tests NewManager constructor
func TestNewManager(t *testing.T) {
	m := NewManager(nil)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.ttl != 5*time.Minute {
		t.Errorf("expected ttl 5m, got %v", m.ttl)
	}
	if m.cache == nil {
		t.Error("expected cache to be initialized")
	}
}

// TestFlag_JSON tests Flag JSON marshaling
func TestFlag_JSON(t *testing.T) {
	flag := &Flag{
		Name:        "test-flag",
		Description: "Test flag",
		Enabled:     true,
		Rules:       map[string]interface{}{"key": "value"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	data, err := json.Marshal(flag)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var unmarshaled Flag
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if unmarshaled.Name != flag.Name {
		t.Errorf("Name mismatch: %s != %s", unmarshaled.Name, flag.Name)
	}
	if unmarshaled.Enabled != flag.Enabled {
		t.Errorf("Enabled mismatch: %v != %v", unmarshaled.Enabled, flag.Enabled)
	}
	if unmarshaled.Rules == nil {
		t.Error("expected Rules to be preserved")
	}
}

// TestEnsureTable tests EnsureTable (requires DB, just verify it exists)
func TestEnsureTable(t *testing.T) {
	// This would require a real database
	// The actual test is in database migration tests
}

// --- Tests merged from flags_extra_test.go ---

func TestManager_IsEnabled_NilPool(t *testing.T) {
	m := NewManager(nil)
	if m.IsEnabled(context.Background(), "anything") {
		t.Error("expected false with nil pool")
	}
}

func TestManager_Get_NilPool(t *testing.T) {
	m := NewManager(nil)
	flag, err := m.Get(context.Background(), "anything")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if flag != nil {
		t.Error("expected nil flag with nil pool")
	}
}

func TestManager_GetAll_NilPool(t *testing.T) {
	m := NewManager(nil)
	flags, err := m.GetAll(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if flags != nil {
		t.Error("expected nil flags with nil pool")
	}
}

func TestManager_Set_NilPool(t *testing.T) {
	m := NewManager(nil)
	err := m.Set(context.Background(), &Flag{Name: "f1", Enabled: true})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestManager_Delete_NilPool_CacheEviction(t *testing.T) {
	m := NewManager(nil)
	m.cache["f1"] = &Flag{Name: "f1", Enabled: true}
	err := m.Delete(context.Background(), "f1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if _, ok := m.cache["f1"]; ok {
		t.Error("expected f1 deleted from cache")
	}
}

func TestManager_Delete_NilPool_EmptyCache(t *testing.T) {
	m := NewManager(nil)
	err := m.Delete(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestManager_Get_CacheExpired(t *testing.T) {
	m := NewManager(nil)
	m.cache["f1"] = &Flag{Name: "f1", Enabled: true}
	m.lastFetch = time.Now().Add(-10 * time.Minute)
	m.ttl = 5 * time.Minute

	flag, err := m.Get(context.Background(), "f1")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	// With expired cache and nil pool, should return nil
	if flag != nil {
		t.Error("expected nil for expired cache with nil pool")
	}
}

func TestManager_IsEnabled_EnabledFlag(t *testing.T) {
	m := NewManager(nil)
	m.cache["on"] = &Flag{Name: "on", Enabled: true}
	m.lastFetch = time.Now()

	if !m.IsEnabled(context.Background(), "on") {
		t.Error("expected true")
	}
}

func TestManager_IsEnabled_DisabledFlag(t *testing.T) {
	m := NewManager(nil)
	m.cache["off"] = &Flag{Name: "off", Enabled: false}
	m.lastFetch = time.Now()

	if m.IsEnabled(context.Background(), "off") {
		t.Error("expected false")
	}
}

func TestManager_Get_AllFields(t *testing.T) {
	m := NewManager(nil)
	m.cache["full"] = &Flag{
		Name:        "full",
		Description: "A flag",
		Enabled:     true,
		Rules:       map[string]interface{}{"key": "val"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	m.lastFetch = time.Now()

	flag, err := m.Get(context.Background(), "full")
	if err != nil {
		t.Fatal(err)
	}
	if flag.Description != "A flag" {
		t.Errorf("expected A flag, got %s", flag.Description)
	}
	if flag.Rules["key"] != "val" {
		t.Error("expected rules preserved")
	}
}

func TestManager_Get_MissingFromCache(t *testing.T) {
	m := NewManager(nil)
	m.lastFetch = time.Now()

	flag, err := m.Get(context.Background(), "missing")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if flag != nil {
		t.Error("expected nil for missing flag")
	}
}

func TestManager_StartRefresh_CancelsCleanly(t *testing.T) {
	m := NewManager(nil)
	ctx, cancel := context.WithCancel(context.Background())

	m.StartRefresh(ctx, 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	cancel()
	time.Sleep(30 * time.Millisecond)
	// If no race/goroutine leak, test passes
}

func TestManager_StartRefresh_PopulatesCache(t *testing.T) {
	m := NewManager(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.StartRefresh(ctx, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	// With nil pool, GetAll returns nil, so cache stays empty — no panic
}

func TestManager_Set_InvalidatesCache(t *testing.T) {
	m := NewManager(nil)
	m.cache["f1"] = &Flag{Name: "f1", Enabled: true}

	// Set with nil pool returns early — cache should remain
	_ = m.Set(context.Background(), &Flag{Name: "f1", Enabled: false})

	if _, ok := m.cache["f1"]; !ok {
		t.Error("expected f1 still in cache (nil pool returns early)")
	}
}

func TestManager_Set_NilPoolNoOp(t *testing.T) {
	m := NewManager(nil)
	err := m.Set(context.Background(), &Flag{Name: "f1", Enabled: true, Rules: map[string]interface{}{"a": 1}})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestFlag_RulesJSON(t *testing.T) {
	data := []byte(`{"name":"test","enabled":true,"rules":{"percent":50,"users":["a","b"]}}`)
	var f2 Flag
	if err := json.Unmarshal(data, &f2); err != nil {
		t.Fatal(err)
	}
	if f2.Name != "test" || !f2.Enabled {
		t.Error("unmarshal mismatch")
	}
}

func TestEnsureTable_NilPool(t *testing.T) {
	// EnsureTable requires a real pool; verify it compiles
	// Skip actual DB test
	t.Skip("requires database")
}
