package router

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/skills"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// RAGHandlers provides HTTP handlers for the skill marketplace RAG system.
type RAGHandlers struct {
	rag       *skills.RAGEngine
	skillRepo *repository.SkillRepository
}

// NewRAGHandlers creates new RAG HTTP handlers.
func NewRAGHandlers(rag *skills.RAGEngine, skillRepo *repository.SkillRepository) *RAGHandlers {
	return &RAGHandlers{rag: rag, skillRepo: skillRepo}
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
		response.InternalError(w, "search failed: "+err.Error())
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
		response.InternalError(w, "suggestion failed: "+err.Error())
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
		response.InternalError(w, "failed to get trending skills: "+err.Error())
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
		response.InternalError(w, "failed to get categories: "+err.Error())
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
		response.BadRequest(w, "failed to parse upload: "+err.Error())
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
		response.BadRequest(w, "failed to read package: "+err.Error())
		return
	}

	// Scan the package for security issues
	scanner := skills.NewSkillScanner()
	result, err := scanner.ScanPackage(req.Context(), packageData)
	if err != nil {
		response.BadRequest(w, "package scan failed: "+err.Error())
		return
	}

	if !result.Passed {
		response.BadRequest(w, fmt.Sprintf("package failed security scan (score: %.2f)", result.Score))
		return
	}

	// Mark as published and update
	err = h.skillRepo.Update(req.Context(), skillID, skill.Name, skill.Description, skill.Version, skill.Category)
	if err != nil {
		response.InternalError(w, "failed to update skill: "+err.Error())
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
		response.InternalError(w, "reindex failed: "+err.Error())
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
