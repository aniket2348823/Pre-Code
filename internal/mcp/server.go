// Package mcp implements the VigilAgent MCP (Model Context Protocol) server.
// It exposes VigilAgent's deterministic verification pipeline as MCP tools
// that can be consumed by Cursor, Cline, Claude Desktop, and other MCP clients.
//
// Architecture:
//
//	MCP Client (Cursor/Cline) ──stdio──▶ MCP Server ──HTTP──▶ VigilAgent Backend
//	                                     (this binary)              /api/v1/review
//	                                                              /api/v1/middleware/process
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/vigilagent/vigilagent/internal/signing"
)

// Server is the VigilAgent MCP server.
type Server struct {
	apiURL     string
	gatewayURL string // optional: separate gateway URL for /v1/provenance tools
	apiKey     string
	llmKey     string // optional: user's LLM key for BYOK via env var
	client     *http.Client
	mcpServer  *server.MCPServer
}

// NewServer creates a new VigilAgent MCP server.
func NewServer(apiURL, apiKey, llmKey string) *Server {
	s := &Server{
		apiURL: apiURL,
		apiKey: apiKey,
		llmKey: llmKey,
		client: &http.Client{
			Timeout: 120 * time.Second, // review pipeline can be slow
		},
	}
	s.mcpServer = s.buildMCPServer()
	return s
}

// SetGatewayURL points the provenance/audit tools (verify_provenance,
// create_scan_attestation) at the Secure AI Gateway, which serves
// /v1/provenance/*. When unset, those tools fall back to apiURL — which works
// when the backend and gateway are co-located or for tests.
func (s *Server) SetGatewayURL(url string) {
	s.gatewayURL = url
}

// Run starts the MCP server on the stdio transport.
// mcp-go's ServeStdio already exits cleanly when stdin closes (EOF -> return
// nil, see StdioServer.processInputStream), so no extra EOF watcher is needed.
// A concurrent reader on os.Stdin would race with mcp-go's own bufio reader
// and steal bytes from the JSON-RPC stream, corrupting the protocol.
func (s *Server) Run() error {
	return server.ServeStdio(s.mcpServer)
}

// ─── MCP Server Construction ─────────────────────────────────────────────

