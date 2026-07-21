package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/vigilagent/vigilagent/internal/llm"
)

const FixtureSchemaVersion = 1

// WorkloadTask is one real task definition. Its attributes feed the router's
// real complexity classifier — the harness does not hardcode a tier.
type WorkloadTask struct {
	ID                string   `json:"id"`
	Prompt            string   `json:"prompt"`
	System            string   `json:"system,omitempty"`
	Type              string   `json:"type"`
	FilesChanged      int      `json:"files_changed"`
	RequiresReasoning bool     `json:"requires_reasoning"`
	IsNovel           bool     `json:"is_novel"`
	Tags              []string `json:"tags,omitempty"`
}

// Workload is the ordered execution sequence plus unique task definitions.
// Sequence may repeat a task ID to exercise the cache.
type Workload struct {
	Sequence []string       `json:"sequence"`
	Tasks    []WorkloadTask `json:"tasks"`
}

// FixtureEntry is one real recorded provider response for a (task, model) pair.
type FixtureEntry struct {
	TaskID       string  `json:"task_id"`
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	Content      string  `json:"content"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

// Fixture is the versioned store of real recorded responses.
type Fixture struct {
	SchemaVersion int            `json:"schema_version"`
	PremiumModel  string         `json:"premium_model"`
	Entries       []FixtureEntry `json:"entries"`

	index map[string]FixtureEntry
}

// Report is the final measured result.
type Report struct {
	BaselineCost   float64 `json:"baseline_cost"`
	OptimizedCost  float64 `json:"optimized_cost"`
	TotalSaved     float64 `json:"total_saved"`
	RoutingPortion float64 `json:"routing_portion"`
	CachePortion   float64 `json:"cache_portion"`
	PercentSaved   float64 `json:"percent_saved"`
	Tasks          int     `json:"tasks"`
	CacheHits      int     `json:"cache_hits"`
}

func LoadWorkload(path string) (*Workload, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workload %s: %w", path, err)
	}
	var w Workload
	if err := json.Unmarshal(b, &w); err != nil {
		return nil, fmt.Errorf("parse workload: %w", err)
	}
	return &w, nil
}

func (w *Workload) TaskByID(id string) (WorkloadTask, bool) {
	for _, t := range w.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return WorkloadTask{}, false
}

func LoadFixture(path string) (*Fixture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixture %s (record one with --live first): %w", path, err)
	}
	var f Fixture
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse fixture: %w", err)
	}
	if f.SchemaVersion != FixtureSchemaVersion {
		return nil, fmt.Errorf("fixture schema v%d != expected v%d; re-record with --live", f.SchemaVersion, FixtureSchemaVersion)
	}
	return &f, nil
}

func (f *Fixture) Save(path string) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (f *Fixture) Lookup(taskID, model string) (FixtureEntry, bool) {
	if f.index == nil {
		f.index = make(map[string]FixtureEntry, len(f.Entries))
		for _, e := range f.Entries {
			f.index[e.TaskID+"\x00"+e.Model] = e
		}
	}
	e, ok := f.index[taskID+"\x00"+model]
	return e, ok
}

// _ documents that complexity derives from WorkloadTask attributes via the real
// llm classifier (see engine.go); keeps the llm import meaningful in this file.
var _ = llm.PriceTable
