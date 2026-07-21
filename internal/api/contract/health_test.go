package contract

import (
	"encoding/json"
	"testing"
)

func TestHealthResponse_JSONRoundTrip(t *testing.T) {
	original := HealthResponse{
		Status:  ServiceHealthy,
		Version: "1.0.0",
		Uptime:  "72h15m",
		Services: map[string]ServiceHealth{
			"postgresql": {Status: ServiceHealthy, LatencyMs: 2},
			"redis":      {Status: ServiceHealthy, LatencyMs: 1},
			"nats":       {Status: ServiceDegraded, LatencyMs: 50},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded HealthResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Status != ServiceHealthy {
		t.Errorf("Status = %q, want healthy", decoded.Status)
	}
	if decoded.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", decoded.Version)
	}
	if len(decoded.Services) != 3 {
		t.Fatalf("Services len = %d, want 3", len(decoded.Services))
	}
	if decoded.Services["redis"].LatencyMs != 1 {
		t.Errorf("redis latency = %d, want 1", decoded.Services["redis"].LatencyMs)
	}
}

func TestServiceHealth_StatusValues(t *testing.T) {
	tests := []struct {
		status ServiceStatus
		valid  bool
	}{
		{ServiceHealthy, true},
		{ServiceDegraded, true},
		{ServiceDown, true},
		{ServiceStatus("unknown"), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			isKnown := tt.status == ServiceHealthy || tt.status == ServiceDegraded || tt.status == ServiceDown
			if isKnown != tt.valid {
				t.Errorf("ServiceStatus(%q) known = %v, want %v", tt.status, isKnown, tt.valid)
			}
		})
	}
}

func TestHealthResponse_OverallStatus(t *testing.T) {
	tests := []struct {
		name     string
		services map[string]ServiceHealth
		want     ServiceStatus
	}{
		{
			name: "all healthy",
			services: map[string]ServiceHealth{
				"postgresql": {Status: ServiceHealthy},
				"redis":      {Status: ServiceHealthy},
			},
			want: ServiceHealthy,
		},
		{
			name: "one degraded",
			services: map[string]ServiceHealth{
				"postgresql": {Status: ServiceHealthy},
				"redis":      {Status: ServiceDegraded},
			},
			want: ServiceDegraded,
		},
		{
			name: "one down overrides degraded",
			services: map[string]ServiceHealth{
				"postgresql": {Status: ServiceDown},
				"redis":      {Status: ServiceDegraded},
				"nats":       {Status: ServiceHealthy},
			},
			want: ServiceDown,
		},
		{
			name:     "empty services is healthy",
			services: map[string]ServiceHealth{},
			want:     ServiceHealthy,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := HealthResponse{Services: tt.services}
			if got := h.OverallStatus(); got != tt.want {
				t.Errorf("OverallStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}
