package attackgraph

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		{"Database connection pool", "database"},
		{"Login page redesign", "auth"},
		{"db migration script", "database"},
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

func TestGenericPath_Direct(t *testing.T) {
	result := genericPath(FindingsRequest{Findings: nil})
	if result != nil {
		t.Error("nil findings should return nil")
	}
}

func TestGenericPath_WithData(t *testing.T) {
	result := genericPath(FindingsRequest{
		Findings: []FindingInput{{Title: "XSS vulnerability", Severity: "high"}},
	})
	if result == nil {
		t.Fatal("expected generic path")
	}
	if result.ID != "generic-exploitation" {
		t.Errorf("expected generic ID, got %s", result.ID)
	}
	if len(result.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(result.Steps))
	}
}

func TestSummarize_Empty(t *testing.T) {
	engine := NewEngine()
	s := engine.summarize(nil, "api")
	if s != "No attack paths identified" {
		t.Errorf("expected no attack paths, got %q", s)
	}
}

func TestMatchesRule_EmptyRule(t *testing.T) {
	r := graphRule{}
	f := FindingInput{Title: "test"}
	if !matchesRule(r, "anything", f) {
		t.Error("empty rule should match everything")
	}
}

func TestMatchesRule_EntityMismatch(t *testing.T) {
	r := graphRule{entity: "payment"}
	f := FindingInput{Title: "test"}
	if matchesRule(r, "auth", f) {
		t.Error("entity mismatch should not match")
	}
}

func TestMatchesRule_FindingMismatch(t *testing.T) {
	r := graphRule{finding: "sql injection"}
	f := FindingInput{Title: "xss vulnerability"}
	if matchesRule(r, "payment", f) {
		t.Error("finding mismatch should not match")
	}
}

func TestBuildPath(t *testing.T) {
	r := graphRule{
		pathName: "Test Path",
		impact:   "high",
		severity: "critical",
		steps:    []AttackStep{{Index: 1, Action: "step1"}},
	}
	f := FindingInput{Title: "finding1"}
	path := buildPath(r, f)
	if path.Name != "Test Path" {
		t.Errorf("expected Test Path, got %s", path.Name)
	}
	if path.ID != "test-path" {
		t.Errorf("expected test-path, got %s", path.ID)
	}
}

func TestHandler_EmptyBody(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandler_MultipleFindings(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())
	body, _ := json.Marshal(FindingsRequest{
		Description: "Payment API",
		Findings: []FindingInput{
			{Title: "SQL Injection in payment", Severity: "critical"},
			{Title: "Hardcoded secret in payment", Severity: "high"},
		},
		Entity: "payment",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp GraphResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Paths) < 1 {
		t.Error("expected at least 1 path")
	}
}

func TestHandler_BrokenAuth_InferEntity(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())
	body, _ := json.Marshal(FindingsRequest{
		Description: "Login page with broken auth",
		Findings: []FindingInput{
			{Title: "Broken authentication", Severity: "critical"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp GraphResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Paths) != 1 {
		t.Errorf("expected 1 path for broken auth, got %d", len(resp.Paths))
	}
}

func TestEngine_CredentialLeak(t *testing.T) {
	engine := NewEngine()
	ctx := context.Background()
	req := FindingsRequest{
		Description: "Auth system",
		Findings: []FindingInput{
			{Title: "Credential leak in logs", Severity: "high"},
		},
		Entity: "auth",
	}
	resp := engine.Generate(ctx, req)
	if len(resp.Paths) != 1 {
		t.Errorf("expected 1 path, got %d", len(resp.Paths))
	}
}

func TestEngine_APIRateLimit(t *testing.T) {
	engine := NewEngine()
	ctx := context.Background()
	req := FindingsRequest{
		Description: "API gateway",
		Findings: []FindingInput{
			{Title: "Missing rate limit on login", Severity: "high"},
		},
		Entity: "api",
	}
	resp := engine.Generate(ctx, req)
	if len(resp.Paths) != 1 {
		t.Errorf("expected 1 path, got %d", len(resp.Paths))
	}
}

func TestEngine_DuplicatePathDedup(t *testing.T) {
	engine := NewEngine()
	ctx := context.Background()
	req := FindingsRequest{
		Description: "Payment system",
		Findings: []FindingInput{
			{Title: "SQL Injection in payment", Severity: "critical"},
			{Title: "SQL Injection in checkout", Severity: "critical"},
		},
		Entity: "payment",
	}
	resp := engine.Generate(ctx, req)
	seen := map[string]bool{}
	for _, p := range resp.Paths {
		if seen[p.ID] {
			t.Errorf("duplicate path ID: %s", p.ID)
		}
		seen[p.ID] = true
	}
}