func (s *Server) buildMCPServer() *server.MCPServer {
	srv := server.NewMCPServer(
		"vigilagent",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Tool 1: vigil_verify — Full Shift-Zero pipeline
	srv.AddTool(
		mcp.NewTool("vigil_verify",
			mcp.WithDescription("Run the full VigilAgent Shift-Zero verification pipeline on code. Returns findings, confidence score, reviewer verdicts, and fixed code. Use this when you need to verify code quality, security, and architecture."),
			mcp.WithString("code",
				mcp.Required(),
				mcp.Description("The source code to verify"),
			),
			mcp.WithString("prompt",
				mcp.Description("The original developer request or context (e.g. 'Create a secure payment system')"),
			),
			mcp.WithString("language",
				mcp.Description("Programming language: go, python, javascript, typescript, rust, java"),
			),
			mcp.WithString("filename",
				mcp.Description("Filename for context-aware scanning (e.g. 'main.go')"),
			),
			mcp.WithString("api_key",
				mcp.Description("Your LLM provider API key (e.g. sk-...). When provided, the backend uses your key for the review pipeline instead of its own configured keys."),
			),
		),
		s.handleVerify,
	)

	// Tool 2: vigil_scan — Deterministic engine only (fast, no LLM cost)
	srv.AddTool(
		mcp.NewTool("vigil_scan",
			mcp.WithDescription("Run VigilAgent's deterministic static analysis engine on code. Returns findings with severity, confidence, and fix suggestions. No LLM calls — pure deterministic scanning."),
			mcp.WithString("code",
				mcp.Required(),
				mcp.Description("The source code to scan"),
			),
			mcp.WithString("language",
				mcp.Description("Programming language: go, python, javascript, typescript, rust, java"),
			),
			mcp.WithString("filename",
				mcp.Description("Filename for context-aware scanning"),
			),
		),
		s.handleScan,
	)

	// Tool 3: vigil_review — Run LLM reviewers only (all 5 roles, single call)
	srv.AddTool(
		mcp.NewTool("vigil_review",
			mcp.WithDescription("Run VigilAgent's specialized LLM reviewers on code. All 5 reviewer roles (Security Architect, Staff Engineer, DevSecOps, Cloud Architect, Red Team) run in a SINGLE LLM call. Returns per-role verdicts."),
			mcp.WithString("code",
				mcp.Required(),
				mcp.Description("The source code to review"),
			),
			mcp.WithString("prompt",
				mcp.Description("The original developer request or context"),
			),
			mcp.WithString("language",
				mcp.Description("Programming language"),
			),
			mcp.WithString("api_key",
				mcp.Description("Your LLM provider API key. When provided, the backend uses your key for the review pipeline."),
			),
		),
		s.handleReview,
	)

	// Tool 4: vigil_confidence — Get confidence score
	srv.AddTool(
		mcp.NewTool("vigil_confidence",
			mcp.WithDescription("Compute a calibrated confidence score for code based on deterministic analysis and reviewer evidence. Returns a grade (A-F) and percentage."),
			mcp.WithString("code",
				mcp.Required(),
				mcp.Description("The source code to score"),
			),
			mcp.WithString("language",
				mcp.Description("Programming language"),
			),
			mcp.WithString("api_key",
				mcp.Description("Your LLM provider API key. When provided, the backend uses your key for the review pipeline."),
			),
		),
		s.handleConfidence,
	)

	// Tool 5: vigil_process — Middleware pipeline
	srv.AddTool(
		mcp.NewTool("vigil_process",
			mcp.WithDescription("Run VigilAgent's middleware pipeline: scan code, validate requirements, check compliance, and extract reusable patterns. Returns structured results with metrics."),
			mcp.WithString("description",
				mcp.Required(),
				mcp.Description("Description of what the code is supposed to do"),
			),
			mcp.WithString("code",
				mcp.Description("Source code to process (optional, can scan without code for requirements/compliance check)"),
			),
			mcp.WithString("language",
				mcp.Description("Programming language"),
			),
			mcp.WithString("task_type",
				mcp.Description("Task type: bug_fix, feature, refactoring, security, architecture"),
			),
		),
		s.handleProcess,
	)

	// Tool 6: vigil_dual_engine — Parallel dual-engine analysis (deterministic + LLM)
	srv.AddTool(
		mcp.NewTool("vigil_dual_engine",
			mcp.WithDescription("Run VigilAgent's parallel dual-engine analysis on code. BOTH engines run simultaneously: deterministic (Semgrep, builtin rules) + LLM (GPT-4o-mini semantic analysis). Findings are merged with corroboration scoring — issues found by both engines get HIGH confidence. Returns grade, score, and detailed findings."),
			mcp.WithString("code",
				mcp.Required(),
				mcp.Description("The source code to analyze with dual-engine"),
			),
			mcp.WithString("language",
				mcp.Description("Programming language: go, python, javascript, typescript, rust, java"),
			),
			mcp.WithString("api_key",
				mcp.Description("Your LLM provider API key (e.g. sk-...). When provided, the backend uses your key for the LLM engine instead of its own configured keys."),
			),
		),
		s.handleDualEngine,
	)

	// Tool 7: vigil_improve — Improve AI-generated code via the verification pipeline
	srv.AddTool(
		mcp.NewTool("vigil_improve",
			mcp.WithDescription("Improve an AI-generated code snippet using VigilAgent's dual-engine verification pipeline. Runs deterministic analysis and LLM review in parallel, then returns the improved code with the issues found and fixed. Use this after an LLM produces code, to harden and correct it."),
			mcp.WithString("code",
				mcp.Required(),
				mcp.Description("The AI-generated source code to improve"),
			),
			mcp.WithString("prompt",
				mcp.Description("The original request the code was generated for (context)"),
			),
			mcp.WithString("language",
				mcp.Description("Programming language: go, python, javascript, typescript, rust, java"),
			),
			mcp.WithString("filename",
				mcp.Description("Filename for context-aware scanning"),
			),
			mcp.WithString("api_key",
				mcp.Description("Your LLM provider API key. When provided, the backend uses your key for the review pipeline."),
			),
		), s.handleImprove,
	)

	// Tool 8: vigil_suggest — Line-anchored accept/reject suggestions
	// Runs ALL 5 specialized reviewers (Security Architect, Staff Engineer,
	// DevSecOps, Cloud Architect, Red Team) in a SINGLE LLM call and returns
	// per-line fixes the user can accept or reject.
	srv.AddTool(
		mcp.NewTool("vigil_suggest",
			mcp.WithDescription("Review AI-generated code and return line-anchored accept/reject suggestions. Runs ALL 5 reviewer roles (Security Architect, Staff Engineer, DevSecOps, Cloud Architect, Red Team) in a SINGLE LLM call. Each suggestion carries the exact line range and replacement text, so the user can apply or dismiss it per line. The code is never auto-modified."),
			mcp.WithString("code",
				mcp.Required(),
				mcp.Description("The AI-generated source code to review"),
			),
			mcp.WithString("prompt",
				mcp.Description("The original request the code was generated for (context)"),
			),
			mcp.WithString("language",
				mcp.Description("Programming language: go, python, javascript, typescript, rust, java"),
			),
			mcp.WithString("filename",
				mcp.Description("Filename for context-aware scanning"),
			),
			mcp.WithString("api_key",
				mcp.Description("Your LLM provider API key. When provided, the backend uses your key for the review pipeline."),
			),
		),
		s.handleSuggest,
	)

	// Tool 9: analyze_code_security — spec-named alias for dual-engine analysis
	srv.AddTool(
		mcp.NewTool("analyze_code_security",
			mcp.WithDescription("Scan code with VigilAgent's parallel dual-engine security analysis (deterministic + LLM engines). Spec tool: analyze_code_security."),
			mcp.WithString("code",
				mcp.Required(),
				mcp.Description("The source code to analyze"),
			),
			mcp.WithString("language",
				mcp.Description("Programming language: go, python, javascript, typescript, rust, java"),
			),
			mcp.WithString("api_key",
				mcp.Description("Your LLM provider API key (optional BYOK)"),
			),
		),
		s.handleDualEngine,
	)

	// Tool 10: analyze_design_security — design-stage gate (spec section 7)
	srv.AddTool(
		mcp.NewTool("analyze_design_security",
			mcp.WithDescription("Scan a design document, prompt, or architecture plan for security risks BEFORE code generation (design-stage gate). Returns findings like missing authorization boundaries, secrets in the spec, or unsafe commands."),
			mcp.WithString("design",
				mcp.Required(),
				mcp.Description("The design document / prompt / architecture plan text to analyze"),
			),
			mcp.WithString("api_key",
				mcp.Description("Your LLM provider API key (optional BYOK)"),
			),
		),
		s.handleAnalyzeDesignSecurity,
	)

	// Tool 11: validate_generated_diff — scan the added lines of a generated patch
	srv.AddTool(
		mcp.NewTool("validate_generated_diff",
			mcp.WithDescription("Validate an AI-generated unified diff: extracts the added lines and runs dual-engine security analysis on them. Use before applying a generated patch."),
			mcp.WithString("diff",
				mcp.Required(),
				mcp.Description("The unified diff (git diff) to validate"),
			),
			mcp.WithString("language",
				mcp.Description("Programming language of the changed files"),
			),
			mcp.WithString("api_key",
				mcp.Description("Your LLM provider API key (optional BYOK)"),
			),
		),
		s.handleValidateGeneratedDiff,
	)

	// Tool 12: get_secure_remediation — line-anchored accept/reject fixes
	srv.AddTool(
		mcp.NewTool("get_secure_remediation",
			mcp.WithDescription("Review code and return secure remediation as line-anchored accept/reject suggestions (all 5 reviewer roles in one LLM call). The code is never auto-modified."),
			mcp.WithString("code",
				mcp.Required(),
				mcp.Description("The code to remediate"),
			),
			mcp.WithString("prompt",
				mcp.Description("The original request the code was generated for"),
			),
			mcp.WithString("language",
				mcp.Description("Programming language"),
			),
			mcp.WithString("api_key",
				mcp.Description("Your LLM provider API key (optional BYOK)"),
			),
		),
		s.handleSuggest,
	)

	// Tool 13: verify_provenance — verify a signed scan attestation
	srv.AddTool(
		mcp.NewTool("verify_provenance",
			mcp.WithDescription("Verify a signed provenance/attestation record for a scan. Provide the scan_id and signature returned by the gateway; returns whether the record is authentic and untampered."),
			mcp.WithString("scan_id",
				mcp.Required(),
				mcp.Description("The scan id from X-VigilAgent-Scan-ID or a provenance record"),
			),
			mcp.WithString("signature",
				mcp.Required(),
				mcp.Description("The HMAC signature from X-VigilAgent-Provenance-Signature"),
			),
		),
		s.handleVerifyProvenance,
	)

	// Tool 14: create_scan_attestation — signed audit record for a scan
	srv.AddTool(
		mcp.NewTool("create_scan_attestation",
			mcp.WithDescription("Run a review and create a SIGNED provenance attestation record (scan_id, content hash, decision, timestamp) for audit. Verifiable later via verify_provenance."),
			mcp.WithString("code",
				mcp.Required(),
				mcp.Description("The code that was scanned"),
			),
			mcp.WithString("prompt",
				mcp.Description("The original request for context"),
			),
			mcp.WithString("language",
				mcp.Description("Programming language"),
			),
			mcp.WithString("api_key",
				mcp.Description("Your LLM provider API key (optional BYOK)"),
			),
		),
		s.handleCreateScanAttestation,
	)

	return srv
}

// ─── Tool Handlers ───────────────────────────────────────────────────────

func (s *Server) handleVerify(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// MCP tool execution timeout: 60s for full pipeline reviews.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	code, _ := req.RequireString("code")
	if code == "" {
		return mcp.NewToolResultError("code is required"), nil
	}
	prompt := req.GetString("prompt", "")
	language := req.GetString("language", "")
	filename := req.GetString("filename", "")
	apiKey := s.resolveLLMKey(req.GetString("api_key", ""))

	payload := map[string]interface{}{
		"code":     code,
		"prompt":   prompt,
		"language": language,
		"filename": filename,
	}

	resp, err := s.callBackendWithKey(ctx, "/api/v1/review", payload, apiKey)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return mcp.NewToolResultError("VigilAgent review timed out (60s limit)"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("VigilAgent review failed: %v", err)), nil
	}

	pretty, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(fmt.Sprintf("%v", resp)), nil
	}

	return mcp.NewToolResultText(string(pretty)), nil
}

