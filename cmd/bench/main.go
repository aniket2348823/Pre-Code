package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/vigilagent/vigilagent/internal/llm"
)

type options struct {
	live         bool
	workload     string
	fixture      string
	premiumModel string
	minSavings   float64
	asJSON       bool
}

func ctxBackground() context.Context { return context.Background() }

func saveJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// run executes one lane and returns a process exit code.
func run(opt options) (int, error) {
	w, err := LoadWorkload(opt.workload)
	if err != nil {
		return 1, err
	}

	var fx *Fixture
	if opt.live {
		keys := Keys{OpenAI: os.Getenv("OPENAI_API_KEY"), Anthropic: os.Getenv("ANTHROPIC_API_KEY")}
		fx, err = Record(context.Background(), w, opt.premiumModel, keys)
		if err != nil {
			return 1, err
		}
		if err := fx.Save(opt.fixture); err != nil {
			return 1, err
		}
		fmt.Fprintf(os.Stderr, "recorded %d entries to %s\n", len(fx.Entries), opt.fixture)
	} else {
		fx, err = LoadFixture(opt.fixture)
		if err != nil {
			return 1, err
		}
	}

	providerNames := providerNamesFromFixture(fx)
	rep, err := Measure(w, fx, opt.premiumModel, BuildRouter(providerNames))
	if err != nil {
		return 1, err
	}

	if opt.asJSON {
		s, err := rep.JSON()
		if err != nil {
			return 1, err
		}
		fmt.Println(s)
	} else {
		fmt.Print(rep.Human())
	}

	if !rep.MeetsThreshold(opt.minSavings) {
		fmt.Fprintf(os.Stderr, "FAIL: %.1f%% saved < %.1f%% threshold\n", rep.PercentSaved, opt.minSavings)
		return 1, nil
	}
	return 0, nil
}

// providerNamesFromFixture returns the distinct provider names the fixture
// touched, so the replay router registers exactly those as healthy. Falls back
// to the price table when a provider field is blank.
func providerNamesFromFixture(fx *Fixture) []string {
	set := map[string]bool{}
	for _, e := range fx.Entries {
		p := e.Provider
		if p == "" {
			if info, ok := llm.PriceTable[e.Model]; ok {
				p = info.Provider
			}
		}
		if p != "" {
			set[p] = true
		}
	}
	if len(set) == 0 { // safety net: register the common two
		return []string{"anthropic", "openai"}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	return names
}

func main() {
	var opt options
	flag.BoolVar(&opt.live, "live", false, "call real providers and record a fixture")
	flag.StringVar(&opt.workload, "workload", "cmd/bench/workload.json", "workload JSON path")
	flag.StringVar(&opt.fixture, "fixture", "cmd/bench/fixture.json", "fixture JSON path")
	flag.StringVar(&opt.premiumModel, "premium-model", "claude-opus-4", "baseline premium model")
	flag.Float64Var(&opt.minSavings, "min-savings", 0, "CI gate: minimum percent saved")
	flag.BoolVar(&opt.asJSON, "json", false, "emit JSON report")
	flag.Parse()

	code, err := run(opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	os.Exit(code)
}
