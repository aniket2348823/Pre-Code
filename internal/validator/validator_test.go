package validator

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateRequired(t *testing.T) {
	rules := []Rule{{Field: "name", Required: true, MaxLen: 100}}
	result := Validate(map[string]interface{}{}, rules)
	if result.Valid {
		t.Error("expected invalid for missing required field")
	}
	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}
}

func TestValidateMinLen(t *testing.T) {
	rules := []Rule{{Field: "name", MinLen: 3}}
	result := Validate(map[string]interface{}{"name": "ab"}, rules)
	if result.Valid {
		t.Error("expected invalid for short field")
	}
}

func TestValidateMaxLen(t *testing.T) {
	rules := []Rule{{Field: "name", MaxLen: 5}}
	result := Validate(map[string]interface{}{"name": "toolongvalue"}, rules)
	if result.Valid {
		t.Error("expected invalid for long field")
	}
}

func TestValidatePattern(t *testing.T) {
	rules := []Rule{{Field: "slug", Pattern: `^[a-z0-9-]+$`}}
	result := Validate(map[string]interface{}{"slug": "valid-slug"}, rules)
	if !result.Valid {
		t.Error("expected valid slug")
	}
	result = Validate(map[string]interface{}{"slug": "Invalid Slug!"}, rules)
	if result.Valid {
		t.Error("expected invalid slug")
	}
}

func TestValidateCustom(t *testing.T) {
	rules := []Rule{{
		Field: "email",
		Custom: func(value interface{}) error {
			s, _ := value.(string)
			if !strings.Contains(s, "@") {
				return fmt.Errorf("invalid email")
			}
			return nil
		},
	}}
	result := Validate(map[string]interface{}{"email": "not-an-email"}, rules)
	if result.Valid {
		t.Error("expected invalid email")
	}
}

func TestValidateValidInput(t *testing.T) {
	rules := []Rule{
		{Field: "name", Required: true, MinLen: 1, MaxLen: 100},
		{Field: "slug", Pattern: `^[a-z0-9-]+$`},
	}
	result := Validate(map[string]interface{}{
		"name": "My Project",
		"slug": "my-project",
	}, rules)
	if !result.Valid {
		t.Errorf("expected valid, got %d errors", len(result.Errors))
	}
}

func TestValidateEmail(t *testing.T) {
	if err := ValidateEmail("user@example.com"); err != nil {
		t.Errorf("expected valid email, got %v", err)
	}
	if err := ValidateEmail("not-email"); err == nil {
		t.Error("expected invalid email")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("123456789012"); err != nil {
		t.Errorf("expected valid password, got %v", err)
	}
	if err := ValidatePassword("short"); err == nil {
		t.Error("expected invalid short password")
	}
}

func TestValidateSlug(t *testing.T) {
	if err := ValidateSlug("my-project"); err != nil {
		t.Errorf("expected valid slug, got %v", err)
	}
	if err := ValidateSlug("My Project!"); err == nil {
		t.Error("expected invalid slug")
	}
}

func TestValidateLanguage(t *testing.T) {
	if err := ValidateLanguage("go"); err != nil {
		t.Errorf("expected valid language, got %v", err)
	}
	if err := ValidateLanguage("brainfuck"); err == nil {
		t.Error("expected invalid language")
	}
}

func TestValidateNilValue(t *testing.T) {
	rules := []Rule{{Field: "name", Required: false}}
	result := Validate(map[string]interface{}{"name": nil}, rules)
	if !result.Valid {
		t.Error("expected valid for optional nil")
	}
}

func TestValidate_NilFields(t *testing.T) {
	rules := []Rule{{Field: "name", Required: true}}
	result := Validate(nil, rules)
	if result.Valid {
		t.Error("nil fields should be invalid for required rule")
	}
}

func TestValidate_NilRules(t *testing.T) {
	result := Validate(map[string]interface{}{"name": "test"}, nil)
	if !result.Valid {
		t.Error("nil rules should be valid")
	}
}

func TestValidateEmail_EdgeCases(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"", true}, {"user@", true}, {"@domain.com", true},
		{"user@.com", true}, {"user@domain", true},
		{"user@domain.com", false}, {"test@test.co", false},
	}
	for _, tt := range tests {
		err := ValidateEmail(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateEmail(%q) err=%v, wantErr=%v", tt.input, err, tt.wantErr)
		}
	}
}

func TestValidatePassword_Boundaries(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"", true}, {"short", true}, {"12345678901", true},
		{"123456789012", false}, {"Abcdefghijk1", false},
		{"Unicode密码123", false}, {"            ", false},
		{strings.Repeat("a", 10000), false},
	}
	for _, tt := range tests {
		err := ValidatePassword(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidatePassword(%q) err=%v, wantErr=%v", tt.input, err, tt.wantErr)
		}
	}
}

