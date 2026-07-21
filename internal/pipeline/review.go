// Package pipeline implements the Shift-Zero review pipeline:
//
//	Main LLM → Deterministic Engine → Parallel Specialized Reviewer LLMs
//	  → Evidence Aggregation → Knowledge Graph → Skill Extraction
//	  → Confidence Scoring → Final Output
//
// This is the middleware between the user and the LLM — the trust layer
// that makes AI-generated software design verifiable and trustworthy.
package pipeline

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/attackgraph"
	"github.com/vigilagent/vigilagent/internal/confidence"
	"github.com/vigilagent/vigilagent/internal/knowledge"
	"github.com/vigilagent/vigilagent/internal/llm"
	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/skillengine"
)

// ReviewRequest is the input to the full Shift-Zero pipeline.
type ReviewRequest struct {
	// Prompt is the developer's original request (e.g. "Create a secure payment system").
	Prompt string `json:"prompt"`
	// Code is optional code to review (if empty, LLM generates from prompt).
	Code string `json:"code,omitempty"`
	// Language is the programming language.
	Language string `json:"language,omitempty"`
	// Filename is the optional filename for context-aware scanning.
	Filename string `json:"filename,omitempty"`
	// Context provides additional project context for reviewers.
	Context string `json:"context,omitempty"`
}

// ReviewReport is the complete output of the Shift-Zero pipeline.
type ReviewReport struct {
	// OriginalPrompt is what the developer asked.
	OriginalPrompt string `json:"original_prompt"`
	// MainLLMResponse is the initial LLM output.
	MainLLMResponse string `json:"main_llm_response"`
	// Deterministic findings (Layer 1-5 of the engine).
	DeterministicFindings []scanner.Finding `json:"deterministic_findings"`
	// Reviewer outputs from all parallel specialized LLMs.
	Reviewers []ReviewerOutput `json:"reviewers"`
	// Evidence from all sources combined.
	Evidence []confidence.Evidence `json:"evidence"`
	// Knowledge graph context that was applied.
	KnowledgeGraphContext string `json:"knowledge_graph_context,omitempty"`
	// Skills extracted from this review.
	Skills []*skillengine.Skill `json:"skills,omitempty"`
	// Attack paths identified.
	AttackPaths string `json:"attack_paths,omitempty"`
	// Calibrated confidence score.
	Confidence *confidence.Score `json:"confidence"`
	// Final improved output (after review + fix).
	FinalOutput string `json:"final_output"`
	// Retries used (max 2).
	Retries int `json:"retries"`
	// Total duration of the pipeline.
	Duration time.Duration `json:"duration"`
	// Summary for the developer.
	Summary string `json:"summary"`
}

// reviewerDef defines a specialized reviewer LLM.
type reviewerDef struct {
	name        string
	role        string
	instruction string
}

// ReviewerOutput holds the output from a single specialized reviewer LLM.
type ReviewerOutput struct {
	Name    string `json:"name"`    // e.g. "security", "architecture", "cost"
	Role    string `json:"role"`    // e.g. "Principal Security Architect"
	Verdict string `json:"verdict"` // "pass", "fail", "warn"
	Findings []string `json:"findings"` // specific issues found
	Suggestions []string `json:"suggestions"` // improvement suggestions
	RawOutput string `json:"raw_output"` // full LLM response
}

// ShiftZeroPipeline is the full review pipeline orchestrator.
type ShiftZeroPipeline struct {
	llmRouter    *llm.ModelRouter
	engine       *scanner.Engine
	knowledge    *knowledge.Graph
	skills       *skillengine.Engine
	attackGraph  *attackgraph.Engine
	confidence   *confidence.Engine
	pipeline     *Pipeline
}

