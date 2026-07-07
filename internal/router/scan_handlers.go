package router

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// maxScanBytes caps the size of submitted code to bound memory and scan time.
const maxScanBytes = 1 << 20 // 1 MiB

// scanHandler runs the deterministic static-analysis engine over submitted code
// and returns a merged, confidence-scored report (Layer 4: static analysis).
func (r *Router) scanHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	req.Body = http.MaxBytesReader(w, req.Body, maxScanBytes)
	var input struct {
		Language string `json:"language"`
		Code     string `json:"code"`
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.JSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "code exceeds 1MB limit"})
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
