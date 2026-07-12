package errors

import (
	"testing"
)

func TestNew(t *testing.T) {
	err := New(ErrNotFound, "user not found")
	if err.Code != ErrNotFound {
		t.Errorf("expected code %s, got %s", ErrNotFound, err.Code)
	}
	if err.Message != "user not found" {
		t.Errorf("expected message 'user not found', got %q", err.Message)
	}
	if err.HTTPStatus() != 404 {
		t.Errorf("expected HTTP status 404, got %d", err.HTTPStatus())
	}
}

func TestNewf(t *testing.T) {
	err := Newf(ErrDBError, "failed to query table %s", "users")
	if err.Code != ErrDBError {
		t.Errorf("expected code %s, got %s", ErrDBError, err.Code)
	}
	if err.Message != "failed to query table users" {
		t.Errorf("unexpected message: %q", err.Message)
	}
}

func TestWithDetails(t *testing.T) {
	err := New(ErrInvalidBody, "bad request").WithDetails(map[string]string{"field": "email"})
	if err.Details == nil {
		t.Error("expected details to be set")
	}
	details, ok := err.Details.(map[string]string)
	if !ok {
		t.Error("expected details to be map[string]string")
	}
	if details["field"] != "email" {
		t.Errorf("expected field 'email', got %q", details["field"])
	}
}

func TestErrorString(t *testing.T) {
	err := New(ErrNotFound, "resource missing")
	expected := "[RES_001] resource missing"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestHTTPStatusMapping(t *testing.T) {
	tests := []struct {
		code   ErrorCode
		status int
	}{
		// Auth — 401
		{ErrMissingAuth, 401},
		{ErrInvalidCredentials, 401},
		{ErrTokenExpired, 401},
		{ErrTokenInvalid, 401},
		{ErrAccountDisabled, 401},
		{ErrEmailNotVerified, 401},
		{ErrAPIKeyInvalid, 401},
		// Auth — 403
		{ErrInsufficientPerms, 403},
		// Auth — 429
		{ErrAccountLocked, 429},
		// Auth — 400/409
		{ErrPasswordTooWeak, 400},
		{ErrDuplicateEmail, 409},
		// Validation — 400
		{ErrInvalidBody, 400},
		{ErrMissingField, 400},
		{ErrInvalidEmail, 400},
		{ErrInvalidID, 400},
		{ErrPayloadTooLarge, 413},
		{ErrInvalidQuery, 400},
		{ErrInvalidPagination, 400},
		// Resource
		{ErrNotFound, 404},
		{ErrAlreadyExists, 409},
		{ErrConflict, 409},
		{ErrDeleted, 410},
		// Scanner
		{ErrScanFailed, 500},
		{ErrScanTimeout, 504},
		{ErrScanInputEmpty, 400},
		{ErrUnsupportedLang, 400},
		{ErrReviewFailed, 500},
		{ErrNoLLMProvider, 503},
		// Skill
		{ErrSkillNotFound, 404},
		{ErrSkillNotPublished, 404},
		{ErrSkillVersionInvalid, 400},
		{ErrSkillUploadFailed, 500},
		{ErrSkillScanFailed, 500},
		{ErrSkillSearchFailed, 500},
		// Billing
		{ErrBillingNotConfigured, 503},
		{ErrSubscriptionNotFound, 404},
		{ErrCheckoutFailed, 400},
		{ErrPaymentFailed, 402},
		// Infrastructure
		{ErrRateLimited, 429},
		{ErrServiceDown, 503},
		{ErrDBError, 500},
		{ErrCacheError, 503},
		{ErrQueueError, 500},
		{ErrWebhookFailed, 500},
	}

	for _, tt := range tests {
		err := New(tt.code, "test")
		if err.HTTPStatus() != tt.status {
			t.Errorf("code %s: expected HTTP %d, got %d", tt.code, tt.status, err.HTTPStatus())
		}
	}
}

func TestPredefinedErrorCodes(t *testing.T) {
	// Verify all ErrorCode constants are non-empty and map to valid HTTP statuses
	codes := []ErrorCode{
		ErrMissingAuth, ErrInvalidCredentials, ErrTokenExpired, ErrTokenInvalid,
		ErrAccountLocked, ErrAccountDisabled, ErrInsufficientPerms, ErrEmailNotVerified,
		ErrPasswordTooWeak, ErrDuplicateEmail, ErrAPIKeyInvalid,
		ErrInvalidBody, ErrMissingField, ErrInvalidEmail, ErrInvalidID,
		ErrPayloadTooLarge, ErrInvalidQuery, ErrInvalidPagination,
		ErrNotFound, ErrAlreadyExists, ErrConflict, ErrDeleted,
		ErrScanFailed, ErrScanTimeout, ErrScanInputEmpty, ErrUnsupportedLang,
		ErrReviewFailed, ErrNoLLMProvider,
		ErrSkillNotFound, ErrSkillNotPublished, ErrSkillVersionInvalid,
		ErrSkillUploadFailed, ErrSkillScanFailed, ErrSkillSearchFailed,
		ErrBillingNotConfigured, ErrCheckoutFailed, ErrSubscriptionNotFound, ErrPaymentFailed,
		ErrRateLimited, ErrServiceDown, ErrDBError, ErrCacheError, ErrQueueError, ErrWebhookFailed,
	}

	for _, code := range codes {
		if code == "" {
			t.Error("found empty ErrorCode constant")
			continue
		}
		err := New(code, "test")
		if err.HTTPStatus() == 0 {
			t.Errorf("code %s: HTTPStatus() returned 0", code)
		}
	}
}

func TestCustomHTTPStatus(t *testing.T) {
	err := New(ErrNotFound, "custom status")
	err.Status = 418 // I'm a teapot
	if err.HTTPStatus() != 418 {
		t.Errorf("expected custom HTTP status 418, got %d", err.HTTPStatus())
	}
}

func TestWithDetailsPreservesCodeAndMessage(t *testing.T) {
	err := New(ErrDBError, "db failed").WithDetails(map[string]string{"table": "users"})
	if err.Code != ErrDBError {
		t.Errorf("expected code %s, got %s", ErrDBError, err.Code)
	}
	if err.Message != "db failed" {
		t.Errorf("expected message 'db failed', got %q", err.Message)
	}
}
