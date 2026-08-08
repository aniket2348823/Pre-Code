package router

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/llm"
	"github.com/vigilagent/vigilagent/internal/pipeline"
	"github.com/vigilagent/vigilagent/internal/telemetry"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// reviewHandler runs the full Shift-Zero pipeline:
// Main LLM → Deterministic Engine → Parallel Reviewer LLMs → Evidence → Knowledge Graph → Skill Extraction → Confidence → Output
//
// POST /api/v1/review
//
// Supports BYOK (Bring Your Own Key) via the X-LLM-Key header.
// When present, a temporary LLM provider is created from the user's key
// and used for the review pipeline instead of (or as fallback to) the
// backend's configured providers.
//
// Request body:
//
//	{
//	  "prompt": "Create a secure payment system",
//	  "code": "func main() { ... }",    // optional, generates from prompt if empty
//	  "language": "go",                  // optional, defaults to "go"
//	  "filename": "main.go",            // optional, defaults to "input.<lang>"
//	  "context": "existing project..."  // optional
//	}
func (r *Router) reviewHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input pipeline.ReviewRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Prompt == "" && input.Code == "" {
		response.BadRequest(w, "prompt or code is required")
		return
	}

	// Validate and normalise language.
	input.Language = strings.TrimSpace(strings.ToLower(input.Language))
	switch input.Language {
	case "":
		// Leave empty so the pipeline infers the language from the prompt and
		// the generated code. This is what makes prompt-only generation work
		// for ANY language (e.g. "generate a python script" is scanned as
		// python, not forced to go by a premature default).
	case "go":
		input.Language = "go"
	case "python", "py":
		input.Language = "python"
	case "javascript", "js":
		input.Language = "javascript"
	case "typescript", "ts":
		input.Language = "typescript"
	case "rust", "rs":
		input.Language = "rust"
	case "java":
		input.Language = "java"
	default:
		response.BadRequest(w, "unsupported language: "+input.Language+" — supported: go, python, javascript, typescript, rust, java")
		return
	}

	// Validate filename (no path traversal, reasonable length).
	if input.Filename != "" {
		input.Filename = strings.TrimSpace(input.Filename)
		if strings.Contains(input.Filename, "..") || strings.ContainsAny(input.Filename, "/\\") {
			response.BadRequest(w, "filename must not contain path separators or '..'")
			return
		}
		if len(input.Filename) > 255 {
			response.BadRequest(w, "filename too long (max 255 characters)")
			return
		}
	}

	// ── BYOK: Read user's LLM key from X-LLM-Key header ──
	// When a client (VS Code extension, MCP server, or direct API caller)
	// sends an LLM provider key via this header, we create a temporary
	// provider and register it on a per-request ModelRouter so the review
	// pipeline uses the user's own key instead of the backend's configured keys.
	llmRouter := r.llmRouter // default: use backend-configured router
	if userKey := req.Header.Get("X-LLM-Key"); userKey != "" {
		llmRouter = r.buildBYOKRouter(userKey)
	}

	// Reject if no LLM router available.
	if llmRouter == nil {
		response.Error(w, http.StatusServiceUnavailable, "LLM router not configured — review endpoint requires an LLM provider (pass X-LLM-Key header or configure backend providers)")
		return
	}

	// Build the full Shift-Zero pipeline.
	szp := pipeline.NewShiftZeroPipeline(
		llmRouter,
		r.engine,
		r.knowledge,
		r.skillEng,
		r.attackGraph,
		r.confidence,
		r.pipeline,
	)
	// Suggestion mode: return line-anchored accept/reject suggestions instead
	// of auto-rewriting the code (the user decides per suggestion).
	szp.SuggestionMode = input.SuggestionMode

	report, err := szp.Run(req.Context(), &input)
	if err != nil {
		response.InternalError(w, "review pipeline failed: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, report)

	// Observe confidence score for Grafana dashboard.
	if report.Confidence != nil {
		telemetry.VerificationConfidenceTracks.WithLabelValues(input.Language, report.Confidence.Grade).Observe(report.Confidence.Confidence)
	}
}

// buildBYOKRouter creates a temporary ModelRouter from a user-provided API key.
// It auto-detects the provider from the key prefix and registers it.
// The returned router is ephemeral and not cached.
func (r *Router) buildBYOKRouter(apiKey string) *llm.ModelRouter {
	provider := detectProviderFromKey(apiKey)

	// The BYOK router's default model MUST belong to the user's provider.
	// rankCandidates only considers models whose provider is registered on the
	// router: with an NVIDIA key, only nvidia_nim is registered, so a hardcoded
	// gpt-4o (provider=openai) default yields ZERO candidates and the pipeline
	// fails with "no healthy provider supports the task's requirements" (the
	// pipeline tasks don't set TargetModel, so the router default is the only
	// guaranteed candidate). Prefer the backend's configured default model when
	// it belongs to the same provider; otherwise fall back to a small model from
	// that provider's catalog.
	defaultModel := defaultModelForProvider(provider)
	if backend := r.llmRouter; backend != nil {
		if m := backend.DefaultModel(); m != "" {
			if info, ok := llm.LookupPrice(m); ok && info.Provider == provider {
				defaultModel = m
			}
		}
	}

	router := llm.NewModelRouter(&llm.RouterConfig{
		DefaultModel:  defaultModel,
		BudgetPerTask: 5.00, // generous per-request budget for BYOK
	})

	switch provider {
	case "openai":
		router.RegisterProvider("openai", llm.NewOpenAI(apiKey))
		slog.Info("BYOK: registered temporary OpenAI provider")
	case "anthropic":
		router.RegisterProvider("anthropic", llm.NewAnthropic(apiKey))
		slog.Info("BYOK: registered temporary Anthropic provider")
	case "gemini":
		p, err := llm.NewGemini(apiKey)
		if err != nil {
			slog.Warn("BYOK: failed to create Gemini provider", "error", err)
			return router
		}
		router.RegisterProvider("gemini", p)
		slog.Info("BYOK: registered temporary Gemini provider")
	case "openrouter":
		router.RegisterProvider("openrouter", llm.NewOpenRouter(apiKey))
		slog.Info("BYOK: registered temporary OpenRouter provider")
	case "mistral":
		router.RegisterProvider("mistral", llm.NewMistral(apiKey))
		slog.Info("BYOK: registered temporary Mistral provider")
	case "groq":
		router.RegisterProvider("groq", llm.NewGroq(apiKey))
		slog.Info("BYOK: registered temporary Groq provider")
	case "nvidia_nim":
		router.RegisterProvider("nvidia_nim", llm.NewNVIDIANIM(apiKey))
		slog.Info("BYOK: registered temporary NVIDIA NIM provider")
	case "cohere":
		router.RegisterProvider("cohere", llm.NewCohere(apiKey))
		slog.Info("BYOK: registered temporary Cohere provider")
	default:
		// Unknown key prefix — try OpenAI as default (most common)
		router.RegisterProvider("openai", llm.NewOpenAI(apiKey))
		slog.Info("BYOK: unknown key prefix, defaulting to OpenAI provider")
	}

	return router
}

// defaultModelForProvider returns a small, cheap model for the given provider
// that exists in the LLM price table, so a BYOK router always has at least one
// candidate that maps to the provider it registered. Falls back to gpt-4o-mini
// for unknown providers (the historical BYOK behavior).
func defaultModelForProvider(provider string) string {
	switch provider {
	case "openai":
		return "gpt-4o-mini"
	case "anthropic":
		return "claude-haiku-3.5"
	case "gemini":
		return "gemini-2.0-flash"
	case "nvidia_nim":
		return "meta/llama-3.1-8b-instruct"
	case "mistral":
		return "mistral-small-latest"
	case "groq":
		return "llama-3.1-8b-instant"
	case "cohere":
		return "command-r"
	case "openrouter":
		// OpenRouter serves many providers; use a generic cheap model name.
		return "gpt-4o-mini"
	default:
		return "gpt-4o-mini"
	}
}

// detectProviderFromKey infers the LLM provider from the API key prefix.
func detectProviderFromKey(key string) string {
	// Check MORE SPECIFIC prefixes first — sk-ant- and sk-or- both start with sk-
	switch {
	case strings.HasPrefix(key, "sk-ant-"):
		return "anthropic"
	case strings.HasPrefix(key, "sk-or-"):
		return "openrouter"
	case strings.HasPrefix(key, "nvapi-"):
		return "nvidia_nim"
	case strings.HasPrefix(key, "AIza"):
		return "gemini"
	case strings.HasPrefix(key, "ms-"):
		return "mistral"
	case strings.HasPrefix(key, "gsk_"):
		return "groq"
	case strings.HasPrefix(key, "co-"):
		return "cohere"
	case strings.HasPrefix(key, "sk-"):
		return "openai"
	default:
		return "unknown"
	}
}
