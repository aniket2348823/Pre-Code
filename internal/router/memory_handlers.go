package router

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/memory"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// searchMemoryHandler searches across memory layers (POST /v1/memory/search).
func (r *Router) searchMemoryHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	if r.memory == nil {
		response.InternalError(w, "memory system not configured")
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

	results, err := r.memory.SearchMemory(req.Context(), input.Query, input.Types, input.Limit, 0)
	if err != nil {
		response.InternalError(w, "memory search failed")
		return
	}
	if results == nil {
		results = []memory.MemoryResult{}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"results": results,
		"total":   len(results),
		"query":   input.Query,
	})
}

// createMemoryHandler creates a memory episode (POST /v1/memory).
func (r *Router) createMemoryHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	if r.memory == nil {
		response.InternalError(w, "memory system not configured")
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

	// Derive a title from metadata or first line of content
	title := input.Content
	if len(title) > 80 {
		title = title[:80]
	}
	if name, ok := input.Metadata["name"].(string); ok && name != "" {
		title = name
	}

	// Store the memory based on type
	switch input.Type {
	case "episodic":
		importance := 0.5
		if imp, ok := input.Metadata["importance"]; ok {
			switch v := imp.(type) {
			case float64:
				importance = v
			case string:
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					importance = f
				}
			}
		}
		if err := r.memory.StoreEpisode(req.Context(), claims.UserID, input.Type, title, input.Content, importance); err != nil {
			response.InternalError(w, "failed to store memory")
			return
		}
	case "semantic":
		if input.ProjectID == "" {
			response.BadRequest(w, "project_id is required for semantic memory")
			return
		}
		if err := r.memory.StorePattern(req.Context(), claims.UserID, input.ProjectID, "codebase", title, input.Content); err != nil {
			response.InternalError(w, "failed to store memory")
			return
		}
	case "procedural":
		r.memory.AddWorkingMessage("system", input.Content, 0)
	}

	response.Created(w, map[string]interface{}{
		"type":       input.Type,
		"content":    input.Content,
		"project_id": input.ProjectID,
	})
}
