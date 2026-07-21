# cmd/bench Real Cost-Saving Harness — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a `cmd/bench` binary that prints one honestly-measured cost-saving number (baseline vs router+cache), computed entirely from real provider responses.

**Architecture:** Two lanes over one measurement engine. `--live` calls real Anthropic/OpenAI providers and records each `(task, model)` real response+usage+cost into `fixture.json`. The default lane replays that fixture deterministically at $0. Both lanes feed the same engine, which reuses the real `llm.ModelRouter` routing decision and real `llm.InMemoryCache` — only the provider *call* is swapped for a fixture lookup. Baseline (always-premium, no cache) and optimized (routed + cache) are both tallied from real recorded costs; savings are split into a routing portion and a cache portion whose sum equals total savings.

**Tech Stack:** Go, `internal/llm` (existing router/cache/adapters), stdlib `flag`/`encoding/json`/`testing`.

## Global Constraints

- Module path: `github.com/vigilagent/vigilagent` — all imports use this prefix.
- Go toolchain already in `go.mod`; no new dependencies.
- `cmd/bench` is `package main`; its tests are `package main` in the same directory.
- Premium baseline model default: `claude-opus-4` (top tier). Overridable by `--premium-model`.
- Provider-for-a-model resolves via `llm.PriceTable[model].Provider`.
- Fixture `schema_version` is `1`; replay refuses a mismatched version.
- No fabricated responses anywhere: every cost is a recorded real provider cost, or a real $0 cache hit.

---

## File Structure

- `internal/server/server.go` — MODIFY: add missing `cost` import (prereq, unblocks build).
- `cmd/bench/types.go` — CREATE: shared types (`WorkloadTask`, `Workload`, `FixtureEntry`, `Fixture`, `Report`) + JSON load/save + fixture lookup map + `tierComplexityFields`.
- `cmd/bench/workload.json` — CREATE: real, checked-in task set with deliberate repeats.
- `cmd/bench/engine.go` — CREATE: baseline + optimized passes, split formulas, reusing real `Route` + `InMemoryCache`.
- `cmd/bench/report.go` — CREATE: compute percentages, format human + JSON output, threshold verdict.
- `cmd/bench/recorder.go` — CREATE: `--live` real-provider recording into a `Fixture`.
- `cmd/bench/main.go` — CREATE: flags, lane selection, wiring, CI exit code.
- `cmd/bench/*_test.go` — CREATE alongside the above.

---

## Task 0: Fix build — add cost import to server.go

**Files:**
- Modify: `internal/server/server.go` (imports block, lines ~9-22)

**Interfaces:**
- Consumes: nothing.
- Produces: a compiling tree so later tasks that link `internal/llm` build.

- [ ] **Step 1: Confirm the failure**

Run: `cd "/d/Antigravity 2/Precode" && go build ./internal/server/ 2>&1`
Expected: `internal\server\server.go:110:15: undefined: cost`

- [ ] **Step 2: Add the import**

In `internal/server/server.go`, the import block currently ends the `internal/*` group with `config`, `database`. Add the `cost` line in alphabetical position (after `config`, before `database`):

```go
	"github.com/vigilagent/vigilagent/internal/config"
	"github.com/vigilagent/vigilagent/internal/cost"
	"github.com/vigilagent/vigilagent/internal/database"
```

- [ ] **Step 3: Verify whole tree builds**

Run: `cd "/d/Antigravity 2/Precode" && go build ./... 2>&1; echo "exit: $?"`
Expected: no output, `exit: 0`

- [ ] **Step 4: Verify existing tests still pass**

Run: `cd "/d/Antigravity 2/Precode" && go test ./internal/... 2>&1 | tail -15`
Expected: all `ok` / no `FAIL`.

