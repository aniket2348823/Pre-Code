package response

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/vigilagent/vigilagent/internal/requestid"
)

// Content from response.go
// --- Error Code Constants ---

const (
	// Auth errors
	CodeAUTHInvalidToken       = "AUTH_004"
	CodeAUTHExpiredToken       = "AUTH_003"
	CodeAUTHMissingToken       = "AUTH_001"
	CodeAUTHInsufficientPerms  = "AUTH_007"
	CodeAUTHAccountLocked      = "AUTH_005"
	CodeAUTHInvalidCredentials = "AUTH_002"
	CodeAUTHAccountDisabled    = "AUTH_006"
	CodeAUTHAPIKeyInvalid      = "AUTH_011"
	CodeAUTHAPIKeyExpired      = "AUTH_013"
	CodeAUTHDuplicateEmail     = "AUTH_010"
	CodeAUTHEmailNotVerified   = "AUTH_008"
	CodeAUTHPasswordTooWeak    = "AUTH_009"
	CodeAUTHHashFailed         = "AUTH_012"

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
	CodeBadRequest       = "VAL_001"
	CodeMethodNotAllowed = "METHOD_001"
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

// WriteError writes a structured error response with optional details.
// Kept for backward compatibility with earlier response helpers.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code string, message string, details interface{}) {
	ErrorWithDetails(w, r, status, code, message, details)
}

// WritePaginated writes a paginated response with total/has_more metadata.
// page/perPage/total drive the Meta block. Kept for backward compatibility.
func WritePaginated(w http.ResponseWriter, r *http.Request, data interface{}, page, perPage, total int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	offset := (page - 1) * perPage
	hasMore := offset+perPage < total
	meta := &Meta{
		Total:   total,
		Limit:   perPage,
		Offset:  offset,
		HasMore: hasMore,
	}
	SuccessWithMeta(w, r, http.StatusOK, data, meta)
}

// Content from hateoas.go
// --- HATEOAS types ---

// Link represents a hypermedia link (HAL/JSON-LD compatible).
type Link struct {
	Href   string `json:"href"`
	Method string `json:"method,omitempty"`
	Type   string `json:"type,omitempty"`
	Title  string `json:"title,omitempty"`
}

// Links is an ordered collection of links keyed by relation name.
type Links map[string]Link

// --- Link construction helpers ---

// NewLink creates a Link with just href.
func NewLink(href string) Link {
	return Link{Href: href}
}

// NewMethodLink creates a Link with href and HTTP method.
func NewMethodLink(href, method string) Link {
	return Link{Href: href, Method: method}
}

// NewTypedLink creates a Link with href, method, and content type.
func NewTypedLink(href, method, contentType string) Link {
	return Link{Href: href, Method: method, Type: contentType}
}

// --- Adding links to data (additive, backward-compatible) ---

// AddLink attaches a single hypermedia link to data.
// data must be a pointer to a struct, *map[string]interface{}, or map[string]interface{}.
// If data is a *map[string]interface{}, _links is added/merged in-place.
// Returns the (possibly modified) data for convenience.
func AddLink(data interface{}, rel, href, method string) interface{} {
	link := NewMethodLink(href, method)
	return addLinkToData(data, rel, link)
}

// AddSelfLink adds a "self" link to data.
func AddSelfLink(data interface{}, href string) interface{} {
	return AddLink(data, "self", href, "GET")
}

// addLinkToData is the internal implementation that injects _links into a map.
func addLinkToData(data interface{}, rel string, link Link) interface{} {
	switch d := data.(type) {
	case *map[string]interface{}:
		if *d == nil {
			*d = make(map[string]interface{})
		}
		setLink(*d, rel, link)
		return d
	case map[string]interface{}:
		setLink(d, rel, link)
		return d
	case *map[string]string:
		// Convert to map[string]interface{} and wrap
		wrapped := make(map[string]interface{}, len(*d))
		for k, v := range *d {
			wrapped[k] = v
		}
		setLink(wrapped, rel, link)
		*d = make(map[string]string)
		for k, v := range wrapped {
			if k == "_links" {
				continue
			}
			if s, ok := v.(string); ok {
				(*d)[k] = s
			}
		}
		// Store _links separately in the original map's context
		return wrapped
	default:
		return data
	}
}

