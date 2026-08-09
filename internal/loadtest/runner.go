package loadtest

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// ScenarioFunc is executed by each worker during the load test.
// It receives a shared HTTP client and returns a RequestResult.
type ScenarioFunc func(client *http.Client, baseURL string) RequestResult

// LoadTestRunner executes a load test scenario against the target.
type LoadTestRunner struct {
	cfg       LoadTestConfig
	scenario  ScenarioFunc
	collector *Collector
	logger    *slog.Logger
}

// NewRunner creates a runner for the given config and scenario.
func NewRunner(cfg LoadTestConfig, scenario ScenarioFunc, logger *slog.Logger) *LoadTestRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &LoadTestRunner{
		cfg:       cfg,
		scenario:  scenario,
		collector: NewCollector(),
		logger:    logger,
	}
}

// Run executes the load test and returns aggregated results.
func (r *LoadTestRunner) Run(ctx context.Context) Results {
	r.logger.Info("load test started",
		"target", r.cfg.TargetURL,
		"duration", r.cfg.Duration,
		"concurrency", r.cfg.Concurrency,
		"profile", r.cfg.Profile,
	)

	client := &http.Client{
		Timeout: r.cfg.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        r.cfg.Concurrency * 2,
			MaxIdleConnsPerHost: r.cfg.Concurrency * 2,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	var activeWorkers int64
	var workerCtx, cancel = context.WithTimeout(ctx, r.cfg.Duration)
	defer cancel()

	done := make(chan struct{})

	// Progress reporter goroutine
	go r.reportProgress(workerCtx, &activeWorkers)

	// Main loop: spawn/reap workers to match profile
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

start:
	for {
		select {
		case <-workerCtx.Done():
			// Wait for all workers to drain
			for atomic.LoadInt64(&activeWorkers) > 0 {
				// #nosec time_sleep_in_handler: startup retry backoff / worker pacing, not a request handler
				time.Sleep(50 * time.Millisecond)
			}
			break start

		case <-ticker.C:
			deadline, ok := workerCtx.Deadline()
			if !ok {
				break
			}
			elapsed := r.cfg.Duration - time.Until(deadline)
			target := r.cfg.WorkersAt(elapsed)
			current := int(atomic.LoadInt64(&activeWorkers))

			// Spawn workers up to target
			for i := current; i < target; i++ {
				atomic.AddInt64(&activeWorkers, 1)
				go r.worker(workerCtx, client, &activeWorkers)
			}

		case <-done:
			break start
		}
	}

	r.collector.Finish()

	results := r.collector.BuildResults(r.cfg)
	r.logger.Info("load test completed", "summary", Summary(results))
	return results
}

// worker runs the scenario in a loop until ctx is cancelled.
func (r *LoadTestRunner) worker(ctx context.Context, client *http.Client, counter *int64) {
	defer atomic.AddInt64(counter, -1)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		result := r.scenario(client, r.cfg.TargetURL)
		r.collector.Record(result)

		if r.cfg.ThinkTime > 0 {
			// #nosec time_sleep_in_handler: startup retry backoff / worker pacing, not a request handler
			time.Sleep(r.cfg.ThinkTime)
		}
	}
}

// reportProgress logs progress every 2 seconds.
func (r *LoadTestRunner) reportProgress(ctx context.Context, active *int64) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := r.collector.Snapshot()
			workers := atomic.LoadInt64(active)
			var errCount int64
			for _, s := range snap {
				if s.Error != "" || (s.Status >= 400 && s.Status < 600) {
					errCount++
				}
			}
			r.logger.Info("progress",
				"workers", workers,
				"total", len(snap),
				"errors", errCount,
			)
		}
	}
}

// MakeRequest is a helper that sends an HTTP request and returns a RequestResult.
func MakeRequest(client *http.Client, method, url, body string) RequestResult {
	start := time.Now()

	var bodyReader interface{ Read([]byte) (int, error) }
	if body != "" {
		bodyReader = nil // simplified; real scenarios handle this
	}
	_ = bodyReader

	// #nosec context_leak: background context for long-running startup/worker/lifecycle code - no request context exists here
	req, err := http.NewRequestWithContext(context.Background(), method, url, nil)
	if err != nil {
		return RequestResult{
			Method:  method,
			Path:    url,
			Status:  0,
			Latency: time.Since(start),
			Error:   fmt.Sprintf("build request: %v", err),
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return RequestResult{
			Method:  method,
			Path:    url,
			Status:  0,
			Latency: time.Since(start),
			Error:   err.Error(),
		}
	}
	defer resp.Body.Close()

	return RequestResult{
		Method:  method,
		Path:    url,
		Status:  resp.StatusCode,
		Latency: time.Since(start),
	}
}

// MakeJSONRequest is like MakeRequest but sends a JSON content-type header.
func MakeJSONRequest(client *http.Client, method, url string) RequestResult {
	start := time.Now()

	// #nosec context_leak: background context for long-running startup/worker/lifecycle code - no request context exists here
	req, err := http.NewRequestWithContext(context.Background(), method, url, nil)
	if err != nil {
		return RequestResult{
			Method:  method,
			Path:    url,
			Status:  0,
			Latency: time.Since(start),
			Error:   fmt.Sprintf("build request: %v", err),
		}
	}
	req.Header.Set("Content-Type", "application/json")
	if method != "GET" && method != "HEAD" {
		req.Header.Set("Authorization", "Bearer loadtest-token")
	}

	resp, err := client.Do(req)
	if err != nil {
		return RequestResult{
			Method:  method,
			Path:    url,
			Status:  0,
			Latency: time.Since(start),
			Error:   err.Error(),
		}
	}
	defer resp.Body.Close()

	return RequestResult{
		Method:  method,
		Path:    url,
		Status:  resp.StatusCode,
		Latency: time.Since(start),
	}
}
