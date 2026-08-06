package skillengine

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/vigilagent/vigilagent/internal/auth"
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
		if _, ok := auth.ClaimsFromContext(r.Context()); !ok {
			response.Unauthorized(w, "missing authentication")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		var req ExtractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		if req.Outcome != "" && req.SkillID != "" {
			if req.Outcome != "accepted" && req.Outcome != "rejected" {
				response.Error(w, http.StatusBadRequest, "outcome must be 'accepted' or 'rejected'")
				return
			}
			accepted := req.Outcome == "accepted"
			eng.RecordOutcome(req.SkillID, accepted)
			response.JSON(w, http.StatusOK, map[string]string{"status": "recorded"})
			return
		}

		// Validate the finding so an empty message cannot pollute the skill
		// registry with an empty-trigger entry.
		if strings.TrimSpace(req.Finding.Message) == "" || strings.TrimSpace(req.Finding.Fix) == "" {
			response.Error(w, http.StatusBadRequest, "finding.message and finding.fix are required")
			return
		}

		skill, created := eng.ExtractFromFinding(req.Finding)
		response.JSON(w, http.StatusOK, map[string]any{
			"skill":   skill,
			"created": created,
		})
	}
}
