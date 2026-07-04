package compliance

import (
	"encoding/json"
	"net/http"

	"github.com/vigilagent/vigilagent/pkg/response"
)

// NewHTTPHandler returns an http.HandlerFunc for POST /api/v1/compliance.
// Auth is handled by the router middleware.
func NewHTTPHandler(checker *Checker) http.HandlerFunc {
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
		if checker == nil {
			checker = NewChecker()
		}
		report := checker.Check(input.Description, input.Declared)
		response.JSON(w, http.StatusOK, report)
	}
}
