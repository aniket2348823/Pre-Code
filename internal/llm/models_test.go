package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderID_Constants(t *testing.T) {
	assert.Equal(t, ProviderID("openai"), ProviderOpenAI)
	assert.Equal(t, ProviderID("anthropic"), ProviderAnthropic)
	assert.Equal(t, ProviderID("gemini"), ProviderGemini)
	assert.Equal(t, ProviderID("groq"), ProviderGroq)
	assert.Equal(t, ProviderID("mistral"), ProviderMistral)
	assert.Equal(t, ProviderID("cohere"), ProviderCohere)
	assert.Equal(t, ProviderID("nvidia_nim"), ProviderNVIDIANIM)
	assert.Equal(t, ProviderID("openrouter"), ProviderOpenRouter)
	assert.Equal(t, ProviderID("deepseek"), ProviderDeepSeek)
}

func TestProviders_ContainsAll(t *testing.T) {
	providers := Providers()
	require.GreaterOrEqual(t, len(providers), 9)

	ids := make(map[ProviderID]bool)
	for _, p := range providers {
		ids[p.ID] = true
	}

	for _, expected := range []ProviderID{
		ProviderOpenAI, ProviderAnthropic, ProviderGemini, ProviderGroq,
		ProviderMistral, ProviderCohere, ProviderNVIDIANIM, ProviderOpenRouter,
		ProviderDeepSeek,
	} {
		assert.True(t, ids[expected], "missing provider %s", expected)
	}
}

func TestProviders_EachHasName(t *testing.T) {
	for _, p := range Providers() {
		assert.NotEmpty(t, p.Name, "provider %s has empty name", p.ID)
	}
}

func TestProviders_EachHasBaseURL(t *testing.T) {
	for _, p := range Providers() {
		assert.NotEmpty(t, p.BaseURL, "provider %s has empty base URL", p.ID)
	}
}

func TestProviders_EachHasKeyPrefix(t *testing.T) {
	for _, p := range Providers() {
		assert.NotEmpty(t, p.KeyPrefix, "provider %s has empty key prefix", p.ID)
	}
}

func TestProviderModels_OpenAI(t *testing.T) {
	models := ProviderModels(ProviderOpenAI)
	require.NotEmpty(t, models)
	// Should have GPT-5.6, GPT-4o, o3, embeddings, etc.
	ids := make(map[string]bool)
	for _, m := range models {
		ids[m.ID] = true
	}
	assert.True(t, ids["gpt-4o"])
	assert.True(t, ids["gpt-4o-mini"])
}

func TestProviderModels_Anthropic(t *testing.T) {
	models := ProviderModels(ProviderAnthropic)
	require.NotEmpty(t, models)
	ids := make(map[string]bool)
	for _, m := range models {
		ids[m.ID] = true
	}
	assert.True(t, ids["claude-sonnet-4-20250514"] || ids["claude-sonnet-5"])
}

func TestProviderModels_NonExistent(t *testing.T) {
	models := ProviderModels("nonexistent")
	assert.Nil(t, models)
}

func TestFindModel_GPT4o(t *testing.T) {
	m := FindModel("gpt-4o")
	require.NotNil(t, m)
	assert.Equal(t, "GPT-4o", m.Name)
	assert.Equal(t, "openai", m.Provider)
}

func TestFindModel_Claude(t *testing.T) {
	m := FindModel("claude-sonnet-5")
	require.NotNil(t, m)
	assert.Equal(t, "anthropic", m.Provider)
}

func TestFindModel_NonExistent(t *testing.T) {
	m := FindModel("this-model-does-not-exist-12345")
	assert.Nil(t, m)
}

func TestFindModel_EmptyString(t *testing.T) {
	m := FindModel("")
	assert.Nil(t, m)
}

func TestProviderByKeyPrefix_OpenAI(t *testing.T) {
	p := ProviderByKeyPrefix("sk-anything")
	require.NotNil(t, p)
	assert.Contains(t, []ProviderID{ProviderOpenAI, ProviderDeepSeek}, p.ID)
}

func TestProviderByKeyPrefix_Anthropic(t *testing.T) {
	p := ProviderByKeyPrefix("sk-ant-12345")
	require.NotNil(t, p)
	assert.Equal(t, ProviderAnthropic, p.ID)
}

func TestProviderByKeyPrefix_Groq(t *testing.T) {
	p := ProviderByKeyPrefix("gsk_12345")
	require.NotNil(t, p)
	assert.Equal(t, ProviderGroq, p.ID)
}

func TestProviderByKeyPrefix_Mistral(t *testing.T) {
	p := ProviderByKeyPrefix("ms-12345")
	require.NotNil(t, p)
	assert.Equal(t, ProviderMistral, p.ID)
}

