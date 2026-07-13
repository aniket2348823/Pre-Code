package compliance

import (
	"sort"
	"strings"

	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/util"
)

// Framework identifies a regulatory compliance framework.
type Framework string

const (
	FrameworkSOC2   Framework = "SOC2"
	FrameworkGDPR   Framework = "GDPR"
	FrameworkHIPAA  Framework = "HIPAA"
	FrameworkPCIDSS Framework = "PCI-DSS"
)

// Control represents a single compliance requirement.
type Control struct {
	ID          string           `json:"id"`
	Framework   Framework        `json:"framework"`
	Title       string           `json:"title"`
	Severity    scanner.Severity `json:"severity"`
	Description string           `json:"description"`
}

// Mapping associates a detected entity with the compliance controls it triggers.
type Mapping struct {
	Entity   string    `json:"entity"`
	Controls []Control `json:"controls"`
}

// Report is the checker's output for one description.
type Report struct {
	Frameworks   []Framework       `json:"frameworks"`
	Required     []Mapping         `json:"required"`
	Satisfied    []Mapping         `json:"satisfied"`
	Missing      []Mapping         `json:"missing"`
	frameworkSet map[Framework]bool // tracks which frameworks were triggered
}

// entityRule matches an entity by keywords and lists the controls it mandates.
type entityRule struct {
	entity    string
	keywords  []string   // positive keywords — at least one must match
	excludes  []string   // exclusion keywords — if any match, skip this rule
	minScore  float64    // minimum keyword match score to trigger (0.0–1.0)
	controls  []Control
}

// Checker evaluates a description against the built-in compliance rule set.
type Checker struct {
	rules []entityRule
}

// NewChecker returns a Checker loaded with the built-in compliance rules.
func NewChecker() *Checker {
	return &Checker{rules: builtinRules()}
}

// Check detects entities in the description and returns required, satisfied,
// and missing compliance controls given the declared controls.
// Keyword matching uses word-boundary detection to prevent false positives
// from substring matches (e.g., "auth" should not match "authorization").
func (c *Checker) Check(description string, declared []string) *Report {
	lower := strings.ToLower(description)
	declaredSet := map[string]bool{}
	for _, d := range declared {
		declaredSet[strings.ToLower(strings.TrimSpace(d))] = true
	}

	rep := &Report{
		frameworkSet: map[Framework]bool{},
	}

	seenControl := map[string]bool{}

	for _, rule := range c.rules {
		score := util.ComputeMatchScore(lower, rule.keywords, rule.excludes)
		if score < rule.minScore {
			continue
		}

		for _, ctrl := range rule.controls {
			if seenControl[ctrl.ID] {
				continue
			}
			seenControl[ctrl.ID] = true
			rep.frameworkSet[ctrl.Framework] = true

			req := Mapping{Entity: rule.entity, Controls: []Control{ctrl}}
			rep.Required = append(rep.Required, req)

			if declaredSet[ctrl.ID] {
				rep.Satisfied = append(rep.Satisfied, req)
			} else {
				rep.Missing = append(rep.Missing, req)
			}
		}
	}

	// Collect frameworks
	for fw := range rep.frameworkSet {
		rep.Frameworks = append(rep.Frameworks, fw)
	}
	sort.Slice(rep.Frameworks, func(i, j int) bool { return rep.Frameworks[i] < rep.Frameworks[j] })

	sortMappings(rep.Required)
	sortMappings(rep.Satisfied)
	sortMappings(rep.Missing)
	return rep
}

// computeMatchScore returns a score 0.0–1.0 based on how many keywords match,
// penalized by exclusion keywords. Uses word-boundary detection.




func sortMappings(mappings []Mapping) {
	sort.SliceStable(mappings, func(i, j int) bool {
		if len(mappings[i].Controls) == 0 || len(mappings[j].Controls) == 0 {
			return len(mappings[i].Controls) > len(mappings[j].Controls)
		}
		si := scanner.SeverityRank(mappings[i].Controls[0].Severity)
		sj := scanner.SeverityRank(mappings[j].Controls[0].Severity)
		if si != sj {
			return si > sj
		}
		return mappings[i].Controls[0].ID < mappings[j].Controls[0].ID
	})
}

