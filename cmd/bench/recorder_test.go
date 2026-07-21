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
