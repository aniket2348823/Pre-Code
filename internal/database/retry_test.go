package database

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// --- retryableError ---

func TestRetryableError_Nil(t *testing.T) {
	if retryableError(nil) {
		t.Error("nil should not be retryable")
	}
}

func TestRetryableError_NetError(t *testing.T) {
	err := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	if !retryableError(err) {
		t.Error("net.OpError should be retryable")
	}
}

func TestRetryableError_DNSError(t *testing.T) {
	err := &net.DNSError{Err: "no such host", Name: "db.example.com"}
	if !retryableError(err) {
		t.Error("net.DNSError should be retryable")
	}
}

func TestRetryableError_NetClosed(t *testing.T) {
	err := net.ErrClosed
	if !retryableError(err) {
		t.Error("net.ErrClosed should be retryable")
	}
}

func TestRetryableError_PgError_Class08(t *testing.T) {
	codes := []string{"08000", "08001", "08003", "08006", "08007"}
	for _, code := range codes {
		err := &pgconn.PgError{Code: code}
		if !retryableError(err) {
			t.Errorf("PgError code %s should be retryable", code)
		}
	}
}

func TestRetryableError_PgError_Class40(t *testing.T) {
	codes := []string{"40001", "40P01"}
	for _, code := range codes {
		err := &pgconn.PgError{Code: code}
		if !retryableError(err) {
			t.Errorf("PgError code %s should be retryable", code)
		}
	}
}

func TestRetryableError_PgError_Class57(t *testing.T) {
	codes := []string{"57014", "57P01", "57P02", "57P03"}
	for _, code := range codes {
		err := &pgconn.PgError{Code: code}
		if !retryableError(err) {
			t.Errorf("PgError code %s should be retryable", code)
		}
	}
}

func TestRetryableError_PgError_Class53(t *testing.T) {
	codes := []string{"53100", "53200", "53300"}
	for _, code := range codes {
		err := &pgconn.PgError{Code: code}
		if !retryableError(err) {
			t.Errorf("PgError code %s should be retryable", code)
		}
	}
}

func TestRetryableError_PgError_Class54(t *testing.T) {
	codes := []string{"54000", "54001"}
	for _, code := range codes {
		err := &pgconn.PgError{Code: code}
		if !retryableError(err) {
			t.Errorf("PgError code %s should be retryable", code)
		}
	}
}

func TestRetryableError_PgError_ConstraintViolation(t *testing.T) {
	err := &pgconn.PgError{Code: "23505"} // unique_violation
	if retryableError(err) {
		t.Error("unique_violation should NOT be retryable")
	}
}

func TestRetryableError_PgError_SyntaxError(t *testing.T) {
	err := &pgconn.PgError{Code: "42601"} // syntax_error
	if retryableError(err) {
		t.Error("syntax_error should NOT be retryable")
	}
}

func TestRetryableError_PgError_DataError(t *testing.T) {
	err := &pgconn.PgError{Code: "22P02"} // invalid_text_representation
	if retryableError(err) {
		t.Error("invalid_text_representation should NOT be retryable")
	}
}

func TestRetryableError_ContextDeadline(t *testing.T) {
	err := context.DeadlineExceeded
	if !retryableError(err) {
		t.Error("context.DeadlineExceeded should be retryable")
	}
}

func TestRetryableError_StringKeywords(t *testing.T) {
	keywords := []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"timeout",
		"serialization failure",
		"deadlock detected",
		"query canceled",
		"admin shutdown",
		"too many clients",
		"pool timeout",
		"i/o timeout",
		"broken pipe",
		"bad connection",
	}
	for _, kw := range keywords {
		err := errors.New(kw)
		if !retryableError(err) {
			t.Errorf("error %q should be retryable", kw)
		}
	}
}

func TestRetryableError_NonRetryable(t *testing.T) {
	errs := []error{
		errors.New("table does not exist"),
		errors.New("column missing"),
		errors.New("permission denied"),
		errors.New("random unrelated error"),
	}
	for _, err := range errs {
		if retryableError(err) {
			t.Errorf("error %q should NOT be retryable", err.Error())
		}
	}
}

// --- retryDelay ---

func TestRetryDelay_AttemptOne(t *testing.T) {
	cfg := DefaultRetryConfig()
	delay := retryDelay(1, cfg)
	if delay < cfg.BaseDelay/2 || delay > cfg.BaseDelay+cfg.BaseDelay*cfg.JitterRatio {
		t.Errorf("attempt 1 delay %v out of expected range", delay)
	}
}

func TestRetryDelay_Exponential(t *testing.T) {
	cfg := RetryConfig{BaseDelay: 100 * time.Millisecond, MaxDelay: 5 * time.Second, JitterRatio: 0}
	d1 := retryDelay(1, cfg)
	d2 := retryDelay(2, cfg)
	d3 := retryDelay(3, cfg)
	if d1 != 100*time.Millisecond {
		t.Errorf("attempt 1: expected 100ms, got %v", d1)
	}
	if d2 != 200*time.Millisecond {
		t.Errorf("attempt 2: expected 200ms, got %v", d2)
	}
	if d3 != 400*time.Millisecond {
		t.Errorf("attempt 3: expected 400ms, got %v", d3)
	}
}

