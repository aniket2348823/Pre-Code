package router

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/sse"
	"github.com/vigilagent/vigilagent/internal/skillengine"
	"github.com/vigilagent/vigilagent/pkg/response"
)

const maxMiddlewareBody = 1 << 20 // 1 MiB

// middlewareProcessHandler is the core middleware endpoint that runs the
// full pipeline: context → skill match → scan → critique → extract patterns.
// Returns the result as JSON, or streams via SSE if Accept: text/event-stream.
func (r *Router) middlewareProcessHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	req.Body = http.MaxBytesReader(w, req.Body, maxMiddlewareBody)
	var input struct {
		TaskType    string         `json:"task_type"`
		Description string         `json:"description"`
		Code        string         `json:"code,omitempty"`
		Language    string         `json:"language,omitempty"`
		Filename    string         `json:"filename,omitempty"`
		Budget      float64        `json:"budget,omitempty"`
		Stream      bool           `json:"stream,omitempty"`
		Context     *contextInput  `json:"context,omitempty"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.JSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request exceeds 1MB limit"})
			return
		}
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Description == "" {
		response.BadRequest(w, "description is required")
		return
	}

	// Convert to middlewareInput
	mwInput := middlewareInput{
		TaskType:    input.TaskType,
		Description: input.Description,
		Code:        input.Code,
		Language:    input.Language,
		Filename:    input.Filename,
		Budget:      input.Budget,
		Context:     input.Context,
	}

	// SSE streaming mode
	if input.Stream || req.Header.Get("Accept") == "text/event-stream" {
		r.handleStreamingProcess(w, req, mwInput)
		return
	}

	// Standard JSON mode — run scanner + extraction
	result := r.runMiddlewarePipeline(req, mwInput)
	response.JSON(w, http.StatusOK, result)
}

// handleStreamingProcess runs the middleware pipeline and streams results via SSE.
func (r *Router) handleStreamingProcess(w http.ResponseWriter, req *http.Request, input middlewareInput) {
	stream := sse.NewStreamer(w)
	if stream == nil {
		response.InternalError(w, "streaming not supported")
		return
	}
	defer stream.Close()

	stream.SendStatus("processing", map[string]string{
		"task_type": input.TaskType,
		"message":   "Starting middleware pipeline...",
	})

	result := r.runMiddlewarePipeline(req, input)

	if result.ScanResult != nil && len(result.ScanResult.Findings) > 0 {
		stream.Send(sse.Event{Event: "findings", Data: result.ScanResult.Findings})
	}
	if len(result.SkillsExtracted) > 0 {
		stream.Send(sse.Event{Event: "patterns", Data: result.SkillsExtracted})
	}

	stream.SendDone(result)
}

// middlewareResult is the aggregated output of the middleware pipeline.
type middlewareResult struct {
	Description      string                        `json:"description"`
	TaskType         string                        `json:"task_type"`
	ScanResult       *scanner.Report               `json:"scan_result,omitempty"`
	SkillsExtracted  []*skillengine.Skill          `json:"skills_extracted,omitempty"`
	PipelineResult   *pipelineReport               `json:"pipeline_result,omitempty"`
	Metrics          map[string]interface{}         `json:"metrics,omitempty"`
}

type pipelineReport struct {
	Passed     bool    `json:"passed"`
	Confidence float64 `json:"confidence"`
	Layers     []layer `json:"layers"`
}

type layer struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

type contextInput struct {
	Files    []fileInput `json:"files,omitempty"`
	Language string      `json:"language,omitempty"`
}

type fileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type middlewareInput struct {
	TaskType    string
	Description string
	Code        string
	Language    string
	Filename    string
	Budget      float64
	Context     *contextInput
}

// runMiddlewarePipeline executes the deterministic pipeline (scan → extract patterns).
func (r *Router) runMiddlewarePipeline(req *http.Request, input middlewareInput) *middlewareResult {
	result := &middlewareResult{
		Description: input.Description,
		TaskType:    input.TaskType,
	}

	// Step 1: Run scanner if code is provided
	if input.Code != "" && r.engine != nil {
		report := r.engine.Run(req.Context(), scanner.Input{
			Language: input.Language,
			Code:     input.Code,
			Filename: input.Filename,
		})
		result.ScanResult = report
	}

	// Step 2: Run the unified validation pipeline
	if r.pipeline != nil {
		pipelineReq := &pipelineRequest{
			Description: input.Description,
			Code:        input.Code,
			Language:    input.Language,
			Filename:    input.Filename,
		}
		pipelineReport := r.runPipeline(req, pipelineReq)
		result.PipelineResult = pipelineReport
	}

	// Step 3: Extract vulnerability patterns from scanner findings
	if r.skillEng != nil && result.ScanResult != nil && len(result.ScanResult.Findings) > 0 {
		for _, f := range result.ScanResult.Findings {
			skill, _ := r.skillEng.ExtractFromFinding(skillengine.Finding{
				Severity:   string(f.Severity),
				Message:    f.Message,
				Filename:   f.Filename,
				Line:       f.Line,
				Fix:        f.Fix,
				Analyzers:  f.Analyzers,
				Confidence: f.Confidence,
			})
			result.SkillsExtracted = append(result.SkillsExtracted, skill)
		}
	}

	// Step 4: Compute metrics
	result.Metrics = map[string]interface{}{
		"findings_count":  result.findingCount(),
		"skills_extracted": len(result.SkillsExtracted),
		"pipeline_passed":  result.PipelineResult != nil && result.PipelineResult.Passed,
	}

	return result
}

// runPipeline executes the deterministic validation pipeline (requirements + compliance).
// Scanner results are already computed by runMiddlewarePipeline and should not be duplicated.
func (r *Router) runPipeline(_ *http.Request, input *pipelineRequest) *pipelineReport {
	if r.pipeline == nil {
		return nil
	}

	rep := &pipelineReport{
		Passed: true,
		Layers: []layer{},
	}

	// Run requirements resolver
	if r.requirements != nil {
		reqReport := r.requirements.Resolve(input.Description, nil)
		reqPassed := len(reqReport.Missing) == 0
		rep.Layers = append(rep.Layers, layer{Name: "requirements", Passed: reqPassed})
		if !reqPassed {
			rep.Passed = false
		}
	}

	// Run compliance checker
	if r.complianceChecker != nil {
		compReport := r.complianceChecker.Check(input.Description, nil)
		compPassed := len(compReport.Missing) == 0
		rep.Layers = append(rep.Layers, layer{Name: "compliance", Passed: compPassed})
		if !compPassed {
			rep.Passed = false
		}
	}

	// Compute confidence
	passed := 0
	for _, l := range rep.Layers {
		if l.Passed {
			passed++
		}
	}
	if len(rep.Layers) > 0 {
		rep.Confidence = float64(passed) / float64(len(rep.Layers))
	}

	return rep
}

// middlewareMetricsHandler returns pipeline metrics.
func (r *Router) middlewareMetricsHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	metrics := map[string]interface{}{
		"total_requests": 0,
		"message":        "middleware metrics placeholder",
	}

	if r.engine != nil {
		metrics["scanner_available"] = true
	}
	if r.skillEng != nil {
		metrics["skill_engine_available"] = true
		metrics["skill_count"] = r.skillEng.Count()
	}

	response.JSON(w, http.StatusOK, metrics)
}

// middlewarePatternsHandler lists extracted vulnerability patterns.
func (r *Router) middlewarePatternsHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	if r.skillEng == nil {
		response.JSON(w, http.StatusOK, []interface{}{})
		return
	}

	skills := r.skillEng.GetAllSkills()
	response.JSON(w, http.StatusOK, skills)
}

// pipelineRequest is the input to the pipeline.
type pipelineRequest struct {
	Description string
	Code        string
	Language    string
	Filename    string
}

// findingCount returns the number of findings in the scan result.
func (m *middlewareResult) findingCount() int {
	if m.ScanResult == nil {
		return 0
	}
	return len(m.ScanResult.Findings)
}