// NewShiftZeroPipeline creates the full pipeline with all components.
func NewShiftZeroPipeline(
	router *llm.ModelRouter,
	engine *scanner.Engine,
	kg *knowledge.Graph,
	skills *skillengine.Engine,
	ag *attackgraph.Engine,
	conf *confidence.Engine,
	pipeline *Pipeline,
) *ShiftZeroPipeline {
	if engine == nil {
		engine = scanner.NewEngine(scanner.NewBuiltinAnalyzer())
	}
	if kg == nil {
		kg = knowledge.NewGraph()
	}
	if skills == nil {
		skills = skillengine.NewEngine()
	}
	if ag == nil {
		ag = attackgraph.NewEngine()
	}
	if conf == nil {
		conf = confidence.NewEngine()
	}
	return &ShiftZeroPipeline{
		llmRouter:   router,
		engine:      engine,
		knowledge:   kg,
		skills:      skills,
		attackGraph: ag,
		confidence:  conf,
		pipeline:    pipeline,
	}
}

// Run executes the full Shift-Zero pipeline.
//
// Flow:
//  1. Main LLM generates initial response (or uses provided code)
//  2. Deterministic Engine scans everything (5 layers)
//  3. Parallel Specialized Reviewer LLMs attack/challenge the output
//  4. Evidence Engine aggregates all findings
//  5. Knowledge Graph validates relationships
//  6. Skill Engine extracts reusable skills from validated findings
//  7. Confidence Engine produces calibrated score
//  8. If critical issues found → re-validate (max 2 retries)
//  9. Final Output
func (szp *ShiftZeroPipeline) Run(ctx context.Context, req *ReviewRequest) (*ReviewReport, error) {
	start := time.Now()
	report := &ReviewReport{
		OriginalPrompt: req.Prompt,
	}

	// ════════════════════════════════════════════════════════════════
	// STAGE 1: Main LLM generates initial response
	// ════════════════════════════════════════════════════════════════
	var mainResponse string
	if req.Code != "" {
		// Code provided directly — use it.
		mainResponse = req.Code
	} else {
		// Generate from prompt using Main LLM.
		resp, err := szp.runMainLLM(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("main LLM failed: %w", err)
		}
		mainResponse = resp
	}
	report.MainLLMResponse = mainResponse

	// ════════════════════════════════════════════════════════════════
	// STAGE 2: Deterministic Engine scans everything
	// ════════════════════════════════════════════════════════════════
	codeToScan := mainResponse
	if req.Code != "" {
		codeToScan = req.Code
	}

	language := req.Language
	if language == "" {
		language = inferLanguage(req.Prompt, mainResponse)
	}
	filename := req.Filename
	if filename == "" {
		filename = "input." + languageToFileExt(language)
	}

	scanReport := szp.engine.Run(ctx, scanner.Input{
		Language: language,
		Code:     codeToScan,
		Filename: filename,
	})
	report.DeterministicFindings = scanReport.Findings

	// Also run the full validation pipeline (schema + requirements + compliance + scan).
	if szp.pipeline != nil {
		pipelineReq := &Request{
			Description: req.Prompt,
			Code:        codeToScan,
			Language:    language,
			Filename:    filename,
		}
		szp.pipeline.Run(ctx, pipelineReq)
	}

	// ════════════════════════════════════════════════════════════════
	// STAGE 3: Parallel Specialized Reviewer LLMs
	// ════════════════════════════════════════════════════════════════
	// Build context for reviewers: main LLM output + deterministic findings.
	reviewerContext := szp.buildReviewerContext(mainResponse, scanReport.Findings, req.Context)

	// Run ALL reviewers in parallel.
	reviewers := szp.runReviewersInParallel(ctx, reviewerContext, req.Prompt)
	report.Reviewers = reviewers

	// ════════════════════════════════════════════════════════════════
	// STAGE 4: Evidence Engine — aggregate all evidence
	// ════════════════════════════════════════════════════════════════
	evidence := szp.aggregateEvidence(scanReport.Findings, reviewers)
	report.Evidence = evidence

	// ════════════════════════════════════════════════════════════════
	// STAGE 5: Knowledge Graph validation
	// ════════════════════════════════════════════════════════════════
	kgContext := szp.validateKnowledgeGraph(req.Prompt, mainResponse)
	if kgContext != "" {
		report.KnowledgeGraphContext = kgContext
		// Add KG findings to evidence.
		evidence = append(evidence, confidence.Evidence{
			Source:  "knowledge_graph",
			Verdict: "warn",
			Detail:  kgContext,
			Weight:  0.7,
		})
		report.Evidence = evidence
	}

	// ════════════════════════════════════════════════════════════════
	// STAGE 6: Skill Extraction
	// ════════════════════════════════════════════════════════════════
	skills := szp.extractSkills(scanReport.Findings, reviewers)
	report.Skills = skills

	// ════════════════════════════════════════════════════════════════
	// STAGE 7: Attack Graph Generation
	// ════════════════════════════════════════════════════════════════
	if len(scanReport.Findings) > 0 {
		attackResp := szp.attackGraph.Generate(ctx, attackgraph.FindingsRequest{
			Description: req.Prompt,
			Entity:      inferEntityFromPrompt(req.Prompt),
		})
		if len(attackResp.Paths) > 0 {
			var paths []string
			for _, ap := range attackResp.Paths {
				parts := make([]string, 0, len(ap.Steps))
				for _, step := range ap.Steps {
					parts = append(parts, step.Action)
				}
				paths = append(paths, fmt.Sprintf("%s: %s", ap.Name, strings.Join(parts, " → ")))
			}
			report.AttackPaths = strings.Join(paths, "\n")
		}
	}

	// ════════════════════════════════════════════════════════════════
	// STAGE 8: Confidence Engine — calibrated score
	// ════════════════════════════════════════════════════════════════
	score := szp.confidence.Score(report.Evidence)
	report.Confidence = score

	// ════════════════════════════════════════════════════════════════
	// STAGE 9: Re-validation loop (max 2 retries)
	// ════════════════════════════════════════════════════════════════
	if score.Failed > 0 {
		for retry := 0; retry < 2; retry++ {
			// Ask the Security Reviewer to fix the issues.
			fixedCode, fixErr := szp.runSecurityFix(ctx, mainResponse, scanReport.Findings, reviewers)
			if fixErr != nil {
				break
			}

			report.Retries++
			mainResponse = fixedCode
			report.MainLLMResponse = fixedCode

			// Re-scan the fixed code.
			reScan := szp.engine.Run(ctx, scanner.Input{
				Language: language,
				Code:     fixedCode,
				Filename: filename,
			})

			// Re-check with reviewers if issues remain.
			if len(reScan.Findings) == 0 {
				// All clear — update findings and break.
				report.DeterministicFindings = reScan.Findings
				report.Evidence = append(report.Evidence, confidence.Evidence{
					Source:  "revalidation",
					Verdict: "pass",
					Detail:  fmt.Sprintf("Retry %d: all issues resolved", retry+1),
					Weight:  0.9,
				})
				report.Confidence = szp.confidence.Score(report.Evidence)
				break
			}

			// Still has issues — update and continue.
			report.DeterministicFindings = reScan.Findings
			report.Evidence = append(report.Evidence, confidence.Evidence{
				Source:  "revalidation",
				Verdict: "fail",
				Detail:  fmt.Sprintf("Retry %d: %d issues remain", retry+1, len(reScan.Findings)),
				Weight:  0.9,
			})
			report.Confidence = szp.confidence.Score(report.Evidence)
		}
	}

	// ════════════════════════════════════════════════════════════════
	// FINAL: Build summary and output
	// ════════════════════════════════════════════════════════════════
	report.FinalOutput = mainResponse
	report.Duration = time.Since(start)
	report.Summary = szp.buildSummary(report)

	return report, nil
}

