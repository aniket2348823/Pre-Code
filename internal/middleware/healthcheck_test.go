package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestHealthCache_SetGet(t *testing.T) {
	cache := NewHealthCache(100 * time.Millisecond)

	resp := HealthResponse{
		Status:    HealthStatusHealthy,
		Timestamp: time.Now(),
		Version:   "1.0.0",
	}

	cache.Set("key1", resp)

	got, ok := cache.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", got.Version)
	}
}

func TestHealthCache_Expiry(t *testing.T) {
	cache := NewHealthCache(50 * time.Millisecond)

	resp := HealthResponse{Status: HealthStatusHealthy}
	cache.Set("key1", resp)

	time.Sleep(60 * time.Millisecond)

	_, ok := cache.Get("key1")
	if ok {
		t.Fatal("expected cache miss after expiry")
	}
}

func TestHealthCache_ConcurrentAccess(t *testing.T) {
	cache := NewHealthCache(1 * time.Second)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "key"
			resp := HealthResponse{Status: HealthStatusHealthy, Version: "1.0.0"}
			cache.Set(key, resp)
			cache.Get(key)
		}(i)
	}
	wg.Wait()
}

func TestCheckDependencies_AllHealthy(t *testing.T) {
	checkers := []DependencyChecker{
		{Name: "dep1", CheckFn: func(ctx context.Context) error { return nil }},
		{Name: "dep2", CheckFn: func(ctx context.Context) error { return nil }},
	}

	deps, allHealthy := checkDependencies(context.Background(), checkers)
	if !allHealthy {
		t.Fatal("expected allHealthy = true")
	}
	if deps["dep1"].Status != HealthStatusHealthy {
		t.Errorf("dep1 status = %q, want healthy", deps["dep1"].Status)
	}
	if deps["dep2"].Status != HealthStatusHealthy {
		t.Errorf("dep2 status = %q, want healthy", deps["dep2"].Status)
	}
}

func TestCheckDependencies_OneDown(t *testing.T) {
	checkers := []DependencyChecker{
		{Name: "good", CheckFn: func(ctx context.Context) error { return nil }},
		{Name: "bad", CheckFn: func(ctx context.Context) error { return context.DeadlineExceeded }},
	}

	deps, allHealthy := checkDependencies(context.Background(), checkers)
	if allHealthy {
		t.Fatal("expected allHealthy = false")
	}
	if deps["good"].Status != HealthStatusHealthy {
		t.Errorf("good status = %q, want healthy", deps["good"].Status)
	}
	if deps["bad"].Status != HealthStatusDown {
		t.Errorf("bad status = %q, want down", deps["bad"].Status)
	}
	if deps["bad"].Error == "" {
		t.Error("expected error message for down dependency")
	}
}

func TestCheckDependencies_LatencyMeasured(t *testing.T) {
	checkers := []DependencyChecker{
		{Name: "slow", CheckFn: func(ctx context.Context) error {
			time.Sleep(10 * time.Millisecond)
			return nil
		}},
	}

	deps, _ := checkDependencies(context.Background(), checkers)
	if deps["slow"].Latency < 5 {
		t.Errorf("latency = %dms, want >= 5ms", deps["slow"].Latency)
	}
}

func TestCachedHealthHandler_Liveness(t *testing.T) {
	cache := NewHealthCache(5 * time.Second)
	handler := CachedHealthHandler("1.0.0", cache, "liv", nil, true)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != HealthStatusHealthy {
		t.Errorf("status = %q, want healthy", resp.Status)
	}
	if resp.Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", resp.Version)
	}
	if resp.Dependencies != nil {
		t.Error("liveness should not include dependencies")
	}
}

func TestCachedHealthHandler_ReadinessAllHealthy(t *testing.T) {
	cache := NewHealthCache(5 * time.Second)
	checkers := []DependencyChecker{
		{Name: "db", CheckFn: func(ctx context.Context) error { return nil }},
	}
	handler := CachedHealthHandler("1.0.0", cache, "ready", checkers, false)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != HealthStatusHealthy {
		t.Errorf("status = %q, want healthy", resp.Status)
	}
	if resp.Dependencies["db"].Status != HealthStatusHealthy {
		t.Errorf("db status = %q, want healthy", resp.Dependencies["db"].Status)
	}
}

func TestCachedHealthHandler_ReadinessDepDown(t *testing.T) {
	cache := NewHealthCache(5 * time.Second)
	checkers := []DependencyChecker{
		{Name: "db", CheckFn: func(ctx context.Context) error { return context.DeadlineExceeded }},
	}
	handler := CachedHealthHandler("1.0.0", cache, "ready2", checkers, false)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != HealthStatusDown {
		t.Errorf("status = %q, want down", resp.Status)
	}
}

func TestCachedHealthHandler_CacheHit(t *testing.T) {
	cache := NewHealthCache(5 * time.Second)
	handler := CachedHealthHandler("1.0.0", cache, "cached", nil, true)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req)

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req)

	var resp1, resp2 HealthResponse
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	json.Unmarshal(w2.Body.Bytes(), &resp2)

	if resp1.Version != resp2.Version {
		t.Error("expected same version from cache")
	}
}

func TestCachedHealthHandler_NoCache(t *testing.T) {
	handler := CachedHealthHandler("1.0.0", nil, "nocache", nil, true)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHealthResponse_JSONRoundTrip(t *testing.T) {
	orig := HealthResponse{
		Status:    HealthStatusHealthy,
		Timestamp: time.Now().Truncate(time.Millisecond),
		Version:   "1.2.3",
		Dependencies: map[string]DepHealth{
			"postgres": {Status: HealthStatusHealthy, Latency: 2},
			"redis":    {Status: HealthStatusDown, Latency: 0, Error: "connection refused"},
		},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded HealthResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Status != HealthStatusHealthy {
		t.Errorf("Status = %q, want healthy", decoded.Status)
	}
	if decoded.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", decoded.Version)
	}
	if decoded.Dependencies["redis"].Error != "connection refused" {
		t.Errorf("redis error = %q, want 'connection refused'", decoded.Dependencies["redis"].Error)
	}
}
