package router

import (
	"encoding/json"
	"net/http"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// scanHandler runs the deterministic static-analysis engine over submitted code
// and returns a merged, confidence-scored report (Layer 4: static analysis).
// Body size is enforced by the global limitBodySize middleware (2 MiB).
func (r *Router) scanHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		Language string `json:"language"`
		Code     string `json:"code"`
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
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
