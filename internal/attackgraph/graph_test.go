package attackgraph

import (
	"context"
	"testing"
)

func TestEngine_Generate(t *testing.T) {
	engine := NewEngine()
	ctx := context.Background()

	tests := []struct {
		name     string
		req      FindingsRequest
		wantPath int
	}{
		{
			name: "payment SQL injection finding",
			req: FindingsRequest{
				Description: "Payment processing system",
				Findings: []FindingInput{
					{Title: "SQL Injection in payment form", Severity: "critical"},
				},
				Entity: "payment",
			},
			wantPath: 1,
		},
		{
			name: "auth broken authentication",
			req: FindingsRequest{
				Description: "Login system",
				Findings: []FindingInput{
					{Title: "Broken authentication on login", Severity: "critical"},
				},
				Entity: "auth",
			},
			wantPath: 1,
		},
		{
			name: "no matching findings",
			req: FindingsRequest{
				Description: "General application",
				Findings: []FindingInput{
					{Title: "Minor UI issue", Severity: "low"},
				},
				Entity: "general",
			},
			wantPath: 1, // generic path
		},
		{
			name: "empty findings",
			req: FindingsRequest{
				Description: "Test system",
			},
			wantPath: 0,
		},
		{
			name: "multiple findings",
			req: FindingsRequest{
				Description: "Payment API",
				Findings: []FindingInput{
					{Title: "SQL Injection in payment", Severity: "critical"},
					{Title: "Hardcoded secret in config", Severity: "high"},
				},
				Entity: "payment",
			},
			wantPath: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := engine.Generate(ctx, tt.req)
			if len(resp.Paths) != tt.wantPath {
				t.Errorf("got %d paths, want %d", len(resp.Paths), tt.wantPath)
			}
			if resp.Summary == "" {
				t.Error("summary should not be empty")
			}
		})
	}
}

func TestEngine_InferEntity(t *testing.T) {
	tests := []struct {
		desc string
		want string
	}{
		{"Payment processing system", "payment"},
		{"Authentication service", "auth"},
		{"User management", "user"},
		{"REST API endpoints", "api"},
		{"General application", "general"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := inferEntity(tt.desc)
			if got != tt.want {
				t.Errorf("inferEntity(%q) = %q, want %q", tt.desc, got, tt.want)
			}
		})
	}
}

func TestEngine_BuildPath(t *testing.T) {
	engine := NewEngine()
	ctx := context.Background()

	req := FindingsRequest{
		Description: "Payment processing system",
		Findings: []FindingInput{
			{
				Title:       "SQL Injection in payment",
				Description: "User input directly concatenated into SQL query",
				Severity:    "critical",
				Category:    "injection",
			},
		},
		Entity: "payment",
	}

	resp := engine.Generate(ctx, req)
	if len(resp.Paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(resp.Paths))
	}

	path := resp.Paths[0]
	if path.ID == "" {
		t.Error("path ID should not be empty")
	}
	if len(path.Steps) < 2 {
		t.Errorf("expected at least 2 steps, got %d", len(path.Steps))
	}
	if path.Impact == "" {
		t.Error("path impact should not be empty")
	}
}

func TestEngine_GenericPath(t *testing.T) {
	engine := NewEngine()
	ctx := context.Background()

	req := FindingsRequest{
		Description: "Unknown system",
		Findings: []FindingInput{
			{Title: "Something weird", Severity: "medium"},
		},
	}

	resp := engine.Generate(ctx, req)
	if len(resp.Paths) != 1 {
		t.Fatalf("expected 1 generic path, got %d", len(resp.Paths))
	}

	path := resp.Paths[0]
	if path.ID != "generic-exploitation" {
		t.Errorf("expected generic path ID, got %q", path.ID)
	}
}

func TestEngine_Metadata(t *testing.T) {
	engine := NewEngine()
	ctx := context.Background()

	req := FindingsRequest{
		Description: "Test system",
		Findings: []FindingInput{
			{Title: "Test finding", Severity: "medium"},
		},
	}

	resp := engine.Generate(ctx, req)
	if resp.Metadata == nil {
		t.Error("metadata should not be nil")
	}
	if resp.Metadata["entity"] == "" {
		t.Error("metadata should include entity")
	}
}
