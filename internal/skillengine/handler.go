package skillengine

import (
	"encoding/json"
	"net/http"

	"github.com/vigilagent/vigilagent/pkg/response"
)

// ExtractRequest represents a skill extraction request.
type ExtractRequest struct {
	Finding Finding `json:"finding"`
	Outcome string  `json:"outcome,omitempty"` // "accepted" or "rejected"
	SkillID string  `json:"skill_id,omitempty"`
}

// NewHTTPHandler creates a handler for the skill engine API.
// The eng parameter must be non-nil.
func NewHTTPHandler(eng *Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ExtractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		if req.Outcome != "" && req.SkillID != "" {
			accepted := req.Outcome == "accepted"
			eng.RecordOutcome(req.SkillID, accepted)
			response.JSON(w, http.StatusOK, map[string]string{"status": "recorded"})
			return
		}

		skill, created := eng.ExtractFromFinding(req.Finding)
		response.JSON(w, http.StatusOK, map[string]any{
			"skill":   skill,
			"created": created,
		})
	}
}