// ═══════════════════════════════════════════════════════════════════════════
// HELPER METHODS
// ═══════════════════════════════════════════════════════════════════════════

// runMainLLM calls the Main LLM to generate initial response from prompt.
func (szp *ShiftZeroPipeline) runMainLLM(ctx context.Context, req *ReviewRequest) (string, error) {
	if szp.llmRouter == nil {
		return "", fmt.Errorf("no LLM router configured")
	}

	messages := []llm.Message{
		{Role: "system", Content: "You are a senior software architect. Generate clean, well-structured, production-ready code. Include security best practices by default."},
		{Role: "user", Content: req.Prompt},
	}
	if req.Context != "" {
		messages[1].Content += "\n\nAdditional context: " + req.Context
	}

	resp, err := szp.llmRouter.ExecuteWithFailover(ctx, &llm.Task{
		ID:          "shift-zero-main",
		Type:        "architecture",
		Description: "Main LLM initial response",
		Tags:        []string{"architecture", "security"},
		Messages:    messages,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// buildReviewerContext builds the context string for specialized reviewers.
func (szp *ShiftZeroPipeline) buildReviewerContext(mainLLM string, findings []scanner.Finding, projectCtx string) string {
	var sb strings.Builder
	sb.WriteString("=== MAIN LLM OUTPUT ===\n")
	sb.WriteString(mainLLM)
	sb.WriteString("\n\n")

	if len(findings) > 0 {
		sb.WriteString("=== DETERMINISTIC ENGINE FINDINGS ===\n")
		for i, f := range findings {
			sb.WriteString(fmt.Sprintf("%d. [%s] %s (confidence: %.0f%%)\n   Line %d: %s\n   Fix: %s\n\n",
				i+1, strings.ToUpper(string(f.Severity)), f.Message, f.Confidence*100,
				f.Line, f.Snippet, f.Fix))
		}
	}

	if projectCtx != "" {
		sb.WriteString("=== PROJECT CONTEXT ===\n")
		sb.WriteString(projectCtx)
		sb.WriteString("\n")
	}

	return sb.String()
}

// runReviewersInParallel runs all specialized reviewer LLMs concurrently.
func (szp *ShiftZeroPipeline) runReviewersInParallel(ctx context.Context, reviewerContext string, prompt string) []ReviewerOutput {
	defs := []reviewerDef{
		{
			name: "security",
			role: "Principal Security Architect",
			instruction: `You are a Principal Security Architect. Review the output above with a red-team mindset.

Find:
- Attack vectors and vulnerabilities
- Authentication/authorization gaps
- Injection risks (SQL, XSS, command, etc.)
- Secrets exposure risks
- Missing security controls
- Compliance violations (SOC2, GDPR, HIPAA, PCI DSS)

For each finding, provide:
1. Issue description
2. Severity (Critical/High/Medium/Low)
3. Specific fix recommendation

Be brutally honest. Do not agree with the output without evidence.`,
		},
		{
			name: "architecture",
			role: "Staff Software Engineer",
			instruction: `You are a Staff Software Engineer reviewing architecture. Check for:
- Design anti-patterns and coupling issues
- Scalability bottlenecks
- Single points of failure
- Missing abstractions or over-engineering
- Data flow issues
- Error handling gaps
- Missing observability (logging, metrics, tracing)

For each finding, provide specific improvement suggestions with trade-offs.`,
		},
		{
			name: "compliance",
			role: "DevSecOps Engineer",
			instruction: `You are a DevSecOps Engineer reviewing compliance posture. Check for:
- SOC2 requirements (access control, audit logs, encryption at rest/transit)
- GDPR requirements (data minimization, right to erasure, consent)
- PCI DSS requirements (cardholder data protection, network segmentation)
- HIPAA requirements (PHI protection, audit trails)
- OWASP Top 10 coverage

Map each finding to a specific compliance control.`,
		},
		{
			name: "cost",
			role: "Cloud Architect & Startup CTO",
			instruction: `You are a Cloud Architect and Startup CTO reviewing cost efficiency. Check for:
- Over-engineering (do we need all these services?)
- Expensive cloud patterns (unnecessary multi-AZ, over-provisioned instances)
- Missing cost optimization opportunities
- Unnecessary complexity in the architecture
- Whether simpler alternatives exist

Focus on what can be simplified without sacrificing security.`,
		},
		{
			name: "red_team",
			role: "Red Team Agent",
			instruction: `You are a Red Team security tester. Your job is to ATTACK the output above.
- What would a malicious user do?
- How would you bypass the security controls?
- What edge cases could break the system?
- What data could be leaked?
- What privilege escalation paths exist?

Think like an adversary. Find the weaknesses that defenders miss.`,
		},
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var outputs []ReviewerOutput

	for _, def := range defs {
		wg.Add(1)
		go func(d reviewerDef) {
			defer wg.Done()
			output := szp.runSingleReviewer(ctx, reviewerContext, prompt, d)
			mu.Lock()
			outputs = append(outputs, output)
			mu.Unlock()
		}(def)
	}

	wg.Wait()
	return outputs
}

// runSingleReviewer runs one specialized reviewer LLM.
func (szp *ShiftZeroPipeline) runSingleReviewer(ctx context.Context, context string, prompt string, def reviewerDef) ReviewerOutput {
	if szp.llmRouter == nil {
		return ReviewerOutput{
			Name:    def.name,
			Role:    def.role,
			Verdict: "error",
			Findings: []string{"LLM router not configured"},
		}
	}

	messages := []llm.Message{
		{Role: "system", Content: def.instruction},
		{Role: "user", Content: fmt.Sprintf("Original developer request: %s\n\n%s", prompt, context)},
	}

	resp, err := szp.llmRouter.ExecuteWithFailover(ctx, &llm.Task{
		ID:          "reviewer-" + def.name,
		Type:        "security",
		Description: def.role + " review",
		Tags:        []string{"review", def.name},
		Messages:    messages,
	})

	if err != nil {
		return ReviewerOutput{
			Name:    def.name,
			Role:    def.role,
			Verdict: "error",
			Findings: []string{err.Error()},
		}
	}

	// Parse the response to extract findings and suggestions.
	verdict, findings, suggestions := parseReviewerOutput(resp.Content)

	return ReviewerOutput{
		Name:        def.name,
		Role:        def.role,
		Verdict:     verdict,
		Findings:    findings,
		Suggestions: suggestions,
		RawOutput:   resp.Content,
	}
}

// runSecurityFix asks the Security Reviewer to fix the identified issues.
func (szp *ShiftZeroPipeline) runSecurityFix(ctx context.Context, originalCode string, findings []scanner.Finding, reviewers []ReviewerOutput) (string, error) {
	if szp.llmRouter == nil {
		return originalCode, fmt.Errorf("no LLM router configured")
	}

	// Collect all reviewer suggestions.
	var suggestions []string
	for _, r := range reviewers {
		if r.Verdict == "fail" {
			suggestions = append(suggestions, r.Suggestions...)
		}
	}

	// Build the fix prompt.
	var findingsText strings.Builder
	for _, f := range findings {
		findingsText.WriteString(fmt.Sprintf("- [%s] Line %d: %s (Fix: %s)\n",
			strings.ToUpper(string(f.Severity)), f.Line, f.Message, f.Fix))
	}

	messages := []llm.Message{
		{Role: "system", Content: "You are a code improvement engine. Return ONLY the fixed code. No explanations, no markdown, just clean code. Fix every security issue identified."},
		{Role: "user", Content: fmt.Sprintf("Security findings to fix:\n%s\n\nReviewer suggestions:\n%s\n\nOriginal code:\n```%s\n%s\n```\n\nReturn the COMPLETE fixed code:", findingsText.String(), strings.Join(suggestions, "\n"), "go", originalCode)},
	}

	resp, err := szp.llmRouter.ExecuteWithFailover(ctx, &llm.Task{
		ID:          "security-fix",
		Type:        "bug_fix",
		Description: "Security fix based on reviewer findings",
		Tags:        []string{"security", "fix"},
		Messages:    messages,
	})
	if err != nil {
		return originalCode, err
	}

	// Strip markdown code fences.
	fixed := strings.TrimSpace(resp.Content)
	fixed = strings.TrimPrefix(fixed, "```go")
	fixed = strings.TrimPrefix(fixed, "```")
	fixed = strings.TrimSuffix(fixed, "```")
	fixed = strings.TrimSpace(fixed)

	return fixed, nil
}

// aggregateEvidence combines scanner findings and reviewer outputs into evidence.
func (szp *ShiftZeroPipeline) aggregateEvidence(findings []scanner.Finding, reviewers []ReviewerOutput) []confidence.Evidence {
	var evidence []confidence.Evidence

	// Scanner findings → evidence.
	for _, f := range findings {
		verdict := "pass"
		if f.Severity == scanner.SeverityCritical || f.Severity == scanner.SeverityHigh {
			verdict = "fail"
		} else if f.Severity == scanner.SeverityMedium {
			verdict = "warn"
		}
		evidence = append(evidence, confidence.Evidence{
			Source:   "deterministic_engine",
			Verdict:  verdict,
			Severity: string(f.Severity),
			Detail:   f.Message,
			Weight:   0.8,
		})
	}

	// Reviewer outputs → evidence.
	for _, r := range reviewers {
		evidence = append(evidence, confidence.Evidence{
			Source:  "reviewer_" + r.Name,
			Verdict: r.Verdict,
			Detail:  fmt.Sprintf("%s: %d findings", r.Role, len(r.Findings)),
			Weight:  0.6,
		})
	}

	return evidence
}

// validateKnowledgeGraph checks the prompt against the knowledge graph.
func (szp *ShiftZeroPipeline) validateKnowledgeGraph(prompt string, llmOutput string) string {
	lower := strings.ToLower(prompt + " " + llmOutput)

	// Check for services that should have specific controls.
	controls := map[string][]string{
		"payment":  {"audit_log", "encryption", "rate_limiting", "fraud_detection"},
		"auth":     {"session_management", "mfa", "password_policy"},
		"admin":    {"access_control", "audit_log", "mfa"},
		"database": {"encryption_at_rest", "backup", "access_control"},
		"api":      {"rate_limiting", "input_validation", "authentication"},
	}

	var missing []string
	for service, required := range controls {
		if strings.Contains(lower, service) {
			for _, ctrl := range required {
				if !strings.Contains(lower, strings.ReplaceAll(ctrl, "_", " ")) &&
					!strings.Contains(lower, strings.ReplaceAll(ctrl, "_", "-")) {
					missing = append(missing, fmt.Sprintf("%s requires %s", service, ctrl))
				}
			}
		}
	}

	if len(missing) > 0 {
		return "Knowledge Graph warnings: " + strings.Join(missing, "; ")
	}
	return ""
}

// extractSkills converts validated findings into reusable skills.
func (szp *ShiftZeroPipeline) extractSkills(findings []scanner.Finding, reviewers []ReviewerOutput) []*skillengine.Skill {
	var skills []*skillengine.Skill

	for _, f := range findings {
		skill, _ := szp.skills.ExtractFromFinding(skillengine.Finding{
			Severity:  string(f.Severity),
			Message:   f.Message,
			Filename:  f.Filename,
			Line:      f.Line,
			Fix:       f.Fix,
			Analyzers: f.Analyzers,
			Confidence: f.Confidence,
		})
		skills = append(skills, skill)
	}

	return skills
}

// buildSummary creates a human-readable summary of the review.
func (szp *ShiftZeroPipeline) buildSummary(report *ReviewReport) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Review completed in %s.\n", report.Duration.Round(time.Millisecond)))

	// Findings summary.
	totalFindings := len(report.DeterministicFindings)
	critical := 0
	high := 0
	for _, f := range report.DeterministicFindings {
		if f.Severity == scanner.SeverityCritical {
			critical++
		} else if f.Severity == scanner.SeverityHigh {
			high++
		}
	}
	sb.WriteString(fmt.Sprintf("Deterministic engine: %d findings (%d critical, %d high).\n", totalFindings, critical, high))

	// Reviewer summary.
	passCount, failCount, warnCount := 0, 0, 0
	for _, r := range report.Reviewers {
		switch r.Verdict {
		case "pass":
			passCount++
		case "fail":
			failCount++
		case "warn":
			warnCount++
		}
	}
	sb.WriteString(fmt.Sprintf("Reviewers: %d passed, %d failed, %d warnings.\n", passCount, failCount, warnCount))

	// Confidence.
	if report.Confidence != nil {
		sb.WriteString(fmt.Sprintf("Confidence: %s (%.0f%%) — %s\n", report.Confidence.Grade, report.Confidence.Confidence*100, report.Confidence.Reason))
	}

	// Skills extracted.
	if len(report.Skills) > 0 {
		sb.WriteString(fmt.Sprintf("Skills extracted: %d\n", len(report.Skills)))
	}

	// Retries.
	if report.Retries > 0 {
		sb.WriteString(fmt.Sprintf("Re-validation retries: %d\n", report.Retries))
	}

	return sb.String()
}

// ═══════════════════════════════════════════════════════════════════════════
// UTILITY FUNCTIONS
// ═══════════════════════════════════════════════════════════════════════════

func inferLanguage(prompt, code string) string {
	lower := strings.ToLower(prompt + " " + code)
	switch {
	case strings.Contains(lower, "python") || strings.Contains(lower, "django") || strings.Contains(lower, "flask"):
		return "python"
	case strings.Contains(lower, "javascript") || strings.Contains(lower, "node") || strings.Contains(lower, "react"):
		return "javascript"
	case strings.Contains(lower, "typescript") || strings.Contains(lower, "next"):
		return "typescript"
	case strings.Contains(lower, "rust") || strings.Contains(lower, "cargo"):
		return "rust"
	case strings.Contains(lower, "java") || strings.Contains(lower, "spring"):
		return "java"
	default:
		return "go"
	}
}

func languageToFileExt(lang string) string {
	switch lang {
	case "python":
		return "py"
	case "javascript":
		return "js"
	case "typescript":
		return "ts"
	case "rust":
		return "rs"
	case "java":
		return "java"
	default:
		return "go"
	}
}

func inferEntityFromPrompt(prompt string) string {
	lower := strings.ToLower(prompt)
	switch {
	case strings.Contains(lower, "payment") || strings.Contains(lower, "billing"):
		return "payment"
	case strings.Contains(lower, "auth") || strings.Contains(lower, "login"):
		return "auth"
	case strings.Contains(lower, "api"):
		return "api"
	case strings.Contains(lower, "database") || strings.Contains(lower, "db"):
		return "database"
	default:
		return "service"
	}
}

func parseReviewerOutput(content string) (verdict string, findings []string, suggestions []string) {
	lower := strings.ToLower(content)

	// Determine verdict.
	verdict = "pass"
	if strings.Contains(lower, "critical") || strings.Contains(lower, "high severity") || strings.Contains(lower, "rejected") {
		verdict = "fail"
	} else if strings.Contains(lower, "warning") || strings.Contains(lower, "medium") {
		verdict = "warn"
	}

	// Extract findings (lines starting with - or • or numbered).
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "• ") || strings.HasPrefix(line, "* ") {
			finding := strings.TrimPrefix(line, "- ")
			finding = strings.TrimPrefix(finding, "• ")
			finding = strings.TrimPrefix(finding, "* ")
			if finding != "" {
				if strings.Contains(strings.ToLower(finding), "add") || strings.Contains(strings.ToLower(finding), "implement") || strings.Contains(strings.ToLower(finding), "use") {
					suggestions = append(suggestions, finding)
				} else {
					findings = append(findings, finding)
				}
			}
		}
	}

	return
}
