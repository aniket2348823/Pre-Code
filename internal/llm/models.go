package llm

import (
	"encoding/json"
	"io"
	"sync"
)

// Content from models.go
// ProviderID is the canonical identifier for each LLM provider.
type ProviderID string

const (
	ProviderOpenAI     ProviderID = "openai"
	ProviderAnthropic  ProviderID = "anthropic"
	ProviderGemini     ProviderID = "gemini"
	ProviderGroq       ProviderID = "groq"
	ProviderMistral    ProviderID = "mistral"
	ProviderCohere     ProviderID = "cohere"
	ProviderNVIDIANIM  ProviderID = "nvidia_nim"
	ProviderOpenRouter ProviderID = "openrouter"
	ProviderDeepSeek   ProviderID = "deepseek"
)

// ProviderInfo holds metadata about an LLM provider displayed in the UI.
type ProviderInfo struct {
	ID          ProviderID `json:"id"`
	Name        string     `json:"name"`
	BaseURL     string     `json:"base_url"`
	KeyPrefix   string     `json:"key_prefix"`
	Description string     `json:"description"`
	KeyHint     string     `json:"key_hint"`
}

// ModelCatalogEntry is a single model available through a provider.
type ModelCatalogEntry struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	ContextWindow   int      `json:"context_window"`
	MaxOutput       int      `json:"max_output"`
	InputCostPer1M  float64  `json:"input_cost_per_1m"`
	OutputCostPer1M float64  `json:"output_cost_per_1m"`
	Capabilities    []string `json:"capabilities"`
	Description     string   `json:"description"`
	Deprecated      bool     `json:"deprecated,omitempty"`
}

// ProviderCatalog bundles a provider's metadata with its available models.
type ProviderCatalog struct {
	Provider ProviderInfo        `json:"provider"`
	Models   []ModelCatalogEntry `json:"models"`
}

var (
	providerCatalogOnce sync.Once
	providerCatalog     map[ProviderID]*ProviderCatalog
)

func Providers() []ProviderInfo {
	ensureProviderCatalog()
	out := make([]ProviderInfo, 0, len(providerCatalog))
	for _, cat := range providerCatalog {
		out = append(out, cat.Provider)
	}
	return out
}

func ProviderModels(providerID ProviderID) []ModelCatalogEntry {
	ensureProviderCatalog()
	cat, ok := providerCatalog[providerID]
	if !ok {
		return nil
	}
	return cat.Models
}

func FindModel(modelID string) *ModelCatalogEntry {
	ensureProviderCatalog()
	for _, cat := range providerCatalog {
		for _, m := range cat.Models {
			if m.ID == modelID {
				return &m
			}
		}
	}
	return nil
}

func ProviderByKeyPrefix(key string) *ProviderInfo {
	ensureProviderCatalog()
	var best *ProviderInfo
	bestLen := 0
	for _, cat := range providerCatalog {
		if len(cat.Provider.KeyPrefix) > 0 && hasPrefix(key, cat.Provider.KeyPrefix) {
			if len(cat.Provider.KeyPrefix) > bestLen {
				bestLen = len(cat.Provider.KeyPrefix)
				info := cat.Provider
				best = &info
			}
		}
	}
	return best
}

