// Package validator provides comprehensive input validation for API requests.
// It supports field-level rules, custom validators, and returns structured
// error messages for all validation failures.
package validator

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ValidationError represents a single field validation error.
type ValidationError struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// ValidationResult aggregates all validation errors.
type ValidationResult struct {
	Valid  bool               `json:"valid"`
	Errors []ValidationError  `json:"errors,omitempty"`
}

// AddError appends a validation error.
func (vr *ValidationResult) AddError(field, rule, message string) {
	vr.Valid = false
	vr.Errors = append(vr.Errors, ValidationError{
		Field:   field,
		Rule:    rule,
		Message: message,
	})
}

// Validate runs all configured rules against a set of fields.
func Validate(fields map[string]interface{}, rules []Rule) *ValidationResult {
	vr := &ValidationResult{Valid: true}
	for _, rule := range rules {
		val, _ := fields[rule.Field]
		rule.Validate(val, vr)
	}
	return vr
}

// Rule defines a validation rule for a field.
type Rule struct {
	Field    string
	Required bool
	MinLen   int
	MaxLen   int
	Pattern  string
	Custom   func(value interface{}) error
}

// Validate applies this rule to a value.
func (r Rule) Validate(value interface{}, vr *ValidationResult) {
	if value == nil || value == "" {
		if r.Required {
			vr.AddError(r.Field, "required", fmt.Sprintf("%s is required", r.Field))
		}
		return
	}

	str, ok := value.(string)
	if !ok {
		// For non-string types, only run custom validation
		if r.Custom != nil {
			if err := r.Custom(value); err != nil {
				vr.AddError(r.Field, "custom", err.Error())
			}
		}
		return
	}

	if r.MinLen > 0 && utf8.RuneCountInString(str) < r.MinLen {
		vr.AddError(r.Field, "min_length", fmt.Sprintf("%s must be at least %d characters", r.Field, r.MinLen))
	}
	if r.MaxLen > 0 && utf8.RuneCountInString(str) > r.MaxLen {
		vr.AddError(r.Field, "max_length", fmt.Sprintf("%s must be at most %d characters", r.Field, r.MaxLen))
	}
	if r.Pattern != "" {
		matched, err := regexp.MatchString(r.Pattern, str)
		if err == nil && !matched {
			vr.AddError(r.Field, "pattern", fmt.Sprintf("%s does not match required pattern", r.Field))
		}
	}
	if r.Custom != nil {
		if err := r.Custom(value); err != nil {
			vr.AddError(r.Field, "custom", err.Error())
		}
	}
}

// ValidateEmail checks if a string is a valid email address.
func ValidateEmail(email string) error {
	email = strings.TrimSpace(email)
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		return fmt.Errorf("invalid email address")
	}
	return nil
}

// ValidatePassword checks if a password meets minimum requirements.
func ValidatePassword(password string) error {
	if len(password) < 12 {
		return fmt.Errorf("password must be at least 12 characters")
	}
	return nil
}

// ValidateSlug checks if a string is a valid URL slug.
func ValidateSlug(slug string) error {
	matched, _ := regexp.MatchString(`^[a-z0-9]+(?:-[a-z0-9]+)*$`, slug)
	if !matched {
		return fmt.Errorf("invalid slug format")
	}
	return nil
}

// ValidateLanguage checks if a language string is recognized.
func ValidateLanguage(lang string) error {
	valid := map[string]bool{
		"go": true, "python": true, "javascript": true, "typescript": true,
		"java": true, "c": true, "cpp": true, "rust": true, "ruby": true,
		"php": true, "swift": true, "kotlin": true, "scala": true,
		"html": true, "css": true, "sql": true, "yaml": true, "json": true,
		"sh": true, "bash": true, "dockerfile": true, "markdown": true,
	}
	if !valid[strings.ToLower(lang)] {
		return fmt.Errorf("unsupported language: %s", lang)
	}
	return nil
}