- [ ] **Step 5: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add internal/server/server.go
git commit -m "fix: import cost package in server (unblocks build)"
```

---

## Task 1: Shared types + JSON I/O + fixture lookup

**Files:**
- Create: `cmd/bench/types.go`
- Test: `cmd/bench/types_test.go`

**Interfaces:**
- Consumes: `github.com/vigilagent/vigilagent/internal/llm` (`llm.Complexity`, `llm.PriceTable`).
- Produces:
  - `type WorkloadTask struct { ID, Prompt, System, Type string; FilesChanged int; RequiresReasoning, IsNovel bool; Tags []string }`
  - `type Workload struct { Sequence []string; Tasks []WorkloadTask }`
  - `type FixtureEntry struct { TaskID, Model, Provider, Content string; InputTokens, OutputTokens int; Cost float64 }`
  - `type Fixture struct { SchemaVersion int; PremiumModel string; Entries []FixtureEntry }`
  - `type Report struct { BaselineCost, OptimizedCost, TotalSaved, RoutingPortion, CachePortion, PercentSaved float64; Tasks, CacheHits int }`
  - `const FixtureSchemaVersion = 1`
  - `func LoadWorkload(path string) (*Workload, error)`
  - `func (w *Workload) TaskByID(id string) (WorkloadTask, bool)`
  - `func LoadFixture(path string) (*Fixture, error)` — errors on missing file and on `SchemaVersion != FixtureSchemaVersion`
  - `func (f *Fixture) Save(path string) error`
  - `func (f *Fixture) Lookup(taskID, model string) (FixtureEntry, bool)` — backed by a map built on first call

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"path/filepath"
	"testing"
)

func TestFixtureLookupAndSchemaGuard(t *testing.T) {
	f := &Fixture{
		SchemaVersion: FixtureSchemaVersion,
		PremiumModel:  "claude-opus-4",
		Entries: []FixtureEntry{
			{TaskID: "t1", Model: "gpt-4o-mini", Provider: "openai", Cost: 0.001, InputTokens: 10, OutputTokens: 5, Content: "ok"},
			{TaskID: "t1", Model: "claude-opus-4", Provider: "anthropic", Cost: 0.05, InputTokens: 10, OutputTokens: 5, Content: "ok"},
		},
	}
	path := filepath.Join(t.TempDir(), "fx.json")
	if err := f.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadFixture(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e, ok := got.Lookup("t1", "gpt-4o-mini")
	if !ok || e.Cost != 0.001 {
		t.Fatalf("lookup miss or wrong cost: %+v ok=%v", e, ok)
	}
	if _, ok := got.Lookup("t1", "nope"); ok {
		t.Fatal("expected miss for unknown model")
	}
}

func TestLoadFixtureRejectsBadSchema(t *testing.T) {
	f := &Fixture{SchemaVersion: 99, PremiumModel: "x"}
	path := filepath.Join(t.TempDir(), "fx.json")
	if err := f.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := LoadFixture(path); err == nil {
		t.Fatal("expected schema-version error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/d/Antigravity 2/Precode" && go test ./cmd/bench/ -run TestFixture -v 2>&1 | tail -20`
Expected: build failure — `undefined: Fixture` etc.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/bench/types.go`:

```go
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

// tierComplexityFields is unused here but documents that complexity derives
// from WorkloadTask attributes via the real llm classifier; see engine.go.
var _ = llm.PriceTable
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/d/Antigravity 2/Precode" && go test ./cmd/bench/ -run TestFixture -v 2>&1 | tail -20`
Expected: `PASS` for both tests.

