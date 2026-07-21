package audit

import (
	"encoding/json"
	"net/http"

	"github.com/vigilagent/vigilagent/pkg/response"
)

// TraceRequest is the input for audit trail queries.
type TraceRequest struct {
	Entity  string `json:"entity"`
	Action  string `json:"action"`
	Actor   string `json:"actor"`
	Limit   int    `json:"limit"`
}

// TraceResponse is the output from audit trail queries.
type TraceResponse struct {
	Entries []Entry `json:"entries"`
	Total   int     `json:"total"`
}

// Engine wraps the audit Trail for HTTP handler integration.
type Engine struct {
	trail *Trail
}

// NewEngine creates a new audit engine wrapping a trail.
func NewEngine(trail *Trail) *Engine {
	if trail == nil {
		trail = NewTrail()
	}
	return &Engine{trail: trail}
}

// Trace queries the audit trail.
func (e *Engine) Trace(_ interface{}, req TraceRequest) TraceResponse {
	var entries []Entry
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}

	if req.Entity != "" {
		entries = e.trail.ByAction(req.Entity)
	} else if req.Actor != "" {
		entries = e.trail.ByActor(req.Actor)
	} else {
		entries = e.trail.Recent(limit)
	}

	if len(entries) > limit {
		entries = entries[:limit]
	}

	return TraceResponse{
		Entries: entries,
		Total:   e.trail.Count(),
	}
}

// GetTrail returns the underlying audit trail.
func (e *Engine) GetTrail() *Trail {
	return e.trail
}

// NewHTTPHandler creates a handler for the audit trail API.
func NewHTTPHandler(eng *Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req TraceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "invalid JSON body")
			return
		}

		if req.Entity == "" && req.Actor == "" {
			req.Entity = "all"
		}

		resp := eng.Trace(nil, req)
		response.JSON(w, http.StatusOK, resp)
	}
}