func (s *Server) handleScan(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// MCP tool execution timeout: 30s for deterministic scans.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	code, _ := req.RequireString("code")
	if code == "" {
		return mcp.NewToolResultError("code is required"), nil
	}
	language := req.GetString("language", "")
	filename := req.GetString("filename", "")

	payload := map[string]interface{}{
		"description": "static analysis scan",
		"code":        code,
		"language":    language,
		"filename":    filename,
	}

	resp, err := s.callBackend(ctx, "/api/v1/middleware/process", payload)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return mcp.NewToolResultError("VigilAgent scan timed out (30s limit)"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("VigilAgent scan failed: %v", err)), nil
	}

	pretty, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(fmt.Sprintf("%v", resp)), nil
	}

	return mcp.NewToolResultText(string(pretty)), nil
}

func (s *Server) handleReview(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// MCP tool execution timeout: 60s for LLM reviewer calls.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	code, _ := req.RequireString("code")
	if code == "" {
		return mcp.NewToolResultError("code is required"), nil
	}
	prompt := req.GetString("prompt", "")
	language := req.GetString("language", "")
	apiKey := s.resolveLLMKey(req.GetString("api_key", ""))

	payload := map[string]interface{}{
		"code":     code,
		"prompt":   prompt,
		"language": language,
	}

	resp, err := s.callBackendWithKey(ctx, "/api/v1/review", payload, apiKey)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return mcp.NewToolResultError("VigilAgent review timed out (60s limit)"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("VigilAgent review failed: %v", err)), nil
	}

	result, _ := resp.(map[string]interface{})
	summary := formatReviewSummary(result)

	return mcp.NewToolResultText(summary), nil
}

