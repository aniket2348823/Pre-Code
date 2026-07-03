package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (r *Report) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tasks run:        %d (cache hits: %d)\n", r.Tasks, r.CacheHits)
	fmt.Fprintf(&b, "Baseline cost:    $%.5f  (always premium, no cache)\n", r.BaselineCost)
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