// builtinRules returns the compliance rules with exclusion keywords to
// prevent false positives.
func builtinRules() []entityRule {
	return []entityRule{
		// Payment triggers PCI-DSS + SOC2
		{
			entity:   "payment",
			keywords: []string{"payment", "card data", "credit card", "checkout", "billing", "transaction"},
			excludes: []string{"payment method", "payment gateway"}, // exclude generic payment references
			minScore: 0.15, // at least 15% of keywords must match
			controls: []Control{
				{ID: "pci_encrypt", Framework: FrameworkPCIDSS, Title: "PCI-DSS: Encrypt cardholder data", Severity: scanner.SeverityCritical, Description: "Cardholder data must be encrypted in transit and at rest per PCI-DSS Req 3.4 and 4.1."},
				{ID: "pci_access", Framework: FrameworkPCIDSS, Title: "PCI-DSS: Restrict access to cardholder data", Severity: scanner.SeverityCritical, Description: "Access to cardholder data must be restricted to need-to-know per PCI-DSS Req 7."},
				{ID: "pci_audit", Framework: FrameworkPCIDSS, Title: "PCI-DSS: Maintain audit trail", Severity: scanner.SeverityHigh, Description: "Track all access to cardholder data per PCI-DSS Req 10."},
				{ID: "soc2_logging", Framework: FrameworkSOC2, Title: "SOC2: Security event logging", Severity: scanner.SeverityHigh, Description: "Security-relevant events must be logged per SOC2 CC6.1."},
			},
		},
		// Auth triggers SOC2 + GDPR
		{
			entity:   "auth",
			keywords: []string{"authentication", "authenticate", "login", "sign in", "sign-in", "auth service", "identity provider"},
			excludes: []string{"authorization", "oauth client"}, // exclude authz-only references
			minScore: 0.13,
			controls: []Control{
				{ID: "soc2_access", Framework: FrameworkSOC2, Title: "SOC2: Logical access controls", Severity: scanner.SeverityHigh, Description: "Logical access controls must be in place per SOC2 CC6.1."},
				{ID: "soc2_mfa", Framework: FrameworkSOC2, Title: "SOC2: Multi-factor authentication", Severity: scanner.SeverityHigh, Description: "MFA must be available for sensitive access per SOC2 CC6.1."},
				{ID: "gdpr_access", Framework: FrameworkGDPR, Title: "GDPR: Right to erasure support", Severity: scanner.SeverityMedium, Description: "Authentication system must support account deletion per GDPR Art. 17."},
			},
		},
		// PII triggers GDPR + HIPAA
		{
			entity:   "pii",
			keywords: []string{"pii", "personal data", "personal information", "customer data", "user profile", "email address", "phone number", "patient data", "health data", "medical record", "patient health"},
			excludes: []string{"pii compliance"}, // exclude meta-references about PII policy itself
			minScore: 0.09, // at least ~1 of 11 keywords
			controls: []Control{
				{ID: "gdpr_consent", Framework: FrameworkGDPR, Title: "GDPR: Explicit consent", Severity: scanner.SeverityHigh, Description: "Explicit consent required for PII processing per GDPR Art. 6."},
				{ID: "gdpr_minimize", Framework: FrameworkGDPR, Title: "GDPR: Data minimization", Severity: scanner.SeverityHigh, Description: "Collect only necessary PII per GDPR Art. 5(1)(c)."},
				{ID: "gdpr_retention", Framework: FrameworkGDPR, Title: "GDPR: Data retention policy", Severity: scanner.SeverityMedium, Description: "PII retention must be defined and enforced per GDPR Art. 5(1)(e)."},
				{ID: "hipaa_phi", Framework: FrameworkHIPAA, Title: "HIPAA: PHI protection", Severity: scanner.SeverityCritical, Description: "Protected Health Information must be encrypted and access-controlled per HIPAA §164.312."},
			},
		},
	}
}