func (s *Server) handleConfidence(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// MCP tool execution timeout: 30s for confidence scoring.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	code, _ := req.RequireString("code")
	if code == "" {
		return mcp.NewToolResultError("code is required"), nil
	}
	language := req.GetString("language", "")
	apiKey := s.resolveLLMKey(req.GetString("api_key", ""))

	payload := map[string]interface{}{
		"code":     code,
		"language": language,
	}

	resp, err := s.callBackendWithKey(ctx, "/api/v1/review", payload, apiKey)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return mcp.NewToolResultError("VigilAgent confidence scoring timed out (30s limit)"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("VigilAgent confidence scoring failed: %v", err)), nil
	}

	result, _ := resp.(map[string]interface{})
	summary := formatConfidenceSummary(result)

	return mcp.NewToolResultText(summary), nil
}

func (s *Server) handleProcess(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// MCP tool execution timeout: 30s for middleware processing.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	description, _ := req.RequireString("description")
	if description == "" {
		return mcp.NewToolResultError("description is required"), nil
	}
	code := req.GetString("code", "")
	language := req.GetString("language", "")
	taskType := req.GetString("task_type", "")

	payload := map[string]interface{}{
		"description": description,
		"code":        code,
		"language":    language,
		"task_type":   taskType,
	}

	resp, err := s.callBackend(ctx, "/api/v1/middleware/process", payload)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("VigilAgent process failed: %v", err)), nil
	}

	pretty, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(fmt.Sprintf("%v", resp)), nil
	}

	return mcp.NewToolResultText(string(pretty)), nil
}

