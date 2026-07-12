// Package errors provides application-level error types and HTTP status mapping
// for VigilAgent. Implements the standard error contract documented in
// doc 04-api-contract §3.
package errors

import (
	"fmt"
	"net/http"
)

// ErrorCode represents a machine-readable error code.
type ErrorCode string

// Auth errors
const (
	ErrMissingAuth        ErrorCode = "AUTH_001"
	ErrInvalidCredentials ErrorCode = "AUTH_002"
	ErrTokenExpired       ErrorCode = "AUTH_003"
	ErrTokenInvalid       ErrorCode = "AUTH_004"
	ErrAccountLocked      ErrorCode = "AUTH_005"
	ErrAccountDisabled    ErrorCode = "AUTH_006"
	ErrInsufficientPerms  ErrorCode = "AUTH_007"
	ErrEmailNotVerified   ErrorCode = "AUTH_008"
	ErrPasswordTooWeak    ErrorCode = "AUTH_009"
	ErrDuplicateEmail     ErrorCode = "AUTH_010"
	ErrAPIKeyInvalid      ErrorCode = "AUTH_011"
	ErrHashFailed         ErrorCode = "AUTH_012"
)

// Validation errors
const (
	ErrInvalidBody       ErrorCode = "VAL_001"
	ErrMissingField      ErrorCode = "VAL_002"
	ErrInvalidEmail      ErrorCode = "VAL_003"
	ErrInvalidID         ErrorCode = "VAL_004"
	ErrPayloadTooLarge   ErrorCode = "VAL_005"
	ErrInvalidQuery      ErrorCode = "VAL_006"
	ErrInvalidPagination ErrorCode = "VAL_007"
)

// Resource errors
const (
	ErrNotFound      ErrorCode = "RES_001"
	ErrAlreadyExists ErrorCode = "RES_002"
	ErrConflict      ErrorCode = "RES_003"
	ErrDeleted       ErrorCode = "RES_004"
)

// Scanner/Engine errors
const (
	ErrScanFailed      ErrorCode = "SCAN_001"
	ErrScanTimeout     ErrorCode = "SCAN_002"
	ErrScanInputEmpty  ErrorCode = "SCAN_003"
	ErrUnsupportedLang ErrorCode = "SCAN_004"
	ErrReviewFailed    ErrorCode = "SCAN_005"
	ErrNoLLMProvider   ErrorCode = "SCAN_006"
)

// Skill marketplace errors
const (
	ErrSkillNotFound       ErrorCode = "SKILL_001"
	ErrSkillNotPublished   ErrorCode = "SKILL_002"
	ErrSkillUploadFailed   ErrorCode = "SKILL_003"
	ErrSkillScanFailed     ErrorCode = "SKILL_004"
	ErrSkillVersionInvalid ErrorCode = "SKILL_005"
	ErrSkillSearchFailed   ErrorCode = "SKILL_006"
)

// Billing errors
const (
	ErrBillingNotConfigured ErrorCode = "BILL_001"
	ErrCheckoutFailed       ErrorCode = "BILL_002"
	ErrSubscriptionNotFound ErrorCode = "BILL_003"
	ErrPaymentFailed        ErrorCode = "BILL_004"
)

// Infrastructure errors
const (
	ErrRateLimited   ErrorCode = "INFRA_001"
	ErrServiceDown   ErrorCode = "INFRA_002"
	ErrDBError       ErrorCode = "INFRA_003"
	ErrCacheError    ErrorCode = "INFRA_004"
	ErrQueueError    ErrorCode = "INFRA_005"
	ErrWebhookFailed ErrorCode = "INFRA_006"
)