- [ ] **Step 5: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add cmd/bench/types.go cmd/bench/types_test.go
git commit -m "feat(bench): shared types, fixture I/O, schema guard"
```

---

## Task 2: Measurement engine (baseline + optimized + split)

**Files:**
- Create: `cmd/bench/engine.go`
- Test: `cmd/bench/engine_test.go`

**Interfaces:**
- Consumes: `Workload`, `Fixture`, `Report` (Task 1); `llm.NewModelRouter`, `llm.RouterConfig`, `llm.Task`, `llm.Message`, `llm.ChatRequest`, `llm.ChatResponse`, `llm.NewInMemoryCache`, `llm.CacheKey`, `llm.PriceTable`, `llm.Provider`, `llm.ChatChunk`, `llm.Complexity`.
- Produces:
  - `func BuildRouter(providerNames []string) *llm.ModelRouter` — registers name-only stub providers so `Route` sees healthy providers; stub `Chat` is never called during measurement.
  - `func taskToLLM(wt WorkloadTask) *llm.Task`
  - `func requestFor(model string, wt WorkloadTask) *llm.ChatRequest`
  - `func Measure(w *Workload, fx *Fixture, premiumModel string, router *llm.ModelRouter) (*Report, error)`

**Design notes for the implementer:**
- The engine never calls a real provider. It calls `router.Route(ctx, task)` to get the *decision* (which model the optimized strategy picks), then looks up the real recorded cost in the fixture. Cache behavior uses the real `llm.InMemoryCache` keyed by the real `llm.CacheKey`.
- Baseline pass: for every entry in `w.Sequence`, add `fixture[(taskID, premiumModel)].Cost`. No cache.
- Optimized pass: for every entry in `w.Sequence`, build the request for the routed model, compute its cache key. Cache hit → cost 0, `CacheHits++`. Miss → add `fixture[(taskID, routedModel)].Cost`, then `cache.Set(key, recordedResponse)`.
- Split formulas (asserted in the test):
  - `RoutingPortion = Σ over first (uncached) occurrences [premiumCost − routedCost]`
  - `CachePortion   = Σ over repeat (cached) occurrences premiumCost`
  - Identity: `RoutingPortion + CachePortion == BaselineCost − OptimizedCost`.
- A missing fixture entry for any needed `(taskID, model)` is a named error, never a silent skip.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestMeasureSplitIdentityAndCache(t *testing.T) {
	// Task s1: simple -> router picks gpt-4o-mini (first in simple tier).
	// Premium is claude-opus-4. Sequence runs s1 twice (2nd = cache hit) and
	// c1 once (complex -> routes to premium, routing portion 0).
	w := &Workload{
		Sequence: []string{"s1", "s1", "c1"},
		Tasks: []WorkloadTask{
			{ID: "s1", Prompt: "rename x to y", Type: "rename"},
			{ID: "c1", Prompt: "design auth", Type: "architecture", RequiresReasoning: true, Tags: []string{"security"}},
		},
	}
	fx := &Fixture{
		SchemaVersion: FixtureSchemaVersion,
		PremiumModel:  "claude-opus-4",
		Entries: []FixtureEntry{
			{TaskID: "s1", Model: "gpt-4o-mini", Provider: "openai", Cost: 0.001},
			{TaskID: "s1", Model: "claude-opus-4", Provider: "anthropic", Cost: 0.050},
			{TaskID: "c1", Model: "claude-opus-4", Provider: "anthropic", Cost: 0.080},
		},
	}
	router := BuildRouter([]string{"openai", "anthropic"})
	rep, err := Measure(w, fx, "claude-opus-4", router)
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	// Baseline: premium for all 3 occurrences: 0.05 + 0.05 + 0.08 = 0.18
	if !approx(rep.BaselineCost, 0.18) {
		t.Fatalf("baseline = %v want 0.18", rep.BaselineCost)
	}
	// Optimized: s1 routed (0.001) + s1 cache hit (0) + c1 routed==premium (0.08) = 0.081
	if !approx(rep.OptimizedCost, 0.081) {
		t.Fatalf("optimized = %v want 0.081", rep.OptimizedCost)
	}
	if rep.CacheHits != 1 {
		t.Fatalf("cache hits = %d want 1", rep.CacheHits)
	}
	// Routing portion: first-occurrence deltas: (0.05-0.001) + (0.08-0.08) = 0.049
	if !approx(rep.RoutingPortion, 0.049) {
		t.Fatalf("routing portion = %v want 0.049", rep.RoutingPortion)
	}
	// Cache portion: repeat occurrences at premium: one s1 repeat = 0.05
	if !approx(rep.CachePortion, 0.050) {
		t.Fatalf("cache portion = %v want 0.05", rep.CachePortion)
	}
	// Identity: portions sum to total savings.
	if !approx(rep.RoutingPortion+rep.CachePortion, rep.BaselineCost-rep.OptimizedCost) {
		t.Fatalf("split identity broken")
	}
}

func TestMeasureErrorsOnMissingEntry(t *testing.T) {
	w := &Workload{Sequence: []string{"s1"}, Tasks: []WorkloadTask{{ID: "s1", Prompt: "x", Type: "rename"}}}
	fx := &Fixture{SchemaVersion: FixtureSchemaVersion, PremiumModel: "claude-opus-4"} // no entries
	if _, err := Measure(w, fx, "claude-opus-4", BuildRouter([]string{"openai", "anthropic"})); err == nil {
		t.Fatal("expected missing-entry error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/d/Antigravity 2/Precode" && go test ./cmd/bench/ -run TestMeasure -v 2>&1 | tail -20`
Expected: build failure — `undefined: BuildRouter`, `undefined: Measure`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/bench/engine.go`:

```go
package main

import (
	"context"
	"fmt"

	"github.com/vigilagent/vigilagent/internal/llm"
)

// stubProvider satisfies llm.Provider so the router marks it healthy and Route
// returns candidates. Its Chat is never invoked during measurement — the engine
// reads recorded costs from the fixture, not from providers.
type stubProvider struct{ name string }

func (s stubProvider) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, fmt.Errorf("stub provider %s: Chat must not be called during replay", s.name)
}
func (s stubProvider) Stream(ctx context.Context, req *llm.ChatRequest) (<-chan *llm.ChatChunk, error) {
	return nil, fmt.Errorf("stub provider %s: Stream unsupported", s.name)
}
func (s stubProvider) HealthCheck(ctx context.Context) error { return nil }
func (s stubProvider) Name() string                          { return s.name }

// BuildRouter returns a router with the given providers registered healthy,
// so Route makes real selections without any live provider call.
func BuildRouter(providerNames []string) *llm.ModelRouter {
	r := llm.NewModelRouter(&llm.RouterConfig{
		DefaultModel:        "claude-opus-4",
		BudgetPerTask:       0,
		DefaultOutputTokens: 500,
	})
	for _, name := range providerNames {
		r.RegisterProvider(name, stubProvider{name: name})
	}
	return r
}

