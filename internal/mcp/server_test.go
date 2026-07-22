package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// ─── Mock Backend Server ──────────────────────────────────────────────────

// capturedLLMKey stores the X-LLM-Key header received by the mock backend.
// Tests read this to verify the header was forwarded correctly.
var capturedLLMKey atomic.Value

func mockBackend(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/review", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Capture X-LLM-Key for test assertions
		capturedLLMKey.Store(r.Header.Get("X-LLM-Key"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"original_prompt":   "test prompt",
			"main_llm_response": "func main() {}",
			"confidence": map[string]interface{}{
				"grade":      "A",
				"confidence": 0.95,
				"passed":     4.0,
				"failed":     0.0,
				"warned":     0.0,
				"reason":     "All checks passed",
			},
			"reviewers": []map[string]interface{}{
				{
					"name":        "security",
					"role":        "Principal Security Architect",
					"verdict":     "pass",
					"findings":    []string{},
					"suggestions": []string{},
				},
			},
			"deterministic_findings": []interface{}{},
			"final_output":           "func main() {}",
			"summary":                "Review completed in 100ms.",
		})
	})

	mux.HandleFunc("/api/v1/middleware/process", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"description": "test",
			"task_type":   "scan",
			"scan_result": map[string]interface{}{
				"findings":      []interface{}{},
				"analyzers_run": []string{"builtin"},
			},
			"pipeline_result": map[string]interface{}{
				"passed":     true,
				"confidence": 1.0,
				"layers": []map[string]interface{}{
					{"name": "requirements", "passed": true},
				},
			},
			"skills_extracted": []interface{}{},
			"metrics": map[string]interface{}{
				"findings_count":   0.0,
				"skills_extracted": 0.0,
				"pipeline_passed":  true,
			},
		})
	})

	return httptest.NewServer(mux)
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// extractText pulls the text content from an MCP tool result.
func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("result has no text content")
	return ""
}

// newTestRequest creates a CallToolRequest with the given arguments.
// Uses mcp.CallToolParams directly for proper API compatibility.
func newTestRequest(toolName string, args map[string]interface{}) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	}
}

// ─── Unit Tests ───────────────────────────────────────────────────────────

func TestResolveLLMKey(t *testing.T) {
	tests := []struct {
		name     string
		server   *Server
		toolKey  string
		expected string
	}{
		{"tool key takes priority", &Server{llmKey: "env-key"}, "tool-key", "tool-key"},
		{"falls back to env key", &Server{llmKey: "env-key"}, "", "env-key"},
		{"both empty", &Server{llmKey: ""}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.server.resolveLLMKey(tt.toolKey); got != tt.expected {
				t.Errorf("resolveLLMKey(%q) = %q, want %q", tt.toolKey, got, tt.expected)
			}
		})
	}
}

// ─── Handler Tests: Happy Path ────────────────────────────────────────────

func TestHandleVerify(t *testing.T) {
	backend := mockBackend(t)
	defer backend.Close()
	srv := NewServer(backend.URL, "va_test_key", "")

	result, err := srv.handleVerify(context.Background(), newTestRequest("vigil_verify", map[string]interface{}{
		"code":     "func main() {}",
		"language": "go",
	}))
	if err != nil {
		t.Fatalf("handleVerify error: %v", err)
	}

	text := extractText(t, result)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	confidence, _ := parsed["confidence"].(map[string]interface{})
	if confidence["grade"] != "A" {
		t.Errorf("expected grade A, got %v", confidence["grade"])
	}
}

