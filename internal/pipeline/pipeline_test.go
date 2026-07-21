package pipeline

import (
	"context"
	"testing"

	"github.com/vigilagent/vigilagent/internal/compliance"
	"github.com/vigilagent/vigilagent/internal/requirements"
	"github.com/vigilagent/vigilagent/internal/schema"
	"github.com/vigilagent/vigilagent/internal/scanner"
)

func newTestPipeline() *Pipeline {
	return NewPipeline(schema.NewValidator(), requirements.NewResolver(), compliance.NewChecker(), scanner.DefaultEngine())
}

func TestPipeline_PaymentWithNoDeclared(t *testing.T) {
	p := newTestPipeline()
	rep := p.Run(context.Background(), &Request{
		Description: "Build a payment processing API that stores card data",
	})

	if rep.Passed {
		t.Fatal("payment with no declared controls should fail")
	}
	if rep.Confidence < 0 || rep.Confidence > 1 {
		t.Fatalf("confidence should be in [0,1], got %f", rep.Confidence)
	}
	if rep.Requirements == nil {
		t.Fatal("requirements result should not be nil")
	}
	if rep.Compliance == nil {
		t.Fatal("compliance result should not be nil")
	}
}

func TestPipeline_StaticSitePasses(t *testing.T) {
	p := newTestPipeline()
	rep := p.Run(context.Background(), &Request{
		Description: "a static marketing homepage",
	})

	if !rep.Passed {
		t.Fatalf("static site should pass, got reasons: %v", rep.Reasons)
	}
	if rep.Confidence != 1.0 {
		t.Fatalf("static site should have confidence 1.0, got %f", rep.Confidence)
	}
}

func TestPipeline_WithSchemaValidation(t *testing.T) {
	p := newTestPipeline()
	rep := p.Run(context.Background(), &Request{
		Description: "Build a general-purpose API",
		Declared:    []string{"encryption", "rate_limit", "audit_log", "access_control", "input_validation", "mfa", "pci_encrypt", "pci_access", "pci_audit", "soc2_logging", "soc2_access", "soc2_mfa", "gdpr_access", "gdpr_consent", "gdpr_minimize", "gdpr_retention", "hipaa_phi"},
		Output: map[string]any{
			"components": []string{"api-gateway"},
			"risks":      []string{"sql-injection"},
		},
	})

	if !rep.Passed {
		t.Fatalf("valid architecture with all controls should pass, got reasons: %v", rep.Reasons)
	}
	if rep.Schema == nil {
		t.Fatal("schema result should not be nil when output provided")
	}
}

func TestPipeline_WithSchemaViolation(t *testing.T) {
	p := newTestPipeline()
	rep := p.Run(context.Background(), &Request{
		Description: "a static marketing homepage",
		Output: map[string]any{
			"description": "just a description",
		},
	})

	// Schema rule for architecture requires "components" and "risks" — output is missing both.
	if rep.Passed {
		t.Fatal("missing required schema fields should cause failure")
	}
	found := false
	for _, r := range rep.Reasons {
		if len(r) > 7 && r[:7] == "schema:" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected schema violation reason, got %v", rep.Reasons)
	}
}

func TestPipeline_LayerResults(t *testing.T) {
	p := newTestPipeline()
	rep := p.Run(context.Background(), &Request{
		Description: "Build a payment processing API",
	})

	if len(rep.Layers) < 2 {
		t.Fatalf("expected at least 2 layers, got %d", len(rep.Layers))
	}
	names := map[string]bool{}
	for _, l := range rep.Layers {
		names[l.Name] = true
	}
	if !names["requirements"] {
		t.Fatal("expected requirements layer")
	}
	if !names["compliance"] {
		t.Fatal("expected compliance layer")
	}
}

func TestPipeline_WithCodeAndEngine(t *testing.T) {
	p := newTestPipeline()
	rep := p.Run(context.Background(), &Request{
		Description: "a static marketing homepage",
		Code:        `q := fmt.Sprintf("SELECT * FROM users WHERE id=%d", id)`,
		Language:    "go",
		Filename:    "app.go",
	})

	if rep.ScanResult == nil {
		t.Fatal("scan result should not be nil when code + engine provided")
	}
	found := false
	for _, l := range rep.Layers {
		if l.Name == "static_analysis" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected static_analysis layer in results")
	}
}

func TestPipeline_InfersPaymentEntity(t *testing.T) {
	entity := inferEntity("Build a payment processing API")
	if entity != "payment" {
		t.Fatalf("expected 'payment', got %q", entity)
	}
}

func TestPipeline_InfersAuthEntity(t *testing.T) {
	entity := inferEntity("Build a user login system")
	if entity != "auth" {
		t.Fatalf("expected 'auth', got %q", entity)
	}
}

func TestPipeline_InfersDefaultEntity(t *testing.T) {
	entity := inferEntity("Build a general-purpose tool")
	if entity != "architecture" {
		t.Fatalf("expected 'architecture', got %q", entity)
	}
}
