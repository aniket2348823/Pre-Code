package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/pkg/response"
)

const defaultMaxBatchOps = 10

// BatchOperation represents a single operation in a batch request.
type BatchOperation struct {
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Body   interface{} `json:"body,omitempty"`
}

// BatchResult is the result of a single batch operation.
type BatchResult struct {
	Index    int         `json:"index"`
	Status   int         `json:"status"`
	Body     interface{} `json:"body,omitempty"`
	Error    string      `json:"error,omitempty"`
	Duration int64       `json:"duration_ms"`
}

// BatchRequest is the top-level batch request body.
type BatchRequest struct {
	Operations []BatchOperation `json:"operations"`
	Atomic     bool             `json:"atomic,omitempty"`
	Parallel   bool             `json:"parallel,omitempty"`
}

func (r *Router) maxBatchOperations() int {
	return defaultMaxBatchOps
}

// batchHandler processes multiple API operations in a single request.
func (r *Router) batchHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var batchReq BatchRequest
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&batchReq); err != nil {
		response.ErrorR(w, req, http.StatusBadRequest, "BATCH_001", "invalid request body")
		return
	}

	if len(batchReq.Operations) == 0 {
		response.ErrorR(w, req, http.StatusBadRequest, "BATCH_002", "operations array is required")
		return
	}

	maxOps := r.maxBatchOperations()
	if len(batchReq.Operations) > maxOps {
		response.ErrorR(w, req, http.StatusBadRequest, "BATCH_003",
			fmt.Sprintf("too many operations, max %d", maxOps))
		return
	}

	for i, op := range batchReq.Operations {
		op.Method = strings.ToUpper(strings.TrimSpace(op.Method))
		op.Path = strings.TrimSpace(op.Path)
		if op.Method == "" || op.Path == "" {
			response.ErrorR(w, req, http.StatusBadRequest, "BATCH_004",
				fmt.Sprintf("operation at index %d requires method and path", i))
			return
		}
		// Prevent infinite self-recursion: a batch operation must not target
		// the batch endpoint itself (would recurse until stack overflow).
		if strings.HasSuffix(op.Path, "/batch") || op.Path == "/batch" {
			response.ErrorR(w, req, http.StatusBadRequest, "BATCH_005",
				fmt.Sprintf("operation at index %d cannot target the batch endpoint", i))
			return
		}
		batchReq.Operations[i] = op
	}

	results := make([]BatchResult, len(batchReq.Operations))
	var firstError int = -1

	for i, op := range batchReq.Operations {
		start := time.Now()
		status, body, errMsg := r.executeBatchOp(req, claims, op)
		duration := time.Since(start).Milliseconds()

		results[i] = BatchResult{
			Index:    i,
			Status:   status,
			Body:     body,
			Error:    errMsg,
			Duration: duration,
		}

		if status >= 400 && firstError == -1 {
			firstError = i
		}

		if batchReq.Atomic && firstError >= 0 {
			for j := i + 1; j < len(results); j++ {
				results[j] = BatchResult{
					Index:  j,
					Status: http.StatusFailedDependency,
					Error:  fmt.Sprintf("skipped: atomic batch failed at operation %d", firstError),
				}
			}
			break
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"results":  results,
		"atomic":   batchReq.Atomic,
		"total":    len(results),
		"failures": r.countFailures(results),
	})
}

func (r *Router) executeBatchOp(parentReq *http.Request, claims *auth.Claims, op BatchOperation) (int, interface{}, string) {
	var bodyReader io.Reader
	if op.Body != nil {
		bodyBytes, err := json.Marshal(op.Body)
		if err != nil {
			return http.StatusBadRequest, nil, "failed to marshal operation body"
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	subReq := httptest.NewRequest(op.Method, op.Path, bodyReader)
	subReq.Header.Set("Content-Type", "application/json")

	// Copy auth headers from parent
	if token := parentReq.Header.Get("Authorization"); token != "" {
		subReq.Header.Set("Authorization", token)
	}
	if apiKey := parentReq.Header.Get("X-API-Key"); apiKey != "" {
		subReq.Header.Set("X-API-Key", apiKey)
	}

	// Inject auth claims into context
	ctx := auth.ContextWithClaims(subReq.Context(), claims)
	subReq = subReq.WithContext(ctx)

	recorder := httptest.NewRecorder()

	// Route through the chi mux to get proper middleware and handler matching
	r.Mux.ServeHTTP(recorder, subReq)

	var body interface{}
	if recorder.Body.Len() > 0 {
		json.Unmarshal(recorder.Body.Bytes(), &body)
	}

	status := recorder.Code
	errMsg := ""
	if status >= 400 {
		if bodyMap, ok := body.(map[string]interface{}); ok {
			if errObj, ok := bodyMap["error"]; ok {
				if errMap, ok := errObj.(map[string]interface{}); ok {
					if msg, ok := errMap["message"].(string); ok {
						errMsg = msg
					}
				}
			}
		}
		if errMsg == "" {
			errMsg = http.StatusText(status)
		}
	}

	return status, body, errMsg
}

func (r *Router) countFailures(results []BatchResult) int {
	count := 0
	for _, result := range results {
		if result.Status >= 400 {
			count++
		}
	}
	return count
}

// batchTaskHandler is the existing batch task creation endpoint.
// This is separate from the general batch endpoint.
func (r *Router) batchTaskHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		Tasks []struct {
			Prompt    string `json:"prompt"`
			ProjectID string `json:"project_id"`
		} `json:"tasks"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.ErrorR(w, req, http.StatusBadRequest, "BATCH_001", "invalid request body")
		return
	}

	if len(input.Tasks) == 0 {
		response.ErrorR(w, req, http.StatusBadRequest, "BATCH_002", "tasks array is required")
		return
	}

	if len(input.Tasks) > 10 {
		response.ErrorR(w, req, http.StatusBadRequest, "BATCH_003", "too many tasks, max 10")
		return
	}

	type taskResult struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}

	results := make([]taskResult, len(input.Tasks))
	for i, t := range input.Tasks {
		if t.Prompt == "" || t.ProjectID == "" {
			results[i] = taskResult{Status: "failed", Error: "prompt and project_id are required"}
			continue
		}

		// Verify project membership
		if _, err := r.requireProjectMember(req.Context(), t.ProjectID, claims.UserID); err != nil {
			results[i] = taskResult{Status: "failed", Error: "access denied to project"}
			continue
		}

		task := &repository.Task{
			ProjectID: t.ProjectID,
			UserID:    claims.UserID,
			Prompt:    t.Prompt,
			Status:    "pending",
		}
		if err := r.tasks.Create(req.Context(), task); err != nil {
			results[i] = taskResult{Status: "failed", Error: "failed to create task"}
			continue
		}
		results[i] = taskResult{TaskID: task.ID, Status: "created"}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"results": results,
		"total":   len(results),
	})
}
