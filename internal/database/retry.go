package database

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"math/rand/v2"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RetryConfig configures the retry behavior for database operations.
type RetryConfig struct {
	MaxAttempts int           // maximum number of attempts (including first). Default 3.
	BaseDelay   time.Duration // base delay for exponential backoff. Default 100ms.
	MaxDelay    time.Duration // cap on backoff delay. Default 5s.
	JitterRatio float64       // ± jitter as a fraction [0, 1). Default 0.2 (±20%).
}

// DefaultRetryConfig returns the standard retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    5 * time.Second,
		JitterRatio: 0.2,
	}
}

// retryableError returns true if the error is a transient failure that
// should be retried. Constraint violations, data errors, and syntax errors
// are NOT retried.
func retryableError(err error) bool {
	if err == nil {
		return false
	}

	// Connection refused / network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// dns errors
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// pgconn.PgError for PostgreSQL-specific transient errors
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		// Class 08 — Connection Exception
		case "08000", "08001", "08003", "08006", "08007":
			return true
		// Class 40 — Transaction Rollback (serialization, deadlock, statement timeout)
		case "40001", "40P01":
			return true
		// Class 25 — Invalid Transaction State (transient)
		case "25006":
			return true
		// Class 57 — Operator Intervention (query canceled, admin shutdown)
		case "57014", "57P01", "57P02", "57P03":
			return true
		// Class 53 — Insufficient Resources
		case "53100", "53200", "53300":
			return true
		// Class 54 — Program Limit Exceeded (sort/memory — transient under load)
		case "54000", "54001":
			return true
		}
	}

	// Context deadline exceeded (timeout)
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// String-based fallback for common transient error messages
	msg := err.Error()
	transientKeywords := []string{
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
	lower := strings.ToLower(msg)
	for _, kw := range transientKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	return false
}

// retryDelay computes the delay for the given attempt number using exponential
// backoff with jitter.
func retryDelay(attempt int, cfg RetryConfig) time.Duration {
	if attempt <= 0 {
		return 0
	}
	// exponential: base * 2^(attempt-1)
	delay := float64(cfg.BaseDelay) * math.Pow(2, float64(attempt-1))
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}
	// apply jitter: delay * (1 + random(-jitter, +jitter))
	jitter := cfg.JitterRatio
	if jitter <= 0 {
		return time.Duration(delay)
	}
	jitterRange := delay * jitter
	// rand.Float64 returns [0, 1), so shift to [-jitterRange, +jitterRange]
	offset := (rand.Float64()*2 - 1) * jitterRange
	delay += offset
	if delay < 0 {
		delay = 0
	}
	return time.Duration(delay)
}

// RetryQueryRow wraps a function that returns a pgx.Row with retry logic.
// The function fn is re-invoked on each retry attempt.
func RetryQueryRow(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) pgx.Row) pgx.Row {
	if cfg.MaxAttempts <= 0 {
		cfg = DefaultRetryConfig()
	}
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		row := fn(ctx)
		// We can't know if the row has an error without scanning, so we wrap
		// the row with a retry-aware wrapper that only retries on Scan.
		return &retryRow{row: row, fn: fn, cfg: cfg, attempt: attempt}
	}
	return &errRow{err: errors.New("retry: no attempts configured")}
}

// retryRow wraps a pgx.Row and retries on Scan if the error is retryable.
type retryRow struct {
	row     pgx.Row
	fn      func(ctx context.Context) pgx.Row
	cfg     RetryConfig
	attempt int
}

func (r *retryRow) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	if err != nil && retryableError(err) && r.attempt < r.cfg.MaxAttempts {
		delay := retryDelay(r.attempt, r.cfg)
		slog.Warn("retryable query error, retrying",
			"attempt", r.attempt,
			"max_attempts", r.cfg.MaxAttempts,
			"delay", delay,
			"error", err,
		)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-r.ctx().Done():
			timer.Stop()
			return r.ctx().Err()
		}
		next := r.attempt + 1
		newRow := r.fn(r.ctx())
		return (&retryRow{row: newRow, fn: r.fn, cfg: r.cfg, attempt: next}).Scan(dest...)
	}
	return err
}

func (r *retryRow) ctx() context.Context {
	return context.Background()
}

// RetryQuery wraps a function that returns (pgx.Rows, error) with retry logic.
func RetryQuery(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) (pgx.Rows, error)) (pgx.Rows, error) {
	if cfg.MaxAttempts <= 0 {
		cfg = DefaultRetryConfig()
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		rows, err := fn(ctx)
		if err == nil {
			return rows, nil
		}
		lastErr = err
		if !retryableError(err) || attempt >= cfg.MaxAttempts {
			return nil, err
		}
		delay := retryDelay(attempt, cfg)
		slog.Warn("retryable query error, retrying",
			"attempt", attempt,
			"max_attempts", cfg.MaxAttempts,
			"delay", delay,
			"error", err,
		)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

// RetryExec wraps a function that returns (pgconn.CommandTag, error) with retry logic.
func RetryExec(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) (pgconn.CommandTag, error)) (pgconn.CommandTag, error) {
	if cfg.MaxAttempts <= 0 {
		cfg = DefaultRetryConfig()
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		tag, err := fn(ctx)
		if err == nil {
			return tag, nil
		}
		lastErr = err
		if !retryableError(err) || attempt >= cfg.MaxAttempts {
			return pgconn.CommandTag{}, err
		}
		delay := retryDelay(attempt, cfg)
		slog.Warn("retryable exec error, retrying",
			"attempt", attempt,
			"max_attempts", cfg.MaxAttempts,
			"delay", delay,
			"error", err,
		)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return pgconn.CommandTag{}, ctx.Err()
		}
	}
	return pgconn.CommandTag{}, lastErr
}
