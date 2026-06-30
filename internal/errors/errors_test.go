package errors

import (
	"errors"
	"testing"
)

func TestAppError_ErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *AppError
		want string
	}{
		{"no wrapped error", NotFound("Task"), "RESOURCE_NOT_FOUND: Task not found"},
		{"wrapped error", Wrap(errors.New("boom"), CodeTaskFailed, "task"), "TASK_FAILED: task: boom"},
		{"validation", Validation("bad input", nil), "VALIDATION_ERROR: bad input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAppError_HTTPStatus(t *testing.T) {
	cases := map[Code]int{
		CodeNotFound:       404,
		CodeInvalidToken:   401,
		CodeUnauthorized:   401,
		CodeInsufficient:   403,
		CodeForbidden:      403,
		CodeConflict:       409,
		CodeValidation:     422,
		CodeQuota:          402,
		CodeBudgetExceeded: 402,
		CodeRateLimit:      429,
		CodeProviderDown:   503,
		CodeTaskFailed:     500,
	}
	for code, want := range cases {
		t.Run(string(code), func(t *testing.T) {
			if got := New(code, "x").HTTPStatus(); got != want {
				t.Errorf("got %d, want %d", got, want)
			}
		})
	}
}

func TestAppError_ToBody(t *testing.T) {
	e := WithRequestID(Validation("bad", map[string]any{"field": "title"}), "req-1")
	body := e.ToBody()
	if body.Error.Code != CodeValidation {
		t.Fatalf("code mismatch: %s", body.Error.Code)
	}
	if body.Error.RequestID != "req-1" {
		t.Errorf("missing request id")
	}
	if body.Error.Timestamp == "" {
		t.Errorf("missing timestamp")
	}
	if body.Error.Details["field"] != "title" {
		t.Errorf("missing details")
	}
}

func TestAsAppError(t *testing.T) {
	inner := NotFound("Skill")
	wrapped := Wrap(inner, CodeTaskFailed, "task chain")

	got, ok := AsAppError(wrapped)
	if !ok {
		t.Fatal("expected AsAppError to unwrap")
	}
	if got.Code != CodeTaskFailed {
		t.Errorf("got %s, want TASK_FAILED", got.Code)
	}

	if _, ok := AsAppError(errors.New("plain")); ok {
		t.Error("expected false on plain error")
	}
}
