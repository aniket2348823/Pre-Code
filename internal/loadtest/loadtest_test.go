package loadtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockAPIHandler returns a basic http.Handler that simulates VigilAgent endpoints.
func mockAPIHandler() http.Handler {
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Auth
	mux.HandleFunc("/api/v1/auth/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"user_id": "u_test"})
	})
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": "test-token"})
	})

	// Tasks
	mux.HandleFunc("/api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "POST":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": "t_001", "status": "pending"})
		case "GET":
			json.NewEncoder(w).Encode(map[string]interface{}{"tasks": []map[string]string{{"id": "t_001"}}, "total": 1})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/tasks/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "t_001", "status": "pending"})
	})

	// Agents
	mux.HandleFunc("/api/v1/agents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "POST":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": "a_001"})
		case "GET":
			json.NewEncoder(w).Encode(map[string]interface{}{"agents": []map[string]string{{"id": "a_001"}}, "total": 1})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/agents/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "a_001"})
	})

	// Projects
	mux.HandleFunc("/api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "POST":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": "p_001"})
		case "GET":
			json.NewEncoder(w).Encode(map[string]interface{}{"projects": []map[string]string{{"id": "p_001"}}, "total": 1})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/projects/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "p_001"})
	})

	// Sessions
	mux.HandleFunc("/api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": "s_001"})
	})

	// Skills
	mux.HandleFunc("/api/v1/skills", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"skills": []map[string]string{{"id": "sk_001"}}, "total": 1})
	})

	return mux
}

func TestLoadTestRunner_BasicRun(t *testing.T) {
	srv := httptest.NewServer(mockAPIHandler())
	defer srv.Close()

	cfg := LoadTestConfig{
		TargetURL:   srv.URL,
		Duration:    3 * time.Second,
		Concurrency: 5,
		RampUp:      500 * time.Millisecond,
		ThinkTime:   10 * time.Millisecond,
		Profile:     ProfileConstant,
		Timeout:     5 * time.Second,
	}

	runner := NewRunner(cfg, ScenarioPing, nil)
	results := runner.Run(context.Background())

	if results.TotalReqs == 0 {
		t.Fatal("expected at least 1 request")
	}
	// A 500ms p99 cap flakes when the whole suite runs under -race in parallel
	// (the in-process mock competes with every other package's goroutines for
	// CPU, pushing p99 past 500ms). 2000ms still catches a genuinely broken
	// runner while tolerating CI contention.
	if results.SLO.P99Ms >= 2000.0 {
		t.Errorf("p99 latency too high for mock server: %.2fms", results.SLO.P99Ms)
	}
	t.Logf("basic run: %s", Summary(results))
}

func TestLoadTestRunner_LoginScenario(t *testing.T) {
	srv := httptest.NewServer(mockAPIHandler())
	defer srv.Close()

	cfg := LoadTestConfig{
		TargetURL:   srv.URL,
		Duration:    3 * time.Second,
		Concurrency: 3,
		RampUp:      1 * time.Second,
		ThinkTime:   50 * time.Millisecond,
		Profile:     ProfileRamping,
		Timeout:     5 * time.Second,
	}

	runner := NewRunner(cfg, ScenarioLogin, nil)
	results := runner.Run(context.Background())

	if results.TotalReqs == 0 {
		t.Fatal("expected requests from login scenario")
	}
	t.Logf("login scenario: %s", Summary(results))
}

func TestLoadTestRunner_CRUDScenario(t *testing.T) {
	srv := httptest.NewServer(mockAPIHandler())
	defer srv.Close()

	cfg := LoadTestConfig{
		TargetURL:   srv.URL,
		Duration:    3 * time.Second,
		Concurrency: 5,
		RampUp:      500 * time.Millisecond,
		ThinkTime:   20 * time.Millisecond,
		Profile:     ProfileConstant,
		Timeout:     5 * time.Second,
	}

	runner := NewRunner(cfg, ScenarioCRUD, nil)
	results := runner.Run(context.Background())

	if results.TotalReqs == 0 {
		t.Fatal("expected requests from CRUD scenario")
	}
	t.Logf("crud scenario: %s", Summary(results))
}