// handleDualEngine runs the parallel dual-engine analysis (deterministic + LLM).
func (s *Server) handleImprove(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// MCP tool execution timeout: 90s for the improvement pipeline.
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	code, _ := req.RequireString("code")
	if code == "" {
		return mcp.NewToolResultError("code is required"), nil
	}
	prompt := req.GetString("prompt", "")
	language := req.GetString("language", "")
	filename := req.GetString("filename", "")
	apiKey := s.resolveLLMKey(req.GetString("api_key", ""))

	payload := map[string]interface{}{
		"code":     code,
		"prompt":   prompt,
		"language": language,
		"filename": filename,
	}

	resp, err := s.callBackendWithKey(ctx, "/api/v1/review", payload, apiKey)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return mcp.NewToolResultError("VigilAgent improve timed out (90s limit)"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("VigilAgent improve failed: %v", err)), nil
	}

	result, _ := resp.(map[string]interface{})
	finalOutput, _ := result["final_output"].(string)
	if finalOutput == "" {
		// No improved output produced — surface the full report instead.
		pretty, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return mcp.NewToolResultText(fmt.Sprintf("%v", resp)), nil
		}
		return mcp.NewToolResultText(string(pretty)), nil
	}

	return mcp.NewToolResultText(formatImproveSummary(result, finalOutput)), nil
}

// formatImproveSummary renders the improved code with a confidence summary.
func formatImproveSummary(result map[string]interface{}, finalOutput string) string {
	var b strings.Builder
	b.WriteString("## Improved Code\n\n```\n")
	b.WriteString(finalOutput)
	b.WriteString("\n```\n")

	if score, ok := result["confidence"].(map[string]interface{}); ok {
		if grade, ok := score["grade"].(string); ok {
			fmt.Fprintf(&b, "\nConfidence grade: **%s**\n", grade)
		}
	}
	return b.String()
}

func (s *Server) handleDualEngine(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// MCP tool execution timeout: 60s for dual-engine analysis (LLM pass can be slow).
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	code, _ := req.RequireString("code")
	if code == "" {
		return mcp.NewToolResultError("code is required"), nil
	}
	language := req.GetString("language", "go")
	apiKey := s.resolveLLMKey(req.GetString("api_key", ""))

	payload := map[string]interface{}{
		"code":     code,
		"language": language,
	}

	// Deep-analyze is a protected route mounted at /api/v1 (the bare /v1
	// path 404s against the main API router).
	resp, err := s.callBackendWithKey(ctx, "/api/v1/deep-analyze", payload, apiKey)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return mcp.NewToolResultError("VigilAgent dual-engine analysis timed out (60s limit)"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("VigilAgent dual-engine analysis failed: %v", err)), nil
	}

	summary := formatDualEngineSummary(resp)
	return mcp.NewToolResultText(summary), nil
}

// handleSuggest runs the review pipeline in suggestion mode and returns
// line-anchored accept/reject suggestions (5 roles, 1 LLM call).
func (s *Server) handleSuggest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// MCP tool execution timeout: 90s for the single-call 5-role review.
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	code, _ := req.RequireString("code")
	if code == "" {
		return mcp.NewToolResultError("code is required"), nil
	}
	prompt := req.GetString("prompt", "")
	language := req.GetString("language", "")
	filename := req.GetString("filename", "")
	apiKey := s.resolveLLMKey(req.GetString("api_key", ""))

	payload := map[string]interface{}{
		"code":            code,
		"prompt":          prompt,
		"language":        language,
		"filename":        filename,
		"suggestion_mode": true,
	}

	resp, err := s.callBackendWithKey(ctx, "/api/v1/review", payload, apiKey)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return mcp.NewToolResultError("VigilAgent suggest timed out (90s limit)"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("VigilAgent suggest failed: %v", err)), nil
	}

	return mcp.NewToolResultText(formatSuggestionsSummary(resp)), nil
}

