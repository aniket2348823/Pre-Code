package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/vigilagent/vigilagent/internal/llm"
)

type Keys struct {
	OpenAI    string
	Anthropic string
}

// providersFor builds real adapters for whichever keys are present.
func providersFor(keys Keys) (map[string]llm.Provider, []string) {
	provs := map[string]llm.Provider{}
	if keys.OpenAI != "" {
		provs["openai"] = llm.NewOpenAI(keys.OpenAI)
	}
	if keys.Anthropic != "" {
		provs["anthropic"] = llm.NewAnthropic(keys.Anthropic)
	}
	names := make([]string, 0, len(provs))
	for n := range provs {
		names = append(names, n)
	}
	sort.Strings(names)
	return provs, names
}

// Record calls real providers once per (task, needed model) and captures the
// real response, tokens, and cost into a Fixture. It records both the premium
// baseline model and the router-selected model for each task, so replay can
// compute a real baseline and a real optimized number without estimation.
func Record(ctx context.Context, w *Workload, premiumModel string, keys Keys) (*Fixture, error) {
	provs, _ := providersFor(keys)
	if len(provs) == 0 {
		return nil, fmt.Errorf("no provider API keys supplied; set OPENAI_API_KEY and/or ANTHROPIC_API_KEY")
	}

	// Real router so routed-model selection matches production.
	router := llm.NewModelRouter(&llm.RouterConfig{DefaultModel: premiumModel, DefaultOutputTokens: 500})
	for n, p := range provs {
		router.RegisterProvider(n, p)
	}

	fx := &Fixture{SchemaVersion: FixtureSchemaVersion, PremiumModel: premiumModel}
	recorded := map[string]bool{} // taskID\x00model

	callAndRecord := func(taskID, model string, wt WorkloadTask) error {
		if recorded[taskID+"\x00"+model] {
			return nil
		}
		info, ok := llm.PriceTable[model]
		if !ok {
			return fmt.Errorf("model %q not in price table", model)
		}
		prov, ok := provs[info.Provider]
		if !ok {
			return fmt.Errorf("no API key for provider %q required by model %q", info.Provider, model)
		}
		resp, err := prov.Chat(ctx, requestFor(model, wt))
		if err != nil {
			return fmt.Errorf("live call (%s, %s): %w", taskID, model, err)
		}
		fx.Entries = append(fx.Entries, FixtureEntry{
			TaskID: taskID, Model: model, Provider: info.Provider,
			Content: resp.Content, InputTokens: resp.InputTokens,
			OutputTokens: resp.OutputTokens, Cost: resp.Cost,
		})
		recorded[taskID+"\x00"+model] = true
		return nil
	}

	for _, wt := range w.Tasks {
		decision, err := router.Route(ctx, taskToLLM(wt))
		if err != nil {
			return nil, fmt.Errorf("route %s: %w", wt.ID, err)
		}
		if err := callAndRecord(wt.ID, premiumModel, wt); err != nil {
			return nil, err
		}
		if err := callAndRecord(wt.ID, decision.Model, wt); err != nil {
			return nil, err
		}
	}
	return fx, nil
}