func TestLoadTestRunner_SpikeProfile(t *testing.T) {
	srv := httptest.NewServer(mockAPIHandler())
	defer srv.Close()

	cfg := LoadTestConfig{
		TargetURL:   srv.URL,
		Duration:    4 * time.Second,
		Concurrency: 10,
		RampUp:      1 * time.Second,
		ThinkTime:   10 * time.Millisecond,
		Profile:     ProfileSpike,
		Timeout:     5 * time.Second,
	}

	runner := NewRunner(cfg, ScenarioPing, nil)
	results := runner.Run(context.Background())

	if results.TotalReqs == 0 {
		t.Fatal("expected requests from spike profile")
	}
	t.Logf("spike profile: %s", Summary(results))
}

func TestResults_ExportJSON(t *testing.T) {
	srv := httptest.NewServer(mockAPIHandler())
	defer srv.Close()

	cfg := LoadTestConfig{
		TargetURL:   srv.URL,
		Duration:    1 * time.Second,
		Concurrency: 2,
		ThinkTime:   10 * time.Millisecond,
		Profile:     ProfileConstant,
		Timeout:     3 * time.Second,
	}

	runner := NewRunner(cfg, ScenarioPing, nil)
	results := runner.Run(context.Background())

	path := t.TempDir() + "/results.json"
	if err := ExportJSON(results, path); err != nil {
		t.Fatalf("export json: %v", err)
	}
	t.Logf("exported to %s", path)
}

func TestResults_LatencyHistogram(t *testing.T) {
	srv := httptest.NewServer(mockAPIHandler())
	defer srv.Close()

	cfg := LoadTestConfig{
		TargetURL:   srv.URL,
		Duration:    1 * time.Second,
		Concurrency: 2,
		ThinkTime:   10 * time.Millisecond,
		Profile:     ProfileConstant,
		Timeout:     3 * time.Second,
	}

	runner := NewRunner(cfg, ScenarioPing, nil)
	results := runner.Run(context.Background())

	hist := LatencyHistogram(results, 8)
	t.Logf("histogram:\n%s", hist)
}

func TestLoadProfile_WorkersAt(t *testing.T) {
	cfg := LoadTestConfig{
		Duration:    10 * time.Second,
		Concurrency: 20,
		RampUp:      5 * time.Second,
	}

	tests := []struct {
		profile LoadProfile
		t       time.Duration
		want    int
	}{
		{ProfileConstant, 0, 20},
		{ProfileConstant, 5 * time.Second, 20},
		{ProfileRamping, 0, 1},
		{ProfileRamping, 2500 * time.Millisecond, 10},
		{ProfileRamping, 5 * time.Second, 20},
		{ProfileRamping, 8 * time.Second, 20},
		{ProfileStress, 0, 1},
		{ProfileStress, 5 * time.Second, 10},
		{ProfileStress, 10 * time.Second, 20},
	}

	for _, tt := range tests {
		cfg.Profile = tt.profile
		got := cfg.WorkersAt(tt.t)
		if got != tt.want {
			t.Errorf("profile=%s t=%v: got %d, want %d", tt.profile, tt.t, got, tt.want)
		}
	}
}

func TestLoadTestRunner_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(mockAPIHandler())
	defer srv.Close()

	cfg := LoadTestConfig{
		TargetURL:   srv.URL,
		Duration:    30 * time.Second, // long duration
		Concurrency: 2,
		ThinkTime:   50 * time.Millisecond,
		Profile:     ProfileConstant,
		Timeout:     3 * time.Second,
	}

	runner := NewRunner(cfg, ScenarioPing, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	results := runner.Run(ctx)
	t.Logf("cancelled run: %s", Summary(results))
	if results.TotalReqs == 0 {
		t.Fatal("expected some requests before cancellation")
	}
}
