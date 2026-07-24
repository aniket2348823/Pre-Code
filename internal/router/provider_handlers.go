package router

import (
	"net/http"

	"github.com/vigilagent/vigilagent/internal/llm"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// listProvidersHandler returns all supported providers with their metadata.
//
// GET /api/v1/providers
func (r *Router) listProvidersHandler(w http.ResponseWriter, req *http.Request) {
	providers := llm.Providers()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"providers": providers,
	})
}

// listProviderModelsHandler returns all available models for a specific provider.
//
// GET /api/v1/providers/{providerID}/models
func (r *Router) listProviderModelsHandler(w http.ResponseWriter, req *http.Request) {
	providerID := llm.ProviderID(req.PathValue("providerID"))
	if providerID == "" {
		response.BadRequest(w, "provider ID is required")
		return
	}

	models := llm.ProviderModels(providerID)
	if models == nil {
		response.NotFound(w, "unknown provider: "+string(providerID))
		return
	}

	providerInfo := llm.Providers()
	var info *llm.ProviderInfo
	for _, p := range providerInfo {
		if p.ID == providerID {
			info = &p
			break
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"provider": info,
		"models":   models,
		"count":    len(models),
	})
}

// getModelHandler returns metadata for a specific model across all providers.
//
// GET /api/v1/models/{modelID}
func (r *Router) getModelHandler(w http.ResponseWriter, req *http.Request) {
	modelID := req.PathValue("modelID")
	if modelID == "" {
		response.BadRequest(w, "model ID is required")
		return
	}

	model := llm.FindModel(modelID)
	if model == nil {
		response.NotFound(w, "unknown model: "+modelID)
		return
	}

	response.JSON(w, http.StatusOK, model)
}