// taskToLLM maps a workload task to the router's Task; the router's real
// classifier derives complexity from these attributes.
func taskToLLM(wt WorkloadTask) *llm.Task {
	files := make([]string, wt.FilesChanged)
	return &llm.Task{
		ID:                wt.ID,
		Type:              wt.Type,
		Description:       wt.Prompt,
		FilesChanged:      files,
		RequiresReasoning: wt.RequiresReasoning,
		IsNovel:           wt.IsNovel,
		Tags:              wt.Tags,
		Messages:          []llm.Message{{Role: "user", Content: wt.Prompt}},
	}
}

// requestFor builds the ChatRequest for a model+task the same way the router's
// execution path would, so cache keys collide on identical repeated requests.
func requestFor(model string, wt WorkloadTask) *llm.ChatRequest {
	maxTok := 4096
	if info, ok := llm.PriceTable[model]; ok && info.MaxTokens > 0 {
		maxTok = info.MaxTokens
	}
	return &llm.ChatRequest{
		Model:     model,
		Messages:  []llm.Message{{Role: "user", Content: wt.Prompt}},
		System:    wt.System,
		MaxTokens: maxTok,
	}
}

// Measure runs the baseline and optimized passes over the workload sequence,
// tallying real recorded costs, and returns the split report.
func Measure(w *Workload, fx *Fixture, premiumModel string, router *llm.ModelRouter) (*Report, error) {
	ctx := context.Background()
	cache := llm.NewInMemoryCache(0) // ttl 0 handled below; use large ttl instead
	_ = cache
	realCache := llm.NewInMemoryCache(1 << 62) // effectively non-expiring for a run
	rep := &Report{}
	seen := map[string]bool{} // cache-key -> already paid (uncached) once

	for _, id := range w.Sequence {
		wt, ok := w.TaskByID(id)
		if !ok {
			return nil, fmt.Errorf("sequence references unknown task %q", id)
		}
		rep.Tasks++

		// Baseline: premium model, every occurrence pays.
		pe, ok := fx.Lookup(id, premiumModel)
		if !ok {
			return nil, fmt.Errorf("fixture missing entry for (%s, premium %s)", id, premiumModel)
		}
		rep.BaselineCost += pe.Cost

		// Optimized: route, then cache-check.
		decision, err := router.Route(ctx, taskToLLM(wt))
		if err != nil {
			return nil, fmt.Errorf("route %s: %w", id, err)
		}
		routed := decision.Model
		req := requestFor(routed, wt)
		key := llm.CacheKey(req)

		if _, hit := realCache.Get(key); hit {
			rep.CacheHits++
			rep.CachePortion += pe.Cost // repeat valued at what baseline would pay
			continue
		}

		re, ok := fx.Lookup(id, routed)
		if !ok {
			return nil, fmt.Errorf("fixture missing entry for (%s, routed %s)", id, routed)
		}
		rep.OptimizedCost += re.Cost
		rep.RoutingPortion += pe.Cost - re.Cost
		realCache.Set(key, &llm.ChatResponse{Content: re.Content, Cost: re.Cost, Model: routed})
		seen[key] = true
	}

	rep.TotalSaved = rep.BaselineCost - rep.OptimizedCost
	if rep.BaselineCost > 0 {
		rep.PercentSaved = rep.TotalSaved / rep.BaselineCost * 100
	}
	return rep, nil
}
```

Note: delete the two dead `cache` lines while implementing — they are shown only to warn against `NewInMemoryCache(0)`; use the non-expiring cache. Final code keeps only `realCache`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/d/Antigravity 2/Precode" && go test ./cmd/bench/ -run TestMeasure -v 2>&1 | tail -20`
Expected: `PASS` for both tests. If `s1` does not route to `gpt-4o-mini`, print `decision.Model` and confirm the simple tier's first healthy model — adjust the fixture model name in the test to the actual routed model (the routing logic, not the test, is authoritative).

