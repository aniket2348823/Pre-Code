// Package pipeline implements the unified validation pipeline that chains
// all deterministic engine layers in sequence:
//
//	L1: Schema Validation   (schema.Validator)
//	L2: Requirements Check  (requirements.Resolver)
//	L3: Compliance Check    (compliance.Checker)
//	L4: Static Analysis     (scanner.Engine)
//
// Each layer receives the LLM output and returns its results. The pipeline
// aggregates all results and produces a single pass/fail verdict. No LLM,
// no network — pure deterministic validation.
package pipeline

import (
	"context"
	"strings"

	"github.com/vigilagent/vigilagent/internal/compliance"
	"github.com/vigilagent/vigilagent/internal/requirements"
	"github.com/vigilagent/vigilagent/internal/schema"
	"github.com/vigilagent/vigilagent/internal/scanner"
)

// Request is the input to the unified validation pipeline.
type Request struct {
	Description string         `json:"description"`
	Declared    []string       `json:"declared,omitempty"`
	Output      map[string]any `json:"output,omitempty"` // LLM output to validate (Layer 1)
	Code        string         `json:"code,omitempty"`   // Source code to scan (Layer 4)
	Language    string         `json:"language,omitempty"`
	Filename    string         `json:"filename,omitempty"`
}

// LayerResult holds the output from one validation layer.
type LayerResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

// Report is the aggregated output of the full pipeline.
type Report struct {
	Passed       bool                    `json:"passed"`
	Confidence   float64                 `json:"confidence"`
	Schema       *schema.Report          `json:"schema,omitempty"`
	Requirements *requirements.Report    `json:"requirements,omitempty"`
	Compliance   *compliance.Report      `json:"compliance,omitempty"`
	ScanResult   *scanner.Report         `json:"scan_result,omitempty"`
	Layers       []LayerResult           `json:"layers"`
	Reasons      []string                `json:"reasons,omitempty"`
}

// Pipeline chains all deterministic validation layers.
type Pipeline struct {
	validator    *schema.Validator
	requirements *requirements.Resolver
	compliance   *compliance.Checker
	engine       *scanner.Engine
}

// NewPipeline creates a Pipeline with all deterministic engine components.
func NewPipeline(v *schema.Validator, r *requirements.Resolver, c *compliance.Checker, e *scanner.Engine) *Pipeline {
	if v == nil {
		v = schema.NewValidator()
	}
	if r == nil {
		r = requirements.NewResolver()
	}
	if c == nil {
		c = compliance.NewChecker()
	}
	return &Pipeline{validator: v, requirements: r, compliance: c, engine: e}
}

// Run executes all validation layers and returns the aggregated report.
func (p *Pipeline) Run(ctx context.Context, req *Request) *Report {
	rep := &Report{
		Passed: true,
	}

	// Layer 1: Schema Validation (if LLM output provided)
	if len(req.Output) > 0 {
		entity := "architecture" // default entity type
		if req.Description != "" {
			entity = inferEntity(req.Description)
		}
		schemaRep := p.validator.Validate(entity, req.Output)
		rep.Schema = schemaRep
		rep.Layers = append(rep.Layers, LayerResult{Name: "schema", Passed: schemaRep.Passed})
		if !schemaRep.Passed {
			rep.Passed = false
			for _, v := range schemaRep.Violations {
				rep.Reasons = append(rep.Reasons, "schema: "+v.Message)
			}
		}
	}

	// Layer 2: Requirements Resolution
	reqRep := p.requirements.Resolve(req.Description, req.Declared)
	rep.Requirements = reqRep
	reqPassed := true
	for _, m := range reqRep.Missing {
		if scanner.SeverityRank(m.Control.Severity) >= scanner.SeverityRank(scanner.SeverityCritical) {
			reqPassed = false
			rep.Reasons = append(rep.Reasons, "requirements: missing "+m.Control.ID+" (critical)")
		}
	}
	rep.Layers = append(rep.Layers, LayerResult{Name: "requirements", Passed: reqPassed})
	if !reqPassed {
		rep.Passed = false
	}

	// Layer 3: Compliance Check
	compRep := p.compliance.Check(req.Description, req.Declared)
	rep.Compliance = compRep
	compPassed := true
	for _, m := range compRep.Missing {
		for _, ctrl := range m.Controls {
			if scanner.SeverityRank(ctrl.Severity) >= scanner.SeverityRank(scanner.SeverityCritical) {
				compPassed = false
				rep.Reasons = append(rep.Reasons, "compliance: missing "+ctrl.ID+" ("+string(ctrl.Framework)+")")
			}
		}
	}
	rep.Layers = append(rep.Layers, LayerResult{Name: "compliance", Passed: compPassed})
	if !compPassed {
		rep.Passed = false
	}

	// Layer 4: Static Analysis (if code provided and engine available)
	if req.Code != "" && p.engine != nil {
		scanRep := p.engine.Run(ctx, scanner.Input{
			Language: req.Language,
			Code:     req.Code,
			Filename: req.Filename,
		})
		rep.ScanResult = scanRep
		scanPassed := true
		for _, f := range scanRep.Findings {
			if scanner.SeverityRank(f.Severity) >= scanner.SeverityRank(scanner.SeverityCritical) {
				scanPassed = false
				rep.Reasons = append(rep.Reasons, "scan: "+f.Message+" at "+f.Filename)
			}
		}
		rep.Layers = append(rep.Layers, LayerResult{Name: "static_analysis", Passed: scanPassed})
		if !scanPassed {
			rep.Passed = false
		}
	} else if req.Code != "" {
		// Code provided but no engine — record as skipped
		rep.Layers = append(rep.Layers, LayerResult{Name: "static_analysis", Passed: true})
	}

	// Compute confidence based on layer pass rates
	passed := 0
	for _, l := range rep.Layers {
		if l.Passed {
			passed++
		}
	}
	if len(rep.Layers) > 0 {
		rep.Confidence = float64(passed) / float64(len(rep.Layers))
	}

	return rep
}

// inferEntity picks the most relevant entity type from a description.
func inferEntity(desc string) string {
	lower := strings.ToLower(desc)
	switch {
	case strings.Contains(lower, "payment") || strings.Contains(lower, "card") || strings.Contains(lower, "billing"):
		return "payment"
	case strings.Contains(lower, "auth") || strings.Contains(lower, "login") || strings.Contains(lower, "identity"):
		return "auth"
	case strings.Contains(lower, "security") || strings.Contains(lower, "vulnerability"):
		return "security_review"
	default:
		return "architecture"
	}
}
