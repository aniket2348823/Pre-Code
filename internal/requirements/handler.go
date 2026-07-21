package requirements

import (
	"encoding/json"
	"net/http"

	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// NewHTTPHandler returns an http.HandlerFunc for POST /api/v1/requirements.
// Auth is handled by the router middleware — no per-handler check needed.
func NewHTTPHandler(resolver *Resolver) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var input struct {
			Description string   `json:"description"`
			Declared    []string `json:"declared,omitempty"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			response.BadRequest(w, "invalid request body")
			return
		}
		if input.Description == "" {
			response.BadRequest(w, "description is required")
			return
		}
		if resolver == nil {
			resolver = NewResolver()
		}
		report := resolver.Resolve(input.Description, input.Declared)
		response.JSON(w, http.StatusOK, report)
	}
}

// NewValidateHTTPHandler returns an http.HandlerFunc for POST /api/v1/validate.
// Auth is handled by the router middleware — no per-handler check needed.
func NewValidateHTTPHandler(resolver *Resolver) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var input struct {
			Description string   `json:"description"`
			Declared    []string `json:"declared,omitempty"`
			// Code/Language/Filename reserved for scan integration (Layer 4).
			Code     string `json:"code,omitempty"`
			Language string `json:"language,omitempty"`
			Filename string `json:"filename,omitempty"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			response.BadRequest(w, "invalid request body")
			return
		}
		if input.Description == "" {
			response.BadRequest(w, "description is required")
			return
		}
		if resolver == nil {
			resolver = NewResolver()
		}

		reqReport := resolver.Resolve(input.Description, input.Declared)

		type validateResult struct {
			Requirements *Report  `json:"requirements"`
			Passed       bool     `json:"passed"`
			Reasons      []string `json:"reasons,omitempty"`
		}

		result := &validateResult{
			Requirements: reqReport,
			Passed:       true,
		}

		for _, m := range reqReport.Missing {
			if m.Control.Severity == scanner.SeverityCritical || m.Control.Severity == scanner.SeverityHigh {
				result.Passed = false
				result.Reasons = append(result.Reasons, "missing "+m.Control.ID+": "+m.Control.Title)
			}
		}

		response.JSON(w, http.StatusOK, result)
	}
}
