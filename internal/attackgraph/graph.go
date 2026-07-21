package attackgraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/vigilagent/vigilagent/internal/util"
)

// AttackPath represents a chain of vulnerabilities leading to a security impact.
type AttackPath struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Steps       []AttackStep   `json:"steps"`
	Impact      string         `json:"impact"`
	Severity    string         `json:"severity"`
	Confidence  float64        `json:"confidence"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// AttackStep represents a single step in an attack path.
type AttackStep struct {
	Index        int            `json:"index"`
	Action       string         `json:"action"`
	Vector       string         `json:"vector"`
	Entity       string         `json:"entity"`
	Finding      string         `json:"finding,omitempty"`
	Prerequisite string         `json:"prerequisite,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// FindingsRequest contains findings and context for attack path generation.
type FindingsRequest struct {
	Description string         `json:"description"`
	Findings    []FindingInput `json:"findings,omitempty"`
	Entity      string         `json:"entity,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// FindingInput represents a finding from the scanner for attack graph analysis.
type FindingInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
	Category    string `json:"category"`
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
}

// GraphResponse is the API response for attack graph analysis.
type GraphResponse struct {
	Paths    []AttackPath   `json:"paths"`
	Summary  string         `json:"summary"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// graphRule defines a pattern for generating attack paths.
type graphRule struct {
	entity   string
	finding  string
	pathName string
	impact   string
	severity string
	steps    []AttackStep
}

// Engine generates attack paths based on findings and architecture context.
type Engine struct {
	rules []graphRule
}

// NewEngine creates a new attack graph engine with built-in rules.
func NewEngine() *Engine {
	return &Engine{
		rules: buildRules(),
	}
}

// Generate produces attack paths based on the request.
func (e *Engine) Generate(_ context.Context, req FindingsRequest) GraphResponse {
	entity := strings.ToLower(strings.TrimSpace(req.Entity))
	if entity == "" {
		entity = inferEntity(req.Description)
	}

	var paths []AttackPath
	seen := make(map[string]bool)

	// Check each finding against rules
	for _, f := range req.Findings {
		for _, rule := range e.rules {
			if matchesRule(rule, entity, f) {
				path := buildPath(rule, f)
				if !seen[path.ID] {
					paths = append(paths, path)
					seen[path.ID] = true
				}
			}
		}
	}

	// If no findings matched rules, generate generic paths
	if len(paths) == 0 && len(req.Findings) > 0 {
		path := genericPath(req)
		if path != nil {
			paths = append(paths, *path)
		}
	}

	return GraphResponse{
		Paths:   paths,
		Summary: e.summarize(paths, entity),
		Metadata: map[string]any{
			"entity": entity,
			"rules":  len(e.rules),
		},
	}
}

// summarize generates a human-readable summary of the attack paths.
func (e *Engine) summarize(paths []AttackPath, entity string) string {
	if len(paths) == 0 {
		return "No attack paths identified"
	}
	capitalized := strings.ToUpper(string(entity[0])) + entity[1:]
	return fmt.Sprintf("%s: %d attack path(s) identified", capitalized, len(paths))
}

// matchesRule checks if a finding matches a graph rule using word-boundary matching.
func matchesRule(rule graphRule, entity string, f FindingInput) bool {
	if rule.entity != "" && !util.ContainsWord(entity, rule.entity) {
		return false
	}
	if rule.finding != "" && !util.ContainsWord(strings.ToLower(f.Title), rule.finding) {
		return false
	}
	return true
}

// buildPath creates an AttackPath from a rule and finding.
func buildPath(rule graphRule, f FindingInput) AttackPath {
	path := AttackPath{
		ID:          strings.ReplaceAll(strings.ToLower(rule.pathName), " ", "-"),
		Name:        rule.pathName,
		Description: "Attack path via " + f.Title,
		Steps:       make([]AttackStep, len(rule.steps)),
		Impact:      rule.impact,
		Severity:    rule.severity,
		Confidence:  0.6,
		Metadata: map[string]any{
			"finding": f.Title,
		},
	}
	copy(path.Steps, rule.steps)
	return path
}

// genericPath generates a generic attack path for unmatched findings.
func genericPath(req FindingsRequest) *AttackPath {
	if len(req.Findings) == 0 {
		return nil
	}

	f := req.Findings[0]
	return &AttackPath{
		ID:          "generic-exploitation",
		Name:        "Generic Exploitation Path",
		Description: "Potential exploitation via: " + f.Title,
		Steps: []AttackStep{
			{Index: 1, Action: "Discover vulnerability", Vector: f.Title},
			{Index: 2, Action: "Craft exploit", Vector: "custom"},
			{Index: 3, Action: "Execute attack", Vector: f.Severity},
		},
		Impact:     f.Severity,
		Severity:   f.Severity,
		Confidence: 0.4,
	}
}

// inferEntity picks entity type from description.
func inferEntity(desc string) string {
	lower := strings.ToLower(desc)
	if strings.Contains(lower, "payment") {
		return "payment"
	}
	if strings.Contains(lower, "auth") || strings.Contains(lower, "login") {
		return "auth"
	}
	if strings.Contains(lower, "database") || strings.Contains(lower, "db") {
		return "database"
	}
	if strings.Contains(lower, "user") {
		return "user"
	}
	if strings.Contains(lower, "api") {
		return "api"
	}
	return "general"
}

// buildRules creates the built-in attack graph rules.
func buildRules() []graphRule {
	return []graphRule{
		{
			entity:   "payment",
			finding:  "sql injection",
			pathName: "Payment Data Exfiltration",
			impact:   "PCI-DSS breach",
			severity: "critical",
			steps: []AttackStep{
				{Index: 1, Action: "Identify injection point", Vector: "SQL Injection", Prerequisite: "Accessible payment endpoint"},
				{Index: 2, Action: "Craft malicious query", Vector: "SQL Injection"},
				{Index: 3, Action: "Extract payment data", Vector: "Data Exfiltration"},
				{Index: 4, Action: "Exfiltrate card data", Vector: "PCI-DSS impact"},
			},
		},
		{
			entity:   "auth",
			finding:  "broken authentication",
			pathName: "Authentication Bypass",
			impact:   "Account takeover",
			severity: "critical",
			steps: []AttackStep{
				{Index: 1, Action: "Identify auth weakness", Vector: "Broken Authentication"},
				{Index: 2, Action: "Exploit vulnerability", Vector: "Session Hijack"},
				{Index: 3, Action: "Escalate privileges", Vector: "Privilege Escalation"},
			},
		},
		{
			entity:   "auth",
			finding:  "credential leak",
			pathName: "Credential Harvesting",
			impact:   "Account compromise",
			severity: "high",
			steps: []AttackStep{
				{Index: 1, Action: "Locate exposed credentials", Vector: "Information Disclosure"},
				{Index: 2, Action: "Use credentials", Vector: "Credential Stuffing"},
				{Index: 3, Action: "Access protected resources", Vector: "Unauthorized Access"},
			},
		},
		{
			entity:   "api",
			finding:  "missing rate limit",
			pathName: "API Abuse Path",
			impact:   "Service degradation",
			severity: "high",
			steps: []AttackStep{
				{Index: 1, Action: "Identify unprotected endpoints", Vector: "Missing Rate Limit"},
				{Index: 2, Action: "Launch brute force", Vector: "Brute Force"},
				{Index: 3, Action: "Extract data or disrupt service", Vector: "Denial of Service"},
			},
		},
		{
			entity:   "payment",
			finding:  "hardcoded secret",
			pathName: "Secret Extraction",
			impact:   "System compromise",
			severity: "high",
			steps: []AttackStep{
				{Index: 1, Action: "Source code access", Vector: "Code Repository"},
				{Index: 2, Action: "Extract secret", Vector: "Hardcoded Secret"},
				{Index: 3, Action: "Use secret for unauthorized access", Vector: "Unauthorized Access"},
			},
		},
	}
}