- [ ] **Step 5: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add cmd/bench/engine.go cmd/bench/engine_test.go
git commit -m "feat(bench): measurement engine with routing/cache split"
```

---

## Task 3: Report formatting + threshold verdict

**Files:**
- Create: `cmd/bench/report.go`
- Test: `cmd/bench/report_test.go`

**Interfaces:**
- Consumes: `Report` (Task 1).
- Produces:
  - `func (r *Report) Human() string` — multi-line human-readable summary.
  - `func (r *Report) JSON() (string, error)` — indented JSON of the report.
  - `func (r *Report) MeetsThreshold(minPct float64) bool` — `PercentSaved >= minPct`.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"strings"
	"testing"
)

func TestReportHumanAndThreshold(t *testing.T) {
	r := &Report{
		BaselineCost: 0.18, OptimizedCost: 0.081, TotalSaved: 0.099,
		RoutingPortion: 0.049, CachePortion: 0.05, PercentSaved: 55.0,
		Tasks: 3, CacheHits: 1,
	}
	h := r.Human()
	for _, want := range []string{"baseline", "optimized", "55.0", "routing", "cache"} {
		if !strings.Contains(strings.ToLower(h), want) {
			t.Fatalf("human output missing %q:\n%s", want, h)
		}
	}
	if !r.MeetsThreshold(50) {
		t.Fatal("55%% should meet 50%% threshold")
	}
	if r.MeetsThreshold(60) {
		t.Fatal("55%% should not meet 60%% threshold")
	}
	j, err := r.JSON()
	if err != nil || !strings.Contains(j, "percent_saved") {
		t.Fatalf("json bad: %v\n%s", err, j)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/d/Antigravity 2/Precode" && go test ./cmd/bench/ -run TestReport -v 2>&1 | tail -20`
Expected: build failure — `r.Human undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/bench/report.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (r *Report) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tasks run:        %d (cache hits: %d)\n", r.Tasks, r.CacheHits)
	fmt.Fprintf(&b, "Baseline cost:    $%.5f  (always %s, no cache)\n", r.BaselineCost, "premium")
	fmt.Fprintf(&b, "Optimized cost:   $%.5f  (router + cache)\n", r.OptimizedCost)
	fmt.Fprintf(&b, "Total saved:      $%.5f\n", r.TotalSaved)
	fmt.Fprintf(&b, "  routing portion:$%.5f\n", r.RoutingPortion)
	fmt.Fprintf(&b, "  cache portion:  $%.5f\n", r.CachePortion)
	fmt.Fprintf(&b, "Percent saved:    %.1f%%\n", r.PercentSaved)
	return b.String()
}

func (r *Report) JSON() (string, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *Report) MeetsThreshold(minPct float64) bool {
	return r.PercentSaved >= minPct
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/d/Antigravity 2/Precode" && go test ./cmd/bench/ -run TestReport -v 2>&1 | tail -20`
Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add cmd/bench/report.go cmd/bench/report_test.go
git commit -m "feat(bench): report formatting and threshold verdict"
```

---

## Task 4: Recorder (`--live` real-provider capture)

**Files:**
- Create: `cmd/bench/recorder.go`
- Test: `cmd/bench/recorder_test.go` (unit-tests the pure helper only; real calls are manual)

**Interfaces:**
- Consumes: `Workload`, `Fixture`, `FixtureEntry` (Task 1); `taskToLLM`, `BuildRouter` reused conceptually but recorder builds a *real* router; `llm.NewAnthropic`, `llm.NewOpenAI`, `llm.Provider`, `llm.ChatRequest`, `llm.PriceTable`, `llm.NewModelRouter`, `llm.RouterConfig`.
- Produces:
  - `type Keys struct { OpenAI, Anthropic string }`
  - `func providersFor(keys Keys) (map[string]llm.Provider, []string)` — builds real adapters for whichever keys are present; returns adapters by provider-name and the ordered healthy names.
  - `func Record(ctx context.Context, w *Workload, premiumModel string, keys Keys) (*Fixture, error)` — for each unique task, routes with a real router, then makes real `Chat` calls for both the premium model and the routed model, recording real cost/tokens. De-dupes `(taskID, model)` so premium==routed is recorded once.

**Design notes:**
- Provider for a model = `llm.PriceTable[model].Provider`; pick the matching real adapter. If that provider has no key, return a named error (fail fast) — never fabricate.
- Only unique tasks (`w.Tasks`) are recorded, once per needed model. The sequence/repeats are a replay-time concern (cache), not a recording concern.

- [ ] **Step 1: Write the failing test (pure helper)**

```go
package main

import "testing"

