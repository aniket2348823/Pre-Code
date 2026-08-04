package response

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/vigilagent/vigilagent/internal/requestid"
)

// --- Error Code Constants ---

const (
	// Auth errors
	CodeAUTHInvalidToken        = "AUTH_004"
	CodeAUTHExpiredToken        = "AUTH_003"
	CodeAUTHMissingToken        = "AUTH_001"
	CodeAUTHInsufficientPerms   = "AUTH_007"
	CodeAUTHAccountLocked       = "AUTH_005"
	CodeAUTHInvalidCredentials  = "AUTH_002"
	CodeAUTHAccountDisabled     = "AUTH_006"
	CodeAUTHAPIKeyInvalid       = "AUTH_011"
	CodeAUTHAPIKeyExpired       = "AUTH_013"
	CodeAUTHDuplicateEmail      = "AUTH_010"
	CodeAUTHEmailNotVerified    = "AUTH_008"
	CodeAUTHPasswordTooWeak     = "AUTH_009"
	CodeAUTHHashFailed          = "AUTH_012"

	// Resource errors
	CodeResourceNotFound         = "RES_001"
	CodeResourceConflict         = "RES_003"
	CodeResourceValidationFailed = "VAL_001"
	CodeResourceAlreadyExists    = "RES_002"

	// Rate limit / body
	CodeRateLimitExceeded = "INFRA_001"
	CodeBodyTooLarge      = "VAL_005"

	// Infrastructure
	CodeInternalServerError = "INFRA_003"
	CodeServiceUnavailable  = "INFRA_002"

	// Method
	CodeBadRequest        = "VAL_001"
	CodeMethodNotAllowed  = "METHOD_001"
)

// --- APIResponse envelope ---

// APIResponse is the standardized response envelope for all API responses.
type APIResponse struct {
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     *ErrorBody  `json:"error,omitempty"`
	Meta      *Meta       `json:"meta,omitempty"`
	RequestID string      `json:"request_id"`
}

