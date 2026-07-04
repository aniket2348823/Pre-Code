package schema

import (
	"testing"
)

func TestValidateArchitecture_MissingComponents(t *testing.T) {
	v := NewValidator()
	output := map[string]any{
		"description": "payment gateway",
	}
	rep := v.Validate("architecture", output)
	if rep.Passed {
		t.Fatal("expected violation for missing 'components'")
	}
	if len(rep.Violations) == 0 {
		t.Fatal("expected at least one violation")
	}
	found := false
	for _, viol := range rep.Violations {
		if viol.Field == "components" && viol.Rule == "required_field" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected required_field violation for 'components', got %v", rep.Violations)
	}
}

func TestValidateArchitecture_AllFieldsPresent(t *testing.T) {
	v := NewValidator()
	output := map[string]any{
		"components": []string{"api-gateway", "payment-service"},
		"risks":      []string{"sql-injection"},
	}
	rep := v.Validate("architecture", output)
	if !rep.Passed {
		t.Fatalf("expected pass, got violations: %v", rep.Violations)
	}
}

func TestValidateArchitecture_UnknownTopLevelField(t *testing.T) {
	v := NewValidator()
	output := map[string]any{
		"components":  []string{"api-gateway"},
		"risks":       []string{},
		"experimental": true,
	}
	rep := v.Validate("architecture", output)
	if rep.Passed {
		t.Fatal("expected violation for unknown top-level field")
	}
	found := false
	for _, viol := range rep.Violations {
		if viol.Field == "experimental" && viol.Rule == "unknown_field" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unknown_field violation for 'experimental', got %v", rep.Violations)
	}
}

func TestValidateFinding_MissingSeverity(t *testing.T) {
	v := NewValidator()
	output := map[string]any{
		"message": "SQL injection detected",
		"file":    "app.py",
	}
	rep := v.Validate("finding", output)
	if rep.Passed {
		t.Fatal("expected violation for missing 'severity'")
	}
}

func TestValidateFinding_AllPresent(t *testing.T) {
	v := NewValidator()
	output := map[string]any{
		"severity": "critical",
		"message":  "SQL injection detected",
		"file":     "app.py",
	}
	rep := v.Validate("finding", output)
	if !rep.Passed {
		t.Fatalf("expected pass, got %v", rep.Violations)
	}
}

func TestValidateUnknownEntity_Passes(t *testing.T) {
	v := NewValidator()
	output := map[string]any{"anything": "goes"}
	rep := v.Validate("nonexistent_entity", output)
	if !rep.Passed {
		t.Fatal("unknown entity should pass by default")
	}
}

func TestValidateMaxDepth(t *testing.T) {
	v := NewValidatorWithRules([]Rule{
		{Entity: "deep", MaxDepth: 2},
	})
	output := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": "too deep",
			},
		},
	}
	rep := v.Validate("deep", output)
	if rep.Passed {
		t.Fatal("expected violation for exceeding max depth")
	}
}

func TestComputeDepth(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected int
	}{
		{"flat", map[string]any{"a": 1}, 1},
		{"nested2", map[string]any{"a": map[string]any{"b": 1}}, 2},
		{"nested3", map[string]any{"a": map[string]any{"b": map[string]any{"c": 1}}}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := computeDepth(tt.input)
			if d != tt.expected {
				t.Errorf("computeDepth = %d, want %d", d, tt.expected)
			}
		})
	}
}
