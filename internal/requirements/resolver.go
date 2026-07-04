// Package requirements implements Layer 2 of the deterministic engine: it maps
// security-relevant entities in a project description (payment, auth, PII, …) to
// the controls those entities mandate, then reports which mandatory controls are
// missing given the controls a developer has declared. No LLM, no network — the
// mapping is a fixed, auditable rule set.
package requirements

import (
	"sort"
	"strings"

	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/util"
)

// Control is a required security capability (e.g. encryption, audit logging).
type Control struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Severity    scanner.Severity `json:"severity"`
	Description string           `json:"description"`
}

// Requirement is a Control mandated by a specific detected entity.
type Requirement struct {
	Entity  string  `json:"entity"`
	Control Control `json:"control"`
}

// Report is the resolver's output for one description.
type Report struct {
	Entities  []string      `json:"entities"`
	Required  []Requirement `json:"required"`
	Satisfied []Requirement `json:"satisfied"`
	Missing   []Requirement `json:"missing"`
}

// entityRule matches an entity by keyword and lists the controls it mandates.
type entityRule struct {
	entity   string
	keywords []string // positive keywords — at least one must match
	excludes []string // exclusion keywords — if any match, skip this rule
	minScore float64  // minimum keyword match score to trigger (0.0–1.0)
	controls []Control
}

// Resolver evaluates a description against the built-in entity → control rules.
type Resolver struct {
	rules []entityRule
}

// NewResolver returns a Resolver loaded with the built-in rule set.
func NewResolver() *Resolver {
	return &Resolver{rules: builtinRules()}
}

// Resolve detects entities in the description and returns required, satisfied,
// and missing controls given the declared controls. Matching is case-insensitive
// and uses word-boundary detection to prevent false positives.
func (r *Resolver) Resolve(description string, declared []string) *Report {
	lower := strings.ToLower(description)
	declaredSet := map[string]bool{}
	for _, d := range declared {
		declaredSet[strings.ToLower(strings.TrimSpace(d))] = true
	}

	rep := &Report{}
	seenEntity := map[string]bool{}
	seenControl := map[string]bool{} // first entity to require a control owns it

	for _, rule := range r.rules {
		score := util.ComputeMatchScore(lower, rule.keywords, rule.excludes)
		if score < rule.minScore {
			continue
		}

		if !seenEntity[rule.entity] {
			seenEntity[rule.entity] = true
			rep.Entities = append(rep.Entities, rule.entity)
		}
		for _, c := range rule.controls {
			if seenControl[c.ID] {
				continue
			}
			seenControl[c.ID] = true
			req := Requirement{Entity: rule.entity, Control: c}
			rep.Required = append(rep.Required, req)
			if declaredSet[c.ID] {
				rep.Satisfied = append(rep.Satisfied, req)
			} else {
				rep.Missing = append(rep.Missing, req)
			}
		}
	}

	sort.Strings(rep.Entities)
	sortRequirements(rep.Required)
	sortRequirements(rep.Satisfied)
	sortRequirements(rep.Missing)
	return rep
}

// sortRequirements orders by severity (most severe first) then control ID for
// stable, deterministic output.
func sortRequirements(reqs []Requirement) {
	sort.SliceStable(reqs, func(i, j int) bool {
		ri := scanner.SeverityRank(reqs[i].Control.Severity)
		rj := scanner.SeverityRank(reqs[j].Control.Severity)
		if ri != rj {
			return ri > rj
		}
		return reqs[i].Control.ID < reqs[j].Control.ID
	})
}

// Package-level control constants — allocated once, shared by all rules.
var (
	controlEncryption = Control{ID: "encryption", Title: "Encryption at rest and in transit", Severity: scanner.SeverityCritical, Description: "Sensitive data must be encrypted in transit (TLS) and at rest."}
	controlAuditLog   = Control{ID: "audit_log", Title: "Audit logging", Severity: scanner.SeverityHigh, Description: "Security-relevant actions must be recorded in a tamper-evident audit log."}
	controlRateLimit  = Control{ID: "rate_limit", Title: "Rate limiting", Severity: scanner.SeverityHigh, Description: "Endpoints must be rate-limited to resist abuse and brute force."}
	controlFraud      = Control{ID: "fraud_monitoring", Title: "Fraud monitoring", Severity: scanner.SeverityHigh, Description: "Money-handling flows must be monitored for fraudulent activity."}
	controlMFA        = Control{ID: "mfa", Title: "Multi-factor authentication", Severity: scanner.SeverityHigh, Description: "Authentication must support a second factor for sensitive accounts."}
	controlAccess     = Control{ID: "access_control", Title: "Access control", Severity: scanner.SeverityHigh, Description: "Resources must enforce least-privilege authorization checks."}
	controlPII        = Control{ID: "pii_minimization", Title: "PII minimization & retention", Severity: scanner.SeverityMedium, Description: "Collect only necessary PII and enforce a retention/deletion policy."}
	controlConsent    = Control{ID: "consent", Title: "Consent management", Severity: scanner.SeverityMedium, Description: "User consent must be captured and revocable for personal data."}
	controlInputVal   = Control{ID: "input_validation", Title: "Input validation", Severity: scanner.SeverityHigh, Description: "All external input must be validated and sanitized."}
)

func builtinRules() []entityRule {
	return []entityRule{
		{
			entity:   "payment",
			keywords: []string{"payment", "card data", "credit card", "checkout", "billing", "transaction"},
			excludes: []string{"unpaid", "payment method", "payment gateway"},
			minScore: 0.15,
			controls: []Control{controlEncryption, controlAuditLog, controlRateLimit, controlFraud, controlAccess},
		},
		{
			entity:   "auth",
			keywords: []string{"authentication", "login", "sign in", "sign-in", "auth service", "identity provider"},
			excludes: []string{"authorization", "oauth client"},
			minScore: 0.15,
			controls: []Control{controlMFA, controlRateLimit, controlAuditLog, controlAccess},
		},
		{
			entity:   "pii",
			keywords: []string{"pii", "personal data", "customer data", "user profile", "email address", "phone number"},
			excludes: []string{"pii compliance"},
			minScore: 0.16,
			controls: []Control{controlEncryption, controlPII, controlConsent, controlAccess},
		},
		{
			entity:   "api",
			keywords: []string{" api", "endpoint", "rest ", "graphql", "webhook"},
			excludes: []string{},
			minScore: 0.20,
			controls: []Control{controlRateLimit, controlInputVal, controlAuditLog},
		},
	}
}
