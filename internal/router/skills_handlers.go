package router

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// listSkillsHandler returns a paginated list of skills.
func (r *Router) listSkillsHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	category := req.URL.Query().Get("category")
	sortBy := req.URL.Query().Get("sort_by")
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(req.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	skills, total, err := r.skills.List(req.Context(), category, sortBy, offset, pageSize)
	if err != nil {
		response.InternalError(w, "failed to list skills")
		return
	}
	if skills == nil {
		skills = []repository.Skill{}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"skills": skills,
		"page": map[string]interface{}{
			"page":        page,
			"page_size":   pageSize,
			"total":       total,
			"total_pages": (total + pageSize - 1) / pageSize,
		},
	})
}

// getSkillHandler returns a single skill by ID.
func (r *Router) getSkillHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	skillID := chi.URLParam(req, "skillID")
	skill, err := r.skills.FindByID(req.Context(), skillID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, skill)
}

// createSkillHandler creates a new skill.
func (r *Router) createSkillHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Version     string   `json:"version"`
		Category    string   `json:"category"`
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		response.BadRequest(w, "name is required")
		return
	}
	if input.Version == "" {
		input.Version = "1.0.0"
	}

	skill := &repository.Skill{
		Name:        input.Name,
		Description: input.Description,
		Author:      claims.UserID,
		Version:     input.Version,
		Category:    input.Category,
		Permissions: input.Permissions,
	}
	if err := r.skills.Create(req.Context(), skill); err != nil {
		response.InternalError(w, "failed to create skill")
		return
	}
	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "skill.created",
			Payload: map[string]interface{}{
				"skill_id": skill.ID,
				"name":     skill.Name,
				"version":  skill.Version,
			},
		})
	}
	response.Created(w, skill)
}

// updateSkillHandler updates an existing skill.
func (r *Router) updateSkillHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	skillID := chi.URLParam(req, "skillID")
	skill, err := r.skills.FindByID(req.Context(), skillID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}

	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		Category    string `json:"category"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Name == "" {
		input.Name = skill.Name
	}
	if input.Description == "" {
		input.Description = skill.Description
	}
	if input.Version == "" {
		input.Version = skill.Version
	}

	if err := r.skills.Update(req.Context(), skillID, input.Name, input.Description, input.Version, input.Category); err != nil {
		response.InternalError(w, "failed to update skill")
		return
	}
	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "skill.updated",
			Payload: map[string]interface{}{"skill_id": skillID},
		})
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "skill updated"})
}

// deleteSkillHandler deletes a skill.
func (r *Router) deleteSkillHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	skillID := chi.URLParam(req, "skillID")
	if _, err := r.skills.FindByID(req.Context(), skillID); err != nil {
		response.NotFound(w, err.Error())
		return
	}
	if err := r.skills.Delete(req.Context(), skillID); err != nil {
		response.InternalError(w, "failed to delete skill")
		return
	}
	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "skill.deleted",
			Payload: map[string]interface{}{"skill_id": skillID},
		})
	}
	response.NoContent(w)
}

// rateSkillHandler adds a rating to a skill.
func (r *Router) rateSkillHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	skillID := chi.URLParam(req, "skillID")

	var input struct {
		Rating int    `json:"rating"`
		Review string `json:"review"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Rating < 1 || input.Rating > 5 {
		response.BadRequest(w, "rating must be between 1 and 5")
		return
	}

	rating := &repository.SkillRating{
		SkillID: skillID,
		UserID:  claims.UserID,
		Rating:  input.Rating,
		Review:  input.Review,
	}
	if err := r.skills.AddRating(req.Context(), rating); err != nil {
		response.InternalError(w, "failed to add rating")
		return
	}
	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "skill.rated",
			Payload: map[string]interface{}{
				"skill_id": skillID,
				"rating":   input.Rating,
			},
		})
	}
	response.Created(w, rating)
}

// listSkillRatingsHandler lists ratings for a skill.
func (r *Router) listSkillRatingsHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	skillID := chi.URLParam(req, "skillID")
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(req.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	ratings, total, err := r.skills.ListRatings(req.Context(), skillID, offset, pageSize)
	if err != nil {
		response.InternalError(w, "failed to list ratings")
		return
	}
	if ratings == nil {
		ratings = []repository.SkillRating{}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"ratings": ratings,
		"page": map[string]interface{}{
			"page":        page,
			"page_size":   pageSize,
			"total":       total,
			"total_pages": (total + pageSize - 1) / pageSize,
		},
	})
}

// installSkillHandler installs a skill for the current user.
func (r *Router) installSkillHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	skillID := chi.URLParam(req, "skillID")
	if _, err := r.skills.FindByID(req.Context(), skillID); err != nil {
		response.NotFound(w, err.Error())
		return
	}

	var input struct {
		ProjectID string                 `json:"project_id"`
		Config    map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	inst := &repository.SkillInstallation{
		SkillID:   skillID,
		UserID:    claims.UserID,
		ProjectID: input.ProjectID,
		Status:    "installed",
	}
	if err := r.skills.Install(req.Context(), inst); err != nil {
		response.InternalError(w, "failed to install skill")
		return
	}
	_ = r.skills.IncrementDownloads(req.Context(), skillID)

	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "skill.installed",
			Payload: map[string]interface{}{
				"skill_id":      skillID,
				"user_id":       claims.UserID,
				"installation_id": inst.ID,
			},
		})
	}

	response.Created(w, map[string]interface{}{
		"installation_id": inst.ID,
		"status":          inst.Status,
	})
}
