# cmd/bench — Real Cost-Saving Benchmark Harness

**Date:** 2026-07-03
**Status:** Approved design
**Gap:** Gap 3 — prove one measured cost-saving number for the routing + cache layer.

## Problem

The router's value proposition is cost reduction via two levers: routing simple tasks to
cheaper models, and serving identical requests from cache at zero cost. That claim is
currently unproven. Gap 3 requires a single, reproducible, honestly-measured cost-saving
number computed from real provider data — not estimates and not fabricated mock responses.

## Goal

A `cmd/bench` binary that reports:

- baseline cost (naive strategy: every task → premium model, no cache)
- optimized cost (router-selected model + response cache)
- percent saved, split into a **routing portion** and a **cache portion**

Every dollar figure is derived from real provider responses (real token counts × real
prices). No number is fabricated.

## Architecture

Two lanes share one measurement engine:

```
cmd/bench --live   → call REAL providers → record every (task, model) real
                     response + usage + cost → fixture.json → measure → report
cmd/bench          → load fixture.json → replay REAL recorded data → measure → report
                     (deterministic, $0, CI-gateable)
```

The measurement engine is identical on both lanes. `--live` adds a recording step in front
of it; the default lane replays a previously recorded fixture. The reported number is the
same regardless of lane, because both read real provider data — live from the API, or
replayed from a fixture captured from the API.

### Why record per `(task, model)` pair

The baseline number must be real, not estimated. So during `--live`, for each workload task
the recorder makes real calls to **both**:

1. the **premium baseline model** (real response, tokens, cost) → feeds the baseline pass
2. the **router-selected model** (real response, tokens, cost) → feeds the optimized pass

The fixture stores real usage for both. Replay reconstructs both passes deterministically.
Nothing is invented; the baseline is as real as the optimized figure. (This roughly doubles
live API spend versus estimating the baseline — accepted, because an estimated baseline
would undercut the "real results only" bar.)

### Cache portion = real $0

The workload contains deliberate repeated requests. In the optimized pass, the 2nd and later
occurrences of an identical request hit the cache and cost a real $0. In the baseline pass,
each occurrence re-pays. The delta is the real cache saving. The routing portion is the delta
attributable to model selection on first-occurrence (uncached) requests. Reported separately
so the headline number is not one opaque blob.

### Savings decomposition (exact formulas)

To remove ambiguity, the split is defined as:

- `baseline_total   = Σ over all occurrences  premium_cost(task)`
- `optimized_total  = Σ over first occurrences routed_cost(task)`   (repeats hit cache = $0)
- `routing_portion  = Σ over first occurrences [premium_cost(task) − routed_cost(task)]`
- `cache_portion    = Σ over repeat occurrences premium_cost(task)`

Repeats are valued at **premium** cost in the cache portion because the baseline we compare
against would have paid premium for them. This satisfies the identity
`routing_portion + cache_portion = baseline_total − optimized_total`, so the two portions sum
exactly to total savings with no overlap or gap. `engine_test.go` asserts this identity.

## Components

Each unit has one purpose, a defined interface, and is independently testable.

| Unit | Responsibility | Depends on |
|------|----------------|------------|
| `cmd/bench/workload.go` | Real, hand-authored task set: diverse prompts across complexity tiers (simple / medium / complex) with deliberate exact repeats to exercise cache. Real inputs, loaded from a checked-in JSON file. | — |
| `cmd/bench/recorder.go` | `--live` only: drive real providers, capture `{taskID, model, response, promptTokens, completionTokens, cost}` per (task, model) pair into the fixture. | `internal/llm` providers, real API keys |
| `cmd/bench/fixture.json` | Versioned store of real recorded responses + usage. A schema version field guards format changes. Refreshed on demand via `--live`. | — |
| `cmd/bench/engine.go` | Replay the fixture. Run the **baseline pass** (every task → premium, no cache) and the **optimized pass** (task → `router.Route` selection + response cache), tallying real cost from recorded usage. | `internal/llm` router + cache logic, fixture |
| `cmd/bench/report.go` | Compute and print: baseline $, optimized $, % saved, routing portion, cache portion. Machine-readable (JSON) + human-readable output. | engine result |
| `cmd/bench/main.go` | Flags: `--live`, `--fixture <path>`, `--min-savings <pct>` (CI threshold), `--json`. Wire the units; non-zero exit if `% saved < threshold`. | all above |

### Reusing production logic, not reimplementing it

The optimized pass calls the **real** `llm.ModelRouter` routing decision and the **real**
`llm.InMemoryCache` key/lookup logic, so the benchmark measures the shipped code path, not a
parallel copy. Only the provider *call* is swapped for a fixture lookup keyed by
`(taskID, model)`. This lookup is a replay of real data, not a mock: it returns the exact
response and usage the real provider gave.

## Data flow

1. `--live`: workload → recorder calls real providers (premium + routed model per task) →
   writes `fixture.json` → hands recorded set to engine.
2. default: `main` loads `fixture.json` → hands to engine.
3. engine: for each task, baseline pass tallies premium-model recorded cost (every
   occurrence pays); optimized pass runs real router selection, checks real cache (hit = $0),
   tallies recorded cost of the selected model on misses.
4. report: prints baseline, optimized, % saved, routing portion, cache portion; `main`
   applies threshold gate.

## Error handling

- Missing fixture on default lane → clear error instructing `--live` to record one first.
- Fixture schema-version mismatch → refuse to replay, instruct re-record.
- `--live` with missing API keys → fail fast, name the missing provider.
- A task whose fixture entry is absent (e.g. added to workload after last recording) →
  named error, not a silent skip, so the number can never be quietly partial.

## Testing

- `engine_test.go`: feed a small hand-built recorded set with known costs; assert baseline,
  optimized, routing portion, and cache portion arithmetic exactly. Deterministic, no
  providers.
- `report_test.go`: assert output formatting + threshold exit logic.
- `recorder.go` real-provider path is exercised manually via `--live` (not in CI).

## CI gate

Default (replay) lane runs in CI against the committed fixture and asserts
`% saved >= --min-savings`. Zero API spend, deterministic. Fixture is refreshed manually via
`--live` when prices or model lineup change.

## Prerequisite

`internal/server/server.go` currently fails to build: line ~110 uses `cost.NewBudgetManager`
without importing `github.com/vigilagent/vigilagent/internal/cost`. This one-line import fix
lands first — the harness links `internal/llm` and the tree must compile.

## Out of scope

- **Output quality.** A cheaper model may produce worse output; this number measures cost
  only. The fixture retains full responses so a quality eval can layer on later. Not built now.
- **Semantic / fuzzy cache.** Exact-match cache only, matching the shipped `InMemoryCache`.
- **Latency benchmark.** Separate concern (test-plan performance gate); not this number.
