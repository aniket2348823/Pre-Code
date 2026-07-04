package schema

import (
	"encoding/json"
	"net/http"

	"github.com/vigilagent/vigilagent/pkg/response"
)

// NewHTTPHandler returns an http.HandlerFunc for POST /api/v1/schema.
// Auth is handled by the router middleware.
func NewHTTPHandler(v *Validator) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var input struct {
			Entity string         `json:"entity"`
			Output map[string]any `json:"output"`
		}
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			response.BadRequest(w, "invalid request body")
			return
		}
		if input.Entity == "" {
			response.BadRequest(w, "entity is required")
			return
		}
		if input.Output == nil {
			response.BadRequest(w, "output is required")
			return
		}

		validator := v
		if validator == nil {
			validator = NewValidator()
		}

		report := validator.Validate(input.Entity, input.Output)
		response.JSON(w, http.StatusOK, report)
	}
}