func GetFullCatalog() []*ProviderCatalog {
	ensureProviderCatalog()
	out := make([]*ProviderCatalog, 0, len(providerCatalog))
	for _, cat := range providerCatalog {
		out = append(out, cat)
	}
	return out
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func ensureProviderCatalog() {
	providerCatalogOnce.Do(func() {
		providerCatalog = make(map[ProviderID]*ProviderCatalog, 9)

		// ═══════════════════════════════════════════════════════════════════
		// OPENAI (July 2026)
		// ═══════════════════════════════════════════════════════════════════
		providerCatalog[ProviderOpenAI] = &ProviderCatalog{
			Provider: ProviderInfo{
				ID:          ProviderOpenAI,
				Name:        "OpenAI",
				BaseURL:     "https://api.openai.com/v1",
				KeyPrefix:   "sk-",
				KeyHint:     "sk-...",
				Description: "GPT-5.6, GPT-5.5, o3, o4, GPT-4.1, GPT-4o series",
			},
			Models: []ModelCatalogEntry{
				// ── GPT-5.6 Family (Latest - July 2026) ──
				{ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", Provider: "openai", ContextWindow: 1000000, MaxOutput: 32768, InputCostPer1M: 5.00, OutputCostPer1M: 30.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Latest flagship, most capable"},
				{ID: "gpt-5.6-terra", Name: "GPT-5.6 Terra", Provider: "openai", ContextWindow: 1000000, MaxOutput: 32768, InputCostPer1M: 2.50, OutputCostPer1M: 15.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Balanced performance and cost"},
				{ID: "gpt-5.6-luna", Name: "GPT-5.6 Luna", Provider: "openai", ContextWindow: 1000000, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 6.00, Capabilities: []string{"tools", "vision"}, Description: "Cost-effective GPT-5.6"},
				// ── GPT-5.5 Family ──
				{ID: "gpt-5.5", Name: "GPT-5.5", Provider: "openai", ContextWindow: 400000, MaxOutput: 32768, InputCostPer1M: 5.00, OutputCostPer1M: 30.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Frontier reasoning model"},
				{ID: "gpt-5.5-pro", Name: "GPT-5.5 Pro", Provider: "openai", ContextWindow: 400000, MaxOutput: 65536, InputCostPer1M: 30.00, OutputCostPer1M: 180.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Premium reasoning with extended thinking"},
				// ── GPT-5.4 Family ──
				{ID: "gpt-5.4", Name: "GPT-5.4", Provider: "openai", ContextWindow: 400000, MaxOutput: 32768, InputCostPer1M: 2.50, OutputCostPer1M: 15.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Practical frontier baseline"},
				{ID: "gpt-5.4-pro", Name: "GPT-5.4 Pro", Provider: "openai", ContextWindow: 400000, MaxOutput: 65536, InputCostPer1M: 30.00, OutputCostPer1M: 180.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Premium fallback with Pro behavior"},
				{ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini", Provider: "openai", ContextWindow: 400000, MaxOutput: 16384, InputCostPer1M: 0.75, OutputCostPer1M: 4.50, Capabilities: []string{"tools", "vision"}, Description: "Production workhorse"},
				{ID: "gpt-5.4-nano", Name: "GPT-5.4 Nano", Provider: "openai", ContextWindow: 400000, MaxOutput: 8192, InputCostPer1M: 0.20, OutputCostPer1M: 1.25, Capabilities: []string{"tools"}, Description: "Ultra-cheap utility model"},
				// ── GPT-5 Family ──
				{ID: "gpt-5", Name: "GPT-5", Provider: "openai", ContextWindow: 256000, MaxOutput: 32768, InputCostPer1M: 10.00, OutputCostPer1M: 30.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Previous flagship"},
				{ID: "gpt-5-mini", Name: "GPT-5 Mini", Provider: "openai", ContextWindow: 128000, MaxOutput: 16384, InputCostPer1M: 0.25, OutputCostPer1M: 2.00, Capabilities: []string{"tools", "vision"}, Description: "Budget GPT-5"},
				{ID: "gpt-5-nano", Name: "GPT-5 Nano", Provider: "openai", ContextWindow: 400000, MaxOutput: 8192, InputCostPer1M: 0.05, OutputCostPer1M: 0.40, Capabilities: []string{"tools"}, Description: "Cheapest OpenAI model"},
				// ── GPT-4.1 Family (1M context) ──
				{ID: "gpt-4.1", Name: "GPT-4.1", Provider: "openai", ContextWindow: 1000000, MaxOutput: 32768, InputCostPer1M: 2.00, OutputCostPer1M: 8.00, Capabilities: []string{"tools", "vision"}, Description: "Best value flagship with 1M context"},
				{ID: "gpt-4.1-mini", Name: "GPT-4.1 Mini", Provider: "openai", ContextWindow: 1000000, MaxOutput: 16384, InputCostPer1M: 0.40, OutputCostPer1M: 1.60, Capabilities: []string{"tools", "vision"}, Description: "Cost-effective with 1M context"},
				{ID: "gpt-4.1-nano", Name: "GPT-4.1 Nano", Provider: "openai", ContextWindow: 1000000, MaxOutput: 8192, InputCostPer1M: 0.10, OutputCostPer1M: 0.40, Capabilities: []string{"tools"}, Description: "Cheapest 1M context model"},
				// ── GPT-4o Family ──
				{ID: "gpt-4o", Name: "GPT-4o", Provider: "openai", ContextWindow: 128000, MaxOutput: 16384, InputCostPer1M: 2.50, OutputCostPer1M: 10.00, Capabilities: []string{"tools", "vision"}, Description: "Fast multimodal flagship"},
				{ID: "gpt-4o-mini", Name: "GPT-4o Mini", Provider: "openai", ContextWindow: 128000, MaxOutput: 16384, InputCostPer1M: 0.15, OutputCostPer1M: 0.60, Capabilities: []string{"tools", "vision"}, Description: "Budget multimodal"},
				// ── o-Series (Reasoning) ──
				{ID: "o3", Name: "o3", Provider: "openai", ContextWindow: 200000, MaxOutput: 100000, InputCostPer1M: 2.00, OutputCostPer1M: 8.00, Capabilities: []string{"tools", "reasoning"}, Description: "Advanced reasoning, 80% price cut"},
				{ID: "o3-mini", Name: "o3-mini", Provider: "openai", ContextWindow: 200000, MaxOutput: 100000, InputCostPer1M: 1.10, OutputCostPer1M: 4.40, Capabilities: []string{"tools", "reasoning"}, Description: "Cost-effective reasoning"},
				{ID: "o3-pro", Name: "o3-pro", Provider: "openai", ContextWindow: 200000, MaxOutput: 100000, InputCostPer1M: 20.00, OutputCostPer1M: 80.00, Capabilities: []string{"tools", "reasoning"}, Description: "Premium reasoning with extended thinking"},
				{ID: "o4-mini", Name: "o4-mini", Provider: "openai", ContextWindow: 200000, MaxOutput: 100000, InputCostPer1M: 1.10, OutputCostPer1M: 4.40, Capabilities: []string{"tools", "reasoning"}, Description: "Latest cost-effective reasoning"},
				// ── Open-Weight Models ──
				{ID: "gpt-oss-20b", Name: "GPT-OSS 20B", Provider: "openai", ContextWindow: 131000, MaxOutput: 32768, InputCostPer1M: 0.03, OutputCostPer1M: 0.13, Capabilities: []string{"tools"}, Description: "Open-source small model"},
				{ID: "gpt-oss-120b", Name: "GPT-OSS 120B", Provider: "openai", ContextWindow: 131000, MaxOutput: 32768, InputCostPer1M: 0.04, OutputCostPer1M: 0.17, Capabilities: []string{"tools"}, Description: "Open-source large model"},
				// ── Legacy ──
				{ID: "gpt-4-turbo", Name: "GPT-4 Turbo", Provider: "openai", ContextWindow: 128000, MaxOutput: 4096, InputCostPer1M: 10.00, OutputCostPer1M: 30.00, Capabilities: []string{"tools", "vision"}, Description: "Legacy flagship", Deprecated: true},
				// ── Embeddings ──
				{ID: "text-embedding-3-small", Name: "Text Embedding 3 Small", Provider: "openai", ContextWindow: 8191, MaxOutput: 0, InputCostPer1M: 0.02, OutputCostPer1M: 0, Capabilities: []string{"embeddings"}, Description: "Fast, cost-effective embeddings"},
				{ID: "text-embedding-3-large", Name: "Text Embedding 3 Large", Provider: "openai", ContextWindow: 8191, MaxOutput: 0, InputCostPer1M: 0.13, OutputCostPer1M: 0, Capabilities: []string{"embeddings"}, Description: "Highest-quality embeddings"},
				// ── Image ──
				{ID: "dall-e-3", Name: "DALL-E 3", Provider: "openai", ContextWindow: 0, MaxOutput: 0, InputCostPer1M: 0.04, OutputCostPer1M: 0, Capabilities: []string{"image"}, Description: "Image generation"},
				{ID: "gpt-image-1", Name: "GPT Image 1", Provider: "openai", ContextWindow: 0, MaxOutput: 0, InputCostPer1M: 0.04, OutputCostPer1M: 0, Capabilities: []string{"image"}, Description: "Latest image generation"},
			},
		}

		// ═══════════════════════════════════════════════════════════════════
		// ANTHROPIC (July 2026)
		// ═══════════════════════════════════════════════════════════════════
		providerCatalog[ProviderAnthropic] = &ProviderCatalog{
			Provider: ProviderInfo{
				ID:          ProviderAnthropic,
				Name:        "Anthropic",
				BaseURL:     "https://api.anthropic.com",
				KeyPrefix:   "sk-ant-",
				KeyHint:     "sk-ant-...",
				Description: "Claude Fable 5, Opus 4.8, Sonnet 5, Haiku 4.5",
			},
			Models: []ModelCatalogEntry{
				// ── Claude 5 Series (Latest) ──
				{ID: "claude-fable-5", Name: "Claude Fable 5", Provider: "anthropic", ContextWindow: 1000000, MaxOutput: 128000, InputCostPer1M: 10.00, OutputCostPer1M: 50.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Most capable Claude, Mythos-class flagship"},
				{ID: "claude-mythos-5", Name: "Claude Mythos 5", Provider: "anthropic", ContextWindow: 1000000, MaxOutput: 128000, InputCostPer1M: 10.00, OutputCostPer1M: 50.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Trusted-access Mythos model"},
				{ID: "claude-sonnet-5", Name: "Claude Sonnet 5", Provider: "anthropic", ContextWindow: 1000000, MaxOutput: 128000, InputCostPer1M: 2.00, OutputCostPer1M: 10.00, Capabilities: []string{"tools", "vision"}, Description: "Intro pricing until Aug 31, then $3/$15"},
				// ── Claude 4.8 Series ──
				{ID: "claude-opus-4.8", Name: "Claude Opus 4.8", Provider: "anthropic", ContextWindow: 1000000, MaxOutput: 128000, InputCostPer1M: 5.00, OutputCostPer1M: 25.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Premium reasoning, latest 4.x"},
				// ── Claude 4.7 Series ──
				{ID: "claude-opus-4.7", Name: "Claude Opus 4.7", Provider: "anthropic", ContextWindow: 1000000, MaxOutput: 128000, InputCostPer1M: 5.00, OutputCostPer1M: 25.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Previous Opus, same pricing"},
				// ── Claude 4.6 Series ──
				{ID: "claude-opus-4.6", Name: "Claude Opus 4.6", Provider: "anthropic", ContextWindow: 1000000, MaxOutput: 128000, InputCostPer1M: 5.00, OutputCostPer1M: 25.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Opus 4.6, 67% cheaper than 4.1"},
				{ID: "claude-sonnet-4.6", Name: "Claude Sonnet 4.6", Provider: "anthropic", ContextWindow: 1000000, MaxOutput: 8192, InputCostPer1M: 3.00, OutputCostPer1M: 15.00, Capabilities: []string{"tools", "vision"}, Description: "Balanced performance and cost"},
				// ── Claude 4.5 Series ──
				{ID: "claude-haiku-4.5", Name: "Claude Haiku 4.5", Provider: "anthropic", ContextWindow: 200000, MaxOutput: 8192, InputCostPer1M: 1.00, OutputCostPer1M: 5.00, Capabilities: []string{"tools", "vision"}, Description: "Fast, affordable production model"},
				{ID: "claude-sonnet-4.5", Name: "Claude Sonnet 4.5", Provider: "anthropic", ContextWindow: 200000, MaxOutput: 8192, InputCostPer1M: 3.00, OutputCostPer1M: 15.00, Capabilities: []string{"tools", "vision"}, Description: "Previous-gen balanced", Deprecated: true},
				// ── Legacy ──
				{ID: "claude-3-5-sonnet", Name: "Claude 3.5 Sonnet", Provider: "anthropic", ContextWindow: 200000, MaxOutput: 8192, InputCostPer1M: 3.00, OutputCostPer1M: 15.00, Capabilities: []string{"tools", "vision"}, Description: "Legacy balanced", Deprecated: true},
				{ID: "claude-3-5-haiku", Name: "Claude 3.5 Haiku", Provider: "anthropic", ContextWindow: 200000, MaxOutput: 8192, InputCostPer1M: 0.80, OutputCostPer1M: 4.00, Capabilities: []string{"tools", "vision"}, Description: "Legacy fast model", Deprecated: true},
				{ID: "claude-3-opus", Name: "Claude 3 Opus", Provider: "anthropic", ContextWindow: 200000, MaxOutput: 4096, InputCostPer1M: 15.00, OutputCostPer1M: 75.00, Capabilities: []string{"tools", "vision"}, Description: "Legacy Opus", Deprecated: true},
			},
		}

		// ═══════════════════════════════════════════════════════════════════
		// GOOGLE GEMINI (July 2026)
		// ═══════════════════════════════════════════════════════════════════
		providerCatalog[ProviderGemini] = &ProviderCatalog{
			Provider: ProviderInfo{
				ID:          ProviderGemini,
				Name:        "Google Gemini",
				BaseURL:     "https://generativelanguage.googleapis.com",
				KeyPrefix:   "AIza",
				KeyHint:     "AIza...",
				Description: "Gemini 3.x with 2M context, Flash Live/TTS, media generation",
			},
			Models: []ModelCatalogEntry{
				// ── Gemini 3.x (Current Production) ──
				{ID: "gemini-3.1-pro", Name: "Gemini 3.1 Pro", Provider: "gemini", ContextWindow: 2000000, MaxOutput: 65536, InputCostPer1M: 2.00, OutputCostPer1M: 12.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Current flagship, 2M context. $4/$18 above 200K"},
				{ID: "gemini-3-pro", Name: "Gemini 3 Pro", Provider: "gemini", ContextWindow: 2000000, MaxOutput: 65536, InputCostPer1M: 2.00, OutputCostPer1M: 12.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Stable flagship alternative"},
				{ID: "gemini-3-pro-preview", Name: "Gemini 3 Pro Preview", Provider: "gemini", ContextWindow: 2000000, MaxOutput: 65536, InputCostPer1M: 2.00, OutputCostPer1M: 12.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Preview features, may change"},
				{ID: "gemini-3.6-flash", Name: "Gemini 3.6 Flash", Provider: "gemini", ContextWindow: 1000000, MaxOutput: 65536, InputCostPer1M: 1.50, OutputCostPer1M: 9.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Latest Flash, best coding/agentic"},
				{ID: "gemini-3.5-flash", Name: "Gemini 3.5 Flash", Provider: "gemini", ContextWindow: 1000000, MaxOutput: 65536, InputCostPer1M: 1.50, OutputCostPer1M: 9.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Best coding/agentic Flash model"},
				{ID: "gemini-3-flash", Name: "Gemini 3 Flash", Provider: "gemini", ContextWindow: 1000000, MaxOutput: 8192, InputCostPer1M: 0.50, OutputCostPer1M: 3.00, Capabilities: []string{"tools", "vision"}, Description: "Mid-tier, strong general-purpose"},
				{ID: "gemini-3.1-flash-lite", Name: "Gemini 3.1 Flash-Lite", Provider: "gemini", ContextWindow: 1000000, MaxOutput: 32768, InputCostPer1M: 0.125, OutputCostPer1M: 0.75, Capabilities: []string{"tools", "vision"}, Description: "Cheapest Tier-1 budget model, halved pricing"},
				// ── Gemini 2.5 Flash Variants (Live/TTS) ──
				{ID: "gemini-2.5-flash-live", Name: "Gemini 2.5 Flash Live", Provider: "gemini", ContextWindow: 1000000, MaxOutput: 8192, InputCostPer1M: 0.30, OutputCostPer1M: 2.50, Capabilities: []string{"tools", "vision", "audio"}, Description: "Real-time audio/video streaming"},
				{ID: "gemini-2.5-flash-tts", Name: "Gemini 2.5 Flash TTS", Provider: "gemini", ContextWindow: 1000000, MaxOutput: 8192, InputCostPer1M: 0.30, OutputCostPer1M: 2.50, Capabilities: []string{"audio"}, Description: "Text-to-speech synthesis"},
				// ── Media Generation ──
				{ID: "nano-banana-2", Name: "Nano Banana 2", Provider: "gemini", ContextWindow: 0, MaxOutput: 0, InputCostPer1M: 0.00, OutputCostPer1M: 0.04, Capabilities: []string{"image"}, Description: "Image generation model"},
				{ID: "veo-3.1", Name: "Veo 3.1", Provider: "gemini", ContextWindow: 0, MaxOutput: 0, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"video"}, Description: "Video generation, preview"},
				// ── Gemini 2.5 (Legacy) ──
				{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Provider: "gemini", ContextWindow: 2000000, MaxOutput: 65536, InputCostPer1M: 1.25, OutputCostPer1M: 10.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Legacy flagship. $2.50/$15 above 200K", Deprecated: true},
				{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", Provider: "gemini", ContextWindow: 1000000, MaxOutput: 8192, InputCostPer1M: 0.30, OutputCostPer1M: 2.50, Capabilities: []string{"tools", "vision"}, Description: "Legacy mid-tier, deprecated Oct 2026", Deprecated: true},
				{ID: "gemini-2.5-flash-lite", Name: "Gemini 2.5 Flash-Lite", Provider: "gemini", ContextWindow: 1000000, MaxOutput: 8192, InputCostPer1M: 0.10, OutputCostPer1M: 0.40, Capabilities: []string{"vision"}, Description: "Cheapest legacy model"},
			},
		}

		// ═══════════════════════════════════════════════════════════════════
		// GROQ (July 2026)
		// ═══════════════════════════════════════════════════════════════════
		providerCatalog[ProviderGroq] = &ProviderCatalog{
			Provider: ProviderInfo{
				ID:          ProviderGroq,
				Name:        "Groq",
				BaseURL:     "https://api.groq.com/openai/v1",
				KeyPrefix:   "gsk_",
				KeyHint:     "gsk_...",
				Description: "Ultra-fast LPU inference, 500+ tokens/sec",
			},
			Models: []ModelCatalogEntry{
				// ── Llama 4 ──
				{ID: "llama-4-scout-17b-16e-instruct", Name: "Llama 4 Scout 17B (16E)", Provider: "groq", ContextWindow: 131000, MaxOutput: 8192, InputCostPer1M: 0.11, OutputCostPer1M: 0.34, Capabilities: []string{"tools"}, Description: "Fast MoE Llama, ~600 TPS"},
				// ── Llama 3.3/3.1 ──
				{ID: "llama-3.3-70b-versatile", Name: "Llama 3.3 70B Versatile", Provider: "groq", ContextWindow: 128000, MaxOutput: 32768, InputCostPer1M: 0.59, OutputCostPer1M: 0.79, Capabilities: []string{"tools"}, Description: "Best quality Llama, ~394 TPS"},
				{ID: "llama-3.1-8b-instant", Name: "Llama 3.1 8B Instant", Provider: "groq", ContextWindow: 128000, MaxOutput: 8192, InputCostPer1M: 0.05, OutputCostPer1M: 0.08, Capabilities: []string{"tools"}, Description: "Ultra-fast small model, ~840 TPS"},
				// ── GPT-OSS ──
				{ID: "openai/gpt-oss-20b", Name: "GPT-OSS 20B", Provider: "groq", ContextWindow: 131000, MaxOutput: 32768, InputCostPer1M: 0.075, OutputCostPer1M: 0.30, Capabilities: []string{"tools"}, Description: "OpenAI open-source, ~1000 TPS"},
				{ID: "openai/gpt-oss-120b", Name: "GPT-OSS 120B", Provider: "groq", ContextWindow: 131000, MaxOutput: 32768, InputCostPer1M: 0.15, OutputCostPer1M: 0.60, Capabilities: []string{"tools"}, Description: "OpenAI large open-source, ~500 TPS"},
				// ── Qwen ──
				{ID: "qwen3-32b", Name: "Qwen3 32B", Provider: "groq", ContextWindow: 128000, MaxOutput: 131072, InputCostPer1M: 0.29, OutputCostPer1M: 0.59, Capabilities: []string{"tools", "reasoning"}, Description: "Alibaba reasoning, ~662 TPS"},
				// ── Kimi ──
				{ID: "moonshotai/kimi-k2", Name: "Kimi K2", Provider: "groq", ContextWindow: 128000, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 3.00, Capabilities: []string{"tools", "reasoning"}, Description: "Moonshot AI flagship, ~250 TPS"},
				// ── Mistral ──
				{ID: "mistral-saba-24b", Name: "Mistral Saba 24B", Provider: "groq", ContextWindow: 128000, MaxOutput: 32768, InputCostPer1M: 0.79, OutputCostPer1M: 0.79, Capabilities: []string{"tools"}, Description: "Mistral on Groq LPU"},
				// ── Gemma ──
				{ID: "gemma2-9b-it", Name: "Gemma 2 9B", Provider: "groq", ContextWindow: 8192, MaxOutput: 8192, InputCostPer1M: 0.20, OutputCostPer1M: 0.20, Capabilities: []string{}, Description: "Google open model, ~500 TPS"},
				// ── Legacy ──
				{ID: "llama-3-70b", Name: "Llama 3 70B", Provider: "groq", ContextWindow: 8192, MaxOutput: 8192, InputCostPer1M: 0.59, OutputCostPer1M: 0.79, Capabilities: []string{}, Description: "Legacy Llama 3", Deprecated: true},
				{ID: "llama-3-8b", Name: "Llama 3 8B", Provider: "groq", ContextWindow: 8192, MaxOutput: 8192, InputCostPer1M: 0.05, OutputCostPer1M: 0.08, Capabilities: []string{}, Description: "Legacy small model", Deprecated: true},
				{ID: "mixtral-8x7b-32768", Name: "Mixtral 8x7B", Provider: "groq", ContextWindow: 32768, MaxOutput: 32768, InputCostPer1M: 0.24, OutputCostPer1M: 0.24, Capabilities: []string{"tools"}, Description: "Mixture of Experts", Deprecated: true},
			},
		}

		// ═══════════════════════════════════════════════════════════════════
		// MISTRAL (July 2026)
		// ═══════════════════════════════════════════════════════════════════
		providerCatalog[ProviderMistral] = &ProviderCatalog{
			Provider: ProviderInfo{
				ID:          ProviderMistral,
				Name:        "Mistral AI",
				BaseURL:     "https://api.mistral.ai/v1",
				KeyPrefix:   "ms-",
				KeyHint:     "ms-...",
				Description: "European AI lab, open-weight models, Apache 2.0",
			},
			Models: []ModelCatalogEntry{
				// ── Flagship ──
				{ID: "mistral-large-latest", Name: "Mistral Large 3", Provider: "mistral", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 1.50, Capabilities: []string{"tools", "vision"}, Description: "675B MoE flagship, Apache 2.0"},
				{ID: "mistral-medium-latest", Name: "Mistral Medium 3.5", Provider: "mistral", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 1.50, OutputCostPer1M: 7.50, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Enterprise reasoning, coding, agentic"},
				{ID: "mistral-small-latest", Name: "Mistral Small 4", Provider: "mistral", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.30, Capabilities: []string{"tools", "vision"}, Description: "SOTA multimodal, Apache 2.0"},
				// ── Code ──
				{ID: "codestral-latest", Name: "Codestral", Provider: "mistral", ContextWindow: 32000, MaxOutput: 32000, InputCostPer1M: 0.30, OutputCostPer1M: 0.90, Capabilities: []string{"tools"}, Description: "Code-specialized model"},
				{ID: "devstral-small-latest", Name: "Devstral Small 2", Provider: "mistral", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.30, Capabilities: []string{"tools"}, Description: "Agentic software engineering"},
				// ── Reasoning ──
				{ID: "magistral-medium-latest", Name: "Magistral Medium", Provider: "mistral", ContextWindow: 128000, MaxOutput: 32768, InputCostPer1M: 2.00, OutputCostPer1M: 5.00, Capabilities: []string{"tools", "reasoning"}, Description: "Reasoning model"},
				// ── Multimodal ──
				{ID: "pixtral-large-latest", Name: "Pixtral Large", Provider: "mistral", ContextWindow: 131000, MaxOutput: 32768, InputCostPer1M: 2.00, OutputCostPer1M: 6.00, Capabilities: []string{"tools", "vision"}, Description: "Multimodal Mistral"},
				// ── Lightweight ──
				{ID: "ministral-8b-latest", Name: "Ministral 8B", Provider: "mistral", ContextWindow: 32000, MaxOutput: 32000, InputCostPer1M: 0.10, OutputCostPer1M: 0.10, Capabilities: []string{"tools"}, Description: "Ultra-lightweight model"},
				{ID: "ministral-3b-latest", Name: "Ministral 3B", Provider: "mistral", ContextWindow: 32000, MaxOutput: 32000, InputCostPer1M: 0.04, OutputCostPer1M: 0.04, Capabilities: []string{}, Description: "Smallest Mistral model"},
				// ── Audio ──
				{ID: "voxtral-small-24b-latest", Name: "Voxtral Small 24B", Provider: "mistral", ContextWindow: 32000, MaxOutput: 32000, InputCostPer1M: 0.50, OutputCostPer1M: 1.00, Capabilities: []string{"tools", "audio"}, Description: "Audio-capable Mistral"},
				// ── Legacy ──
				{ID: "open-mixtral-8x22b", Name: "Open Mixtral 8x22B", Provider: "mistral", ContextWindow: 65536, MaxOutput: 32768, InputCostPer1M: 2.00, OutputCostPer1M: 6.00, Capabilities: []string{"tools"}, Description: "Legacy MoE model", Deprecated: true},
				{ID: "open-mixtral-8x7b", Name: "Open Mixtral 8x7B", Provider: "mistral", ContextWindow: 32768, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 0.50, Capabilities: []string{}, Description: "Legacy small MoE", Deprecated: true},
			},
		}

		// ═══════════════════════════════════════════════════════════════════
		// COHERE (July 2026)
		// ═══════════════════════════════════════════════════════════════════
		providerCatalog[ProviderCohere] = &ProviderCatalog{
			Provider: ProviderInfo{
				ID:          ProviderCohere,
				Name:        "Cohere",
				BaseURL:     "https://api.cohere.com",
				KeyPrefix:   "co-",
				KeyHint:     "co-...",
				Description: "RAG-optimized models, enterprise search, Command A",
			},
			Models: []ModelCatalogEntry{
				// ── Command A ──
				{ID: "command-a", Name: "Command A", Provider: "cohere", ContextWindow: 128000, MaxOutput: 4096, InputCostPer1M: 2.50, OutputCostPer1M: 10.00, Capabilities: []string{"tools"}, Description: "Latest Cohere flagship"},
				// ── Command R+ ──
				{ID: "command-r-plus", Name: "Command R+", Provider: "cohere", ContextWindow: 128000, MaxOutput: 4096, InputCostPer1M: 2.50, OutputCostPer1M: 10.00, Capabilities: []string{"tools"}, Description: "Enterprise RAG model"},
				{ID: "command-r-plus-08-2024", Name: "Command R+ 08-2024", Provider: "cohere", ContextWindow: 128000, MaxOutput: 4096, InputCostPer1M: 2.50, OutputCostPer1M: 10.00, Capabilities: []string{"tools"}, Description: "Versioned R+"},
				// ── Command R ──
				{ID: "command-r", Name: "Command R", Provider: "cohere", ContextWindow: 128000, MaxOutput: 4096, InputCostPer1M: 0.15, OutputCostPer1M: 0.60, Capabilities: []string{"tools"}, Description: "Budget RAG model"},
				{ID: "command-r-08-2024", Name: "Command R 08-2024", Provider: "cohere", ContextWindow: 128000, MaxOutput: 4096, InputCostPer1M: 0.15, OutputCostPer1M: 0.60, Capabilities: []string{"tools"}, Description: "Versioned Command R"},
				// ── Command R7B ──
				{ID: "command-r7b-12-2024", Name: "Command R7B", Provider: "cohere", ContextWindow: 128000, MaxOutput: 4096, InputCostPer1M: 0.0375, OutputCostPer1M: 0.15, Capabilities: []string{}, Description: "Cheapest Cohere model"},
				// ── Legacy ──
				{ID: "command", Name: "Command", Provider: "cohere", ContextWindow: 4096, MaxOutput: 4096, InputCostPer1M: 1.00, OutputCostPer1M: 2.00, Capabilities: []string{}, Description: "Legacy Cohere model", Deprecated: true},
				{ID: "command-light", Name: "Command Light", Provider: "cohere", ContextWindow: 4096, MaxOutput: 4096, InputCostPer1M: 0.15, OutputCostPer1M: 0.60, Capabilities: []string{}, Description: "Legacy lightweight", Deprecated: true},
				// ── Embed ──
				{ID: "embed-english-v3.0", Name: "Embed English v3.0", Provider: "cohere", ContextWindow: 512, MaxOutput: 0, InputCostPer1M: 0.10, OutputCostPer1M: 0, Capabilities: []string{"embeddings"}, Description: "English embeddings"},
				{ID: "embed-multilingual-v3.0", Name: "Embed Multilingual v3.0", Provider: "cohere", ContextWindow: 512, MaxOutput: 0, InputCostPer1M: 0.10, OutputCostPer1M: 0, Capabilities: []string{"embeddings"}, Description: "Multilingual embeddings"},
				// ── Rerank ──
				{ID: "rerank-v3", Name: "Rerank v3", Provider: "cohere", ContextWindow: 512, MaxOutput: 0, InputCostPer1M: 2.00, OutputCostPer1M: 0, Capabilities: []string{"rerank"}, Description: "Search result reranking"},
			},
		}

		// ═══════════════════════════════════════════════════════════════════
		// NVIDIA NIM (July 2026)
		// ═══════════════════════════════════════════════════════════════════
		providerCatalog[ProviderNVIDIANIM] = &ProviderCatalog{
			Provider: ProviderInfo{
				ID:          ProviderNVIDIANIM,
				Name:        "NVIDIA NIM",
				BaseURL:     "https://build.nvidia.com/v1",
				KeyPrefix:   "nvapi-",
				KeyHint:     "nvapi-...",
				Description: "Free tier on build.nvidia.com, 100+ models, Nemotron Ultra, GLM-5.2",
			},
			Models: []ModelCatalogEntry{
				// ── Llama 4 ──
				{ID: "nvidia/llama-4-maverick-17b-128e-instruct", Name: "Llama 4 Maverick 17B (128E)", Provider: "nvidia_nim", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "vision"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/llama-4-scout-17b-16e-instruct", Name: "Llama 4 Scout 17B (16E)", Provider: "nvidia_nim", ContextWindow: 524288, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				// ── Llama 3.x ──
				{ID: "nvidia/llama-3.3-70b-instruct", Name: "Llama 3.3 70B Instruct", Provider: "nvidia_nim", ContextWindow: 128000, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/llama-3.3-nemotron-super-49b-v1.5", Name: "Nemotron Super 49B v1.5", Provider: "nvidia_nim", ContextWindow: 128000, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/llama-3.1-405b-instruct", Name: "Llama 3.1 405B Instruct", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/llama-3.1-70b-instruct", Name: "Llama 3.1 70B Instruct", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/llama-3.1-8b-instruct", Name: "Llama 3.1 8B Instruct", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 4096, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/llama-3.2-90b-vision-instruct", Name: "Llama 3.2 90B Vision", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "vision"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/llama-3.2-11b-vision-instruct", Name: "Llama 3.2 11B Vision", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "vision"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/llama-3.2-3b-instruct", Name: "Llama 3.2 3B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 4096, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/llama-3.2-1b-instruct", Name: "Llama 3.2 1B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 4096, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/llama-3.1-nemotron-nano-8b-v1", Name: "Nemotron Nano 8B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 8192, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/llama-3.1-nemotron-nano-vl-8b-v1", Name: "Nemotron Nano VL 8B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 8192, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"vision"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/llama-guard-4-12b", Name: "Llama Guard 4 12B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 8192, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				// ── Nemotron ──
				{ID: "nvidia/nemotron-4-340b-instruct", Name: "Nemotron 4 340B", Provider: "nvidia_nim", ContextWindow: 4096, MaxOutput: 4096, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/nemotron-3-ultra-550b-a55b", Name: "Nemotron 3 Ultra 550B", Provider: "nvidia_nim", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/nemotron-3-super-120b-a12b", Name: "Nemotron 3 Super 120B", Provider: "nvidia_nim", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/nemotron-3-nano-30b-a3b", Name: "Nemotron 3 Nano 30B", Provider: "nvidia_nim", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning", Name: "Nemotron 3 Nano Omni", Provider: "nvidia_nim", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "vision", "audio", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/nemotron-mini-4b-instruct", Name: "Nemotron Mini 4B", Provider: "nvidia_nim", ContextWindow: 4096, MaxOutput: 4096, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/nemotron-nano-12b-v2-vl", Name: "Nemotron Nano 12B VL", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 8192, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"vision"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/nvidia-nemotron-nano-9b-v2", Name: "Nemotron Nano 9B v2", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 8192, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				// ── DeepSeek ──
				{ID: "deepseek-r1", Name: "DeepSeek R1", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", Provider: "nvidia_nim", ContextWindow: 1048576, MaxOutput: 384000, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Provider: "nvidia_nim", ContextWindow: 1048576, MaxOutput: 384000, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "deepseek-v3-0324", Name: "DeepSeek V3 0324", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				// ── MiniMax ──
				{ID: "minimaxai/minimax-m2.7", Name: "MiniMax M2.7 230B", Provider: "nvidia_nim", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "minimaxai/minimax-m3", Name: "MiniMax M3", Provider: "nvidia_nim", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Free on build.nvidia.com"},
				// ── Qwen ──
				{ID: "qwen/qwen2.5-72b-instruct", Name: "Qwen 2.5 72B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				{ID: "qwen/qwq-32b", Name: "Qwen QwQ 32B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 131072, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "qwen/qwen3-32b", Name: "Qwen3 32B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 131072, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "qwen/qwen3-next-80b-a3b-instruct", Name: "Qwen3 Next 80B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "qwen/qwen3.5-397b-a17b", Name: "Qwen3.5 397B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				// ── Mistral ──
				{ID: "nvidia/mistral-nemo-12b-instruct", Name: "Mistral Nemo 12B", Provider: "nvidia_nim", ContextWindow: 128000, MaxOutput: 4096, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/mistral-large-2-instruct", Name: "Mistral Large 2", Provider: "nvidia_nim", ContextWindow: 128000, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/mistral-medium-3.5-128b", Name: "Mistral Medium 3.5 128B", Provider: "nvidia_nim", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/mistral-small-4-119b-2603", Name: "Mistral Small 4 119B", Provider: "nvidia_nim", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/mistral-nemotron", Name: "Mistral Nemotron", Provider: "nvidia_nim", ContextWindow: 128000, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "nvidia/ministral-14b-instruct-2512", Name: "Ministral 14B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "vision"}, Description: "Free on build.nvidia.com"},
				// ── Zhipu AI ──
				{ID: "zhipu/glm-5.2", Name: "GLM-5.2", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Free on build.nvidia.com"},
				// ── Moonshot ──
				{ID: "moonshotai/kimi-k2.6", Name: "Kimi K2.6 1T MoE", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Free on build.nvidia.com"},
				// ── StepFun ──
				{ID: "stepfun/step-3.5-flash", Name: "Step 3.5 Flash 200B", Provider: "nvidia_nim", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "stepfun/step-3.7-flash", Name: "Step 3.7 Flash", Provider: "nvidia_nim", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Free on build.nvidia.com"},
				// ── ByteDance ──
				{ID: "seed-oss-36b-instruct", Name: "Seed OSS 36B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				// ── Indian Languages ──
				{ID: "sarvam-m", Name: "Sarvam M", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				// ── Google Gemma ──
				{ID: "gemma-2-2b-it", Name: "Gemma 2 2B", Provider: "nvidia_nim", ContextWindow: 8192, MaxOutput: 8192, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				{ID: "gemma-3n-e2b-it", Name: "Gemma 3N E2B", Provider: "nvidia_nim", ContextWindow: 8192, MaxOutput: 8192, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				{ID: "gemma-3n-e4b-it", Name: "Gemma 3N E4B", Provider: "nvidia_nim", ContextWindow: 8192, MaxOutput: 8192, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				{ID: "gemma-4-31b-it", Name: "Gemma 4 31B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				// ── OpenAI OSS ──
				{ID: "gpt-oss-120b", Name: "GPT-OSS 120B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on build.nvidia.com"},
				{ID: "gpt-oss-20b", Name: "GPT-OSS 20B", Provider: "nvidia_nim", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				// ── Mixtral ──
				{ID: "nvidia/mixtral-8x7b-instruct-v0.1", Name: "Mixtral 8x7B", Provider: "nvidia_nim", ContextWindow: 32768, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
				// ── Solar ──
				{ID: "solar-10.7b-instruct", Name: "Solar 10.7B", Provider: "nvidia_nim", ContextWindow: 4096, MaxOutput: 4096, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free on build.nvidia.com"},
			},
		}

		// ═══════════════════════════════════════════════════════════════════
		// DEEPSEEK (July 2026)
		// ═══════════════════════════════════════════════════════════════════
		providerCatalog[ProviderDeepSeek] = &ProviderCatalog{
			Provider: ProviderInfo{
				ID:          ProviderDeepSeek,
				Name:        "DeepSeek",
				BaseURL:     "https://api.deepseek.com",
				KeyPrefix:   "sk-",
				KeyHint:     "sk-...",
				Description: "Cheapest AI API, V4 Flash $0.14/M, 1M context",
			},
			Models: []ModelCatalogEntry{
				// ── V4 Series (Latest) ──
				{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", Provider: "deepseek", ContextWindow: 1000000, MaxOutput: 384000, InputCostPer1M: 0.14, OutputCostPer1M: 0.28, Capabilities: []string{"tools", "reasoning"}, Description: "Cheapest frontier model, 98% cache discount"},
				{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Provider: "deepseek", ContextWindow: 1000000, MaxOutput: 384000, InputCostPer1M: 0.435, OutputCostPer1M: 0.87, Capabilities: []string{"tools", "reasoning"}, Description: "1.6T parameter flagship, promo pricing"},
				// ── Legacy (deprecated July 24, 2026) ──
				{ID: "deepseek-chat", Name: "DeepSeek V3 (Chat)", Provider: "deepseek", ContextWindow: 65536, MaxOutput: 8192, InputCostPer1M: 0.27, OutputCostPer1M: 1.10, Capabilities: []string{"tools"}, Description: "Being deprecated, use V4 Flash", Deprecated: true},
				{ID: "deepseek-reasoner", Name: "DeepSeek R1 (Reasoner)", Provider: "deepseek", ContextWindow: 65536, MaxOutput: 8192, InputCostPer1M: 0.55, OutputCostPer1M: 2.19, Capabilities: []string{"reasoning"}, Description: "Being deprecated, use V4 Flash thinking", Deprecated: true},
			},
		}

		// ═══════════════════════════════════════════════════════════════════
		// OPENROUTER (July 2026)
		// ═══════════════════════════════════════════════════════════════════
		providerCatalog[ProviderOpenRouter] = &ProviderCatalog{
			Provider: ProviderInfo{
				ID:          ProviderOpenRouter,
				Name:        "OpenRouter",
				BaseURL:     "https://openrouter.ai/api/v1",
				KeyPrefix:   "sk-or-",
				KeyHint:     "sk-or-...",
				Description: "Unified gateway to 300+ models from all providers",
			},
			Models: []ModelCatalogEntry{
				// ── xAI via OpenRouter ──
				{ID: "x-ai/grok-4.5", Name: "Grok 4.5", Provider: "openrouter", ContextWindow: 500000, MaxOutput: 32768, InputCostPer1M: 2.00, OutputCostPer1M: 6.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "xAI Grok 4.5, 500K context"},
				{ID: "x-ai/grok-4.20", Name: "Grok 4.20", Provider: "openrouter", ContextWindow: 2000000, MaxOutput: 32768, InputCostPer1M: 1.25, OutputCostPer1M: 2.50, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "xAI Grok 4.20, 2M context"},
				{ID: "x-ai/grok-4.20-multi-agent", Name: "Grok 4.20 Multi-Agent", Provider: "openrouter", ContextWindow: 2000000, MaxOutput: 32768, InputCostPer1M: 2.00, OutputCostPer1M: 6.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Multi-agent parallel workflows"},
				{ID: "x-ai/grok-4.3", Name: "Grok 4.3", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 32768, InputCostPer1M: 1.25, OutputCostPer1M: 2.50, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "xAI Grok 4.3, 1M context"},
				{ID: "x-ai/grok-3", Name: "Grok 3", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 3.00, OutputCostPer1M: 15.00, Capabilities: []string{"tools", "vision"}, Description: "xAI Grok 3"},
				{ID: "x-ai/grok-3-mini", Name: "Grok 3 Mini", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.30, OutputCostPer1M: 0.50, Capabilities: []string{"tools", "reasoning"}, Description: "xAI Grok 3 Mini"},
				// ── OpenAI via OpenRouter ──
				{ID: "openai/gpt-5.6-sol", Name: "GPT-5.6 Sol", Provider: "openrouter", ContextWindow: 1050000, MaxOutput: 32768, InputCostPer1M: 5.00, OutputCostPer1M: 30.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "GPT-5.6 flagship, 1.05M context"},
				{ID: "openai/gpt-5.6-sol-pro", Name: "GPT-5.6 Sol Pro", Provider: "openrouter", ContextWindow: 1050000, MaxOutput: 65536, InputCostPer1M: 5.00, OutputCostPer1M: 30.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "GPT-5.6 Sol Pro, extended output"},
				{ID: "openai/gpt-5.6-terra", Name: "GPT-5.6 Terra", Provider: "openrouter", ContextWindow: 1050000, MaxOutput: 32768, InputCostPer1M: 2.50, OutputCostPer1M: 15.00, Capabilities: []string{"tools", "vision"}, Description: "GPT-5.6 balanced"},
				{ID: "openai/gpt-5.6-terra-pro", Name: "GPT-5.6 Terra Pro", Provider: "openrouter", ContextWindow: 1050000, MaxOutput: 65536, InputCostPer1M: 2.50, OutputCostPer1M: 15.00, Capabilities: []string{"tools", "vision"}, Description: "GPT-5.6 Terra Pro"},
				{ID: "openai/gpt-5.6-luna", Name: "GPT-5.6 Luna", Provider: "openrouter", ContextWindow: 1050000, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 6.00, Capabilities: []string{"tools", "vision"}, Description: "GPT-5.6 cost-effective"},
				{ID: "openai/gpt-5.6-luna-pro", Name: "GPT-5.6 Luna Pro", Provider: "openrouter", ContextWindow: 1050000, MaxOutput: 65536, InputCostPer1M: 1.00, OutputCostPer1M: 6.00, Capabilities: []string{"tools", "vision"}, Description: "GPT-5.6 Luna Pro"},
				{ID: "openai/gpt-5.5", Name: "GPT-5.5", Provider: "openrouter", ContextWindow: 400000, MaxOutput: 32768, InputCostPer1M: 5.00, OutputCostPer1M: 30.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "GPT-5.5 frontier"},
				{ID: "openai/gpt-5.4", Name: "GPT-5.4", Provider: "openrouter", ContextWindow: 400000, MaxOutput: 32768, InputCostPer1M: 2.50, OutputCostPer1M: 15.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "GPT-5.4"},
				{ID: "openai/gpt-5.4-mini", Name: "GPT-5.4 Mini", Provider: "openrouter", ContextWindow: 400000, MaxOutput: 16384, InputCostPer1M: 0.75, OutputCostPer1M: 4.50, Capabilities: []string{"tools", "vision"}, Description: "GPT-5.4 Mini"},
				{ID: "openai/gpt-5.4-nano", Name: "GPT-5.4 Nano", Provider: "openrouter", ContextWindow: 400000, MaxOutput: 8192, InputCostPer1M: 0.20, OutputCostPer1M: 1.25, Capabilities: []string{"tools"}, Description: "GPT-5.4 Nano"},
				{ID: "openai/gpt-5", Name: "GPT-5", Provider: "openrouter", ContextWindow: 256000, MaxOutput: 32768, InputCostPer1M: 10.00, OutputCostPer1M: 30.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "GPT-5"},
				{ID: "openai/gpt-5-mini", Name: "GPT-5 Mini", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 16384, InputCostPer1M: 0.25, OutputCostPer1M: 2.00, Capabilities: []string{"tools", "vision"}, Description: "GPT-5 Mini"},
				{ID: "openai/gpt-5-nano", Name: "GPT-5 Nano", Provider: "openrouter", ContextWindow: 400000, MaxOutput: 8192, InputCostPer1M: 0.05, OutputCostPer1M: 0.40, Capabilities: []string{"tools"}, Description: "GPT-5 Nano"},
				{ID: "openai/gpt-4.1", Name: "GPT-4.1", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 32768, InputCostPer1M: 2.00, OutputCostPer1M: 8.00, Capabilities: []string{"tools", "vision"}, Description: "GPT-4.1, 1M context"},
				{ID: "openai/gpt-4.1-mini", Name: "GPT-4.1 Mini", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 16384, InputCostPer1M: 0.40, OutputCostPer1M: 1.60, Capabilities: []string{"tools", "vision"}, Description: "GPT-4.1 Mini"},
				{ID: "openai/gpt-4.1-nano", Name: "GPT-4.1 Nano", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 8192, InputCostPer1M: 0.10, OutputCostPer1M: 0.40, Capabilities: []string{"tools"}, Description: "GPT-4.1 Nano"},
				{ID: "openai/gpt-4o", Name: "GPT-4o", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 16384, InputCostPer1M: 2.50, OutputCostPer1M: 10.00, Capabilities: []string{"tools", "vision"}, Description: "GPT-4o"},
				{ID: "openai/gpt-4o-mini", Name: "GPT-4o Mini", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 16384, InputCostPer1M: 0.15, OutputCostPer1M: 0.60, Capabilities: []string{"tools", "vision"}, Description: "GPT-4o Mini"},
				{ID: "openai/o3", Name: "o3", Provider: "openrouter", ContextWindow: 200000, MaxOutput: 100000, InputCostPer1M: 2.00, OutputCostPer1M: 8.00, Capabilities: []string{"tools", "reasoning"}, Description: "o3 reasoning"},
				{ID: "openai/o3-pro", Name: "o3 Pro", Provider: "openrouter", ContextWindow: 200000, MaxOutput: 100000, InputCostPer1M: 20.00, OutputCostPer1M: 80.00, Capabilities: []string{"tools", "reasoning"}, Description: "o3 Pro premium reasoning"},
				{ID: "openai/o3-mini", Name: "o3-mini", Provider: "openrouter", ContextWindow: 200000, MaxOutput: 100000, InputCostPer1M: 1.10, OutputCostPer1M: 4.40, Capabilities: []string{"tools", "reasoning"}, Description: "o3-mini"},
				{ID: "openai/o4-mini", Name: "o4-mini", Provider: "openrouter", ContextWindow: 200000, MaxOutput: 100000, InputCostPer1M: 1.10, OutputCostPer1M: 4.40, Capabilities: []string{"tools", "reasoning"}, Description: "o4-mini"},
				{ID: "openai/gpt-oss-120b", Name: "GPT-OSS 120B", Provider: "openrouter", ContextWindow: 131000, MaxOutput: 32768, InputCostPer1M: 0.04, OutputCostPer1M: 0.17, Capabilities: []string{"tools"}, Description: "OpenAI open-source 120B"},
				{ID: "openai/gpt-oss-20b", Name: "GPT-OSS 20B", Provider: "openrouter", ContextWindow: 131000, MaxOutput: 32768, InputCostPer1M: 0.03, OutputCostPer1M: 0.13, Capabilities: []string{"tools"}, Description: "OpenAI open-source 20B"},
				{ID: "openai/gpt-oss-20b:free", Name: "GPT-OSS 20B (Free)", Provider: "openrouter", ContextWindow: 131000, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free tier"},
				// ── Anthropic via OpenRouter ──
				{ID: "anthropic/claude-fable-5", Name: "Claude Fable 5", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 128000, InputCostPer1M: 10.00, OutputCostPer1M: 50.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Claude Fable 5 flagship"},
				{ID: "anthropic/claude-opus-4.8", Name: "Claude Opus 4.8", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 128000, InputCostPer1M: 5.00, OutputCostPer1M: 25.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Claude Opus 4.8"},
				{ID: "anthropic/claude-opus-4.7", Name: "Claude Opus 4.7", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 128000, InputCostPer1M: 5.00, OutputCostPer1M: 25.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Claude Opus 4.7"},
				{ID: "anthropic/claude-opus-4.6", Name: "Claude Opus 4.6", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 128000, InputCostPer1M: 5.00, OutputCostPer1M: 25.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Claude Opus 4.6"},
				{ID: "anthropic/claude-sonnet-5", Name: "Claude Sonnet 5", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 128000, InputCostPer1M: 2.00, OutputCostPer1M: 10.00, Capabilities: []string{"tools", "vision"}, Description: "Claude Sonnet 5"},
				{ID: "anthropic/claude-sonnet-4.6", Name: "Claude Sonnet 4.6", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 8192, InputCostPer1M: 3.00, OutputCostPer1M: 15.00, Capabilities: []string{"tools", "vision"}, Description: "Claude Sonnet 4.6"},
				{ID: "anthropic/claude-haiku-4.5", Name: "Claude Haiku 4.5", Provider: "openrouter", ContextWindow: 200000, MaxOutput: 8192, InputCostPer1M: 1.00, OutputCostPer1M: 5.00, Capabilities: []string{"tools", "vision"}, Description: "Claude Haiku 4.5"},
				// ── Google via OpenRouter ──
				{ID: "google/gemini-3.6-flash", Name: "Gemini 3.6 Flash", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 65536, InputCostPer1M: 1.50, OutputCostPer1M: 7.50, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Gemini 3.6 Flash, latest"},
				{ID: "google/gemini-3.5-flash", Name: "Gemini 3.5 Flash", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 65536, InputCostPer1M: 1.50, OutputCostPer1M: 9.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Gemini 3.5 Flash"},
				{ID: "google/gemini-3.5-flash-lite", Name: "Gemini 3.5 Flash Lite", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.30, OutputCostPer1M: 2.50, Capabilities: []string{"tools", "vision"}, Description: "Gemini 3.5 Flash Lite"},
				{ID: "google/gemini-3.1-pro-preview", Name: "Gemini 3.1 Pro Preview", Provider: "openrouter", ContextWindow: 2000000, MaxOutput: 65536, InputCostPer1M: 2.00, OutputCostPer1M: 12.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Gemini 3.1 Pro Preview, 2M ctx"},
				{ID: "google/gemini-3.1-flash-lite", Name: "Gemini 3.1 Flash Lite", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 32768, InputCostPer1M: 0.25, OutputCostPer1M: 1.50, Capabilities: []string{"tools", "vision"}, Description: "Gemini 3.1 Flash Lite"},
				{ID: "google/gemini-3-flash-preview", Name: "Gemini 3 Flash Preview", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 8192, InputCostPer1M: 0.50, OutputCostPer1M: 3.00, Capabilities: []string{"tools", "vision"}, Description: "Gemini 3 Flash Preview"},
				{ID: "google/gemini-2.5-pro", Name: "Gemini 2.5 Pro", Provider: "openrouter", ContextWindow: 2000000, MaxOutput: 65536, InputCostPer1M: 1.25, OutputCostPer1M: 10.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Gemini 2.5 Pro"},
				{ID: "google/gemini-2.5-flash", Name: "Gemini 2.5 Flash", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 8192, InputCostPer1M: 0.30, OutputCostPer1M: 2.50, Capabilities: []string{"tools", "vision"}, Description: "Gemini 2.5 Flash"},
				{ID: "google/gemini-2.5-flash-lite", Name: "Gemini 2.5 Flash Lite", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 8192, InputCostPer1M: 0.10, OutputCostPer1M: 0.40, Capabilities: []string{"vision"}, Description: "Gemini 2.5 Flash Lite"},
				{ID: "google/gemma-4-31b-it", Name: "Gemma 4 31B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.07, OutputCostPer1M: 0.07, Capabilities: []string{"tools", "reasoning"}, Description: "Google Gemma 4 31B"},
				{ID: "google/gemma-4-31b-it:free", Name: "Gemma 4 31B (Free)", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free tier"},
				{ID: "google/gemma-4-26b-a4b-it", Name: "Gemma 4 26B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.05, OutputCostPer1M: 0.05, Capabilities: []string{"tools", "reasoning"}, Description: "Google Gemma 4 26B MoE"},
				{ID: "google/gemma-4-26b-a4b-it:free", Name: "Gemma 4 26B (Free)", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free tier"},
				// ── Meta via OpenRouter ──
				{ID: "meta-llama/llama-4-maverick", Name: "Llama 4 Maverick", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.20, OutputCostPer1M: 0.60, Capabilities: []string{"tools", "vision"}, Description: "Llama 4 Maverick 128E MoE"},
				{ID: "meta-llama/llama-4-scout", Name: "Llama 4 Scout", Provider: "openrouter", ContextWindow: 524288, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.30, Capabilities: []string{"tools"}, Description: "Llama 4 Scout"},
				{ID: "meta-llama/llama-3.3-70b-instruct", Name: "Llama 3.3 70B", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.10, Capabilities: []string{"tools"}, Description: "Llama 3.3 70B"},
				{ID: "meta-llama/llama-3.1-70b-instruct", Name: "Llama 3.1 70B", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.10, Capabilities: []string{"tools"}, Description: "Llama 3.1 70B"},
				{ID: "meta-llama/llama-3.1-8b-instruct", Name: "Llama 3.1 8B", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 8192, InputCostPer1M: 0.05, OutputCostPer1M: 0.05, Capabilities: []string{"tools"}, Description: "Llama 3.1 8B"},
				{ID: "meta-llama/llama-guard-4-12b", Name: "Llama Guard 4 12B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 8192, InputCostPer1M: 0.10, OutputCostPer1M: 0.10, Capabilities: []string{"tools"}, Description: "Llama Guard 4 safety"},
				{ID: "meta/muse-spark-1.1", Name: "Muse Spark 1.1", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 1.25, OutputCostPer1M: 4.25, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Meta multimodal"},
				// ── DeepSeek via OpenRouter ──
				{ID: "deepseek/deepseek-v4-flash", Name: "DeepSeek V4 Flash", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 384000, InputCostPer1M: 0.14, OutputCostPer1M: 0.28, Capabilities: []string{"tools", "reasoning"}, Description: "DeepSeek V4 Flash"},
				{ID: "deepseek/deepseek-v4-pro", Name: "DeepSeek V4 Pro", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 384000, InputCostPer1M: 0.435, OutputCostPer1M: 0.87, Capabilities: []string{"tools", "reasoning"}, Description: "DeepSeek V4 Pro"},
				{ID: "deepseek/deepseek-v3.2", Name: "DeepSeek V3.2", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.27, OutputCostPer1M: 1.10, Capabilities: []string{"tools"}, Description: "DeepSeek V3.2"},
				{ID: "deepseek/deepseek-chat", Name: "DeepSeek V3 Chat", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 8192, InputCostPer1M: 0.27, OutputCostPer1M: 1.10, Capabilities: []string{"tools"}, Description: "DeepSeek V3 Chat"},
				{ID: "deepseek/deepseek-r1", Name: "DeepSeek R1", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.55, OutputCostPer1M: 2.19, Capabilities: []string{"reasoning"}, Description: "DeepSeek R1 reasoning"},
				// ── Mistral via OpenRouter ──
				{ID: "mistralai/mistral-large", Name: "Mistral Large", Provider: "openrouter", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 1.50, Capabilities: []string{"tools", "vision"}, Description: "Mistral Large"},
				{ID: "mistralai/mistral-large-2512", Name: "Mistral Large 2512", Provider: "openrouter", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 1.50, Capabilities: []string{"tools", "vision"}, Description: "Mistral Large Dec 2025"},
				{ID: "mistralai/mistral-medium-3", Name: "Mistral Medium 3", Provider: "openrouter", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 1.50, OutputCostPer1M: 7.50, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Mistral Medium 3"},
				{ID: "mistralai/mistral-medium-3-5", Name: "Mistral Medium 3.5", Provider: "openrouter", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 1.50, OutputCostPer1M: 7.50, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Mistral Medium 3.5"},
				{ID: "mistralai/mistral-small-2603", Name: "Mistral Small 2603", Provider: "openrouter", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.30, Capabilities: []string{"tools", "vision"}, Description: "Mistral Small Mar 2026"},
				{ID: "mistralai/codestral-2508", Name: "Codestral", Provider: "openrouter", ContextWindow: 32000, MaxOutput: 32000, InputCostPer1M: 0.30, OutputCostPer1M: 0.90, Capabilities: []string{"tools"}, Description: "Codestral code model"},
				{ID: "mistralai/devstral-2512", Name: "Devstral 2", Provider: "openrouter", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.30, Capabilities: []string{"tools"}, Description: "Devstral agentic coding"},
				{ID: "mistralai/ministral-14b-2512", Name: "Ministral 14B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.30, Capabilities: []string{"tools", "vision"}, Description: "Ministral 14B VLM"},
				{ID: "mistralai/ministral-8b-2512", Name: "Ministral 8B", Provider: "openrouter", ContextWindow: 32000, MaxOutput: 32000, InputCostPer1M: 0.10, OutputCostPer1M: 0.10, Capabilities: []string{"tools"}, Description: "Ministral 8B"},
				{ID: "mistralai/ministral-3b-2512", Name: "Ministral 3B", Provider: "openrouter", ContextWindow: 32000, MaxOutput: 32000, InputCostPer1M: 0.04, OutputCostPer1M: 0.04, Capabilities: []string{}, Description: "Ministral 3B"},
				{ID: "mistralai/voxtral-small-24b-2507", Name: "Voxtral Small 24B", Provider: "openrouter", ContextWindow: 32000, MaxOutput: 32000, InputCostPer1M: 0.50, OutputCostPer1M: 1.00, Capabilities: []string{"tools", "audio"}, Description: "Audio-capable Mistral"},
				{ID: "mistralai/mixtral-8x22b-instruct", Name: "Mixtral 8x22B", Provider: "openrouter", ContextWindow: 65536, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 1.50, Capabilities: []string{"tools"}, Description: "Mixtral 8x22B MoE"},
				// ── Qwen via OpenRouter ──
				{ID: "qwen/qwen3.7-max", Name: "Qwen3.7 Max", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 4.00, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen3.7 Max"},
				{ID: "qwen/qwen3.7-plus", Name: "Qwen3.7 Plus", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 2.00, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen3.7 Plus"},
				{ID: "qwen/qwen3.6-max-preview", Name: "Qwen3.6 Max Preview", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 4.00, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen3.6 Max Preview"},
				{ID: "qwen/qwen3.6-plus", Name: "Qwen3.6 Plus", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 2.00, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen3.6 Plus"},
				{ID: "qwen/qwen3.6-flash", Name: "Qwen3.6 Flash", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.15, OutputCostPer1M: 0.60, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen3.6 Flash"},
				{ID: "qwen/qwen3.5-397b-a17b", Name: "Qwen3.5 397B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 4.00, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen3.5 flagship MoE"},
				{ID: "qwen/qwen3.5-122b-a10b", Name: "Qwen3.5 122B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 2.00, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen3.5 122B MoE"},
				{ID: "qwen/qwen3.5-27b", Name: "Qwen3.5 27B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.15, OutputCostPer1M: 0.60, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen3.5 27B"},
				{ID: "qwen/qwen3-235b-a22b", Name: "Qwen3 235B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 1.00, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen3 235B MoE"},
				{ID: "qwen/qwen3-32b", Name: "Qwen3 32B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 131072, InputCostPer1M: 0.29, OutputCostPer1M: 0.59, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen3 32B"},
				{ID: "qwen/qwen3-next-80b-a3b-instruct", Name: "Qwen3 Next 80B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.40, OutputCostPer1M: 1.20, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen3 Next 80B MoE"},
				{ID: "qwen/qwen3-coder", Name: "Qwen3 Coder", Provider: "openrouter", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 2.00, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen3 Coder"},
				{ID: "qwen/qwen-plus", Name: "Qwen Plus", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 2.00, Capabilities: []string{"tools", "reasoning"}, Description: "Qwen Plus"},
				// ── Moonshot via OpenRouter ──
				{ID: "moonshotai/kimi-k3", Name: "Kimi K3", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 3.00, OutputCostPer1M: 15.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Kimi K3, 1M context"},
				{ID: "moonshotai/kimi-k2.7-code", Name: "Kimi K2.7 Code", Provider: "openrouter", ContextWindow: 262144, MaxOutput: 32768, InputCostPer1M: 0.82, OutputCostPer1M: 3.75, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Kimi K2.7 Code"},
				{ID: "moonshotai/kimi-k2.6", Name: "Kimi K2.6", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.60, OutputCostPer1M: 2.40, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Kimi K2.6 1T MoE"},
				{ID: "moonshotai/kimi-k2.5", Name: "Kimi K2.5", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.59, OutputCostPer1M: 2.29, Capabilities: []string{"tools", "reasoning"}, Description: "Kimi K2.5"},
				{ID: "moonshotai/kimi-k2", Name: "Kimi K2", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 3.00, Capabilities: []string{"tools", "reasoning"}, Description: "Kimi K2"},
				// ── Zhipu (GLM) via OpenRouter ──
				{ID: "z-ai/glm-5.2", Name: "GLM 5.2", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.79, OutputCostPer1M: 2.48, Capabilities: []string{"tools", "reasoning"}, Description: "GLM 5.2, 1M context"},
				{ID: "z-ai/glm-5.1", Name: "GLM 5.1", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 1.50, Capabilities: []string{"tools", "reasoning"}, Description: "GLM 5.1"},
				{ID: "z-ai/glm-5", Name: "GLM 5", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.40, OutputCostPer1M: 1.20, Capabilities: []string{"tools", "reasoning"}, Description: "GLM 5"},
				{ID: "z-ai/glm-4.7", Name: "GLM 4.7", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.20, OutputCostPer1M: 0.60, Capabilities: []string{"tools", "reasoning"}, Description: "GLM 4.7"},
				{ID: "z-ai/glm-4.6", Name: "GLM 4.6", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.15, OutputCostPer1M: 0.45, Capabilities: []string{"tools", "reasoning"}, Description: "GLM 4.6"},
				{ID: "z-ai/glm-4.5", Name: "GLM 4.5", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.30, Capabilities: []string{"tools"}, Description: "GLM 4.5"},
				// ── Cohere via OpenRouter ──
				{ID: "cohere/command-a", Name: "Command A", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 4096, InputCostPer1M: 2.50, OutputCostPer1M: 10.00, Capabilities: []string{"tools"}, Description: "Cohere Command A"},
				{ID: "cohere/command-r-plus-08-2024", Name: "Command R+", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 4096, InputCostPer1M: 2.50, OutputCostPer1M: 10.00, Capabilities: []string{"tools"}, Description: "Command R+"},
				{ID: "cohere/command-r-08-2024", Name: "Command R", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 4096, InputCostPer1M: 0.15, OutputCostPer1M: 0.60, Capabilities: []string{"tools"}, Description: "Command R"},
				{ID: "cohere/command-r7b-12-2024", Name: "Command R7B", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 4096, InputCostPer1M: 0.04, OutputCostPer1M: 0.15, Capabilities: []string{}, Description: "Command R7B"},
				{ID: "cohere/north-mini-code:free", Name: "North Mini Code (Free)", Provider: "openrouter", ContextWindow: 256000, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools"}, Description: "Free tier"},
				// ── MiniMax via OpenRouter ──
				{ID: "minimax/minimax-m2.7", Name: "MiniMax M2.7", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 4.00, Capabilities: []string{"tools", "reasoning"}, Description: "MiniMax M2.7 230B"},
				{ID: "minimax/minimax-m3", Name: "MiniMax M3", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 4.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "MiniMax M3 multimodal"},
				{ID: "minimax/minimax-m2.5", Name: "MiniMax M2.5", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.80, OutputCostPer1M: 3.20, Capabilities: []string{"tools", "reasoning"}, Description: "MiniMax M2.5"},
				// ── NVIDIA via OpenRouter (free) ──
				{ID: "nvidia/nemotron-3-super-120b-a12b", Name: "Nemotron 3 Super 120B", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free on OpenRouter"},
				{ID: "nvidia/nemotron-3-super-120b-a12b:free", Name: "Nemotron 3 Super 120B (Free)", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free tier"},
				{ID: "nvidia/nemotron-3-ultra-550b-a55b", Name: "Nemotron 3 Ultra 550B", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 2.00, Capabilities: []string{"tools", "reasoning"}, Description: "Nemotron 3 Ultra"},
				{ID: "nvidia/nemotron-3-ultra-550b-a55b:free", Name: "Nemotron 3 Ultra 550B (Free)", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free tier"},
				{ID: "nvidia/nemotron-3-nano-30b-a3b", Name: "Nemotron 3 Nano 30B", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.04, OutputCostPer1M: 0.08, Capabilities: []string{"tools", "reasoning"}, Description: "Nemotron 3 Nano"},
				{ID: "nvidia/nemotron-3-nano-30b-a3b:free", Name: "Nemotron 3 Nano 30B (Free)", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free tier"},
				{ID: "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free", Name: "Nemotron Nano Omni (Free)", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "vision", "audio", "reasoning"}, Description: "Free tier"},
				{ID: "nvidia/nemotron-nano-12b-v2-vl:free", Name: "Nemotron Nano VL (Free)", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 8192, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"vision"}, Description: "Free tier"},
				{ID: "nvidia/nemotron-nano-9b-v2:free", Name: "Nemotron Nano 9B v2 (Free)", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 8192, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free tier"},
				// ── StepFun via OpenRouter ──
				{ID: "stepfun/step-3.7-flash", Name: "Step 3.7 Flash", Provider: "openrouter", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.14, OutputCostPer1M: 0.58, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Step 3.7 Flash"},
				{ID: "stepfun/step-3.5-flash", Name: "Step 3.5 Flash", Provider: "openrouter", ContextWindow: 262000, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.40, Capabilities: []string{"tools", "reasoning"}, Description: "Step 3.5 Flash 200B"},
				// ── Tencent via OpenRouter ──
				{ID: "tencent/hy3", Name: "Tencent Hy3", Provider: "openrouter", ContextWindow: 262144, MaxOutput: 32768, InputCostPer1M: 0.14, OutputCostPer1M: 0.58, Capabilities: []string{"tools", "reasoning"}, Description: "Tencent Hy3"},
				// ── InclusionAI via OpenRouter ──
				{ID: "inclusionai/ling-3.0-flash:free", Name: "Ling 3.0 Flash (Free)", Provider: "openrouter", ContextWindow: 262144, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free tier"},
				// ── Meituan via OpenRouter ──
				{ID: "meituan/longcat-2.0", Name: "LongCat 2.0", Provider: "openrouter", ContextWindow: 1048756, MaxOutput: 32768, InputCostPer1M: 0.30, OutputCostPer1M: 1.20, Capabilities: []string{"tools", "reasoning"}, Description: "Meituan 1.6T MoE"},
				// ── Thinking Machines via OpenRouter ──
				{ID: "thinkingmachines/inkling", Name: "Inkling", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 4.05, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Inkling 975B MoE"},
				// ── Poolside via OpenRouter ──
				{ID: "poolside/laguna-s-2.1", Name: "Laguna S 2.1", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.20, Capabilities: []string{"tools", "reasoning"}, Description: "Poolside Laguna S coding"},
				{ID: "poolside/laguna-s-2.1:free", Name: "Laguna S 2.1 (Free)", Provider: "openrouter", ContextWindow: 262144, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free tier"},
				{ID: "poolside/laguna-xs-2.1", Name: "Laguna XS 2.1", Provider: "openrouter", ContextWindow: 262144, MaxOutput: 32768, InputCostPer1M: 0.06, OutputCostPer1M: 0.12, Capabilities: []string{"tools", "reasoning"}, Description: "Poolside Laguna XS"},
				{ID: "poolside/laguna-xs-2.1:free", Name: "Laguna XS 2.1 (Free)", Provider: "openrouter", ContextWindow: 262144, MaxOutput: 32768, InputCostPer1M: 0.00, OutputCostPer1M: 0.00, Capabilities: []string{"tools", "reasoning"}, Description: "Free tier"},
				// ── Perplexity via OpenRouter ──
				{ID: "perplexity/sonar", Name: "Sonar", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 8192, InputCostPer1M: 0.20, OutputCostPer1M: 0.20, Capabilities: []string{"tools"}, Description: "Perplexity Sonar"},
				{ID: "perplexity/sonar-pro", Name: "Sonar Pro", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 8192, InputCostPer1M: 1.50, OutputCostPer1M: 1.50, Capabilities: []string{"tools"}, Description: "Perplexity Sonar Pro"},
				{ID: "perplexity/sonar-reasoning-pro", Name: "Sonar Reasoning Pro", Provider: "openrouter", ContextWindow: 128000, MaxOutput: 8192, InputCostPer1M: 1.50, OutputCostPer1M: 1.50, Capabilities: []string{"tools", "reasoning"}, Description: "Perplexity Sonar Reasoning"},
				// ── IBM via OpenRouter ──
				{ID: "ibm-granite/granite-4.1-8b", Name: "Granite 4.1 8B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.40, Capabilities: []string{"tools"}, Description: "IBM Granite 4.1"},
				// ── NousResearch via OpenRouter ──
				{ID: "nousresearch/hermes-4-70b", Name: "Hermes 4 70B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.30, Capabilities: []string{"tools", "reasoning"}, Description: "Hermes 4 70B"},
				{ID: "nousresearch/hermes-4-405b", Name: "Hermes 4 405B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 3.00, Capabilities: []string{"tools", "reasoning"}, Description: "Hermes 4 405B"},
				// ── Kwaipilot via OpenRouter ──
				{ID: "kwaipilot/kat-coder-pro-v2.5", Name: "KAT-Coder Pro V2.5", Provider: "openrouter", ContextWindow: 256000, MaxOutput: 32768, InputCostPer1M: 0.74, OutputCostPer1M: 2.96, Capabilities: []string{"tools"}, Description: "KAT-Coder Pro coding"},
				{ID: "kwaipilot/kat-coder-air-v2.5", Name: "KAT-Coder Air V2.5", Provider: "openrouter", ContextWindow: 256000, MaxOutput: 32768, InputCostPer1M: 0.15, OutputCostPer1M: 0.60, Capabilities: []string{"tools"}, Description: "KAT-Coder Air coding"},
				// ── Sakana via OpenRouter ──
				{ID: "sakana/fugu-ultra", Name: "Fugu Ultra", Provider: "openrouter", ContextWindow: 1000000, MaxOutput: 32768, InputCostPer1M: 5.00, OutputCostPer1M: 30.00, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Sakana Fugu Ultra 1M ctx"},
				// ── Xiaomi via OpenRouter ──
				{ID: "xiaomi/mimo-v2.5", Name: "MiMo V2.5", Provider: "openrouter", ContextWindow: 1050000, MaxOutput: 32768, InputCostPer1M: 0.105, OutputCostPer1M: 0.28, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Xiaomi MiMo V2.5 1M ctx"},
				{ID: "xiaomi/mimo-v2.5-pro", Name: "MiMo V2.5 Pro", Provider: "openrouter", ContextWindow: 1050000, MaxOutput: 32768, InputCostPer1M: 0.21, OutputCostPer1M: 0.56, Capabilities: []string{"tools", "vision", "reasoning"}, Description: "Xiaomi MiMo V2.5 Pro"},
				// ── Morph via OpenRouter ──
				{ID: "morph/morph-v3-fast", Name: "Morph V3 Fast", Provider: "openrouter", ContextWindow: 1048576, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.30, Capabilities: []string{"tools", "reasoning"}, Description: "Morph V3 Fast 1M ctx"},
				// ── Microsoft via OpenRouter ──
				{ID: "microsoft/phi-4", Name: "Phi-4", Provider: "openrouter", ContextWindow: 16384, MaxOutput: 4096, InputCostPer1M: 0.07, OutputCostPer1M: 0.14, Capabilities: []string{"tools"}, Description: "Microsoft Phi-4"},
				// ── AI21 via OpenRouter ──
				{ID: "ai21/jamba-large-1.7", Name: "Jamba Large 1.7", Provider: "openrouter", ContextWindow: 262144, MaxOutput: 32768, InputCostPer1M: 1.00, OutputCostPer1M: 2.00, Capabilities: []string{"tools", "reasoning"}, Description: "AI21 Jamba Large"},
				// ── Reka via OpenRouter ──
				{ID: "rekaai/reka-flash-3", Name: "Reka Flash 3", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.10, OutputCostPer1M: 0.20, Capabilities: []string{"tools", "vision"}, Description: "Reka Flash 3"},
				// ── Upstage via OpenRouter ──
				{ID: "upstage/solar-pro-3", Name: "Solar Pro 3", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 3.00, OutputCostPer1M: 6.00, Capabilities: []string{"tools", "reasoning"}, Description: "Upstage Solar Pro 3"},
				// ── DeepCogito via OpenRouter ──
				{ID: "deepcogito/cogito-v2.1-671b", Name: "Cogito V2.1 671B", Provider: "openrouter", ContextWindow: 131072, MaxOutput: 32768, InputCostPer1M: 0.50, OutputCostPer1M: 1.50, Capabilities: []string{"tools", "reasoning"}, Description: "Cogito V2.1 671B"},
			},
		}
	})
}

// Content from llm_common.go
// maxResponseBodySize is the maximum size (in bytes) we'll read from an LLM
// provider response body. This prevents OOM from a misbehaving or malicious
// provider returning an enormous payload.
const maxResponseBodySize = 10 * 1024 * 1024 // 10 MB

// safeReadBody reads from r up to maxResponseBodySize bytes.
// If the body exceeds the limit, it returns an error rather than consuming
// unbounded memory.
func safeReadBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxResponseBodySize))
}

// Content from sse_helper.go
// OpenAIStyleSSEEvent represents a standard OpenAI-compatible SSE streaming event.
type OpenAIStyleSSEEvent struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// ParseOpenAIStyleSSE reads OpenAI-compatible SSE events from a decoder and
// sends ChatChunks to the provided channel. It handles the common pattern
// shared by OpenRouter, Mistral, NVIDIA NIM, Groq, and other OpenAI-compatible
// providers. Returns on EOF, decode error, or when a finish_reason is received.
func ParseOpenAIStyleSSE(decoder *json.Decoder, ch chan<- *ChatChunk) {
	defer func() {
		// Ensure channel always gets a finish signal
		select {
		case ch <- &ChatChunk{Finish: true}:
		default:
		}
	}()

	for {
		var event OpenAIStyleSSEEvent
		if err := decoder.Decode(&event); err != nil {
			// EOF or read error — stream ended
			return
		}
		if len(event.Choices) > 0 {
			finish := event.Choices[0].FinishReason != nil
			content := event.Choices[0].Delta.Content

			// Only send if there's content or it's a finish event
			if content != "" || finish {
				ch <- &ChatChunk{
					Content: content,
					Finish:  finish,
				}
			}
			if finish {
				return
			}
		}
	}
}

// OpenAIStyleStreamRequest is the common request body for OpenAI-compatible APIs.
type OpenAIStyleStreamRequest struct {
	Model       string           `json:"model"`
	Messages    []OpenAIStyleMsg `json:"messages"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	Stream      bool             `json:"stream"`
}

// OpenAIStyleMsg is a standard chat message for OpenAI-compatible APIs.
type OpenAIStyleMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// BuildOpenAIMessages converts internal Message slice + system prompt to
// OpenAI-style messages, prepending the system message if non-empty.
func BuildOpenAIMessages(system string, messages []Message) []OpenAIStyleMsg {
	msgs := make([]OpenAIStyleMsg, 0, len(messages)+1)
	if system != "" {
		msgs = append(msgs, OpenAIStyleMsg{Role: "system", Content: system})
	}
	for _, m := range messages {
		msgs = append(msgs, OpenAIStyleMsg{Role: m.Role, Content: m.Content})
	}
	return msgs
}

// OpenAIStyleNonStreamingResponse is the common non-streaming response body.
type OpenAIStyleNonStreamingResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// ReadFullResponse reads a non-streaming OpenAI-style response body.
func ReadFullResponse(body io.Reader) (*OpenAIStyleNonStreamingResponse, error) {
	var resp OpenAIStyleNonStreamingResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
