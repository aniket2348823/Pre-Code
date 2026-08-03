package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Result struct {
	StatusCode int
	Duration   time.Duration
	Err        error
}

func main() {
	targetURL := flag.String("url", "http://localhost:9090/v1/chat/completions", "Target URL to load test")
	totalReqs := flag.Int("n", 30000, "Total number of requests to execute")
	concurrency := flag.Int("c", 1000, "Number of concurrent workers")
	apiKey := flag.String("key", "test-secret-key", "Authorization API Key")
	flag.Parse()

	fmt.Printf("=====================================================\n")
	fmt.Printf("🚀 VIGILAGENT HIGH-THROUGHPUT LOAD TESTING SUITE\n")
	fmt.Printf("=====================================================\n")
	fmt.Printf("Target Endpoint : %s\n", *targetURL)
	fmt.Printf("Total Requests  : %d\n", *totalReqs)
	fmt.Printf("Concurrency     : %d workers\n", *concurrency)
	fmt.Printf("-----------------------------------------------------\n\n")

	// Custom HTTP client with persistent connection reuse for 30k requests
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 90 * time.Second,
		}).DialContext,
		MaxIdleConns:        10000,
		MaxIdleConnsPerHost: 10000,
		MaxConnsPerHost:     10000,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}

	payload := []byte(`{"model":"mock","messages":[{"role":"user","content":"Write a python function to query a database by user id"}]}`)

	jobs := make(chan int, *totalReqs)
	results := make(chan Result, *totalReqs)

	var (
		completedCount int64
		successCount   int64
		errorCount     int64
	)

	startTime := time.Now()

	// Spawn worker pool
	var wg sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				reqStart := time.Now()
				method := "POST"
				var reqBody io.Reader = bytes.NewReader(payload)
				if bytes.HasSuffix([]byte(*targetURL), []byte("/health")) || bytes.HasSuffix([]byte(*targetURL), []byte("/metrics")) {
					method = "GET"
					reqBody = nil
				}

				req, err := http.NewRequest(method, *targetURL, reqBody)
				if err != nil {
					results <- Result{Err: err}
					atomic.AddInt64(&errorCount, 1)
					atomic.AddInt64(&completedCount, 1)
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+*apiKey)

				resp, err := client.Do(req)
				reqDuration := time.Since(reqStart)

				if err != nil {
					if atomic.LoadInt64(&errorCount) < 3 {
						fmt.Printf("Sample Error: %v\n", err)
					}
					results <- Result{Duration: reqDuration, Err: err}
					atomic.AddInt64(&errorCount, 1)
				} else {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					results <- Result{StatusCode: resp.StatusCode, Duration: reqDuration}
					if resp.StatusCode == http.StatusOK {
						atomic.AddInt64(&successCount, 1)
					} else {
						atomic.AddInt64(&errorCount, 1)
					}
				}

				curr := atomic.AddInt64(&completedCount, 1)
				if curr%5000 == 0 || curr == int64(*totalReqs) {
					fmt.Printf("Progress: [%d / %d] requests completed (%.1f%%)...\n",
						curr, *totalReqs, float64(curr)/float64(*totalReqs)*100)
				}
			}
		}()
	}

	// Feed jobs into channel
	for i := 0; i < *totalReqs; i++ {
		jobs <- i
	}
	close(jobs)

	wg.Wait()
	close(results)

	totalDuration := time.Since(startTime)

	// Process and aggregate metrics
	var durations []time.Duration
	statusCodes := make(map[int]int)

	for res := range results {
		if res.StatusCode > 0 {
			statusCodes[res.StatusCode]++
		}
		if res.Duration > 0 {
			durations = append(durations, res.Duration)
		}
	}

	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	var (
		minDuration time.Duration
		maxDuration time.Duration
		avgDuration time.Duration
		p50         time.Duration
		p95         time.Duration
		p99         time.Duration
	)

	if len(durations) > 0 {
		minDuration = durations[0]
		maxDuration = durations[len(durations)-1]

		var sum time.Duration
		for _, d := range durations {
			sum += d
		}
		avgDuration = sum / time.Duration(len(durations))

		p50 = durations[int(float64(len(durations))*0.50)]
		p95 = durations[int(float64(len(durations))*0.95)]
		p99 = durations[int(float64(len(durations))*0.99)]
	}

	rps := float64(successCount) / totalDuration.Seconds()

	fmt.Printf("\n=====================================================\n")
	fmt.Printf("📊 LOAD TEST RESULTS (30,000 REQUESTS BENCHMARK)\n")
	fmt.Printf("=====================================================\n")
	fmt.Printf("Total Requests Executed : %d\n", *totalReqs)
	fmt.Printf("Successful (HTTP 200)   : %d (%.2f%%)\n", successCount, float64(successCount)/float64(*totalReqs)*100)
	fmt.Printf("Failed / Errors         : %d (%.2f%%)\n", errorCount, float64(errorCount)/float64(*totalReqs)*100)
	fmt.Printf("Total Elapsed Time      : %.2f seconds\n", totalDuration.Seconds())
	fmt.Printf("Throughput (RPS)        : %.2f req/sec ⚡\n", rps)
	fmt.Printf("-----------------------------------------------------\n")
	fmt.Printf("Latency Breakdown:\n")
	fmt.Printf("  • Min Latency  : %v\n", minDuration)
	fmt.Printf("  • Avg Latency  : %v\n", avgDuration)
	fmt.Printf("  • Max Latency  : %v\n", maxDuration)
	fmt.Printf("  • P50 Latency  : %v\n", p50)
	fmt.Printf("  • P95 Latency  : %v\n", p95)
	fmt.Printf("  • P99 Latency  : %v\n", p99)
	fmt.Printf("-----------------------------------------------------\n")
	fmt.Printf("Status Codes:\n")
	for code, count := range statusCodes {
		fmt.Printf("  • HTTP %d : %d requests\n", code, count)
	}
	fmt.Printf("=====================================================\n")
}