func TestHandleVerifyWithLLMKey(t *testing.T) {
	backend := mockBackend(t)
	defer backend.Close()
	capturedLLMKey.Store("")

	srv := NewServer(backend.URL, "va_test_key", "")
	_, err := srv.handleVerify(context.Background(), newTestRequest("vigil_verify", map[string]interface{}{
		"code":    "func main() {}",
		"api_key": "sk-user-llm-key",
	}))
	if err != nil {
		t.Fatalf("handleVerify error: %v", err)
	}

	// Verify the X-LLM-Key header was forwarded to the backend
	if got := capturedLLMKey.Load().(string); got != "sk-user-llm-key" {
		t.Errorf("expected X-LLM-Key 'sk-user-llm-key', got %q", got)
	}
}

func TestHandleScan(t *testing.T) {
	backend := mockBackend(t)
	defer backend.Close()
	srv := NewServer(backend.URL, "va_test_key", "")

	result, err := srv.handleScan(context.Background(), newTestRequest("vigil_scan", map[string]interface{}{
		"code":     "func main() {}",
		"language": "go",
		"filename": "main.go",
	}))
	if err != nil {
		t.Fatalf("handleScan error: %v", err)
	}

	text := extractText(t, result)
	var parsed map[string]interface{}
	json.Unmarshal([]byte(text), &parsed)
	if parsed["description"] != "test" {
		t.Errorf("expected description 'test', got %v", parsed["description"])
	}
}

func TestHandleReview(t *testing.T) {
	backend := mockBackend(t)
	defer backend.Close()
	srv := NewServer(backend.URL, "va_test_key", "")

	result, err := srv.handleReview(context.Background(), newTestRequest("vigil_review", map[string]interface{}{
		"code":     "func main() {}",
		"language": "go",
	}))
	if err != nil {
		t.Fatalf("handleReview error: %v", err)
	}

	text := extractText(t, result)
	if !strings.Contains(text, "Confidence") {
		t.Errorf("expected 'Confidence' in review summary, got: %s", text)
	}
}

func TestHandleConfidence(t *testing.T) {
	backend := mockBackend(t)
	defer backend.Close()
	srv := NewServer(backend.URL, "va_test_key", "")

	result, err := srv.handleConfidence(context.Background(), newTestRequest("vigil_confidence", map[string]interface{}{
		"code":     "func main() {}",
		"language": "go",
	}))
	if err != nil {
		t.Fatalf("handleConfidence error: %v", err)
	}

	text := extractText(t, result)
	if !strings.Contains(text, "Grade") {
		t.Errorf("expected 'Grade' in confidence summary, got: %s", text)
	}
}

func TestHandleProcess(t *testing.T) {
	backend := mockBackend(t)
	defer backend.Close()
	srv := NewServer(backend.URL, "va_test_key", "")

	result, err := srv.handleProcess(context.Background(), newTestRequest("vigil_process", map[string]interface{}{
		"description": "test scan",
		"code":        "func main() {}",
		"language":    "go",
	}))
	if err != nil {
		t.Fatalf("handleProcess error: %v", err)
	}

	text := extractText(t, result)
	var parsed map[string]interface{}
	json.Unmarshal([]byte(text), &parsed)
	if parsed["description"] != "test" {
		t.Errorf("expected description 'test', got %v", parsed["description"])
	}
}

// ─── Handler Tests: Missing Required Args ─────────────────────────────────