// formatSuggestionsSummary renders the line-anchored suggestions as a
// reviewable, copy-pasteable markdown report (diff-style per line).
func formatSuggestionsSummary(resp interface{}) string {
	result, ok := resp.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("%v", resp)
	}

	var out strings.Builder
	out.WriteString("🛡️ VigilAgent Suggestions (accept or reject each)\n\n")

	if confidence, ok := result["confidence"].(map[string]interface{}); ok {
		out.WriteString(fmt.Sprintf("Confidence: **%v** (%v)\n\n", confidence["grade"], confidence["confidence"]))
	}

	if suggestions, ok := result["suggestions"].([]interface{}); ok && len(suggestions) > 0 {
		out.WriteString(fmt.Sprintf("## %d line-anchored suggestions\n\n", len(suggestions)))
		for i, s := range suggestions {
			item, _ := s.(map[string]interface{})
			severity := item["severity"]
			role := item["role"]
			message := item["message"]
			lineStart := item["line_start"]
			lineEnd := item["line_end"]
			replacement, _ := item["replacement"].(string)
			corroborated := false
			if c, ok := item["corroborated"].(bool); ok {
				corroborated = c
			}

			icon := "ℹ️"
			switch severity {
			case "critical":
				icon = "🔴"
			case "high":
				icon = "🟠"
			case "medium":
				icon = "🟡"
			case "low":
				icon = "🟢"
			}
			corrobMark := ""
			if corroborated {
				corrobMark = " (✓ corroborated by deterministic engine)"
			}
			out.WriteString(fmt.Sprintf("%d. %s **[%v]** — %v, lines %v–%v%v\n   %v\n",
				i+1, icon, severity, role, lineStart, lineEnd, corrobMark, message))
			if replacement != "" {
				out.WriteString(fmt.Sprintf("   ```diff\n   %v\n   ```\n", replacement))
			}
			out.WriteString("\n")
		}
	} else {
		out.WriteString("✅ No suggestions — the code passed the review.\n")
	}

	if summary, ok := result["summary"].(string); ok && summary != "" {
		out.WriteString("---\n" + summary + "\n")
	}

	return out.String()
}

// handleAnalyzeDesignSecurity scans a design document before code generation
// (spec section 7 — design-stage security gate).
func (s *Server) handleAnalyzeDesignSecurity(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// MCP tool execution timeout: 60s.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	design, _ := req.RequireString("design")
	if design == "" {
		return mcp.NewToolResultError("design is required"), nil
	}
	apiKey := s.resolveLLMKey(req.GetString("api_key", ""))

	payload := map[string]interface{}{"code": design, "language": "design"}
	resp, err := s.callBackendWithKey(ctx, "/api/v1/deep-analyze", payload, apiKey)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("VigilAgent design analysis failed: %v", err)), nil
	}
	return mcp.NewToolResultText(formatDualEngineSummary(resp)), nil
}

// handleValidateGeneratedDiff extracts the added lines from a unified diff and
// runs dual-engine analysis on them (spec: validate_generated_diff).
func (s *Server) handleValidateGeneratedDiff(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// MCP tool execution timeout: 60s.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	diff, _ := req.RequireString("diff")
	if diff == "" {
		return mcp.NewToolResultError("diff is required"), nil
	}
	added := extractAddedLinesFromDiff(diff)
	if strings.TrimSpace(added) == "" {
		return mcp.NewToolResultText("✅ No added lines in this diff — nothing to scan."), nil
	}
	language := req.GetString("language", "")
	apiKey := s.resolveLLMKey(req.GetString("api_key", ""))

	payload := map[string]interface{}{"code": added, "language": language}
	resp, err := s.callBackendWithKey(ctx, "/api/v1/deep-analyze", payload, apiKey)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("VigilAgent diff validation failed: %v", err)), nil
	}
	return mcp.NewToolResultText(formatDualEngineSummary(resp)), nil
}

// handleVerifyProvenance checks a signed provenance record with the gateway's
// provenance/audit service.
func (s *Server) handleVerifyProvenance(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// MCP tool execution timeout: 30s.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	scanID, _ := req.RequireString("scan_id")
	signature, _ := req.RequireString("signature")
	if scanID == "" || signature == "" {
		return mcp.NewToolResultError("scan_id and signature are required"), nil
	}

	payload := map[string]interface{}{"scan_id": scanID, "signature": signature}
	resp, err := s.callGateway(ctx, "/v1/provenance/verify", payload)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("provenance verification failed: %v", err)), nil
	}

	result, _ := resp.(map[string]interface{})
	valid, _ := result["valid"].(bool)
	reason, _ := result["reason"].(string)
	if valid {
		return mcp.NewToolResultText("✅ Provenance verified: the scan record is authentic and untampered."), nil
	}
	return mcp.NewToolResultText("❌ Provenance verification FAILED: " + reason), nil
}

