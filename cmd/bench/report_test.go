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
		t.Fatal("55% should meet 50% threshold")
	}
	if r.MeetsThreshold(60) {
		t.Fatal("55% should not meet 60% threshold")
	}
	j, err := r.JSON()
	if err != nil || !strings.Contains(j, "percent_saved") {
		t.Fatalf("json bad: %v\n%s", err, j)
	}
}