// APIError is a structured error response.
type APIError struct {
	Code    ErrorCode   `json:"code"`
	Message string      `json:"message"`
	Status  int         `json:"-"`
	Details interface{} `json:"details,omitempty"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// HTTPStatus returns the HTTP status code for the error.
func (e *APIError) HTTPStatus() int {
	if e.Status > 0 {
		return e.Status
	}
	return errorCodeToStatus(e.Code)
}

// New creates a new APIError with the given code and message.
func New(code ErrorCode, message string) *APIError {
	return &APIError{
		Code:    code,
		Message: message,
		Status:  errorCodeToStatus(code),
	}
}

// Newf creates a new APIError with a formatted message.
func Newf(code ErrorCode, format string, args ...interface{}) *APIError {
	return &APIError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Status:  errorCodeToStatus(code),
	}
}

// WithDetails returns a copy of the error with additional details.
func (e *APIError) WithDetails(details interface{}) *APIError {
	return &APIError{
		Code:    e.Code,
		Message: e.Message,
		Status:  e.Status,
		Details: details,
	}
}

// errorCodeToStatus maps error codes to HTTP status codes using explicit map lookup.
func errorCodeToStatus(code ErrorCode) int {
	statusMap := map[ErrorCode]int{
		// Auth — 401
		ErrMissingAuth:        http.StatusUnauthorized,
		ErrInvalidCredentials: http.StatusUnauthorized,
		ErrTokenExpired:       http.StatusUnauthorized,
		ErrTokenInvalid:       http.StatusUnauthorized,
		ErrAccountDisabled:    http.StatusUnauthorized,
		ErrEmailNotVerified:   http.StatusUnauthorized,
		ErrAPIKeyInvalid:      http.StatusUnauthorized,
		// Auth — 400
		ErrHashFailed: http.StatusBadRequest,
		// Auth — 403
		ErrInsufficientPerms: http.StatusForbidden,
		// Auth — 429
		ErrAccountLocked: http.StatusTooManyRequests,
		// Auth — 400
		ErrPasswordTooWeak: http.StatusBadRequest,
		ErrDuplicateEmail:  http.StatusConflict,
		// Validation — 400
		ErrInvalidBody:       http.StatusBadRequest,
		ErrMissingField:      http.StatusBadRequest,
		ErrInvalidEmail:      http.StatusBadRequest,
		ErrInvalidID:         http.StatusBadRequest,
		ErrPayloadTooLarge:   http.StatusRequestEntityTooLarge,
		ErrInvalidQuery:      http.StatusBadRequest,
		ErrInvalidPagination: http.StatusBadRequest,
		// Resource — 404
		ErrNotFound: http.StatusNotFound,
		// Resource — 409
		ErrAlreadyExists: http.StatusConflict,
		ErrConflict:      http.StatusConflict,
		// Resource — 410
		ErrDeleted: http.StatusGone,
		// Scanner — 400
		ErrScanInputEmpty:  http.StatusBadRequest,
		ErrUnsupportedLang: http.StatusBadRequest,
		// Scanner — 503
		ErrNoLLMProvider: http.StatusServiceUnavailable,
		// Scanner — 500
		ErrScanFailed:   http.StatusInternalServerError,
		ErrScanTimeout:  http.StatusGatewayTimeout,
		ErrReviewFailed: http.StatusInternalServerError,
		// Skill — 404
		ErrSkillNotFound:     http.StatusNotFound,
		ErrSkillNotPublished: http.StatusNotFound,
		// Skill — 400
		ErrSkillVersionInvalid: http.StatusBadRequest,
		// Skill — 500
		ErrSkillUploadFailed: http.StatusInternalServerError,
		ErrSkillScanFailed:   http.StatusInternalServerError,
		ErrSkillSearchFailed: http.StatusInternalServerError,
		// Billing — 503
		ErrBillingNotConfigured: http.StatusServiceUnavailable,
		// Billing — 404
		ErrSubscriptionNotFound: http.StatusNotFound,
		// Billing — 400/500
		ErrCheckoutFailed: http.StatusBadRequest,
		ErrPaymentFailed:  http.StatusPaymentRequired,
		// Infrastructure — 429
		ErrRateLimited: http.StatusTooManyRequests,
		// Infrastructure — 503
		ErrServiceDown: http.StatusServiceUnavailable,
		// Infrastructure — 500
		ErrDBError:       http.StatusInternalServerError,
		ErrCacheError:    http.StatusServiceUnavailable,
		ErrQueueError:    http.StatusInternalServerError,
		ErrWebhookFailed: http.StatusInternalServerError,
	}

	if status, ok := statusMap[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}
