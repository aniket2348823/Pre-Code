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
