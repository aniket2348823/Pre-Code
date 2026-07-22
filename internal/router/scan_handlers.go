package router

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// scanHandler runs the deterministic static-analysis engine over submitted code
// and returns a merged, confidence-scored report (Layer 4: static analysis).
// Body size is enforced by the global limitBodySize middleware (2 MiB)
// and by this handler directly as defense-in-depth.
func (r *Router) scanHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	// Defense-in-depth: enforce body size directly in the handler
	if req.Body != nil {
		req.Body = http.MaxBytesReader(w, req.Body, maxRequestBodySize)
	}

	var input struct {
		Language string `json:"language"`
		Code     string `json:"code"`
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || (err != nil && strings.Contains(err.Error(), "too large")) {
			response.JSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Code == "" {
		response.BadRequest(w, "code is required")
		return
	}

	engine := r.engine
	if engine == nil {
		engine = scanner.DefaultEngine()
	}
	report := engine.Run(req.Context(), scanner.Input{
		Language: input.Language,
		Code:     input.Code,
		Filename: input.Filename,
	})
	response.JSON(w, http.StatusOK, report)

	// Dispatch webhook notification for scan completion
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "scan.completed",
			Payload: map[string]interface{}{
				"language": input.Language,
				"filename": input.Filename,
				"findings": len(report.Findings),
			},
		})
	}
}
