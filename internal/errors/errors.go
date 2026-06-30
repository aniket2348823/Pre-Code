// Package errors provides application-level error types and HTTP status mapping
// for VigilAgent. Implements the standard error contract documented in
// doc 04-api-contract §3.
package errors

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Code is a stable machine-readable error identifier.
type Code string

const (
	CodeNotFound        Code = "RESOURCE_NOT_FOUND"
	CodeInvalidToken    Code = "AUTH_INVALID_TOKEN"
	CodeInsufficient    Code = "AUTH_INSUFFICIENT_SCOPE"
	CodeConflict        Code = "RESOURCE_CONFLICT"
	CodeValidation      Code = "VALIDATION_ERROR"
	CodeRateLimit       Code = "RATE_LIMIT_EXCEEDED"
	CodeQuota           Code = "QUOTA_EXCEEDED"
	CodeTaskFailed      Code = "TASK_FAILED"
	CodeProviderDown    Code = "PROVIDER_UNAVAILABLE"
	CodeUnauthorized    Code = "UNAUTHORIZED"
	CodeForbidden       Code = "FORBIDDEN"
	CodeBudgetExceeded  Code = "BUDGET_EXCEEDED"
)

// AppError is the canonical application error.
type AppError struct {
	Code      Code
	Message   string
	Details   map[string]any
	RequestID string
	Timestamp time.Time
	Err       error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.Err }

// HTTPStatus maps the error code to an HTTP status code.
func (e *AppError) HTTPStatus() int {
	switch e.Code {
	case CodeNotFound:
		return http.StatusNotFound
	case CodeInvalidToken, CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeInsufficient, CodeForbidden:
		return http.StatusForbidden
	case CodeConflict:
		return http.StatusConflict
	case CodeValidation:
		return http.StatusUnprocessableEntity
	case CodeQuota, CodeBudgetExceeded:
		return http.StatusPaymentRequired
	case CodeRateLimit:
		return http.StatusTooManyRequests
	case CodeProviderDown:
		return http.StatusServiceUnavailable
	case CodeTaskFailed:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func (e *AppError) when() time.Time {
	if e.Timestamp.IsZero() {
		return time.Now().UTC()
	}
	return e.Timestamp
}

// Body is the standard error response body per API contract §3.1.
type Body struct {
	Error BodyInner `json:"error"`
}

type BodyInner struct {
	Code      Code           `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"request_id"`
	Timestamp string         `json:"timestamp"`
}

// ToBody converts the error into the wire format.
func (e *AppError) ToBody() Body {
	return Body{
		Error: BodyInner{
			Code:      e.Code,
			Message:   e.Message,
			Details:   e.Details,
			RequestID: e.RequestID,
			Timestamp: e.when().Format(time.RFC3339),
		},
	}
}

// New constructs an AppError.
func New(code Code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

func Wrap(err error, code Code, message string) *AppError {
	return &AppError{Code: code, Message: message, Err: err}
}

func Validation(message string, details map[string]any) *AppError {
	return &AppError{Code: CodeValidation, Message: message, Details: details}
}

func NotFound(resource string) *AppError {
	return &AppError{Code: CodeNotFound, Message: resource + " not found"}
}

func WithRequestID(e *AppError, id string) *AppError {
	e.RequestID = id
	return e
}

// AsAppError walks the error chain and returns the first *AppError.
func AsAppError(err error) (*AppError, bool) {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

// Predefined errors per API contract.
var (
	ErrNotFound       = NotFound("Resource")
	ErrUnauthorized   = New(CodeUnauthorized, "Authentication required")
	ErrForbidden      = New(CodeForbidden, "Insufficient permissions")
	ErrValidation     = New(CodeValidation, "Invalid request")
	ErrRateLimit      = New(CodeRateLimit, "Rate limit exceeded")
	ErrBudgetExceeded = New(CodeBudgetExceeded, "Budget limit exceeded")
	ErrProviderDown   = New(CodeProviderDown, "LLM provider temporarily unavailable")
)