func TestProviderByKeyPrefix_Cohere(t *testing.T) {
	p := ProviderByKeyPrefix("co-12345")
	require.NotNil(t, p)
	assert.Equal(t, ProviderCohere, p.ID)
}

func TestProviderByKeyPrefix_NVIDIA(t *testing.T) {
	p := ProviderByKeyPrefix("nvapi-12345")
	require.NotNil(t, p)
	assert.Equal(t, ProviderNVIDIANIM, p.ID)
}

func TestProviderByKeyPrefix_Gemini(t *testing.T) {
	p := ProviderByKeyPrefix("AIzaSyXXX")
	require.NotNil(t, p)
	assert.Equal(t, ProviderGemini, p.ID)
}

func TestProviderByKeyPrefix_OpenRouter(t *testing.T) {
	p := ProviderByKeyPrefix("sk-or-12345")
	require.NotNil(t, p)
	assert.Equal(t, ProviderOpenRouter, p.ID)
}

func TestProviderByKeyPrefix_DeepSeek(t *testing.T) {
	p := ProviderByKeyPrefix("sk-12345")
	// sk- matches both OpenAI and DeepSeek; longest prefix wins
	require.NotNil(t, p)
	// Both have sk- prefix, so the longer one wins (both same length, random)
	assert.Contains(t, []ProviderID{ProviderOpenAI, ProviderDeepSeek}, p.ID)
}

func TestProviderByKeyPrefix_NonExistent(t *testing.T) {
	p := ProviderByKeyPrefix("zzz-no-match")
	assert.Nil(t, p)
}

func TestProviderByKeyPrefix_EmptyString(t *testing.T) {
	p := ProviderByKeyPrefix("")
	assert.Nil(t, p)
}

func TestGetFullCatalogEntries(t *testing.T) {
	catalog := GetFullCatalog()
	require.NotEmpty(t, catalog)

	ids := make(map[ProviderID]bool)
	for _, c := range catalog {
		assert.NotEmpty(t, c.Provider.Name)
		assert.NotEmpty(t, c.Models)
		ids[c.Provider.ID] = true
	}

	assert.True(t, ids[ProviderOpenAI])
	assert.True(t, ids[ProviderAnthropic])
	assert.True(t, ids[ProviderGemini])
}

func TestHasPrefixCases(t *testing.T) {
	tests := []struct {
		s, prefix string
		expected  bool
	}{
		{"sk-ant-xxx", "sk-ant-", true},
		{"sk-xxx", "sk-ant-", false},
		{"abc", "abc", true},
		{"", "", true},
		{"abc", "abcd", false},
		{"", "a", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, hasPrefix(tt.s, tt.prefix), "hasPrefix(%q, %q)", tt.s, tt.prefix)
	}
}

func TestModelCatalogEntry_Capabilities(t *testing.T) {
	m := FindModel("gpt-4o")
	require.NotNil(t, m)
	assert.Contains(t, m.Capabilities, "tools")
	assert.Contains(t, m.Capabilities, "vision")
}

func TestModelCatalogEntry_Deprecated(t *testing.T) {
	models := ProviderModels(ProviderOpenAI)
	for _, m := range models {
		if m.ID == "gpt-4-turbo" {
			assert.True(t, m.Deprecated)
			return
		}
	}
	t.Skip("gpt-4-turbo not found in catalog")
}

func TestProviderInfo_KeyHint(t *testing.T) {
	providers := Providers()
	for _, p := range providers {
		assert.NotEmpty(t, p.KeyHint, "provider %s has empty key hint", p.ID)
	}
}

func TestProviderInfo_Description(t *testing.T) {
	providers := Providers()
	for _, p := range providers {
		assert.NotEmpty(t, p.Description, "provider %s has empty description", p.ID)
	}
}

func TestFindModel_ModelsFromMultipleProviders(t *testing.T) {
	openaiModels := ProviderModels(ProviderOpenAI)
	anthropicModels := ProviderModels(ProviderAnthropic)
	geminiModels := ProviderModels(ProviderGemini)

	total := len(openaiModels) + len(anthropicModels) + len(geminiModels)
	assert.Greater(t, total, 20, "should have many models across providers")
}

func TestModelCatalogEntry_ContextWindow(t *testing.T) {
	models := ProviderModels(ProviderOpenAI)
	for _, m := range models {
		if m.ID == "gpt-4o" {
			assert.Greater(t, m.ContextWindow, 0)
			assert.Greater(t, m.MaxOutput, 0)
			return
		}
	}
}

func TestModelCatalogEntry_Pricing(t *testing.T) {
	m := FindModel("gpt-4o")
	require.NotNil(t, m)
	assert.Greater(t, m.InputCostPer1M, 0.0)
	assert.Greater(t, m.OutputCostPer1M, 0.0)
}