func TestValidateSlug_EdgeCases(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"", true}, {"a", false}, {"a-b-c-d-e", false},
		{"UPPER-CASE", true}, {"my project", true},
		{"special!@#", true}, {"a_b", true},
		{"123", false}, {"valid-slug", false},
	}
	for _, tt := range tests {
		err := ValidateSlug(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateSlug(%q) err=%v, wantErr=%v", tt.input, err, tt.wantErr)
		}
	}
}

func TestValidateLanguage_EdgeCases(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"go", false}, {"python", false}, {"javascript", false},
		{"Go", false}, {"PYTHON", false}, {"", true},
		{"brainfuck", true}, {"cobol", true},
	}
	for _, tt := range tests {
		err := ValidateLanguage(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateLanguage(%q) err=%v, wantErr=%v", tt.input, err, tt.wantErr)
		}
	}
}

func TestRule_Validate_NonStringNoCustom(t *testing.T) {
	vr := &ValidationResult{Valid: true}
	r := Rule{Field: "count"}
	r.Validate(42, vr)
	if !vr.Valid {
		t.Error("non-string without custom should be valid")
	}
}

func TestRule_MaxLen(t *testing.T) {
	vr := &ValidationResult{Valid: true}
	r := Rule{Field: "name", MaxLen: 5}
	r.Validate("toolongvalue", vr)
	if vr.Valid {
		t.Error("should fail MaxLen")
	}
	vr = &ValidationResult{Valid: true}
	r.Validate("short", vr)
	if !vr.Valid {
		t.Error("should pass MaxLen")
	}
}

func TestRule_MinLen(t *testing.T) {
	vr := &ValidationResult{Valid: true}
	r := Rule{Field: "name", MinLen: 5}
	r.Validate("ab", vr)
	if vr.Valid {
		t.Error("should fail MinLen")
	}
	vr = &ValidationResult{Valid: true}
	r.Validate("hello", vr)
	if !vr.Valid {
		t.Error("should pass MinLen")
	}
}

func TestRule_CustomFunc(t *testing.T) {
	vr := &ValidationResult{Valid: true}
	r := Rule{Field: "email", Custom: func(value interface{}) error {
		s, _ := value.(string)
		if !strings.Contains(s, "@") {
			return fmt.Errorf("invalid email")
		}
		return nil
	}}
	r.Validate("not-email", vr)
	if vr.Valid {
		t.Error("custom should fail")
	}
}

func TestMultipleRulesOnField(t *testing.T) {
	rules := []Rule{
		{Field: "name", Required: true},
		{Field: "name", MinLen: 5},
		{Field: "name", MaxLen: 10},
	}
	result := Validate(map[string]interface{}{}, rules)
	if result.Valid {
		t.Error("empty name should fail")
	}
	if len(result.Errors) < 1 {
		t.Error("expected at least 1 error")
	}
}

func TestRule_Pattern(t *testing.T) {
	vr := &ValidationResult{Valid: true}
	r := Rule{Field: "slug", Pattern: `^[a-z0-9-]+$`}
	r.Validate("valid-slug", vr)
	if !vr.Valid {
		t.Error("valid slug should pass pattern")
	}
	vr = &ValidationResult{Valid: true}
	r.Validate("Invalid Slug!", vr)
	if vr.Valid {
		t.Error("invalid slug should fail pattern")
	}
}

func TestRule_InvalidRegex(t *testing.T) {
	vr := &ValidationResult{Valid: true}
	r := Rule{Field: "test", Pattern: `[invalid`}
	r.Validate("anything", vr)
	if !vr.Valid {
		t.Error("invalid regex should not cause validation error")
	}
}

func TestRule_NonStringWithCustom(t *testing.T) {
	vr := &ValidationResult{Valid: true}
	r := Rule{Field: "count", Custom: func(value interface{}) error {
		return fmt.Errorf("custom error")
	}}
	r.Validate(42, vr)
	if vr.Valid {
		t.Error("non-string with custom should run custom validation")
	}
	if len(vr.Errors) != 1 || vr.Errors[0].Message != "custom error" {
		t.Errorf("expected custom error, got %v", vr.Errors)
	}
}

func TestRule_NonStringWithCustomPassing(t *testing.T) {
	vr := &ValidationResult{Valid: true}
	r := Rule{Field: "count", Custom: func(value interface{}) error {
		return nil
	}}
	r.Validate(42, vr)
	if !vr.Valid {
		t.Error("non-string with passing custom should be valid")
	}
}
