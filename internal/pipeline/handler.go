package pipeline

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/vigilagent/vigilagent/internal/logging"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// maxBodySize limits request body to 1MB to prevent DoS via large payloads.
const maxBodySize = 1 << 20

// maxDescriptionLen limits description field to 10KB.
const maxDescriptionLen = 10240

// maxCodeLen limits code field to 500KB.
const maxCodeLen = 512000

// NewHTTPHandler returns an http.HandlerFunc for POST /api/v1/validate-full.
// Auth is handled by the router middleware.
func NewHTTPHandler(p *Pipeline) http.HandlerFunc {
	logger := logging.New("pipeline")

	return func(w http.ResponseWriter, req *http.Request) {
		log := logging.WithRequestID(req.Context(), logger)

		// Limit body size to prevent DoS
		body := http.MaxBytesReader(w, req.Body, maxBodySize)

		var input Request
		if err := json.NewDecoder(body).Decode(&input); err != nil {
			log.Warn("invalid request body", "error", err, "remote_addr", req.RemoteAddr)
			response.BadRequest(w, "invalid request body")
			return
		}

		// Input validation and sanitization
		input.Description = strings.TrimSpace(input.Description)
		if input.Description == "" {
			log.Warn("missing description field", "remote_addr", req.RemoteAddr)
			response.BadRequest(w, "description is required")
			return
		}

		// Enforce field length limits
		if len(input.Description) > maxDescriptionLen {
			log.Warn("description exceeds max length", "length", len(input.Description), "max", maxDescriptionLen)
			response.BadRequest(w, "description too long")
			return
		}
		if len(input.Code) > maxCodeLen {
			log.Warn("code exceeds max length", "length", len(input.Code), "max", maxCodeLen)
			response.BadRequest(w, "code too long")
			return
		}

		// Sanitize declared controls — trim and lowercase
		sanitized := make([]string, 0, len(input.Declared))
		seen := make(map[string]bool)
		for _, d := range input.Declared {
			d = strings.ToLower(strings.TrimSpace(d))
			if d != "" && !seen[d] {
				sanitized = append(sanitized, d)
				seen[d] = true
			}
		}
		input.Declared = sanitized

		log.Info("processing validation request",
			"description_length", len(input.Description),
			"declared_controls", len(input.Declared),
			"has_code", input.Code != "",
			"has_output", len(input.Output) > 0,
		)

		report := p.Run(req.Context(), &input)

		log.Info("validation complete",
			"passed", report.Passed,
			"confidence", report.Confidence,
			"layers", len(report.Layers),
			"reasons", len(report.Reasons),
		)

		response.JSON(w, http.StatusOK, report)
	}
}


