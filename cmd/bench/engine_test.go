package main

import (
	"context"
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestMeasureSplitIdentityAndCache(t *testing.T) {
	w := &Workload{
		// s1 runs twice (2nd = cache hit); c1 once.
		Sequence: []string{"s1", "s1", "c1"},
		Tasks: []WorkloadTask{
			{ID: "s1", Prompt: "rename x to y", Type: "rename", FilesChanged: 1},
			{ID: "c1", Prompt: "design auth", Type: "architecture", FilesChanged: 8, RequiresReasoning: true, IsNovel: true, Tags: []string{"security"}},
		},
	}
	const premium = "claude-opus-4"
	router := BuildRouter([]string{"openai", "anthropic"})
	ctx := context.Background()

	ds1, err := router.Route(ctx, taskToLLM(w.Tasks[0]))
	if err != nil {
		t.Fatalf("route s1: %v", err)
	}
	dc1, err := router.Route(ctx, taskToLLM(w.Tasks[1]))
	if err != nil {
		t.Fatalf("route c1: %v", err)
	}
	// The arithmetic below assumes routed != premium for both tasks. If routing
	// ever changes to pick premium, these guards fail loudly — update the test,
	// not the router (routing logic is authoritative).
	if ds1.Model == premium {
		t.Fatalf("expected s1 to route below premium, got %s", ds1.Model)
	}
	if dc1.Model == premium {
		t.Fatalf("expected c1 to route to a non-premium model, got %s", dc1.Model)
	}

	const premS1, routS1, premC1, routC1 = 0.05, 0.001, 0.08, 0.03
	fx := &Fixture{
		SchemaVersion: FixtureSchemaVersion, PremiumModel: premium,
		Entries: []FixtureEntry{
			{TaskID: "s1", Model: premium, Cost: premS1},
			{TaskID: "s1", Model: ds1.Model, Cost: routS1},
			{TaskID: "c1", Model: premium, Cost: premC1},
			{TaskID: "c1", Model: dc1.Model, Cost: routC1},
		},
	}

	rep, err := Measure(w, fx, premium, router)
	if err != nil {
		t.Fatalf("measure: %v", err)
	}

	wantBaseline := premS1 + premS1 + premC1 // 0.18
	wantOptimized := routS1 + routC1          // 0.031 (2nd s1 is a $0 cache hit)
	wantRouting := (premS1 - routS1) + (premC1 - routC1)
	wantCache := premS1 // one s1 repeat, valued at premium

	if !approx(rep.BaselineCost, wantBaseline) {
		t.Fatalf("baseline = %v want %v", rep.BaselineCost, wantBaseline)
	}
	if !approx(rep.OptimizedCost, wantOptimized) {
		t.Fatalf("optimized = %v want %v", rep.OptimizedCost, wantOptimized)
	}
	if rep.CacheHits != 1 {
		t.Fatalf("cache hits = %d want 1", rep.CacheHits)
	}
	if !approx(rep.RoutingPortion, wantRouting) {
		t.Fatalf("routing portion = %v want %v", rep.RoutingPortion, wantRouting)
	}
	if !approx(rep.CachePortion, wantCache) {
		t.Fatalf("cache portion = %v want %v", rep.CachePortion, wantCache)
	}
	// The identity that makes the split honest: portions sum to total savings.
	if !approx(rep.RoutingPortion+rep.CachePortion, rep.BaselineCost-rep.OptimizedCost) {
		t.Fatalf("split identity broken: %v + %v != %v", rep.RoutingPortion, rep.CachePortion, rep.BaselineCost-rep.OptimizedCost)
	}
}

func TestMeasureErrorsOnMissingEntry(t *testing.T) {
	w := &Workload{Sequence: []string{"s1"}, Tasks: []WorkloadTask{{ID: "s1", Prompt: "x", Type: "rename", FilesChanged: 1}}}
	fx := &Fixture{SchemaVersion: FixtureSchemaVersion, PremiumModel: "claude-opus-4"} // no entries
	if _, err := Measure(w, fx, "claude-opus-4", BuildRouter([]string{"openai", "anthropic"})); err == nil {
		t.Fatal("expected missing-entry error")
	}
}