func TestRetryDelay_MaxCap(t *testing.T) {
	cfg := RetryConfig{BaseDelay: 100 * time.Millisecond, MaxDelay: 500 * time.Millisecond, JitterRatio: 0}
	delay := retryDelay(10, cfg)
	if delay > 500*time.Millisecond {
		t.Errorf("delay should be capped at MaxDelay, got %v", delay)
	}
}

func TestRetryDelay_ZeroJitter(t *testing.T) {
	cfg := RetryConfig{BaseDelay: 100 * time.Millisecond, MaxDelay: 5 * time.Second, JitterRatio: 0}
	d1 := retryDelay(1, cfg)
	d2 := retryDelay(1, cfg)
	if d1 != d2 {
		t.Errorf("zero jitter should produce deterministic delay, got %v and %v", d1, d2)
	}
}

func TestRetryDelay_AttemptZero(t *testing.T) {
	delay := retryDelay(0, DefaultRetryConfig())
	if delay != 0 {
		t.Errorf("attempt 0 should return 0 delay, got %v", delay)
	}
}

// --- DefaultRetryConfig ---

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
	if cfg.BaseDelay != 100*time.Millisecond {
		t.Errorf("BaseDelay = %v, want 100ms", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 5*time.Second {
		t.Errorf("MaxDelay = %v, want 5s", cfg.MaxDelay)
	}
	if cfg.JitterRatio != 0.2 {
		t.Errorf("JitterRatio = %f, want 0.2", cfg.JitterRatio)
	}
}

// --- RetryQuery ---

func TestRetryQuery_SuccessFirstAttempt(t *testing.T) {
	cfg := DefaultRetryConfig()
	calls := 0
	result, err := RetryQuery(context.Background(), cfg, func(ctx context.Context) (pgxRows, error) {
		calls++
		return &mockRows{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil rows")
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryQuery_TransientErrorRetries(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, JitterRatio: 0}
	calls := 0
	_, err := RetryQuery(context.Background(), cfg, func(ctx context.Context) (pgxRows, error) {
		calls++
		if calls < 3 {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
		}
		return &mockRows{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryQuery_NonRetryableErrorNoRetry(t *testing.T) {
	cfg := DefaultRetryConfig()
	calls := 0
	_, err := RetryQuery(context.Background(), cfg, func(ctx context.Context) (pgxRows, error) {
		calls++
		return nil, errors.New("table does not exist")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("non-retryable error should not retry, got %d calls", calls)
	}
}

func TestRetryQuery_ExhaustsAttempts(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, JitterRatio: 0}
	calls := 0
	_, err := RetryQuery(context.Background(), cfg, func(ctx context.Context) (pgxRows, error) {
		calls++
		return nil, errors.New("connection refused")
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryQuery_ContextCancelledDuringRetry(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: time.Second, JitterRatio: 0}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	_, err := RetryQuery(ctx, cfg, func(ctx context.Context) (pgxRows, error) {
		calls++
		return nil, errors.New("connection refused")
	})
	if err == nil {
		t.Fatal("expected error after context cancel")
	}
	if calls != 1 {
		t.Errorf("expected 1 call before cancel, got %d", calls)
	}
}

// --- RetryExec ---

func TestRetryExec_SuccessFirstAttempt(t *testing.T) {
	cfg := DefaultRetryConfig()
	calls := 0
	tag, err := RetryExec(context.Background(), cfg, func(ctx context.Context) (pgconn.CommandTag, error) {
		calls++
		return pgconn.CommandTag{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
	_ = tag
}

func TestRetryExec_TransientErrorRetries(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, JitterRatio: 0}
	calls := 0
	_, err := RetryExec(context.Background(), cfg, func(ctx context.Context) (pgconn.CommandTag, error) {
		calls++
		if calls < 3 {
			return pgconn.CommandTag{}, errors.New("connection reset")
		}
		return pgconn.CommandTag{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryExec_NonRetryableErrorNoRetry(t *testing.T) {
	cfg := DefaultRetryConfig()
	calls := 0
	_, err := RetryExec(context.Background(), cfg, func(ctx context.Context) (pgconn.CommandTag, error) {
		calls++
		return pgconn.CommandTag{}, &pgconn.PgError{Code: "23505"}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("non-retryable error should not retry, got %d calls", calls)
	}
}

// pgxRows interface for test compatibility
type pgxRows = interface {
	Close()
	Err() error
	CommandTag() pgconn.CommandTag
	FieldDescriptions() []pgconn.FieldDescription
	Next() bool
	Scan(dest ...any) error
	Values() ([]any, error)
	RawValues() [][]byte
}
