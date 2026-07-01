package router

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// searchMemoryHandler searches across memory layers (POST /v1/memory/search).
func (r *Router) searchMemoryHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		Query     string   `json:"query"`
		ProjectID string   `json:"project_id,omitempty"`
		Limit     int      `json:"limit,omitempty"`
		Types     []string `json:"types,omitempty"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		response.BadRequest(w, "query is required")
		return
	}
	if input.Limit <= 0 {
		input.Limit = 10
	}
	if input.Limit > 100 {
		input.Limit = 100
	}

	// Return empty results — memory system integration is a placeholder
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"results": []interface{}{},
		"page": map[string]interface{}{
			"total":       0,
			"total_pages": 0,
		},
		"query": input.Query,
	})
}

// createMemoryHandler creates a memory episode (POST /v1/memory).
func (r *Router) createMemoryHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		Type      string                 `json:"type"`
		Content   string                 `json:"content"`
		ProjectID string                 `json:"project_id,omitempty"`
		Metadata  map[string]interface{} `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	input.Content = strings.TrimSpace(input.Content)
	if input.Content == "" {
		response.BadRequest(w, "content is required")
		return
	}
	validTypes := map[string]bool{"episodic": true, "semantic": true, "procedural": true}
	if input.Type == "" {
		input.Type = "episodic"
	}
	if !validTypes[input.Type] {
		response.BadRequest(w, "type must be one of: episodic, semantic, procedural")
		return
	}

	response.Created(w, map[string]interface{}{
		"memory": map[string]interface{}{
			"type":       input.Type,
			"content":    input.Content,
			"project_id": input.ProjectID,
			"metadata":   input.Metadata,
		},
		"message": "memory created (placeholder - memory persistence coming soon)",
	})
}
