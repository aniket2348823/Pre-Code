package router

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// ExportData represents the export format.
type ExportData struct {
	Version       string               `json:"version"`
	ExportedAt    time.Time            `json:"exported_at"`
	UserID        string               `json:"user_id"`
	Conversations []ConversationExport `json:"conversations,omitempty"`
	Skills        []SkillExport        `json:"skills,omitempty"`
}

// ConversationExport represents an exported conversation.
type ConversationExport struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	AgentID   string    `json:"agent_id,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// SkillExport represents an exported skill.
type SkillExport struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// exportConversationsHandler exports user conversations.
func (r *Router) exportConversationsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing auth")
		return
	}

	export := ExportData{
		Version:    "1.0",
		ExportedAt: time.Now(),
		UserID:     claims.UserID,
	}

	// Get sessions for the user
	if r.sessions != nil {
		ctx := req.Context()
		sessions, err := r.sessions.ListByUser(ctx, claims.UserID)
		if err == nil {
			for _, sess := range sessions {
				export.Conversations = append(export.Conversations, ConversationExport{
					ID:        sess.ID,
					ProjectID: sess.ProjectID,
					AgentID:   sess.AgentID,
					Status:    sess.Status,
					CreatedAt: sess.CreatedAt,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=conversations-export.json")
	json.NewEncoder(w).Encode(export)
}

// exportSkillsHandler exports installed skills.
func (r *Router) exportSkillsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing auth")
		return
	}

	export := ExportData{
		Version:    "1.0",
		ExportedAt: time.Now(),
		UserID:     claims.UserID,
		Skills:     []SkillExport{},
	}

	// Export only the user's OWN installed skills. Listing the global
	// marketplace catalog here would dump every published skill into a
	// single user's export (and leak the whole catalog beyond intent).
	if r.skills != nil {
		ctx := req.Context()
		installs, err := r.skills.ListInstallsByUser(ctx, claims.UserID)
		if err == nil {
			for _, inst := range installs {
				if skill, err := r.skills.FindByID(ctx, inst.SkillID); err == nil {
					export.Skills = append(export.Skills, SkillExport{
						Name:    skill.Name,
						Version: skill.Version,
					})
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=skills-export.json")
	json.NewEncoder(w).Encode(export)
}

// importDataHandler imports exported data.
func (r *Router) importDataHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing auth")
		return
	}

	var export ExportData
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&export); err != nil {
		response.BadRequest(w, "invalid import data")
		return
	}

	if export.Version != "1.0" {
		response.BadRequest(w, "unsupported export version")
		return
	}

	imported := map[string]int{
		"skills": 0,
	}

	// Import skills: resolve each exported skill NAME to its real published
	// skill ID. Writing the name directly as skill_id would create garbage
	// install rows that never match a real skill.
	if r.skills != nil && len(export.Skills) > 0 {
		ctx := req.Context()
		for _, skill := range export.Skills {
			resolved, err := r.skills.FindByName(ctx, skill.Name)
			if err != nil {
				continue // unknown/removed skill — skip, don't create a broken install
			}
			inst := &repository.SkillInstallation{
				SkillID: resolved.ID,
				UserID:  claims.UserID,
				Status:  "active",
			}
			if err := r.skills.Install(ctx, inst); err != nil {
				continue
			}
			imported["skills"]++
		}
	}

	response.Success(w, http.StatusOK, map[string]interface{}{
		"imported": imported,
		"message":  "Import completed successfully",
	})
}
