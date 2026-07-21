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
