package router

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/skills"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/pagination"
	"github.com/vigilagent/vigilagent/pkg/query"
	"github.com/vigilagent/vigilagent/pkg/response"
	"github.com/vigilagent/vigilagent/pkg/validation"
)

// Content from skills_handlers.go
// listSkillsHandler returns a paginated list of skills.
func (r *Router) listSkillsHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	category := req.URL.Query().Get("category")
	sortBy := req.URL.Query().Get("sort_by")

	pag := pagination.ParseRequest(req)
	skills, total, err := r.skills.List(req.Context(), category, sortBy, 0, pag.Limit)
	if err != nil {
		response.InternalError(w, "failed to list skills")
		return
	}
	if skills == nil {
		skills = []repository.Skill{}
	}

	filter, sortVal := query.Parse(req)
	processed, meta := query.ProcessList(skills, filter, sortVal, pag)
	if meta != nil {
		meta.Total = total
	}
	response.SuccessWithMeta(w, req, http.StatusOK, processed, meta)
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
		response.NotFound(w, "skill not found")
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
	v, ok := validation.DecodeAndValidate(w, req, &input)
	if !ok {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	v.Required("name", input.Name)
	if v.WriteResponse(w, req) {
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
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	skillID := chi.URLParam(req, "skillID")
	skill, err := r.skills.FindByID(req.Context(), skillID)
	if err != nil {
		response.NotFound(w, "skill not found")
		return
	}
	if skill.Author != claims.UserID {
		response.Forbidden(w, "only the author can update a skill")
		return
	}
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		Category    string `json:"category"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
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
			Type:    "skill.updated",
			Payload: map[string]interface{}{"skill_id": skillID},
		})
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "skill updated"})
}

// deleteSkillHandler deletes a skill.
func (r *Router) deleteSkillHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	skillID := chi.URLParam(req, "skillID")
	skill, err := r.skills.FindByID(req.Context(), skillID)
	if err != nil {
		response.NotFound(w, "skill not found")
		return
	}
	if skill.Author != claims.UserID {
		response.Forbidden(w, "only the author can delete a skill")
		return
	}
	if err := r.skills.Delete(req.Context(), skillID); err != nil {
		response.InternalError(w, "failed to delete skill")
		return
	}
	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type:    "skill.deleted",
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
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
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
		response.NotFound(w, "skill not found")
		return
	}

	var input struct {
		ProjectID string                 `json:"project_id"`
		Config    map[string]interface{} `json:"config"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// Verify the user is a member of the target project before writing an
	// installation scoped to it — otherwise any user could attach skills to
	// another org's project (cross-tenant write).
	if input.ProjectID != "" {
		if _, err := r.requireProjectMember(req.Context(), input.ProjectID, claims.UserID); err != nil {
			response.Forbidden(w, "access denied")
			return
		}
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
	if err := r.skills.IncrementDownloads(req.Context(), skillID); err != nil {
		slog.Warn("failed to increment skill downloads", "error", err, "skill_id", skillID)
	}

	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "skill.installed",
			Payload: map[string]interface{}{
				"skill_id":        skillID,
				"user_id":         claims.UserID,
				"installation_id": inst.ID,
			},
		})
	}

	response.Created(w, map[string]interface{}{
		"installation_id": inst.ID,
		"status":          inst.Status,
	})
}

// Content from skills_rag_handlers.go
// RAGHandlers provides HTTP handlers for the skill marketplace RAG system.
type RAGHandlers struct {
	rag       *skills.RAGEngine
	skillRepo *repository.SkillRepository
	publishMu sync.Mutex
	publishTS map[string][]time.Time
}

// NewRAGHandlers creates new RAG HTTP handlers.
func NewRAGHandlers(rag *skills.RAGEngine, skillRepo *repository.SkillRepository) *RAGHandlers {
	return &RAGHandlers{
		rag:       rag,
		skillRepo: skillRepo,
		publishTS: make(map[string][]time.Time),
	}
}

func (h *RAGHandlers) allowPublish(userID string) bool {
	h.publishMu.Lock()
	defer h.publishMu.Unlock()
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	timestamps := h.publishTS[userID]
	valid := timestamps[:0]
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	if len(valid) >= 5 {
		h.publishTS[userID] = valid
		return false
	}
	h.publishTS[userID] = append(valid, now)
	return true
}

// SearchHandler performs hybrid RAG search across skills.
func (h *RAGHandlers) SearchHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	q := req.URL.Query()
	query := skills.SearchQuery{
		Raw:      q.Get("q"),
		Language: q.Get("language"),
		Category: q.Get("category"),
		SortBy:   q.Get("sort"),
	}

	if query.Raw == "" {
		response.BadRequest(w, "q query parameter is required")
		return
	}

	if minRating := q.Get("min_rating"); minRating != "" {
		if v, err := strconv.ParseFloat(minRating, 64); err == nil {
			query.MinRating = v
		}
	}
	if minDownloads := q.Get("min_downloads"); minDownloads != "" {
		if v, err := strconv.Atoi(minDownloads); err == nil {
			query.MinDownloads = v
		}
	}
	if page := q.Get("page"); page != "" {
		if p, err := strconv.Atoi(page); err == nil && p > 0 {
			pageSize, _ := strconv.Atoi(q.Get("page_size"))
			if pageSize <= 0 {
				pageSize = 20
			}
			query.Offset = (p - 1) * pageSize
			query.Limit = pageSize
		}
	}

	result, err := h.rag.HybridSearch(req.Context(), query)
	if err != nil {
		response.InternalError(w, "search failed")
		return
	}

	response.JSON(w, http.StatusOK, result)
}

