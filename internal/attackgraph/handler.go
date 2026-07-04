package attackgraph

import (
	"encoding/json"
	"net/http"

	"github.com/vigilagent/vigilagent/pkg/response"
)

// NewHTTPHandler creates a handler for the attack graph API.
// The eng parameter must be non-nil.
func NewHTTPHandler(eng *Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req FindingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		resp := eng.Generate(r.Context(), req)
		response.JSON(w, http.StatusOK, resp)
	}
}
