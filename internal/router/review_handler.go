package router

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/pipeline"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// reviewHandler runs the full Shift-Zero pipeline:
// Main LLM → Deterministic Engine → Parallel Reviewer LLMs → Evidence → Knowledge Graph → Skill Extraction → Confidence → Output
//
// POST /api/v1/review
//
// Request body:
//
//	{
//	  "prompt": "Create a secure payment system",
//	  "code": "func main() { ... }",    // optional, generates from prompt if empty
//	  "language": "go",                  // optional, defaults to "go"
//	  "filename": "main.go",            // optional, defaults to "input.<lang>"
//	  "context": "existing project..."  // optional
//	}
func (r *Router) reviewHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	// Reject if no LLM router configured.
	if r.llmRouter == nil {
		response.Error(w, http.StatusServiceUnavailable, "LLM router not configured — review endpoint requires an LLM provider")
		return
	}

	var input pipeline.ReviewRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Prompt == "" && input.Code == "" {
		response.BadRequest(w, "prompt or code is required")
		return
	}

	// Validate and normalise language.
	input.Language = strings.TrimSpace(strings.ToLower(input.Language))
	switch input.Language {
	case "", "go":
		input.Language = "go"
	case "python", "py":
		input.Language = "python"
	case "javascript", "js":
		input.Language = "javascript"
	case "typescript", "ts":
		input.Language = "typescript"
	case "rust", "rs":
		input.Language = "rust"
	case "java":
		input.Language = "java"
	default:
		response.BadRequest(w, "unsupported language: "+input.Language+" — supported: go, python, javascript, typescript, rust, java")
		return
	}

	// Validate filename (no path traversal, reasonable length).
	if input.Filename != "" {
		input.Filename = strings.TrimSpace(input.Filename)
		if strings.Contains(input.Filename, "..") || strings.ContainsAny(input.Filename, "/\\") {
			response.BadRequest(w, "filename must not contain path separators or '..'")
			return
		}
		if len(input.Filename) > 255 {
			response.BadRequest(w, "filename too long (max 255 characters)")
			return
		}
	}

	// Build the full Shift-Zero pipeline using persistent Router-level instances.
	// IMPORTANT: These are NOT created per-request — they live on the Router struct
	// and survive across requests by design. This allows findings, skills, and
	// knowledge graph edges to accumulate over time, making the system smarter
	// with every review. Do not replace with per-request instances.
	szp := pipeline.NewShiftZeroPipeline(
		r.llmRouter,
		r.engine,
		r.knowledge,
		r.skillEng,
		r.attackGraph,
		r.confidence,
		r.pipeline,
	)

	report, err := szp.Run(req.Context(), &input)
	if err != nil {
		response.InternalError(w, "review pipeline failed: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, report)
}