// SuggestHandler returns autocomplete suggestions.
func (h *RAGHandlers) SuggestHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	partial := req.URL.Query().Get("q")
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10
	}

	suggestions, err := h.rag.SuggestSkills(req.Context(), partial, limit)
	if err != nil {
		response.InternalError(w, "suggestion failed")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"suggestions": suggestions,
	})
}

// TrendingHandler returns trending skills.
func (h *RAGHandlers) TrendingHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10
	}

	skillsList, err := h.rag.GetTrending(req.Context(), limit)
	if err != nil {
		response.InternalError(w, "failed to get trending skills")
		return
	}

	if skillsList == nil {
		skillsList = []repository.Skill{}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"skills": skillsList,
	})
}

// CategoriesHandler returns skill categories with counts.
func (h *RAGHandlers) CategoriesHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	categories, err := h.rag.GetByCategory(req.Context())
	if err != nil {
		response.InternalError(w, "failed to get categories")
		return
	}

	if categories == nil {
		categories = []skills.CategoryCount{}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"categories": categories,
	})
}

// PublishHandler uploads and publishes a skill package.
func (h *RAGHandlers) PublishHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	if !h.allowPublish(claims.UserID) {
		w.Header().Set("Retry-After", "60")
		response.Error(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	skillID := chi.URLParam(req, "skillID")
	skill, err := h.skillRepo.FindByID(req.Context(), skillID)
	if err != nil {
		response.NotFound(w, "skill not found")
		return
	}

	// Only the author can publish
	if skill.Author != claims.UserID {
		response.Forbidden(w, "only the author can publish a skill")
		return
	}

	// Limit upload to 10MB
	req.Body = http.MaxBytesReader(w, req.Body, 10<<20)

	if err := req.ParseMultipartForm(10 << 20); err != nil {
		response.BadRequest(w, "failed to parse upload")
		return
	}

	file, header, err := req.FormFile("package")
	if err != nil {
		response.BadRequest(w, "package file is required")
		return
	}
	defer file.Close()

	if header.Filename == "" {
		response.BadRequest(w, "filename is required")
		return
	}

	// Read package data
	packageData, err := io.ReadAll(io.LimitReader(file, 10<<20))
	if err != nil {
		response.BadRequest(w, "failed to read package")
		return
	}

	// Scan the package for security issues
	scanner := skills.NewSkillScanner()
	result, err := scanner.ScanPackage(req.Context(), packageData)
	if err != nil {
		response.BadRequest(w, "package scan failed")
		return
	}

	if !result.Passed {
		response.BadRequest(w, fmt.Sprintf("package failed security scan (score: %.2f)", result.Score))
		return
	}

	// Mark as published and update
	err = h.skillRepo.Update(req.Context(), skillID, skill.Name, skill.Description, skill.Version, skill.Category)
	if err != nil {
		response.InternalError(w, "failed to update skill")
		return
	}

	// Index for RAG search
	if h.rag != nil {
		_ = h.rag.IndexSkill(req.Context(), *skill)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"message":  "skill published successfully",
		"skill_id": skillID,
		"scan": map[string]interface{}{
			"passed": result.Passed,
			"score":  result.Score,
		},
	})
}

// DownloadHandler downloads a skill package.
func (h *RAGHandlers) DownloadHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	skillID := chi.URLParam(req, "skillID")
	skill, err := h.skillRepo.FindByID(req.Context(), skillID)
	if err != nil {
		response.NotFound(w, "skill not found")
		return
	}

	if !skill.IsPublished {
		response.NotFound(w, "skill is not published")
		return
	}

	// Increment download count
	_ = h.skillRepo.IncrementDownloads(req.Context(), skillID)

	// Return skill metadata as JSON (binary package storage TBD)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"skill_id":  skill.ID,
		"name":      skill.Name,
		"version":   skill.Version,
		"author":    skill.Author,
		"category":  skill.Category,
		"downloads": skill.Downloads + 1,
		"message":   "package download endpoint — implement binary storage for production",
	})
}

// ReindexHandler triggers a full reindex of all skills.
func (h *RAGHandlers) ReindexHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	if claims.Role != "admin" && claims.Role != "superadmin" {
		response.Forbidden(w, "admin access required")
		return
	}

	count, err := h.rag.ReindexAll(req.Context())
	if err != nil {
		response.InternalError(w, "reindex failed")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("reindexed %d skills", count),
		"count":   count,
	})
}

// RegisterRoutes registers all RAG-related routes on the given router.
func (h *RAGHandlers) RegisterRoutes(r chi.Router) {
	r.Get("/skills/search", h.SearchHandler)
	r.Get("/skills/suggest", h.SuggestHandler)
	r.Get("/skills/trending", h.TrendingHandler)
	r.Get("/skills/categories", h.CategoriesHandler)
	r.Post("/skills/{skillID}/publish", h.PublishHandler)
	r.Get("/skills/{skillID}/download", h.DownloadHandler)
	r.Post("/skills/reindex", h.ReindexHandler)
}
