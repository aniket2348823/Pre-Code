package main

import (
	"path/filepath"
	"testing"
)

// TestRunReplayEndToEnd builds a fixture in memory, saves it, and runs the
// replay path through run(), asserting the report and threshold gate.
func TestRunReplayEndToEnd(t *testing.T) {
	dir := t.TempDir()
	wlPath := filepath.Join(dir, "workload.json")
	fxPath := filepath.Join(dir, "fixture.json")

	w := &Workload{
		Sequence: []string{"s1", "s1", "c1"},
		Tasks: []WorkloadTask{
			{ID: "s1", Prompt: "rename x", Type: "rename", FilesChanged: 1},
			{ID: "c1", Prompt: "design auth", Type: "architecture", FilesChanged: 8, RequiresReasoning: true, Tags: []string{"security"}},
		},
	}
	if err := saveJSON(wlPath, w); err != nil {
		t.Fatal(err)
	}
	// Determine routed models via the same router the engine uses.
	router := BuildRouter([]string{"openai", "anthropic"})
	ds1, err := router.Route(ctxBackground(), taskToLLM(w.Tasks[0]))
	if err != nil {
		t.Fatal(err)
	}
	dc1, err := router.Route(ctxBackground(), taskToLLM(w.Tasks[1]))
	if err != nil {
		t.Fatal(err)
	}
	fx := &Fixture{
		SchemaVersion: FixtureSchemaVersion, PremiumModel: "claude-opus-4",
		Entries: []FixtureEntry{
			{TaskID: "s1", Model: "claude-opus-4", Cost: 0.05},
			{TaskID: "s1", Model: ds1.Model, Cost: 0.001},
			{TaskID: "c1", Model: "claude-opus-4", Cost: 0.08},
			{TaskID: "c1", Model: dc1.Model, Cost: 0.03},
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
		t.Fatalf("expected pass (savings well above 50%%) got exit %d", code)
	}

	// Same run with an impossible threshold must fail the gate.
	code2, _ := run(options{live: false, workload: wlPath, fixture: fxPath, premiumModel: "claude-opus-4", minSavings: 99})
	if code2 == 0 {
		t.Fatal("expected non-zero exit below threshold")
	}
}