// handleCreateScanAttestation runs a review and creates a SIGNED provenance
// attestation record for audit (spec: create_scan_attestation).
func (s *Server) handleCreateScanAttestation(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// MCP tool execution timeout: 90s (review + attestation).
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	code, _ := req.RequireString("code")
	if code == "" {
		return mcp.NewToolResultError("code is required"), nil
	}
	prompt := req.GetString("prompt", "")
	language := req.GetString("language", "")
	apiKey := s.resolveLLMKey(req.GetString("api_key", ""))

	// 1. Review the code (suggestion mode: never auto-modifies).
	reviewPayload := map[string]interface{}{
		"code": code, "prompt": prompt, "language": language, "suggestion_mode": true,
	}
	reviewResp, err := s.callBackendWithKey(ctx, "/api/v1/review", reviewPayload, apiKey)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("VigilAgent review failed: %v", err)), nil
	}
	result, _ := reviewResp.(map[string]interface{})
	decision := deriveAttestationDecision(result)

	// 2. Attest: sign a record anchored to the exact scanned content.
	attestPayload := map[string]interface{}{
		"provider":      "vigilagent-mcp",
		"model":         "review-pipeline",
		"decision":      decision,
		"response_hash": signing.HashContent(code),
		"client_type":   "mcp",
	}
	attestResp, err := s.callGateway(ctx, "/v1/provenance/attest", attestPayload)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("attestation creation failed: %v", err)), nil
	}

	pretty, err := json.MarshalIndent(attestResp, "", "  ")
	if err != nil {
		return mcp.NewToolResultText(fmt.Sprintf("%v", attestResp)), nil
	}
	return mcp.NewToolResultText(string(pretty)), nil
}

