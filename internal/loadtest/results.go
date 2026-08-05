package loadtest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// RequestResult records a single HTTP request outcome.
type RequestResult struct {
	Method   string        `json:"method"`
	Path     string        `json:"path"`
	Status   int           `json:"status"`
	Latency  time.Duration `json:"latency"`
	Error    string        `json:"error,omitempty"`
	TimingMs float64       `json:"timing_ms"`
}

// Results aggregates all request results from a load test run.
type Results struct {
	Config       LoadTestConfig   `json:"config"`
	StartedAt    time.Time        `json:"started_at"`
	FinishedAt   time.Time        `json:"finished_at"`
	TotalReqs    int64            `json:"total_requests"`
	SuccessCount int64            `json:"success_count"`
	ErrorCount   int64            `json:"error_count"`
	Errors       map[string]int64 `json:"errors"`
	Requests     []RequestResult  `json:"-"`
	Latencies    []float64        `json:"-"`
	SLO          SLOResult        `json:"slo"`
}

// SLOResult holds pass/fail for each SLO target.
type SLOResult struct {
	P50Ms float64 `json:"p50_ms"`
	P95Ms float64 `json:"p95_ms"`
	P99Ms float64 `json:"p99_ms"`
	P99OK bool    `json:"p99_under_500ms"`
}

// Collector accumulates request results thread-safely.
type Collector struct {
	mu       sync.Mutex
	results  []RequestResult
	errors   map[string]int64
	started  time.Time
	finished time.Time
}

// NewCollector creates a ready-to-use Collector.
func NewCollector() *Collector {
	return &Collector{
		errors:  make(map[string]int64),
		started: time.Now(),
	}
}

// Record stores a single request result.
func (c *Collector) Record(r RequestResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r.TimingMs = float64(r.Latency.Microseconds()) / 1000.0
	c.results = append(c.results, r)
	if r.Error != "" {
		c.errors[r.Error]++
	}
}

// Finish marks the test as completed.
func (c *Collector) Finish() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finished = time.Now()
}

// Snapshot returns a deep copy of the current results for inspection.
func (c *Collector) Snapshot() []RequestResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]RequestResult, len(c.results))
	copy(out, c.results)
	return out
}

// BuildResults aggregates collected data into a Results struct.
func (c *Collector) BuildResults(cfg LoadTestConfig) Results {
	c.mu.Lock()
	snap := make([]RequestResult, len(c.results))
	copy(snap, c.results)
	errMap := make(map[string]int64, len(c.errors))
	for k, v := range c.errors {
		errMap[k] = v
	}
	started := c.started
	finished := c.finished
	c.mu.Unlock()

	var latencies []float64
	var success, fail int64

	for _, r := range snap {
		latencies = append(latencies, r.TimingMs)
		if r.Error != "" || (r.Status >= 400 && r.Status < 600) {
			fail++
		} else {
			success++
		}
	}

	sort.Float64s(latencies)

	res := Results{
		Config:       cfg,
		StartedAt:    started,
		FinishedAt:   finished,
		TotalReqs:    int64(len(snap)),
		SuccessCount: success,
		ErrorCount:   fail,
		Errors:       errMap,
		Requests:     snap,
		Latencies:    latencies,
	}

	if len(latencies) > 0 {
		res.SLO = SLOResult{
			P50Ms: percentile(latencies, 50),
			P95Ms: percentile(latencies, 95),
			P99Ms: percentile(latencies, 99),
			P99OK: percentile(latencies, 99) < 500.0,
		}
	}

	return res
}

// percentile computes the p-th percentile from a sorted slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p / 100.0) * float64(len(sorted)-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[lower]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

// ExportJSON writes the results to a JSON file.
func ExportJSON(res Results, path string) error {
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// ExportCSV writes individual request results to a CSV file.
func ExportCSV(res Results, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create csv: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	w.Write([]string{"method", "path", "status", "latency_ms", "error"})

	for _, r := range res.Requests {
		w.Write([]string{
			r.Method,
			r.Path,
			fmt.Sprintf("%d", r.Status),
			fmt.Sprintf("%.2f", r.TimingMs),
			r.Error,
		})
	}
	return nil
}

// LatencyHistogram returns a text-based histogram of latency buckets.
func LatencyHistogram(res Results, buckets int) string {
	if len(res.Latencies) == 0 {
		return "no data"
	}
	min := res.Latencies[0]
	max := res.Latencies[len(res.Latencies)-1]
	if min == max {
		return fmt.Sprintf("[%.1f ms] %d requests", min, len(res.Latencies))
	}

	step := (max - min) / float64(buckets)
	counts := make([]int, buckets)
	for _, v := range res.Latencies {
		idx := int((v - min) / step)
		if idx >= buckets {
			idx = buckets - 1
		}
		counts[idx]++
	}

	maxCount := 0
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}

	var sb strings.Builder
	barWidth := 40
	for i := 0; i < buckets; i++ {
		lo := min + float64(i)*step
		hi := lo + step
		barLen := 0
		if maxCount > 0 {
			barLen = counts[i] * barWidth / maxCount
		}
		bar := strings.Repeat("#", barLen)
		fmt.Fprintf(&sb, "%7.1f-%7.1f ms | %s %d\n", lo, hi, bar, counts[i])
	}
	return sb.String()
}

// ErrorRate returns the error percentage.
func ErrorRate(res Results) float64 {
	if res.TotalReqs == 0 {
		return 0
	}
	return float64(res.ErrorCount) / float64(res.TotalReqs) * 100.0
}

// Throughput returns requests per second.
func Throughput(res Results) float64 {
	dur := res.FinishedAt.Sub(res.StartedAt).Seconds()
	if dur <= 0 {
		return 0
	}
	return float64(res.TotalReqs) / dur
}

// Summary returns a one-line summary string.
func Summary(res Results) string {
	dur := res.FinishedAt.Sub(res.StartedAt)
	return fmt.Sprintf(
		"reqs=%d success=%d errors=%d err_rate=%.1f%% p50=%.1fms p95=%.1fms p99=%.1fms (ok=%v) dur=%.1fs rps=%.1f",
		res.TotalReqs, res.SuccessCount, res.ErrorCount,
		ErrorRate(res),
		res.SLO.P50Ms, res.SLO.P95Ms, res.SLO.P99Ms, res.SLO.P99OK,
		dur.Seconds(), Throughput(res),
	)
}