func TestProvidersForPresentKeysOnly(t *testing.T) {
	_, names := providersFor(Keys{OpenAI: "sk-x"}) // anthropic absent
	if len(names) != 1 || names[0] != "openai" {
		t.Fatalf("expected only openai, got %v", names)
	}
	_, names2 := providersFor(Keys{OpenAI: "sk-x", Anthropic: "sk-y"})
	if len(names2) != 2 {
		t.Fatalf("expected 2 providers, got %v", names2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "/d/Antigravity 2/Precode" && go test ./cmd/bench/ -run TestProvidersFor -v 2>&1 | tail -20`
Expected: build failure — `undefined: providersFor`, `undefined: Keys`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/bench/recorder.go`:

```go
package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/vigilagent/vigilagent/internal/llm"
)

type Keys struct {
	OpenAI    string
	Anthropic string
}

// providersFor builds real adapters for whichever keys are present.
func providersFor(keys Keys) (map[string]llm.Provider, []string) {
	provs := map[string]llm.Provider{}
	if keys.OpenAI != "" {
		provs["openai"] = llm.NewOpenAI(keys.OpenAI)
	}
	if keys.Anthropic != "" {
		provs["anthropic"] = llm.NewAnthropic(keys.Anthropic)
	}
	names := make([]string, 0, len(provs))
	for n := range provs {
		names = append(names, n)
	}
	sort.Strings(names)
	return provs, names
}

// Record calls real providers once per (task, needed model) and captures the
// real response, tokens, and cost into a Fixture.
func Record(ctx context.Context, w *Workload, premiumModel string, keys Keys) (*Fixture, error) {
	provs, names := providersFor(keys)
	if len(provs) == 0 {
		return nil, fmt.Errorf("no provider API keys supplied; set OPENAI_API_KEY and/or ANTHROPIC_API_KEY")
	}

	// Real router so routed-model selection matches production.
	router := llm.NewModelRouter(&llm.RouterConfig{DefaultModel: premiumModel, DefaultOutputTokens: 500})
	for n, p := range provs {
		router.RegisterProvider(n, p)
	}

	fx := &Fixture{SchemaVersion: FixtureSchemaVersion, PremiumModel: premiumModel}
	recorded := map[string]bool{} // taskID\x00model

	callAndRecord := func(taskID, model string, wt WorkloadTask) error {
		if recorded[taskID+"\x00"+model] {
			return nil
		}
		info, ok := llm.PriceTable[model]
		if !ok {
			return fmt.Errorf("model %q not in price table", model)
		}
		prov, ok := provs[info.Provider]
		if !ok {
			return fmt.Errorf("no API key for provider %q required by model %q", info.Provider, model)
		}
		resp, err := prov.Chat(ctx, requestFor(model, wt))
		if err != nil {
			return fmt.Errorf("live call (%s, %s): %w", taskID, model, err)
		}
		fx.Entries = append(fx.Entries, FixtureEntry{
			TaskID: taskID, Model: model, Provider: info.Provider,
			Content: resp.Content, InputTokens: resp.InputTokens,
			OutputTokens: resp.OutputTokens, Cost: resp.Cost,
		})
		recorded[taskID+"\x00"+model] = true
		return nil
	}

	for _, wt := range w.Tasks {
		decision, err := router.Route(ctx, taskToLLM(wt))
		if err != nil {
			return nil, fmt.Errorf("route %s: %w", wt.ID, err)
		}
		if err := callAndRecord(wt.ID, premiumModel, wt); err != nil {
			return nil, err
		}
		if err := callAndRecord(wt.ID, decision.Model, wt); err != nil {
			return nil, err
		}
	}
	_ = names
	return fx, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "/d/Antigravity 2/Precode" && go test ./cmd/bench/ -run TestProvidersFor -v 2>&1 | tail -20`
Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add cmd/bench/recorder.go cmd/bench/recorder_test.go
git commit -m "feat(bench): real-provider recorder for --live"
```

---

## Task 5: main.go wiring, flags, workload fixture, CI gate

**Files:**
- Create: `cmd/bench/main.go`
- Create: `cmd/bench/workload.json`
- Test: end-to-end smoke via a recorded temp fixture in `cmd/bench/main_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: a runnable binary. Flags: `--live`, `--fixture <path>` (default `cmd/bench/fixture.json`), `--workload <path>` (default `cmd/bench/workload.json`), `--premium-model <name>` (default `claude-opus-4`), `--min-savings <pct>` (default `0`), `--json`. Exit non-zero if replay result is below `--min-savings`.

- [ ] **Step 1: Create the real workload fixture**

Create `cmd/bench/workload.json` — real prompts, mixed tiers, deliberate repeats (`s1` and `m1` recur to exercise cache):

```json
{
  "sequence": ["s1", "s2", "m1", "s1", "c1", "m1", "s3"],
  "tasks": [
    { "id": "s1", "prompt": "Rename the variable `usr` to `user` across this function and return the updated code.", "type": "rename", "files_changed": 1 },
    { "id": "s2", "prompt": "Add a one-line docstring to this Go function describing what it returns.", "type": "documentation", "files_changed": 1 },
    { "id": "s3", "prompt": "Reformat this JSON to two-space indentation.", "type": "formatting", "files_changed": 1 },
    { "id": "m1", "prompt": "Refactor this handler to extract validation into a separate function.", "type": "refactoring", "files_changed": 3 },
    { "id": "c1", "prompt": "Design a secure multi-tenant authentication scheme with token rotation and per-org isolation.", "type": "architecture", "files_changed": 8, "requires_reasoning": true, "is_novel": true, "tags": ["security", "production"] }
  ]
}
```

- [ ] **Step 2: Write the failing end-to-end test**

```go
package main

import (
	"path/filepath"
	"testing"
)

// TestRunReplayEndToEnd builds a fixture in memory, saves it, and runs the
// replay path through run(), asserting a computed report and threshold gate.
func TestRunReplayEndToEnd(t *testing.T) {
	dir := t.TempDir()
	wlPath := filepath.Join(dir, "workload.json")
	fxPath := filepath.Join(dir, "fixture.json")

	w := &Workload{
		Sequence: []string{"s1", "s1", "c1"},
		Tasks: []WorkloadTask{
			{ID: "s1", Prompt: "rename x", Type: "rename", FilesChanged: 1},
			{ID: "c1", Prompt: "design auth", Type: "architecture", RequiresReasoning: true, Tags: []string{"security"}},
		},
	}
	if err := saveJSON(wlPath, w); err != nil {
		t.Fatal(err)
	}
	// Determine routed model for s1 via the same router the engine uses.
	routed, err := BuildRouter([]string{"openai", "anthropic"}).Route(ctxBackground(), taskToLLM(w.Tasks[0]))
	if err != nil {
		t.Fatal(err)
	}
	fx := &Fixture{
		SchemaVersion: FixtureSchemaVersion, PremiumModel: "claude-opus-4",
		Entries: []FixtureEntry{
			{TaskID: "s1", Model: routed.Model, Cost: 0.001},
			{TaskID: "s1", Model: "claude-opus-4", Cost: 0.05},
			{TaskID: "c1", Model: "claude-opus-4", Cost: 0.08},
		},
	}
	if err := fx.Save(fxPath); err != nil {
		t.Fatal(err)
	}

	code, err := run(options{
		live: false, workload: wlPath, fixture: fxPath,
		premiumModel: "claude-opus-4", minSavings: 50, asJSON: false,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected pass (savings ~55%%) got exit %d", code)
	}

	// Same run with an impossible threshold must fail the gate.
	code2, _ := run(options{live: false, workload: wlPath, fixture: fxPath, premiumModel: "claude-opus-4", minSavings: 99})
	if code2 == 0 {
		t.Fatal("expected non-zero exit below threshold")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd "/d/Antigravity 2/Precode" && go test ./cmd/bench/ -run TestRunReplay -v 2>&1 | tail -20`
Expected: build failure — `undefined: run`, `options`, `saveJSON`, `ctxBackground`.

- [ ] **Step 4: Write minimal implementation**

Create `cmd/bench/main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/vigilagent/vigilagent/internal/llm"
)

type options struct {
	live         bool
	workload     string
	fixture      string
	premiumModel string
	minSavings   float64
	asJSON       bool
}

func ctxBackground() context.Context { return context.Background() }

func saveJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// run executes one lane and returns a process exit code.
func run(opt options) (int, error) {
	w, err := LoadWorkload(opt.workload)
	if err != nil {
		return 1, err
	}

	var fx *Fixture
	if opt.live {
		keys := Keys{OpenAI: os.Getenv("OPENAI_API_KEY"), Anthropic: os.Getenv("ANTHROPIC_API_KEY")}
		fx, err = Record(context.Background(), w, opt.premiumModel, keys)
		if err != nil {
			return 1, err
		}
		if err := fx.Save(opt.fixture); err != nil {
			return 1, err
		}
		fmt.Fprintf(os.Stderr, "recorded %d entries to %s\n", len(fx.Entries), opt.fixture)
	} else {
		fx, err = LoadFixture(opt.fixture)
		if err != nil {
			return 1, err
		}
	}

	providerNames := providerNamesFromFixture(fx)
	rep, err := Measure(w, fx, opt.premiumModel, BuildRouter(providerNames))
	if err != nil {
		return 1, err
	}

	if opt.asJSON {
		s, err := rep.JSON()
		if err != nil {
			return 1, err
		}
		fmt.Println(s)
	} else {
		fmt.Print(rep.Human())
	}

	if !rep.MeetsThreshold(opt.minSavings) {
		fmt.Fprintf(os.Stderr, "FAIL: %.1f%% saved < %.1f%% threshold\n", rep.PercentSaved, opt.minSavings)
		return 1, nil
	}
	return 0, nil
}

// providerNamesFromFixture returns the distinct provider names the fixture
// touched, so the replay router registers exactly those as healthy. Falls back
// to the price table when a provider field is blank.
func providerNamesFromFixture(fx *Fixture) []string {
	set := map[string]bool{}
	for _, e := range fx.Entries {
		p := e.Provider
		if p == "" {
			if info, ok := llm.PriceTable[e.Model]; ok {
				p = info.Provider
			}
		}
		if p != "" {
			set[p] = true
		}
	}
	if len(set) == 0 { // safety net: register the common two
		return []string{"anthropic", "openai"}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	return names
}

func main() {
	var opt options
	flag.BoolVar(&opt.live, "live", false, "call real providers and record a fixture")
	flag.StringVar(&opt.workload, "workload", "cmd/bench/workload.json", "workload JSON path")
	flag.StringVar(&opt.fixture, "fixture", "cmd/bench/fixture.json", "fixture JSON path")
	flag.StringVar(&opt.premiumModel, "premium-model", "claude-opus-4", "baseline premium model")
	flag.Float64Var(&opt.minSavings, "min-savings", 0, "CI gate: minimum percent saved")
	flag.BoolVar(&opt.asJSON, "json", false, "emit JSON report")
	flag.Parse()

	code, err := run(opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	os.Exit(code)
}
```

- [ ] **Step 5: Run the end-to-end test**

Run: `cd "/d/Antigravity 2/Precode" && go test ./cmd/bench/ -run TestRunReplay -v 2>&1 | tail -20`
Expected: `PASS`.

- [ ] **Step 6: Run the full bench package test suite + vet**

Run: `cd "/d/Antigravity 2/Precode" && go test ./cmd/bench/ 2>&1 | tail -5 && go vet ./cmd/bench/ 2>&1 | tail -5`
Expected: `ok  .../cmd/bench`, no vet output.

- [ ] **Step 7: Record a real fixture, then prove the number (manual, needs keys + budget)**

> This step spends real API money and needs `OPENAI_API_KEY` + `ANTHROPIC_API_KEY`. You are currently rate-limited ($10/5h) — run it once the cap resets. If keys are absent, skip and leave `fixture.json` uncommitted; the replay lane + CI gate still work in tests.

Run:
```bash
cd "/d/Antigravity 2/Precode"
go run ./cmd/bench --live --workload cmd/bench/workload.json --fixture cmd/bench/fixture.json
go run ./cmd/bench --fixture cmd/bench/fixture.json --min-savings 20
```
Expected: recorder prints `recorded N entries`; replay prints the report with a positive `Percent saved` and exits 0.

- [ ] **Step 8: Commit**

```bash
cd "/d/Antigravity 2/Precode"
git add cmd/bench/main.go cmd/bench/workload.json cmd/bench/main_test.go
# Commit fixture.json only if you recorded a real one in Step 7:
# git add cmd/bench/fixture.json
git commit -m "feat(bench): main wiring, flags, workload, CI gate"
```

---

## Self-Review

**Spec coverage:**
- Two lanes / one engine → Tasks 2 (engine), 4 (recorder), 5 (main lane switch). ✓
- Both numbers real, baseline recorded not estimated → recorder records premium + routed per task (Task 4); engine tallies premium from fixture (Task 2). ✓
- Reuse shipped router + cache, swap only the provider call → engine uses `llm.Route` + `llm.InMemoryCache` + `llm.CacheKey`; stub providers only satisfy health (Task 2). ✓
- Routing/cache split with exact formulas + identity test → `TestMeasureSplitIdentityAndCache` (Task 2). ✓
- Cache portion = real $0 on repeat → engine cache-hit branch adds 0 to OptimizedCost (Task 2). ✓
- Flags `--live/--fixture/--min-savings/--json` (+ `--workload/--premium-model`) → Task 5. ✓
- Error handling: missing fixture (Task 1 `LoadFixture`), schema mismatch (Task 1), missing key on live (Task 4), missing entry not silent (Task 2). ✓
- CI gate deterministic $0 → replay lane + threshold exit (Task 5). ✓
- Prereq build fix → Task 0. ✓
- Out of scope (quality/semantic cache/latency) → not built. ✓

**Placeholder scan:** No TBD/TODO; every code step shows full code. The one dead-line warning in Task 2 Step 3 is called out explicitly to remove. ✓

**Type consistency:** `Fixture`/`FixtureEntry`/`Workload`/`WorkloadTask`/`Report` fields identical across Tasks 1-5. `requestFor`, `taskToLLM`, `BuildRouter`, `Measure`, `Record`, `providersFor`, `run`, `options`, `saveJSON`, `ctxBackground` each defined once and consumed with matching signatures. Cache-key collision relies on `requestFor` producing identical requests for identical tasks — consistent in engine and recorder. ✓

**Known runtime dependency to verify during Task 2:** the simple-tier routed model for `s1`. The test notes that routing logic is authoritative — if `Route` picks a different model than `gpt-4o-mini`, update the fixture model name in the test to match, not the other way around.