// extractAddedLinesFromDiff returns the concatenated added lines of a unified
// diff (context and removed lines are dropped; +++ file headers are skipped).
func extractAddedLinesFromDiff(diff string) string {
	var sb strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			sb.WriteString(strings.TrimPrefix(line, "+"))
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// deriveAttestationDecision maps a review confidence grade to a policy decision.
func deriveAttestationDecision(result map[string]interface{}) string {
	if confidence, ok := result["confidence"].(map[string]interface{}); ok {
		if grade, ok := confidence["grade"].(string); ok {
			switch grade {
			case "A", "B":
				return "allow"
			case "C":
				return "allow_with_notice"
			default:
				return "hold_for_review"
			}
		}
	}
	return "hold_for_review"
}

// resolveLLMKey returns the tool-level api_key if provided, otherwise falls
// back to the env-var LLM key set at server startup (VIGILAGENT_LLM_KEY).
func (s *Server) resolveLLMKey(toolKey string) string {
	if toolKey != "" {
		return toolKey
	}
	return s.llmKey
}

// ─── HTTP Client ─────────────────────────────────────────────────────────

func (s *Server) callBackend(ctx context.Context, path string, payload interface{}) (interface{}, error) {
	return s.callBackendWithKey(ctx, path, payload, "")
}

// callGateway sends a request to the Secure AI Gateway (provenance endpoints).
// Uses gatewayURL when configured, otherwise falls back to apiURL (co-located
// deployments and tests).
func (s *Server) callGateway(ctx context.Context, path string, payload interface{}) (interface{}, error) {
	base := s.gatewayURL
	if base == "" {
		base = s.apiURL
	}
	return s.doCall(ctx, base, path, payload, "")
}

// callBackendWithKey sends a request to the VigilAgent backend, optionally
// passing the user's LLM key via X-LLM-Key header for BYOK support.
func (s *Server) callBackendWithKey(ctx context.Context, path string, payload interface{}, llmKey string) (interface{}, error) {
	return s.doCall(ctx, s.apiURL, path, payload, llmKey)
}

// doCall performs the HTTP round-trip against the given base URL.
func (s *Server) doCall(ctx context.Context, baseURL, path string, payload interface{}, llmKey string) (interface{}, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := baseURL + path
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	if llmKey != "" {
		req.Header.Set("X-LLM-Key", llmKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("backend request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("backend returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return result, nil
}

// ─── Formatting Helpers ──────────────────────────────────────────────────

func formatReviewSummary(result map[string]interface{}) string {
	var out string

	if confidence, ok := result["confidence"].(map[string]interface{}); ok {
		grade := confidence["grade"]
		score := confidence["confidence"]
		reason := confidence["reason"]
		out += fmt.Sprintf("## Confidence: %v (%v)\nReason: %v\n\n", grade, score, reason)
	}

	if reviewers, ok := result["reviewers"].([]interface{}); ok {
		out += "## Reviewer Verdicts\n\n"
		for _, r := range reviewers {
			rev, _ := r.(map[string]interface{})
			name := rev["name"]
			verdict := rev["verdict"]
			icon := "✅"
			if verdict == "fail" {
				icon = "❌"
			} else if verdict == "warn" {
				icon = "⚠️"
			}
			out += fmt.Sprintf("%s **%v**: %v\n", icon, name, verdict)
			if findings, ok := rev["findings"].([]interface{}); ok && len(findings) > 0 {
				for _, f := range findings {
					out += fmt.Sprintf("  - %v\n", f)
				}
			}
			if suggestions, ok := rev["suggestions"].([]interface{}); ok && len(suggestions) > 0 {
				out += "  Suggestions:\n"
				for _, s := range suggestions {
					out += fmt.Sprintf("  - %v\n", s)
				}
			}
			out += "\n"
		}
	}

	if deterministic, ok := result["deterministic_findings"].([]interface{}); ok && len(deterministic) > 0 {
		out += fmt.Sprintf("## Deterministic Findings: %d\n\n", len(deterministic))
		for _, f := range deterministic {
			finding, _ := f.(map[string]interface{})
			severity := finding["severity"]
			message := finding["message"]
			fix := finding["fix"]
			out += fmt.Sprintf("- [%v] %v\n  Fix: %v\n", severity, message, fix)
		}
	}

	if finalOutput, ok := result["final_output"].(string); ok && finalOutput != "" {
		out += fmt.Sprintf("## Final Output\n\n```\n%s\n```\n", finalOutput)
	}

	return out
}

func formatConfidenceSummary(result map[string]interface{}) string {
	var out string

	if confidence, ok := result["confidence"].(map[string]interface{}); ok {
		out += "## Confidence Score\n\n"
		out += fmt.Sprintf("- **Grade:** %v\n", confidence["grade"])
		out += fmt.Sprintf("- **Score:** %v%%\n", confidence["confidence"])
		out += fmt.Sprintf("- **Reason:** %v\n", confidence["reason"])

		if passed, ok := confidence["passed"].(float64); ok {
			out += fmt.Sprintf("- **Passed:** %.0f\n", passed)
		}
		if failed, ok := confidence["failed"].(float64); ok {
			out += fmt.Sprintf("- **Failed:** %.0f\n", failed)
		}
		if warned, ok := confidence["warned"].(float64); ok {
			out += fmt.Sprintf("- **Warnings:** %.0f\n", warned)
		}
	} else {
		out += "No confidence data available.\n"
	}

	return out
}

// formatDualEngineSummary formats the dual-engine analysis result for MCP output.
func formatDualEngineSummary(resp interface{}) string {
	result, ok := resp.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("%v", resp)
	}

	var out string

	// Extract result object
	var dualResult map[string]interface{}
	if r, ok := result["result"].(map[string]interface{}); ok {
		dualResult = r
	} else {
		dualResult = result
	}

	// Header with grade and score
	if grade, ok := dualResult["grade"].(string); ok {
		score := dualResult["score"]
		out += fmt.Sprintf("🛡️ VigilAgent Dual-Engine Analysis: Grade %s (%v%%)\n\n", grade, score)
	}

	// Engine stats
	if stats, ok := dualResult["engine_stats"].(map[string]interface{}); ok {
		out += "## Engine Statistics\n\n"
		if det, ok := stats["deterministic"].(map[string]interface{}); ok {
			out += fmt.Sprintf("- **Deterministic:** %v findings in %v\n", det["findings_count"], det["latency_ms"])
		}
		if llm, ok := stats["llm"].(map[string]interface{}); ok {
			out += fmt.Sprintf("- **LLM Engine:** %v findings in %v (model: %v)\n", llm["findings_count"], llm["latency_ms"], llm["model"])
		}
		if total, ok := stats["total_latency_ms"].(float64); ok {
			out += fmt.Sprintf("- **Total Latency:** %.0fms\n", total)
		}
		out += "\n"
	}

	// Findings
	if findings, ok := dualResult["findings"].([]interface{}); ok && len(findings) > 0 {
		out += fmt.Sprintf("## Findings (%d)\n\n", len(findings))
		for i, f := range findings {
			finding, _ := f.(map[string]interface{})
			severity := finding["severity"]
			message := finding["message"]
			engine := finding["engine"]
			fix := finding["fix"]
			corroborated := ""
			if ruleID, ok := finding["rule_id"].(string); ok && len(ruleID) > 0 && ruleID[len(ruleID)-4:] == "+llm" {
				corroborated = " ✓ corroborated"
			}
			icon := "❓"
			switch severity {
			case "critical":
				icon = "🔴"
			case "high":
				icon = "🟠"
			case "medium":
				icon = "🟡"
			case "low":
				icon = "🟢"
			case "info":
				icon = "ℹ️"
			}
			out += fmt.Sprintf("%d. %s **[%v]** (%s)%s\n   %v\n", i+1, icon, strings.ToUpper(fmt.Sprintf("%v", severity)), engine, corroborated, message)
			if fix != nil && fmt.Sprintf("%v", fix) != "" {
				out += fmt.Sprintf("   💡 Fix: %v\n", fix)
			}
		}
	} else {
		out += "✅ No issues found. Code looks clean!\n"
	}

	out += "\n---\nPowered by VigilAgent Dual-Engine Analysis"
	return out
}
