package response

import (
	"encoding/json"
	"net/http"

	"github.com/vigilagent/vigilagent/internal/requestid"
)

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
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
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
			Code:    "ERROR",
			Message: message,
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
			Code:    code,
			Message: message,
		},
		RequestID: rid(r),
	})
}

// ErrorWithDetails writes an error response with additional details and request_id.
func ErrorWithDetails(w http.ResponseWriter, r *http.Request, status int, code string, message string, details interface{}) {
	JSON(w, status, APIResponse{
		Success: false,
		Error: &ErrorBody{
			Code:    code,
			Message: message,
			Details: details,
		},
		RequestID: rid(r),
	})
}

// ValidationErrorResponse writes a validation error response with field-level details.
func ValidationErrorResponse(w http.ResponseWriter, r *http.Request, errors []ValidationErrorDetail) {
	JSON(w, http.StatusBadRequest, APIResponse{
		Success: false,
		Error: &ErrorBody{
			Code:    "VALIDATION_001",
			Message: "Request validation failed",
			Details: errors,
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
	ErrorR(w, r, http.StatusNotFound, "RES_001", message)
}

// BadRequestR writes a 400 Bad Request response with request_id.
func BadRequestR(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusBadRequest, "VAL_001", message)
}

// UnauthorizedR writes a 401 Unauthorized response with request_id.
func UnauthorizedR(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusUnauthorized, "AUTH_001", message)
}

// ForbiddenR writes a 403 Forbidden response with request_id.
func ForbiddenR(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusForbidden, "AUTH_007", message)
}

// InternalErrorR writes a 500 Internal Server Error response with request_id.
func InternalErrorR(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusInternalServerError, "INFRA_002", message)
}

// TooManyRequests writes a 429 Too Many Requests response.
func TooManyRequests(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusTooManyRequests, "INFRA_001", message)
}

// Conflict writes a 409 Conflict response.
func Conflict(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusConflict, "RES_002", message)
}

// ServiceUnavailable writes a 503 Service Unavailable response.
func ServiceUnavailable(w http.ResponseWriter, r *http.Request, message string) {
	ErrorR(w, r, http.StatusServiceUnavailable, "INFRA_002", message)
}
