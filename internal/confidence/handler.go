package confidence

import (
	"encoding/json"
	"net/http"

	"github.com/vigilagent/vigilagent/pkg/response"
)

// ScoreRequest represents a confidence scoring request.
type ScoreRequest struct {
	Evidence []Evidence `json:"evidence"`
}

// NewHTTPHandler creates a handler for the confidence engine API.
// The eng parameter must be non-nil.
func NewHTTPHandler(eng *Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ScoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		score := eng.Score(req.Evidence)
		response.JSON(w, http.StatusOK, score)
	}
}