// ErrorBody contains structured error information.
type ErrorBody struct {
	Code      string      `json:"code"`
	Message   string      `json:"message"`
	Details   interface{} `json:"details,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
	Timestamp string      `json:"timestamp,omitempty"`
}

// Meta contains pagination and list metadata.
type Meta struct {
	Total      int    `json:"total,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Offset     int    `json:"offset,omitempty"`
	HasMore    bool   `json:"has_more,omitempty"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ValidationErrorDetail is a single field validation error.
type ValidationErrorDetail struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// --- Raw JSON helper ---

// JSON writes a raw JSON response.
func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// --- Core functions (backward-compatible signatures) ---

// Success writes a success response (old signature preserved).
func Success(w http.ResponseWriter, status int, data interface{}) {
	reqID := w.Header().Get("X-Request-Id")
	JSON(w, status, APIResponse{
		Success:   true,
		Data:      data,
		RequestID: reqID,
	})
}

// Error writes an error response (old signature preserved for backward compatibility).
func Error(w http.ResponseWriter, status int, message string) {
	reqID := w.Header().Get("X-Request-Id")
	JSON(w, status, APIResponse{
		Success: false,
		Error: &ErrorBody{
			Code:      "ERROR",
			Message:   message,
			RequestID: reqID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		RequestID: reqID,
	})
}

// Created writes a 201 Created response (old signature preserved).
func Created(w http.ResponseWriter, data interface{}) {
	Success(w, http.StatusCreated, data)
}

// NoContent writes a 204 No Content response.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// NotFound writes a 404 Not Found response.
func NotFound(w http.ResponseWriter, message string) {
	Error(w, http.StatusNotFound, message)
}

// BadRequest writes a 400 Bad Request response.
func BadRequest(w http.ResponseWriter, message string) {
	Error(w, http.StatusBadRequest, message)
}

// Unauthorized writes a 401 Unauthorized response.
func Unauthorized(w http.ResponseWriter, message string) {
	Error(w, http.StatusUnauthorized, message)
}

// Forbidden writes a 403 Forbidden response.
func Forbidden(w http.ResponseWriter, message string) {
	Error(w, http.StatusForbidden, message)
}

// InternalError writes a 500 Internal Server Error response.
func InternalError(w http.ResponseWriter, message string) {
	Error(w, http.StatusInternalServerError, message)
}

// --- Request-aware functions (new, with request_id support) ---

// rid extracts the request ID from request context, returning empty string if nil.
func rid(r *http.Request) string {
	if r != nil {
		return requestid.FromContext(r.Context())
	}
	return ""
}

// SuccessR writes a success response with request_id.
func SuccessR(w http.ResponseWriter, r *http.Request, status int, data interface{}) {
	JSON(w, status, APIResponse{
		Success:   true,
		Data:      data,
		RequestID: rid(r),
	})
}

// SuccessWithMeta writes a success response with pagination metadata and request_id.
func SuccessWithMeta(w http.ResponseWriter, r *http.Request, status int, data interface{}, meta *Meta) {
	JSON(w, status, APIResponse{
		Success:   true,
		Data:      data,
		Meta:      meta,
		RequestID: rid(r),
	})
}

// ErrorR writes an error response with a structured code and request_id.
func ErrorR(w http.ResponseWriter, r *http.Request, status int, code string, message string) {
	JSON(w, status, APIResponse{
		Success: false,
		Error: &ErrorBody{
			Code:      code,
			Message:   message,
			RequestID: rid(r),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		RequestID: rid(r),
	})
}

// ErrorWithDetails writes an error response with additional details and request_id.
func ErrorWithDetails(w http.ResponseWriter, r *http.Request, status int, code string, message string, details interface{}) {
	JSON(w, status, APIResponse{
		Success: false,
		Error: &ErrorBody{
			Code:      code,
			Message:   message,
			Details:   details,
			RequestID: rid(r),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		RequestID: rid(r),
	})
}

// ValidationErrorResponse writes a validation error response with field-level details.
func ValidationErrorResponse(w http.ResponseWriter, r *http.Request, errors []ValidationErrorDetail) {
	JSON(w, http.StatusBadRequest, APIResponse{
		Success: false,
		Error: &ErrorBody{
			Code:      CodeResourceValidationFailed,
			Message:   "Request validation failed",
			Details:   errors,
			RequestID: rid(r),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		RequestID: rid(r),
	})
}

// CreatedR writes a 201 Created response with request_id.
func CreatedR(w http.ResponseWriter, r *http.Request, data interface{}) {
	SuccessR(w, r, http.StatusCreated, data)
}

// NotFoundR writes a 404 Not Found response with request_id.
func NotFoundR(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusNotFound, CodeResourceNotFound, message)
}

// BadRequestR writes a 400 Bad Request response with request_id.
func BadRequestR(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusBadRequest, CodeBadRequest, message)
}

// UnauthorizedR writes a 401 Unauthorized response with request_id.
func UnauthorizedR(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusUnauthorized, CodeAUTHMissingToken, message)
}

// ForbiddenR writes a 403 Forbidden response with request_id.
func ForbiddenR(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusForbidden, CodeAUTHInsufficientPerms, message)
}

// InternalErrorR writes a 500 Internal Server Error response with request_id.
func InternalErrorR(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusInternalServerError, CodeServiceUnavailable, message)
}

// TooManyRequests writes a 429 Too Many Requests response with Retry-After and rate limit headers.
func TooManyRequests(w http.ResponseWriter, r *http.Request, message string) {
	retryAfter := 60
	if r != nil {
		if ra := r.Header.Get("X-RateLimit-Retry-After"); ra != "" {
			fmt.Sscanf(ra, "%d", &retryAfter)
		}
	}
	TooManyRequestsAfter(w, r, message, retryAfter)
}

// TooManyRequestsAfter writes a 429 with explicit Retry-After seconds and rate limit headers.
func TooManyRequestsAfter(w http.ResponseWriter, r *http.Request, message string, retryAfterSeconds int) {
	resetTime := time.Now().Add(time.Duration(retryAfterSeconds) * time.Second)
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSeconds))
	w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetTime.Unix()))
	w.Header().Set("X-RateLimit-Limit", "0")
	w.Header().Set("X-RateLimit-Remaining", "0")

	ErrorR(w, r, http.StatusTooManyRequests, CodeRateLimitExceeded, message)
}

// Conflict writes a 409 Conflict response.
func Conflict(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusConflict, CodeResourceConflict, message)
}

// ServiceUnavailable writes a 503 Service Unavailable response.
func ServiceUnavailable(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusServiceUnavailable, CodeServiceUnavailable, message)
}

// --- New standardized helpers ---

// ErrorWithCode writes a structured error with a specific error code, request_id, and timestamp.
func ErrorWithCode(w http.ResponseWriter, r *http.Request, status int, code string, message string) {
	ErrorR(w, r, status, code, message)
}

// ConflictError writes a 409 Conflict for duplicate resources.
func ConflictError(w http.ResponseWriter, r *http.Request, resource string) {
	ErrorWithCode(w, r, http.StatusConflict, CodeResourceConflict, fmt.Sprintf("%s already exists", resource))
}

// ValidationError writes a 400 with field-level validation details.
func ValidationError(w http.ResponseWriter, r *http.Request, fields []ValidationErrorDetail) {
	ValidationErrorResponse(w, r, fields)
}

// NotFoundError writes a 404 with a standardized not-found code.
func NotFoundError(w http.ResponseWriter, r *http.Request, resource string) {
	ErrorWithCode(w, r, http.StatusNotFound, CodeResourceNotFound, fmt.Sprintf("%s not found", resource))
}

// RateLimitError writes a 429 with Retry-After headers.
func RateLimitError(w http.ResponseWriter, r *http.Request, retryAfterSeconds int) {
	TooManyRequestsAfter(w, r, "rate limit exceeded", retryAfterSeconds)
}

// MethodNotAllowed writes a 405 Method Not Allowed response.
func MethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ErrorWithCode(w, r, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "method not allowed")
}