// setLink writes a link into the _links key of a map, merging if _links exists.
func setLink(data map[string]interface{}, rel string, link Link) {
	existing, ok := data["_links"]
	if !ok || existing == nil {
		data["_links"] = Links{rel: link}
		return
	}
	existingLinks, ok := existing.(Links)
	if !ok {
		existingLinks = Links{}
	}
	existingLinks[rel] = link
	data["_links"] = existingLinks
}

// --- Collection pagination links (HAL-style) ---

// AddCollectionLinks generates first/next/prev/last pagination links and attaches them to data.
// basePath is the resource URL (e.g., "/api/v1/items").
// page is 1-based. perPage is items per page. total is total item count.
func AddCollectionLinks(data interface{}, basePath string, page, perPage, total int) interface{} {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}

	totalPages := int(math.Ceil(float64(total) / float64(perPage)))
	if totalPages < 1 {
		totalPages = 1
	}

	basePath = ensureNoQuery(basePath)

	// Always add self
	selfHref := pageURL(basePath, page, perPage)
	addLinkToData(data, "self", NewLink(selfHref))

	// first
	firstHref := pageURL(basePath, 1, perPage)
	addLinkToData(data, "first", NewLink(firstHref))

	// last
	lastHref := pageURL(basePath, totalPages, perPage)
	addLinkToData(data, "last", NewLink(lastHref))

	// next
	if page < totalPages {
		nextHref := pageURL(basePath, page+1, perPage)
		addLinkToData(data, "next", NewLink(nextHref))
	}

	// prev
	if page > 1 {
		prevHref := pageURL(basePath, page-1, perPage)
		addLinkToData(data, "prev", NewLink(prevHref))
	}

	return data
}

// pageURL builds a paginated URL: basePath?page=N&per_page=M
func pageURL(basePath string, page, perPage int) string {
	return fmt.Sprintf("%s?page=%d&per_page=%d", basePath, page, perPage)
}

// ensureNoQuery strips query parameters from a path.
func ensureNoQuery(path string) string {
	if idx := strings.IndexByte(path, '?'); idx != -1 {
		return path[:idx]
	}
	return path
}

// --- Embedded resources ---

// AddEmbedded adds an embedded resource collection under the _embedded key (HAL convention).
func AddEmbedded(data interface{}, name string, resources interface{}) interface{} {
	switch d := data.(type) {
	case *map[string]interface{}:
		if *d == nil {
			*d = make(map[string]interface{})
		}
		setEmbedded(*d, name, resources)
		return d
	case map[string]interface{}:
		setEmbedded(d, name, resources)
		return d
	}
	return data
}

// setEmbedded writes an embedded resource into the _embedded key of a map.
func setEmbedded(data map[string]interface{}, name string, resources interface{}) {
	existing, ok := data["_embedded"]
	if !ok || existing == nil {
		data["_embedded"] = map[string]interface{}{name: resources}
		return
	}
	existingMap, ok := existing.(map[string]interface{})
	if !ok {
		existingMap = map[string]interface{}{}
	}
	existingMap[name] = resources
	data["_embedded"] = existingMap
}

// --- HAL document wrapper ---

// HALDocument is a top-level HAL representation with _links and _embedded.
type HALDocument struct {
	Links    Links                  `json:"_links,omitempty"`
	Embedded map[string]interface{} `json:"_embedded,omitempty"`
	Data     interface{}            `json:"data,omitempty"`
}

// NewHALDocument creates a HAL document with a self link.
func NewHALDocument(selfHref string) *HALDocument {
	return &HALDocument{
		Links: Links{"self": NewLink(selfHref)},
	}
}

// WithLink adds a link to the HAL document and returns it for chaining.
func (h *HALDocument) WithLink(rel, href string) *HALDocument {
	h.Links[rel] = NewLink(href)
	return h
}

// WithMethodLink adds a link with method to the HAL document.
func (h *HALDocument) WithMethodLink(rel, href, method string) *HALDocument {
	h.Links[rel] = NewMethodLink(href, method)
	return h
}

// WithEmbedded adds an embedded resource to the HAL document.
func (h *HALDocument) WithEmbedded(name string, resources interface{}) *HALDocument {
	if h.Embedded == nil {
		h.Embedded = make(map[string]interface{})
	}
	h.Embedded[name] = resources
	return h
}

// WithData sets the primary resource data.
func (h *HALDocument) WithData(data interface{}) *HALDocument {
	h.Data = data
	return h
}
