// Package loadtest provides HTTP load testing capabilities for VigilAgent.
package loadtest

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// LoadProfile defines the shape of concurrent connections over time.
type LoadProfile string

const (
	ProfileConstant LoadProfile = "constant" // flat concurrency
	ProfileRamping  LoadProfile = "ramping"  // linear ramp from 0 to Concurrency
	ProfileSpike    LoadProfile = "spike"    // normal → sudden spike → normal
	ProfileStress   LoadProfile = "stress"   // increment until failure
)

// LoadTestConfig controls a single load test run.
type LoadTestConfig struct {
	TargetURL   string        `json:"target_url"`
	Duration    time.Duration `json:"duration"`
	Concurrency int           `json:"concurrency"`
	RampUp      time.Duration `json:"ramp_up"`
	ThinkTime   time.Duration `json:"think_time"`
	Profile     LoadProfile   `json:"profile"`
	Timeout     time.Duration `json:"timeout"` // per-request timeout
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() LoadTestConfig {
	return LoadTestConfig{
		TargetURL:   "http://localhost:8080",
		Duration:    30 * time.Second,
		Concurrency: 10,
		RampUp:      5 * time.Second,
		ThinkTime:   100 * time.Millisecond,
		Profile:     ProfileConstant,
		Timeout:     10 * time.Second,
	}
}

// LoadFromFile reads config from a JSON file.
func LoadFromFile(path string) (LoadTestConfig, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config file: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config file: %w", err)
	}
	return cfg, nil
}

// WorkersAt returns the number of concurrent workers active at time t.
// The behaviour depends on the configured profile.
func (c LoadTestConfig) WorkersAt(t time.Duration) int {
	switch c.Profile {
	case ProfileRamping:
		if t >= c.RampUp {
			return c.Concurrency
		}
		ratio := float64(t) / float64(c.RampUp)
		n := int(ratio * float64(c.Concurrency))
		if n < 1 {
			n = 1
		}
		return n

	case ProfileSpike:
		mid := c.Duration / 2
		spikeStart := mid - 2*time.Second
		spikeEnd := mid + 2*time.Second
		if t >= spikeStart && t <= spikeEnd {
			return c.Concurrency * 3
		}
		norm := c.Concurrency / 3
		if norm < 1 {
			norm = 1
		}
		return norm

	case ProfileStress:
		// Scale linearly over duration: at t=Duration → Concurrency
		ratio := float64(t) / float64(c.Duration)
		n := int(ratio * float64(c.Concurrency))
		if n < 1 {
			n = 1
		}
		return n

	default: // constant
		return c.Concurrency
	}
}