func TestHandleVerifyMissingCode(t *testing.T) {
	srv := NewServer("http://localhost:9999", "va_test_key", "")
	result, err := srv.handleVerify(context.Background(), newTestRequest("vigil_verify", map[string]interface{}{
		"language": "go",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing code")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "code is required") {
		t.Errorf("expected 'code is required' error, got: %s", text)
	}
}

func TestHandleScanMissingCode(t *testing.T) {
	srv := NewServer("http://localhost:9999", "va_test_key", "")
	result, err := srv.handleScan(context.Background(), newTestRequest("vigil_scan", map[string]interface{}{
		"language": "go",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing code")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "code is required") {
		t.Errorf("expected 'code is required' error, got: %s", text)
	}
}

func TestHandleReviewMissingCode(t *testing.T) {
	srv := NewServer("http://localhost:9999", "va_test_key", "")
	result, err := srv.handleReview(context.Background(), newTestRequest("vigil_review", map[string]interface{}{
		"language": "go",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing code")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "code is required") {
		t.Errorf("expected 'code is required' error, got: %s", text)
	}
}

func TestHandleConfidenceMissingCode(t *testing.T) {
	srv := NewServer("http://localhost:9999", "va_test_key", "")
	result, err := srv.handleConfidence(context.Background(), newTestRequest("vigil_confidence", map[string]interface{}{
		"language": "go",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing code")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "code is required") {
		t.Errorf("expected 'code is required' error, got: %s", text)
	}
}

func TestHandleProcessMissingDescription(t *testing.T) {
	srv := NewServer("http://localhost:9999", "va_test_key", "")
	result, err := srv.handleProcess(context.Background(), newTestRequest("vigil_process", map[string]interface{}{
		"code": "func main() {}",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing description")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "description is required") {
		t.Errorf("expected 'description is required' error, got: %s", text)
	}
}

// ─── Handler Tests: Backend Errors ────────────────────────────────────────

func TestHandleVerifyBackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer backend.Close()

	srv := NewServer(backend.URL, "va_test_key", "")
	result, err := srv.handleVerify(context.Background(), newTestRequest("vigil_verify", map[string]interface{}{
		"code": "func main() {}",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for backend error")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "VigilAgent review failed") {
		t.Errorf("expected 'VigilAgent review failed' in error, got: %s", text)
	}
	if !strings.Contains(text, "500") {
		t.Errorf("expected status 500 in error message, got: %s", text)
	}
}

func TestHandleScanBackendError(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer backend.Close()

	srv := NewServer(backend.URL, "va_test_key", "")
	result, err := srv.handleScan(context.Background(), newTestRequest("vigil_scan", map[string]interface{}{
		"code": "func main() {}",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error result for backend error")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "VigilAgent scan failed") {
		t.Errorf("expected 'VigilAgent scan failed' in error, got: %s", text)
	}
	if !strings.Contains(text, "503") {
		t.Errorf("expected status 503 in error message, got: %s", text)
	}
}

// ─── Formatting Tests ─────────────────────────────────────────────────────

func TestFormatReviewSummary(t *testing.T) {
	result := map[string]interface{}{
		"confidence": map[string]interface{}{
			"grade": "B", "confidence": 0.85, "reason": "Good but warnings",
		},
		"reviewers": []interface{}{
			map[string]interface{}{
				"name": "security", "verdict": "pass",
				"findings": []interface{}{}, "suggestions": []interface{}{"Add rate limiting"},
			},
			map[string]interface{}{
				"name": "cost", "verdict": "warn",
				"findings": []interface{}{"Over-provisioned"}, "suggestions": []interface{}{},
			},
		},
		"deterministic_findings": []interface{}{
			map[string]interface{}{"severity": "medium", "message": "Missing validation", "fix": "Add middleware"},
		},
		"final_output": "func main() {}",
	}

	summary := formatReviewSummary(result)
	for _, want := range []string{"Confidence", "Reviewer Verdicts", "✅", "⚠️", "Deterministic Findings", "Final Output"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q", want)
		}
	}
}

func TestFormatConfidenceSummary(t *testing.T) {
	result := map[string]interface{}{
		"confidence": map[string]interface{}{
			"grade": "A", "confidence": 0.95, "reason": "All passed",
			"passed": 4.0, "failed": 0.0, "warned": 1.0,
		},
	}
	summary := formatConfidenceSummary(result)
	for _, want := range []string{"Grade", "A", "Passed"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q", want)
		}
	}
}

func TestFormatConfidenceSummaryNoData(t *testing.T) {
	summary := formatConfidenceSummary(map[string]interface{}{})
	if !strings.Contains(summary, "No confidence data") {
		t.Error("expected 'No confidence data' message")
	}
}
