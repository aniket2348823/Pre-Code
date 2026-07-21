package schema

import (
	"fmt"
	"sort"
	"strings"
)

// Rule defines a schema constraint for a named entity type.
type Rule struct {
	Entity          string   `json:"entity"`
	RequiredFields  []string `json:"required_fields"`
	ForbiddenFields []string `json:"forbidden_fields,omitempty"`
	MaxDepth        int      `json:"max_depth,omitempty"` // 0 = no limit
	AllowedTopLevel []string `json:"allowed_top_level,omitempty"`
}

// Violation describes a single schema validation failure.
type Violation struct {
	Rule    string `json:"rule"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// Report is the output of schema validation.
type Report struct {
	Violations []Violation `json:"violations"`
	Passed     bool        `json:"passed"`
}

// Validator checks LLM output against a set of schema rules.
type Validator struct {
	rules map[string]Rule
}

// NewValidator returns a Validator loaded with the built-in rule set.
func NewValidator() *Validator {
	return &Validator{rules: builtinRules()}
}

// NewValidatorWithRules returns a Validator with custom rules.
func NewValidatorWithRules(rules []Rule) *Validator {
	m := make(map[string]Rule, len(rules))
	for _, r := range rules {
		m[r.Entity] = r
	}
	return &Validator{rules: m}
}

// Validate checks the provided LLM output against rules matching the entity.
// The output is a map representation of the LLM response.
// entity hints which rule to apply (e.g. "architecture", "finding", "payment").
func (v *Validator) Validate(entity string, output map[string]any) *Report {
	rep := &Report{}

	// Try exact entity match first, then fuzzy
	rule, ok := v.rules[strings.ToLower(entity)]
	if !ok {
		// Try partial match
		for k, r := range v.rules {
			if strings.Contains(strings.ToLower(entity), k) || strings.Contains(k, strings.ToLower(entity)) {
				rule = r
				ok = true
				break
			}
		}
	}
	if !ok {
		// No matching rule — pass by default
		rep.Passed = true
		return rep
	}

	// Check required fields
	for _, field := range rule.RequiredFields {
		if _, exists := output[field]; !exists {
			rep.Violations = append(rep.Violations, Violation{
				Rule:    "required_field",
				Field:   field,
				Message: fmt.Sprintf("required field %q is missing", field),
			})
		}
	}

	// Check forbidden fields
	for _, field := range rule.ForbiddenFields {
		if _, exists := output[field]; exists {
			rep.Violations = append(rep.Violations, Violation{
				Rule:    "forbidden_field",
				Field:   field,
				Message: fmt.Sprintf("forbidden field %q is present", field),
			})
		}
	}

	// Check allowed top-level keys
	if len(rule.AllowedTopLevel) > 0 {
		allowed := make(map[string]bool, len(rule.AllowedTopLevel))
		for _, k := range rule.AllowedTopLevel {
			allowed[k] = true
		}
		for key := range output {
			if !allowed[key] {
				rep.Violations = append(rep.Violations, Violation{
					Rule:    "unknown_field",
					Field:   key,
					Message: fmt.Sprintf("field %q is not in the allowed set", key),
				})
			}
		}
	}

	// Check max depth
	if rule.MaxDepth > 0 {
		actualDepth := computeDepth(output)
		if actualDepth > rule.MaxDepth {
			rep.Violations = append(rep.Violations, Violation{
				Rule:    "max_depth",
				Message: fmt.Sprintf("output depth %d exceeds maximum %d", actualDepth, rule.MaxDepth),
			})
		}
	}

	// Sort violations for deterministic output
	sort.Slice(rep.Violations, func(i, j int) bool {
		if rep.Violations[i].Rule != rep.Violations[j].Rule {
			return rep.Violations[i].Rule < rep.Violations[j].Rule
		}
		return rep.Violations[i].Field < rep.Violations[j].Field
	})

	rep.Passed = len(rep.Violations) == 0
	return rep
}

// computeDepth returns the maximum nesting depth of a map.
func computeDepth(m map[string]any) int {
	maxChild := 0
	for _, v := range m {
		if sub, ok := v.(map[string]any); ok {
			d := computeDepth(sub)
			if d > maxChild {
				maxChild = d
			}
		}
	}
	return 1 + maxChild
}

// builtinRules returns the default schema validation rules.
func builtinRules() map[string]Rule {
	return map[string]Rule{
		"architecture": {
			Entity:         "architecture",
			RequiredFields: []string{"components", "risks"},
			MaxDepth:       5,
			AllowedTopLevel: []string{"components", "risks", "controls", "threats", "description", "diagram"},
		},
		"finding": {
			Entity:         "finding",
			RequiredFields: []string{"severity", "message", "file"},
		},
		"payment": {
			Entity:         "payment",
			RequiredFields: []string{"components"},
		},
		"auth": {
			Entity:         "auth",
			RequiredFields: []string{"components"},
		},
		"security_review": {
			Entity:         "security_review",
			RequiredFields: []string{"findings", "summary"},
			AllowedTopLevel: []string{"findings", "summary", "recommendations", "risk_level"},
		},
		"vulnerability_report": {
			Entity:         "vulnerability_report",
			RequiredFields: []string{"vulnerabilities", "summary"},
			AllowedTopLevel: []string{"vulnerabilities", "summary", "risk_score", "remediations"},
		},
	}
}
