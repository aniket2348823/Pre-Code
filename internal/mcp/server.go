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
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server is the VigilAgent MCP server.
type Server struct {
	apiURL    string
	apiKey    string
	llmKey    string // optional: user's LLM key for BYOK via env var
	client    *http.Client
	mcpServer *server.MCPServer
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

// Run starts the MCP server on stdio transport with EOF detection
// to prevent zombie processes when parent crashes.
func (s *Server) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Detect stdin EOF to exit cleanly when parent process crashes.
	// Uses a channel signal instead of os.Exit to allow deferred cleanup.
	quit := make(chan struct{}, 1)
	go func() {
		defer cancel() // stop the stdin reader when server exits
		buf := make([]byte, 1)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			_, err := io.ReadAtLeast(os.Stdin, buf, 1)
			if err != nil {
				slog.Info("MCP server: stdin closed/EOF, shutting down")
				quit <- struct{}{}
				return
			}
		}
	}()

	// Run server in goroutine, watch for EOF signal.
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ServeStdio(s.mcpServer)
	}()

	select {
	case <-quit:
		return nil
	case err := <-errCh:
		return err
	}
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

	// Tool 3: vigil_review — Run LLM reviewers only
	srv.AddTool(
		mcp.NewTool("vigil_review",
			mcp.WithDescription("Run VigilAgent's parallel specialized LLM reviewers on code. Returns verdicts from Security Architect, Staff Engineer, DevSecOps, Cloud Architect, and Red Team agents."),
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

	resp, err := s.callBackendWithKey(ctx, "/v1/deep-analyze", payload, apiKey)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return mcp.NewToolResultError("VigilAgent dual-engine analysis timed out (60s limit)"), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("VigilAgent dual-engine analysis failed: %v", err)), nil
	}

	summary := formatDualEngineSummary(resp)
	return mcp.NewToolResultText(summary), nil
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

// callBackendWithKey sends a request to the VigilAgent backend, optionally
// passing the user's LLM key via X-LLM-Key header for BYOK support.
func (s *Server) callBackendWithKey(ctx context.Context, path string, payload interface{}, llmKey string) (interface{}, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := s.apiURL + path
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
