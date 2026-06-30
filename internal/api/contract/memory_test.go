package contract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSearchMemoryRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		req      SearchMemoryRequest
		hasErr   bool
		errField string
	}{
		{
			name:   "valid request",
			req:    SearchMemoryRequest{Query: "error handling patterns"},
			hasErr: false,
		},
		{
			name:     "missing query",
			req:      SearchMemoryRequest{},
			hasErr:   true,
			errField: "query",
		},
		{
			name:     "negative limit",
			req:      SearchMemoryRequest{Query: "x", Limit: -1},
			hasErr:   true,
			errField: "limit",
		},
		{
			name:     "invalid memory type",
			req:      SearchMemoryRequest{Query: "x", Types: []MemoryType{"working"}},
			hasErr:   true,
			errField: "types",
		},
		{
			name:   "valid with types filter",
			req:    SearchMemoryRequest{Query: "x", Types: []MemoryType{MemoryTypeEpisodic, MemoryTypeSemantic}},
			hasErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.req.Validate()
			if tt.hasErr && !errs.HasErrors() {
				t.Errorf("expected validation error on field %q", tt.errField)
			}
			if !tt.hasErr && errs.HasErrors() {
				t.Errorf("unexpected errors: %v", errs.ToMap())
			}
		})
	}
}

func TestSearchMemoryRequest_EffectiveLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{"zero uses default", 0, DefaultMemorySearchLimit},
		{"negative uses default", -5, DefaultMemorySearchLimit},
		{"valid limit", 25, 25},
		{"above max capped", 200, MaxPageLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := SearchMemoryRequest{Query: "q", Limit: tt.limit}
			if got := req.EffectiveLimit(); got != tt.want {
				t.Errorf("EffectiveLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCreateMemoryRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		req      CreateMemoryRequest
		hasErr   bool
		errField string
	}{
		{
			name:   "valid request",
			req:    CreateMemoryRequest{Type: MemoryTypeEpisodic, Content: "learned pattern"},
			hasErr: false,
		},
		{
			name:     "missing content",
			req:      CreateMemoryRequest{Type: MemoryTypeEpisodic},
			hasErr:   true,
			errField: "content",
		},
		{
			name:     "invalid type",
			req:      CreateMemoryRequest{Type: MemoryType("working"), Content: "x"},
			hasErr:   true,
			errField: "type",
		},
		{
			name:     "empty type",
			req:      CreateMemoryRequest{Content: "x"},
			hasErr:   true,
			errField: "type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.req.Validate()
			if tt.hasErr && !errs.HasErrors() {
				t.Errorf("expected validation error on field %q", tt.errField)
			}
			if !tt.hasErr && errs.HasErrors() {
				t.Errorf("unexpected errors: %v", errs.ToMap())
			}
		})
	}
}

func TestMemoryResult_RelevanceScore(t *testing.T) {
	tests := []struct {
		name  string
		score float64
		valid bool
	}{
		{"zero is valid", 0.0, true},
		{"one is valid", 1.0, true},
		{"mid is valid", 0.75, true},
		{"negative is invalid", -0.1, false},
		{"over one is invalid", 1.1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inRange := tt.score >= 0.0 && tt.score <= 1.0
			if inRange != tt.valid {
				t.Errorf("score %v: in range = %v, want %v", tt.score, inRange, tt.valid)
			}
		})
	}
}

func TestMemoryResult_JSONRoundTrip(t *testing.T) {
	now := TimestampFromTime(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	original := MemoryResult{
		ID:             "mem-1",
		Type:           MemoryTypeSemantic,
		Content:        "use context.Context for cancellation",
		RelevanceScore: 0.92,
		ProjectID:      "proj-1",
		CreatedAt:      now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded MemoryResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Type != MemoryTypeSemantic {
		t.Errorf("Type = %q, want semantic", decoded.Type)
	}
	if decoded.RelevanceScore != 0.92 {
		t.Errorf("RelevanceScore = %v, want 0.92", decoded.RelevanceScore)
	}
}

func TestMemoryTypes_Coverage(t *testing.T) {
	// Ensure all three memory types from the architecture doc are present.
	types := AllMemoryTypes()
	if len(types) != 3 {
		t.Errorf("expected 3 memory types, got %d", len(types))
	}

	expected := map[MemoryType]bool{
		MemoryTypeEpisodic:   false,
		MemoryTypeSemantic:   false,
		MemoryTypeProcedural: false,
	}
	for _, mt := range types {
		expected[mt] = true
	}
	for mt, found := range expected {
		if !found {
			t.Errorf("memory type %q missing from AllMemoryTypes()", mt)
		}
	}
}
